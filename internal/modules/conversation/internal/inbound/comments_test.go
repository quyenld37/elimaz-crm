package inbound_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// Mã bài, comment thử trên Page 1 (dạng mã của Graph: <page>_<post>, <post>_<comment>).
const (
	post1 = pageExt1 + "_900000000000001"
	post2 = pageExt1 + "_900000000000002"
)

// commentEv là sự kiện comment verb của from trên bài post lúc sec (parent rỗng: comment gốc của bài).
func commentEv(verb, post, id, parent, from, name, text string, sec int) channel.CommentPayload {
	if parent == "" {
		parent = post // Graph gửi parent_id = mã bài cho comment gốc
	}
	return channel.CommentPayload{
		PageID: pageExt1, PostID: post, CommentID: id, ParentID: parent, FromID: from, FromName: name, Text: text,
		Verb: verb, FromPage: from == pageExt1, IsHidden: verb == "hide", CreatedAt: at(sec),
	}
}

// sendComment giao sự kiện comment (khóa chống trùng như adapter facebook: comment, verb, nội dung; thứ tự theo Page,
// bài, người).
func (e *env) sendComment(p channel.CommentPayload) {
	e.t.Helper()
	e.enqueue(facebook.Key, channel.EventCommentChanged, "c:"+p.CommentID+":"+p.Verb+":"+p.Text, pageExt1,
		p.PostID+":"+p.FromID, p.CreatedAt, p)
	e.drain()
}

// cmsg là một comment đọc lại theo mã ngoài.
type cmsg struct {
	ID, ConversationID       uuid.UUID
	Direction, Kind, Origin  string
	Body, Parent, SenderName *string
	PostID                   *uuid.UUID
	Hidden, Live             bool
	RemovedAt, EditedAt      *time.Time
	Attachments              []byte
}

func (e *env) commentRow(extID string) (cmsg, bool) {
	e.t.Helper()
	var m cmsg
	found := false
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT id, conversation_id, direction, kind, origin, body, parent_external_id,
			sender_name, post_id, is_hidden, is_live, removed_at, edited_at, attachments
			FROM conversation.messages WHERE channel_account_id = $1 AND external_id = $2`, e.page1, extID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			found = true
			if err := rows.Scan(&m.ID, &m.ConversationID, &m.Direction, &m.Kind, &m.Origin, &m.Body, &m.Parent,
				&m.SenderName, &m.PostID, &m.Hidden, &m.Live, &m.RemovedAt, &m.EditedAt, &m.Attachments); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return rows.Err()
	})
	return m, found
}

func (e *env) mustComment(extID string) cmsg {
	e.t.Helper()
	m, ok := e.commentRow(extID)
	if !ok {
		e.t.Fatalf("không có comment %s", extID)
	}
	return m
}

// commentConv đọc hội thoại comment của người from dưới bài post.
func (e *env) commentConv(post, from string) (conv, uuid.UUID) {
	e.t.Helper()
	var c conv
	var postID uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT c.id, c.customer_id, c.kind, c.status, c.unread_count, c.last_snippet,
			c.last_message_at, c.last_customer_message_at, c.last_staff_message_at, c.version, c.post_id
			FROM conversation.conversations c WHERE c.channel_account_id = $1 AND c.kind = 'comment'
			AND c.external_thread_ref = $2`, e.page1, post+":"+from).
			Scan(&c.ID, &c.CustomerID, &c.Kind, &c.Status, &c.Unread, &c.Snippet, &c.LastMessageAt, &c.LastCustomerAt,
				&c.LastStaffAt, &c.Version, &postID)
	})
	return c, postID
}

// revision là một dòng lịch sử của tin.
type revision struct {
	Body            *string
	Hidden, Removed *bool
	Source          string
}

