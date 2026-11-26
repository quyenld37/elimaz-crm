package query_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
)

func (e *env) list(u identity.AuthUser, f conversation.ListFilter) ([]conversation.ListRow, *conversation.PageCursor, error) {
	e.t.Helper()
	var (
		rows []conversation.ListRow
		next *conversation.PageCursor
	)
	err := e.read(func(tx pgx.Tx) (err error) {
		rows, next, err = conversation.List(e.t.Context(), tx, u, f)
		return err //nolint:wrapcheck // test
	})
	return rows, next, err
}

func ids(rows []conversation.ListRow) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// [Task 159, CH01, CH06, CH07] 30 hội thoại trên Page A: trạng thái (12 open, 8 pending, 6 done, 4 spam), loại (10
// comment), người phụ trách (6 của tôi, 6 của Bình, 18 chưa ai), chưa đọc (15), nhãn "Đã lên đơn" (7) và một nhãn đã
// xóa (3). Mỗi bộ lọc, một bộ lọc ghép ra đúng số dòng; mới nhất trước; dòng có tên khách, người phụ trách, nhãn, cờ
// đơn nháp. assignee lạ → ErrInvalidInput.
func TestList_Filters(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	me := e.user("toi@elimaz.test", uuid.Nil, "conversation.read")
	binh := e.user("binh@elimaz.test", e.pageA, "conversation.read")
	var tagLive, tagGone uuid.UUID
	e.scan(&tagLive, `INSERT INTO conversation.tags (tenant_id, name, color) VALUES (current_setting('app.tenant_id')::uuid,
		'Đã lên đơn', 'green') RETURNING id`)
	e.scan(&tagGone, `INSERT INTO conversation.tags (tenant_id, name, color, deleted_at) VALUES
		(current_setting('app.tenant_id')::uuid, 'Cũ', 'red', now()) RETURNING id`)
	cust := e.customer("Khách lọc", "")
	all := e.seedFilterSet(cust, me.UserID, binh.UserID, tagLive, tagGone)
	for _, c := range []struct {
		name string
		f    conversation.ListFilter
		want int
	}{
		{"không lọc", conversation.ListFilter{}, 30},
		{"status open", conversation.ListFilter{Status: "open"}, 12},
		{"status pending", conversation.ListFilter{Status: "pending"}, 8},
		{"status spam", conversation.ListFilter{Status: "spam"}, 4},
		{"nhãn Đã lên đơn", conversation.ListFilter{TagID: &tagLive}, 7},
		{"nhãn đã xóa", conversation.ListFilter{TagID: &tagGone}, 0},
		{"của tôi", conversation.ListFilter{Assignee: conversation.AssigneeMe}, 6},
		{"chưa ai nhận", conversation.ListFilter{Assignee: conversation.AssigneeNone}, 18},
		{"của Bình theo mã", conversation.ListFilter{Assignee: binh.UserID.String()}, 6},
		{"comment", conversation.ListFilter{Kind: "comment"}, 10},
		{"inbox", conversation.ListFilter{Kind: "inbox"}, 20},
		{"chưa đọc", conversation.ListFilter{Unread: true}, 15},
		{"open + của tôi", conversation.ListFilter{Status: "open", Assignee: conversation.AssigneeMe}, 3},
		{"comment + chưa đọc", conversation.ListFilter{Kind: "comment", Unread: true}, 5},
		{"Page A", conversation.ListFilter{PageID: &e.pageA}, 30},
		{"Page B", conversation.ListFilter{PageID: &e.pageB}, 0},
	} {
		c.f.Limit = 100
		rows, next, err := e.list(me, c.f)
		if err != nil || next != nil || len(rows) != c.want {
			t.Errorf("%s: %d dòng (tiếp %v, lỗi %v), muốn %d", c.name, len(rows), next, err, c.want)
		}
	}
	rows, _, err := e.list(me, conversation.ListFilter{Limit: 100})
	if err != nil || rows[0].ID != all[29] || rows[29].ID != all[0] {
		t.Fatalf("thứ tự mới nhất trước sai (lỗi %v)", err)
	}
	first := rows[len(rows)-1] // all[0]: comment, của tôi, chưa đọc, nhãn "Đã lên đơn"
	if first.CustomerName != "Khách lọc" || first.AssigneeID == nil || *first.AssigneeID != me.UserID ||
		first.AssigneeName == "" || len(first.Tags) != 1 || first.Tags[0].Name != "Đã lên đơn" ||
		first.UnreadCount != 2 || first.PostID == nil || first.HasOpenDraft {
		t.Fatalf("dòng all[0] = %+v", first)
	}
	if r := rows[0]; len(r.Tags) != 0 || r.Tags == nil {
		t.Fatalf("hội thoại không nhãn: Tags = %#v, muốn rỗng khác nil", r.Tags)
	}
	if _, _, err := e.list(me, conversation.ListFilter{Assignee: "ai-do"}); !errors.Is(err, conversation.ErrInvalidInput) {
		t.Fatalf("assignee lạ: lỗi %v, muốn ErrInvalidInput", err)
	}
}

// seedFilterSet chèn 30 hội thoại của TestList_Filters trên Page A (hội thoại i lúc base + i phút): trạng thái theo
// khoảng (<12 open, <20 pending, <26 done, còn lại spam), i%3 == 0 comment, i%5 == 0 của me, i%5 == 1 của binh, i chẵn
// chưa đọc 2; i < 7 gắn tagLive, 7 ≤ i < 10 gắn tagGone.
func (e *env) seedFilterSet(cust, me, binh, tagLive, tagGone uuid.UUID) []uuid.UUID {
	e.t.Helper()
	base := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	all := make([]uuid.UUID, 30)
	for i := range 30 {
		c := conv{page: e.pageA, customer: cust, at: base.Add(time.Duration(i) * time.Minute)}
		switch {
		case i < 12:
			c.status = "open"
		case i < 20:
			c.status = "pending"
		case i < 26:
			c.status = "done"
		default:
			c.status = "spam"
		}
		if i%3 == 0 {
			c.kind = "comment"
		}
		switch i % 5 {
		case 0:
			c.assignee = &me
		case 1:
			c.assignee = &binh
		}
		if i%2 == 0 {
			c.unread = 2
		}
		all[i] = e.conv(c)
		switch {
		case i < 7:
			e.exec(`INSERT INTO conversation.conversation_tag_links (tenant_id, conversation_id, tag_id)
				VALUES (current_setting('app.tenant_id')::uuid, $1, $2)`, all[i], tagLive)
		case i < 10:
			e.exec(`INSERT INTO conversation.conversation_tag_links (tenant_id, conversation_id, tag_id)
				VALUES (current_setting('app.tenant_id')::uuid, $1, $2)`, all[i], tagGone)
		}
	}
	return all
}

// [Task 159, CH01] 120 hội thoại (từng nhóm 3 dòng cùng last_message_at, thứ tự phụ theo id), limit 50 → 3 trang 50,
// 50, 20; không trùng, không sót, đúng thứ tự (last_message_at, id) giảm dần. updatedSince = T: hội thoại có
// last_message_at = T − 20 s (transaction commit muộn) vẫn có; T − 40 s thì không; T + 1 phút có.
func TestList_PagingAndUpdatedSince(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("doc@elimaz.test", e.pageA, "conversation.read")
	cust := e.customer("Khách trang", "")
	base := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	want := make([]uuid.UUID, 0, 120)
	type key struct {
		at time.Time
		id uuid.UUID
	}
	keys := make([]key, 0, 120)
	for i := range 120 {
		at := base.Add(time.Duration(i/3) * time.Minute)
		id := e.conv(conv{page: e.pageA, customer: cust, at: at})
		keys = append(keys, key{at, id})
	}
	slices.SortFunc(keys, func(a, b key) int {
		if c := b.at.Compare(a.at); c != 0 {
			return c
		}
		return slices.Compare(b.id[:], a.id[:])
	})
	for _, k := range keys {
		want = append(want, k.id)
	}
	var got []uuid.UUID
	var sizes []int
	var after *conversation.PageCursor
	for range 5 {
		rows, next, err := e.list(u, conversation.ListFilter{Limit: 50, After: after})
		if err != nil {
			t.Fatal(err)
		}
		got, sizes = append(got, ids(rows)...), append(sizes, len(rows))
		if next == nil {
			break
		}
		// Con trỏ đi qua API dạng chuỗi.
		c, err := customer.ParseListCursor(next.String())
		if err != nil {
			t.Fatal(err)
		}
		after = &c
	}
	if !slices.Equal(sizes, []int{50, 50, 20}) || !slices.Equal(got, want) {
		t.Fatalf("trang %v; thứ tự khớp = %v", sizes, slices.Equal(got, want))
	}

	since := base.Add(2 * time.Hour)
	late := e.conv(conv{page: e.pageA, customer: cust, at: since.Add(-20 * time.Second)})
	old := e.conv(conv{page: e.pageA, customer: cust, at: since.Add(-40 * time.Second)})
	fresh := e.conv(conv{page: e.pageA, customer: cust, at: since.Add(time.Minute)})
	rows, _, err := e.list(u, conversation.ListFilter{UpdatedSince: &since})
	if err != nil {
		t.Fatal(err)
	}
	if g := ids(rows); !slices.Equal(g, []uuid.UUID{fresh, late}) || slices.Contains(g, old) {
		t.Fatalf("updatedSince: %v, muốn [%s %s]", g, fresh, late)
	}
}