func (e *env) revisions(msgID uuid.UUID) []revision {
	e.t.Helper()
	var out []revision
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT body, is_hidden, removed, source FROM conversation.message_revisions
			WHERE message_id = $1 ORDER BY recorded_at, id`, msgID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var r revision
			if err := rows.Scan(&r.Body, &r.Hidden, &r.Removed, &r.Source); err != nil {
				return err //nolint:wrapcheck // test
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out
}

// CH02: comment add trên bài P1 → 1 bài (unknown, chưa đọc), 1 hội thoại comment có post_id, 1 tin comment in (tên
// người comment, không có cha), chưa đọc 1, báo hiệu; đúng 1 lệnh fetch_post; chạy lệnh trên máy Meta giả → bài có
// đường dẫn, đoạn trích, giờ đăng. Comment thứ hai cùng người cùng bài → cùng hội thoại, không thêm lệnh; cùng người
// bài khác → hội thoại mới, cùng khách; comment trả lời comment khác → parent_external_id đúng; khách được tạo với tên
// thật từ comment.
func TestCommentAddCreatesPostConversationMessage(t *testing.T) {
	t.Parallel()
	fg := fakegraphFor(t)
	fg.AddPost(fakePost(post1, "Áo thun mới về, đủ size S M L, giá 199k. "+longText(200)))
	fg.AddPost(fakePost(post2, "Váy hoa"))
	e := newEnvWith(t, fg)
	e.enqueue(facebook.Key, channel.EventCommentChanged, "c:c1:add", pageExt1, post1+":u-7",
		at(0), commentEv("add", post1, "c1", "", "u-7", "Lan Nguyễn", "Chốt 1 áo size M", 0))
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages`); n != 0 {
		t.Fatalf("chưa chạy mà đã có %d tin", n)
	}
	e.drainInboundOnly()

	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.posts WHERE kind = 'unknown' AND permalink IS NULL`); n != 1 {
		t.Fatalf("bài unknown chưa đọc = %d, mong 1", n)
	}
	c, postID := e.commentConv(post1, "u-7")
	if c.Kind != "comment" || postID == uuid.Nil || c.Unread != 1 || deref(c.Snippet) != "Chốt 1 áo size M" ||
		c.LastCustomerAt == nil || !c.LastCustomerAt.Equal(at(0)) {
		t.Fatalf("hội thoại comment: %+v, bài %s", c, postID)
	}
	m := e.mustComment("c1")
	if m.ConversationID != c.ID || m.Kind != "comment" || m.Direction != "in" || m.Origin != "customer" ||
		deref(m.Body) != "Chốt 1 áo size M" || m.Parent != nil || m.PostID == nil || *m.PostID != postID ||
		deref(m.SenderName) != "Lan Nguyễn" || m.Hidden || m.Live || m.RemovedAt != nil {
		t.Fatalf("tin comment: %+v", m)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customers WHERE full_name = 'Lan Nguyễn' AND name_source <> 'placeholder'`); n != 1 {
		t.Errorf("khách tên thật từ comment = %d, mong 1", n)
	}
	if len(e.changedSignals(c.ID)) != 1 {
		t.Errorf("báo hiệu = %d, mong 1", len(e.changedSignals(c.ID)))
	}
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND tenant_id = $2`, inbound.FetchPostKind, e.tenant); n != 1 {
		t.Fatalf("lệnh fetch_post = %d, mong 1", n)
	}

	e.drain() // chạy lệnh đọc bài trên máy Meta giả
	e.noDeadJobs()
	var permalink, snip string
	var created time.Time
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT permalink, snippet, created_time FROM conversation.posts WHERE id = $1
			AND fetched_at IS NOT NULL`, postID).Scan(&permalink, &snip, &created)
	})
	if permalink != "https://www.facebook.com/"+post1 || len([]rune(snip)) != 120 || !created.Equal(fakePostCreated) {
		t.Fatalf("bài sau khi đọc: %q, %q (%d ký tự), %s", permalink, snip, len([]rune(snip)), created)
	}

	e.sendComment(commentEv("add", post1, "c2", "", "u-7", "Lan Nguyễn", "Còn màu đen không?", 5))
	if c2, _ := e.commentConv(post1, "u-7"); c2.ID != c.ID || c2.Unread != 2 {
		t.Errorf("comment thứ hai cùng bài: %+v (mong hội thoại %s, chưa đọc 2)", c2, c.ID)
	}
	e.sendComment(commentEv("add", post1, "c3", "c2", "u-8", "Minh", "Mình cũng hỏi màu đen", 6))
	if r := e.mustComment("c3"); deref(r.Parent) != "c2" {
		t.Errorf("comment trả lời: cha = %q, mong c2", deref(r.Parent))
	}
	e.sendComment(commentEv("add", post2, "c4", "", "u-7", "Lan Nguyễn", "Váy này bao nhiêu?", 7))
	other, otherPost := e.commentConv(post2, "u-7")
	if other.ID == uuid.Nil || other.ID == c.ID || otherPost == postID || other.CustomerID != c.CustomerID {
		t.Errorf("bài khác: %+v (bài %s), mong hội thoại mới cùng khách %s", other, otherPost, c.CustomerID)
	}
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND tenant_id = $2`, inbound.FetchPostKind, e.tenant); n != 2 {
		t.Errorf("lệnh fetch_post = %d, mong 2 (một mỗi bài)", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customers`); n != 2 {
		t.Errorf("khách = %d, mong 2", n)
	}
	e.noDeadJobs()
}

// CH13: khách sửa comment → nội dung mới, edited_at, 1 dòng lịch sử giữ nội dung cũ (source customer); giao lại → không
// thêm; khách xóa → removed_at có giá trị, giữ nội dung, lịch sử 2 dòng theo thứ tự (dòng 2 removed); xóa lại → không
// thêm. Không tăng chưa đọc.
func TestCommentEditAndRemoveKeepHistory(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.sendComment(commentEv("add", post1, "c1", "", "u-7", "Lan", "Chốt 1 áo size M", 0))
	e.sendComment(commentEv("edited", post1, "c1", "", "u-7", "Lan", "Chốt 2 áo size M", 10))
	m := e.mustComment("c1")
	if deref(m.Body) != "Chốt 2 áo size M" || m.EditedAt == nil || !m.EditedAt.Equal(at(10)) {
		t.Fatalf("sau sửa: %+v", m)
	}
	revs := e.revisions(m.ID)
	if len(revs) != 1 || deref(revs[0].Body) != "Chốt 1 áo size M" || revs[0].Source != "customer" ||
		revs[0].Removed == nil || *revs[0].Removed || revs[0].Hidden == nil || *revs[0].Hidden {
		t.Fatalf("lịch sử sau sửa: %+v", revs)
	}
	e.sendComment(commentEv("edited", post1, "c1", "", "u-7", "Lan", "Chốt 2 áo size M", 10)) // giao lại
	e.enqueue(facebook.Key, channel.EventCommentChanged, "c:c1:edited:cu", pageExt1, post1+":u-7", at(5),
		commentEv("edited", post1, "c1", "", "u-7", "Lan", "bản sửa cũ hơn", 5)) // bản sửa cũ tới muộn
	e.drain()
	if n := len(e.revisions(m.ID)); n != 1 || deref(e.mustComment("c1").Body) != "Chốt 2 áo size M" {
		t.Fatalf("giao lại / sửa cũ: %d dòng lịch sử, nội dung %q", n, deref(e.mustComment("c1").Body))
	}
	e.sendComment(commentEv("remove", post1, "c1", "", "u-7", "Lan", "", 20))
	m = e.mustComment("c1")
	if m.RemovedAt == nil || !m.RemovedAt.Equal(at(20)) || deref(m.Body) != "Chốt 2 áo size M" {
		t.Fatalf("sau xóa: %+v", m)
	}
	revs = e.revisions(m.ID)
	if len(revs) != 2 || deref(revs[1].Body) != "Chốt 2 áo size M" || revs[1].Removed == nil || !*revs[1].Removed ||
		revs[1].Source != "customer" {
		t.Fatalf("lịch sử sau xóa: %+v", revs)
	}
	e.enqueue(facebook.Key, channel.EventCommentChanged, "c:c1:remove:lai", pageExt1, post1+":u-7", at(21),
		commentEv("remove", post1, "c1", "", "u-7", "Lan", "", 21))
	e.drain()
	if n := len(e.revisions(m.ID)); n != 2 {
		t.Errorf("xóa lại: %d dòng lịch sử, mong 2", n)
	}
	if c, _ := e.commentConv(post1, "u-7"); c.Unread != 1 {
		t.Errorf("chưa đọc = %d, mong 1 (sửa, xóa không tăng)", c.Unread)
	}
	e.noDeadJobs()
}