// [Task 159, 05 mục 1] Tìm khách: q = "5678" (đuôi SĐT) → hội thoại của Lan (và của hồ sơ B đã gộp vào Lan); q = "lan
// nguyen" (không dấu, không theo thứ tự hoa thường) → như trên; q = SĐT đủ của Lan → có hội thoại gắn B; q = "56" →
// customer.ErrQueryTooShort; khách không khớp → rỗng.
func TestList_Search(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("tim@elimaz.test", uuid.Nil, "conversation.read")
	lan := e.customer("Lan Nguyễn", "0901235678")
	b := e.customer("Hồ sơ phụ", "")
	minh := e.customer("Minh Trần", "0912340000")
	e.merge(lan, b)
	base := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	convLan := e.conv(conv{page: e.pageA, customer: lan, at: base})
	convB := e.conv(conv{page: e.pageB, customer: b, at: base.Add(time.Minute)})
	e.conv(conv{page: e.pageA, customer: minh, at: base.Add(2 * time.Minute)})
	for _, q := range []string{"5678", "lan nguyen", "0901235678", "NGUYEN lan"} {
		rows, _, err := e.list(u, conversation.ListFilter{Q: q})
		if err != nil || !slices.Equal(ids(rows), []uuid.UUID{convB, convLan}) {
			t.Errorf("q %q: %v (lỗi %v), muốn [B, Lan]", q, ids(rows), err)
		}
	}
	if _, _, err := e.list(u, conversation.ListFilter{Q: "56"}); !errors.Is(err, customer.ErrQueryTooShort) {
		t.Fatalf(`q "56": lỗi %v, muốn ErrQueryTooShort`, err)
	}
	if rows, _, err := e.list(u, conversation.ListFilter{Q: "khong ai ten nay"}); err != nil || len(rows) != 0 {
		t.Fatalf("không khớp: %d dòng, lỗi %v", len(rows), err)
	}
}

// [Task 501] Dòng danh sách, chi tiết, hội thoại của khách mang thẻ bài (giờ đăng, đường dẫn, ảnh) và cờ khách tiềm
// năng mới (CH08): created_customer + hồ sơ không gộp + chưa có đơn nào (kể cả nháp). Khách có đơn nháp, khách có hồ
// sơ khác gộp vào, khách đã gộp vào hồ sơ khác, hội thoại không tạo khách → false.
func TestList_PostCardAndNewLead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("co-moi@elimaz.test", uuid.Nil, "conversation.read")
	posted := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	post := e.post(e.pageA, "post-anh", "post", &posted)
	img := uuid.New()
	e.exec(`UPDATE conversation.posts SET permalink = 'https://www.facebook.com/post-anh', snippet = 'Áo len',
		image_file_id = $2 WHERE id = $1`, post, img)
	fresh, withDraft, rootOf, merged, old := e.customer("Mới", ""), e.customer("Có nháp", ""), e.customer("Gốc", ""),
		e.customer("Đã gộp", ""), e.customer("Cũ", "")
	e.exec(`UPDATE customer.customers SET root_id = $1 WHERE id = $2`, rootOf, merged)
	e.order(withDraft, "DH0000501", "draft", 100, time.Now())
	at := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	mk := func(cust uuid.UUID, created bool, i int) uuid.UUID {
		id := e.conv(conv{page: e.pageA, customer: cust, kind: "comment", post: &post, at: at.Add(time.Duration(i) * time.Second)})
		e.exec(`UPDATE conversation.conversations SET created_customer = $2 WHERE id = $1`, id, created)
		return id
	}
	want := map[uuid.UUID]bool{
		mk(fresh, true, 0): true, mk(withDraft, true, 1): false, mk(rootOf, true, 2): false, mk(merged, true, 3): false,
		mk(old, false, 4): false,
	}
	rows, _, err := e.list(u, conversation.ListFilter{})
	if err != nil || len(rows) != len(want) {
		t.Fatalf("danh sách: %d dòng (lỗi %v)", len(rows), err)
	}
	for _, r := range rows {
		if r.NewLead != want[r.ID] {
			t.Errorf("hội thoại %s: NewLead = %v, muốn %v", r.ID, r.NewLead, want[r.ID])
		}
		if r.PostPostedAt == nil || !r.PostPostedAt.Equal(posted) || r.PostPermalink != "https://www.facebook.com/post-anh" ||
			r.PostImageFileID == nil || *r.PostImageFileID != img || r.PostSnippet != "Áo len" {
			t.Errorf("thẻ bài: %+v", r.CustomerConversation)
		}
	}
	for id, w := range want {
		var c conversation.Conversation
		if err := e.read(func(tx pgx.Tx) (err error) {
			c, err = conversation.Get(t.Context(), tx, u, id)
			return err //nolint:wrapcheck // test
		}); err != nil || c.NewLead != w || c.PostImageFileID == nil || c.PostPostedAt == nil || c.PostPermalink == "" {
			t.Errorf("chi tiết %s: %+v (lỗi %v)", id, c.CustomerConversation, err)
		}
	}
	var group []conversation.CustomerConversation
	if err := e.read(func(tx pgx.Tx) (err error) {
		group, _, err = conversation.ListCustomerConversations(t.Context(), tx, u, fresh, nil, 0)
		return err //nolint:wrapcheck // test
	}); err != nil || len(group) != 1 || !group[0].NewLead || group[0].PostImageFileID == nil || group[0].PostPermalink == "" {
		t.Errorf("hội thoại của khách: %+v (lỗi %v)", group, err)
	}
}