// Sai thứ tự: xóa tới trước add → tin có removed_at (1 dòng lịch sử removed); add đến sau không xóa removed_at, không
// thêm tin. Sửa tới trước add → tin mang nội dung sửa; add (nội dung cũ) đến sau không ghi đè. Ẩn tới trước add → tin
// ẩn; add sau giữ ẩn.
func TestCommentStateEventsBeforeAdd(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.sendComment(commentEv("remove", post1, "c1", "", "u-7", "Lan", "", 20))
	m := e.mustComment("c1")
	if m.RemovedAt == nil || !m.RemovedAt.Equal(at(20)) || m.Kind != "comment" {
		t.Fatalf("xóa trước add: %+v", m)
	}
	if revs := e.revisions(m.ID); len(revs) != 1 || revs[0].Removed == nil || !*revs[0].Removed {
		t.Fatalf("lịch sử xóa trước add: %+v", revs)
	}
	e.sendComment(commentEv("add", post1, "c1", "", "u-7", "Lan", "Chốt 1 áo", 0))
	if m2 := e.mustComment("c1"); m2.ID != m.ID || m2.RemovedAt == nil {
		t.Fatalf("add sau xóa: %+v", m2)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages`); n != 1 {
		t.Fatalf("tin = %d, mong 1", n)
	}

	e.sendComment(commentEv("edited", post1, "c2", "", "u-7", "Lan", "Bản đã sửa", 30))
	e.sendComment(commentEv("add", post1, "c2", "", "u-7", "Lan", "Bản gốc", 25))
	if m := e.mustComment("c2"); deref(m.Body) != "Bản đã sửa" || m.EditedAt == nil {
		t.Fatalf("sửa trước add: %+v", m)
	}

	e.sendComment(commentEv("hide", post1, "c3", "", "u-7", "Lan", "Số em 0912345678", 40))
	e.sendComment(commentEv("add", post1, "c3", "", "u-7", "Lan", "Số em 0912345678", 35))
	if m := e.mustComment("c3"); !m.Hidden {
		t.Fatalf("ẩn trước add: %+v", m)
	}
	e.noDeadJobs()
}

// Ẩn → is_hidden, 1 dòng lịch sử (page_external, ẩn); tin vẫn trong danh sách tin của hội thoại; ẩn lại → không thêm;
// hiện → bỏ ẩn, dòng lịch sử thứ hai.
func TestCommentHideUnhide(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.sendComment(commentEv("add", post1, "c1", "", "u-7", "Lan", "Số em 0912345678", 0))
	e.sendComment(commentEv("hide", post1, "c1", "", "u-7", "Lan", "Số em 0912345678", 5))
	m := e.mustComment("c1")
	if !m.Hidden {
		t.Fatalf("sau ẩn: %+v", m)
	}
	if ms := e.messages(m.ConversationID); len(ms) != 1 || ms[0].ID != m.ID {
		t.Fatalf("tin ẩn phải còn trong hội thoại: %+v", ms)
	}
	revs := e.revisions(m.ID)
	if len(revs) != 1 || revs[0].Hidden == nil || !*revs[0].Hidden || revs[0].Source != "page_external" ||
		deref(revs[0].Body) != "Số em 0912345678" {
		t.Fatalf("lịch sử ẩn: %+v", revs)
	}
	e.enqueue(facebook.Key, channel.EventCommentChanged, "c:c1:hide:lai", pageExt1, post1+":u-7", at(6),
		commentEv("hide", post1, "c1", "", "u-7", "Lan", "Số em 0912345678", 6))
	e.drain()
	if n := len(e.revisions(m.ID)); n != 1 {
		t.Errorf("ẩn lại: %d dòng, mong 1", n)
	}
	e.sendComment(commentEv("unhide", post1, "c1", "", "u-7", "Lan", "Số em 0912345678", 7))
	if m := e.mustComment("c1"); m.Hidden {
		t.Fatalf("sau hiện: %+v", m)
	}
	if revs := e.revisions(m.ID); len(revs) != 2 || revs[1].Hidden == nil || *revs[1].Hidden {
		t.Fatalf("lịch sử hiện: %+v", revs)
	}
	e.noDeadJobs()
}

// Comment của chính Page trả lời comment khách → tin out page_external trong hội thoại của comment cha, không tạo
// khách mới, không tăng chưa đọc, mốc tin nhân viên cập nhật. Comment Page không thuộc hội thoại nào (comment gốc của
// bài) → bỏ qua, đếm inbound_ignored{page_comment_no_thread}; comment không có người comment (Meta không gửi from)
// → bỏ qua, đếm inbound_ignored{comment_without_from}, việc không dead. Ảnh trong comment → mục đính kèm chờ tải.
func TestCommentFromPage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	cus := commentEv("add", post1, "c1", "", "u-7", "Lan", "", 0)
	cus.PhotoURL = "https://scontent.fbcdn.net/anh.jpg"
	e.sendComment(cus)
	if m := e.mustComment("c1"); string(m.Attachments) == "[]" {
		t.Errorf("comment có ảnh mà không có mục đính kèm: %s", m.Attachments)
	}
	before := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customers`)
	e.sendComment(commentEv("add", post1, "c2", "c1", pageExt1, "Shop", "Dạ còn ạ", 10))
	m := e.mustComment("c2")
	c, _ := e.commentConv(post1, "u-7")
	if m.ConversationID != c.ID || m.Direction != "out" || m.Origin != "page_external" || deref(m.Parent) != "c1" {
		t.Fatalf("comment của Page: %+v (hội thoại %s)", m, c.ID)
	}
	if after := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customers`); after != before {
		t.Errorf("khách %d → %d", before, after)
	}
	if c.Unread != 1 || c.LastStaffAt == nil || !c.LastStaffAt.Equal(at(10)) || deref(c.Snippet) != "Dạ còn ạ" {
		t.Errorf("hội thoại sau comment Page: %+v", c)
	}
	e.reg.Drain()
	e.sendComment(commentEv("add", post1, "c3", "", pageExt1, "Shop", "Bài mới đây cả nhà", 20))
	if _, ok := e.commentRow("c3"); ok {
		t.Error("comment gốc của Page không được ghi")
	}
	noFrom := commentEv("add", post1, "c4", "", "", "", "Ai đó comment", 30)
	e.sendComment(noFrom)
	if _, ok := e.commentRow("c4"); ok {
		t.Error("comment không có người comment không được ghi")
	}
	rows := e.reg.Drain()
	if d := ignoredCount(rows, "page_comment_no_thread"); d != 1 {
		t.Errorf("inbound_ignored{page_comment_no_thread} tăng %d, mong 1", d)
	}
	if d := ignoredCount(rows, "comment_without_from"); d != 1 {
		t.Errorf("inbound_ignored{comment_without_from} tăng %d, mong 1", d)
	}
	e.noDeadJobs()
}

// drainInboundOnly chạy việc vào tới hết bằng worker không nhận lệnh (lệnh fetch_post giữ pending).
func (e *env) drainInboundOnly() {
	e.t.Helper()
	testkit.DrainDue(e.t, e.staff.Owner, queue.NewWorker(e.staff.App, e.handlers, queue.WorkerOptions{WorkerID: "w-inbound"}))
}

// ignoredCount là tổng inbound_ignored nhãn label trong rows.
func ignoredCount(rows []metrics.Row, label string) int64 {
	var n int64
	for _, r := range rows {
		if r.Name == "inbound_ignored" && r.Label == label {
			n += r.Count
		}
	}
	return n
}

// fakePostCreated là giờ đăng của bài trên máy Meta giả.
var fakePostCreated = time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // hằng thời gian của test

// fakegraphFor là máy Meta giả cho env (newEnvWith thêm Page 1).
func fakegraphFor(t *testing.T) *fakegraph.Server {
	t.Helper()
	return fakegraph.New(t, fakegraph.Options{AppID: ownAppID, AppSecret: secret32})
}

func fakePost(id, message string) fakegraph.Post {
	return fakegraph.Post{ID: id, PageID: pageExt1, Message: message, PermalinkURL: "https://www.facebook.com/" + id, Created: fakePostCreated}
}

// longText là n chữ "ồ" (nhiều byte mỗi ký tự, để kiểm cắt theo ký tự).
func longText(n int) string { return strings.Repeat("ồ", n) }
