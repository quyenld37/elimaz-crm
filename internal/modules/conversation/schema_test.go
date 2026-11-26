package conversation_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// schemaEnv: database thử có hai tenant; tenant A có một khách, một Page (mã ngẫu nhiên: khóa ngoại tới
// channel.channel_accounts thêm ở 0412, Task 153: Page là tài khoản kênh thật), một bài viết, một hội thoại inbox và một hội thoại comment.
type schemaEnv struct {
	t              *testing.T
	staff          *testkit.Staff
	a, b           uuid.UUID
	page           uuid.UUID
	customer       uuid.UUID
	post           uuid.UUID
	inbox, comment uuid.UUID
}

func pgCode(err error) string {
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pe.Code
	}
	return ""
}

func newSchemaEnv(t *testing.T) *schemaEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &schemaEnv{t: t, staff: s, a: s.Tenant(t, "A"), b: s.Tenant(t, "B")}
	e.page = s.ChannelAccount(t, e.a, "facebook", "page-a", "Page A")
	ctx := t.Context()
	s.Exec(t, e.a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Chị Lan', 'chi lan') RETURNING id`).Scan(&e.customer)
	})
	e.app(e.a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id, kind)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'post-1', 'live') RETURNING id`, e.page).Scan(&e.post); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.conversations
			(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, last_message_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox', 'psid-1', now()) RETURNING id`,
			e.page, e.customer).Scan(&e.inbox); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO conversation.conversations
			(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, post_id, last_message_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'comment', 'post-1:psid-1', $3, now()) RETURNING id`,
			e.page, e.customer, e.post).Scan(&e.comment)
	})
	return e
}

// app chạy fn bằng crm_app trong ngữ cảnh tenant; lỗi làm test đỏ.
func (e *schemaEnv) app(tenant uuid.UUID, fn func(pgx.Tx) error) {
	e.t.Helper()
	if err := e.tryApp(tenant, fn); err != nil {
		e.t.Fatal(err)
	}
}

func (e *schemaEnv) tryApp(tenant uuid.UUID, fn func(pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, fn) //nolint:wrapcheck // test
}

// insertMessage chèn một tin bằng crm_app vào hội thoại comment của A với các cột cols (tên → giá trị) cộng cột bắt
// buộc; trả lỗi của câu chèn.
func (e *schemaEnv) insertMessage(direction, kind, origin string, extra map[string]any) error {
	cols := "tenant_id, conversation_id, channel_account_id, direction, kind, origin, sent_at"
	vals := "current_setting('app.tenant_id')::uuid, $1, $2, $3, $4, $5, now()"
	args := []any{e.comment, e.page, direction, kind, origin}
	for _, c := range []string{"send_status", "external_id", "parent_external_id", "command_job_id", "client_ref"} {
		if v, ok := extra[c]; ok {
			args = append(args, v)
			cols += ", " + c
			vals += ", $" + strconv.Itoa(len(args))
		}
	}
	return e.tryApp(e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), "INSERT INTO conversation.messages ("+cols+") VALUES ("+vals+")", args...)
		return err //nolint:wrapcheck // test
	})
}

// [Task 151] Lược đồ đúng luật tenant (RLS ép, UNIQUE (tenant_id, id), khóa ngoại ghép); hai view công bố có
// security_invoker và đúng cột như docs/views.md; tags_v bỏ nhãn đã xóa; tenant B không đọc được hội thoại, nhãn của A.
func TestSchema_RulesViewsTenant(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	ctx := t.Context()
	testkit.CheckSchemaRulesClean(t, e.staff.Owner)
	testkit.CheckViews(t, e.staff.Owner, "conversation", map[string]string{
		"conversations_v": "tenant_id, id, channel_account_id, customer_id, kind, status, assignee_id, post_id, " +
			"last_message_at, last_customer_message_at, created_at",
		"tags_v": "tenant_id, id, name, color, sort",
	})
	e.app(e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO conversation.tags (tenant_id, name, color, deleted_at)
			VALUES (current_setting('app.tenant_id')::uuid, 'Khách sỉ', 'blue', NULL),
			       (current_setting('app.tenant_id')::uuid, 'Đã xóa', 'red', now())`)
		return err //nolint:wrapcheck // test
	})
	count := func(tenant uuid.UUID, q string) int {
		var n int
		e.app(tenant, func(tx pgx.Tx) error { return tx.QueryRow(ctx, q).Scan(&n) })
		return n
	}
	for _, c := range []struct {
		q    string
		a, b int
	}{
		{"SELECT count(*) FROM " + conversation.ViewTags, 1, 0},
		{"SELECT count(*) FROM " + conversation.ViewTags + " WHERE name = 'Đã xóa'", 0, 0},
		{"SELECT count(*) FROM conversation.tags", 2, 0},
		{"SELECT count(*) FROM " + conversation.ViewConversations, 2, 0},
		{"SELECT count(*) FROM conversation.conversations", 2, 0},
		{"SELECT count(*) FROM conversation.posts", 1, 0},
	} {
		if a, b := count(e.a, c.q), count(e.b, c.q); a != c.a || b != c.b {
			t.Errorf("%s: A %d, B %d; mong A %d, B %d", c.q, a, b, c.a, c.b)
		}
	}
	// Tên nhãn trùng (không phân biệt hoa thường) giữa hai nhãn còn sống → unique; trùng tên nhãn đã xóa được.
	err := e.tryApp(e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO conversation.tags (tenant_id, name, color)
			VALUES (current_setting('app.tenant_id')::uuid, 'KHÁCH SỈ', 'blue')`)
		return err //nolint:wrapcheck // test
	})
	if pgCode(err) != "23505" {
		t.Errorf("nhãn trùng tên: %v, mong 23505", err)
	}
	e.app(e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO conversation.tags (tenant_id, name, color)
			VALUES (current_setting('app.tenant_id')::uuid, 'đã xóa', 'red')`)
		return err //nolint:wrapcheck // test
	})
}

// [Task 151] Ràng buộc của hội thoại: comment phải có bài viết, inbox không có; hội thoại trùng luồng → unique;
// unread_count âm → CHECK.
func TestSchema_ConversationChecks(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	ctx := t.Context()
	for _, c := range []struct {
		name, kind, ref string
		post            *uuid.UUID
		unread          int
		code            string
	}{
		{"comment thiếu bài", "comment", "x-1", nil, 0, "23514"},
		{"inbox có bài", "inbox", "x-2", &e.post, 0, "23514"},
		{"trùng luồng inbox", "inbox", "psid-1", nil, 0, "23505"},
		{"chưa đọc âm", "inbox", "x-3", nil, -1, "23514"},
		{"hợp lệ", "inbox", "x-4", nil, 0, ""},
	} {
		err := e.tryApp(e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO conversation.conversations
				(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, post_id, unread_count, last_message_at)
				VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4, $5, $6, now())`,
				e.page, e.customer, c.kind, c.ref, c.post, c.unread)
			return err //nolint:wrapcheck // test
		})
		if pgCode(err) != c.code {
			t.Errorf("%s: %v, mong mã %q", c.name, err, c.code)
		}
	}
}

// [Task 151, CH07 nền] Ghi chú nội bộ không bao giờ ra ngoài: có send_status, external_id hay command_job_id, hoặc
// direction = in → vi phạm CHECK; tin của nhân viên (không phải ghi chú) phải có send_status; direction in ⇔ origin
// customer.
func TestSchema_NoteNeverLeaves(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	for _, c := range []struct {
		name                    string
		direction, kind, origin string
		extra                   map[string]any
		code                    string
	}{
		{"ghi chú hợp lệ", "out", "note", "staff", nil, ""},
		{"ghi chú có send_status queued", "out", "note", "staff", map[string]any{"send_status": "queued"}, "23514"},
		{"ghi chú có external_id", "out", "note", "staff", map[string]any{"external_id": "mid-note"}, "23514"},
		{"ghi chú có command_job_id", "out", "note", "staff", map[string]any{"command_job_id": uuid.New()}, "23514"},
		{"ghi chú đi vào", "in", "note", "customer", nil, "23514"},
		{"tin nhân viên thiếu send_status", "out", "message", "staff", nil, "23514"},
		{"tin tự động thiếu send_status", "out", "comment", "automation", nil, "23514"},
		{"tin nhân viên queued", "out", "message", "staff", map[string]any{"send_status": "queued"}, ""},
		{"tin vào từ nhân viên", "in", "message", "staff", map[string]any{"send_status": "sent"}, "23514"},
		{"tin ra từ khách", "out", "message", "customer", nil, "23514"},
		{"tin khách", "in", "comment", "customer", map[string]any{"external_id": "c-1"}, ""},
		{"trùng external_id", "in", "comment", "customer", map[string]any{"external_id": "c-1"}, "23505"},
		{"send_status lạ", "out", "message", "staff", map[string]any{"send_status": "lost"}, "23514"},
	} {
		if err := e.insertMessage(c.direction, c.kind, c.origin, c.extra); pgCode(err) != c.code {
			t.Errorf("%s: %v, mong mã %q", c.name, err, c.code)
		}
	}
	// client_ref trùng trong tenant → unique.
	ref := uuid.New()
	if err := e.insertMessage("out", "message", "staff", map[string]any{"send_status": "queued", "client_ref": ref}); err != nil {
		t.Fatal(err)
	}
	if err := e.insertMessage("out", "message", "staff", map[string]any{"send_status": "queued", "client_ref": ref}); pgCode(err) != "23505" {
		t.Errorf("client_ref trùng: %v, mong 23505", err)
	}
}

// [Task 151, CH05 nền] Một tin riêng đang sống mỗi comment: hai private_reply queued cùng parent_external_id → unique;
// tin đầu chuyển failed → chèn tin thứ hai được; tin đang sống ở comment khác không ảnh hưởng.
func TestSchema_OnePrivateReplyPerComment(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	ctx := t.Context()
	reply := func(parent, status string) error {
		return e.insertMessage("out", "private_reply", "staff",
			map[string]any{"send_status": status, "parent_external_id": parent})
	}
	if err := reply("c-1", "queued"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"queued", "sent", "checking", "in_doubt"} {
		if err := reply("c-1", s); pgCode(err) != "23505" {
			t.Errorf("tin riêng thứ hai %s: %v, mong 23505", s, err)
		}
	}
	if err := reply("c-2", "queued"); err != nil {
		t.Errorf("comment khác: %v", err)
	}
	e.app(e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE conversation.messages SET send_status = 'failed'
			WHERE kind = 'private_reply' AND parent_external_id = 'c-1'`)
		return err //nolint:wrapcheck // test
	})
	if err := reply("c-1", "queued"); err != nil {
		t.Errorf("sau khi tin đầu failed: %v", err)
	}
	for _, s := range []string{"failed", "expired", "discarded"} {
		if err := reply("c-1", s); err != nil {
			t.Errorf("tin riêng đã kết thúc %s: %v", s, err)
		}
	}
}

// [Task 151] conversation_assignments, message_revisions chỉ ghi thêm: crm_app UPDATE, DELETE → lỗi (trigger hoặc
// thiếu quyền); chủ bảng cũng bị trigger chặn; DELETE trên message_revisions sau set_config('app.retention','on',true)
// → được (dọn theo hạn giữ, Task 164); UPDATE vẫn bị chặn khi bật retention.
func TestSchema_AppendOnly(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	ctx := t.Context()
	if err := e.insertMessage("in", "comment", "customer", map[string]any{"external_id": "c-9"}); err != nil {
		t.Fatal(err)
	}
	e.app(e.a, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO conversation.conversation_assignments
			(tenant_id, conversation_id, to_user_id, reason) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'manager')`,
			e.inbox, uuid.New()); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(ctx, `INSERT INTO conversation.message_revisions (tenant_id, message_id, body, source)
			SELECT tenant_id, id, 'bản cũ', 'customer' FROM conversation.messages WHERE external_id = 'c-9'`)
		return err //nolint:wrapcheck // test
	})
	for _, q := range []string{
		"UPDATE conversation.conversation_assignments SET reason = 'self'",
		"DELETE FROM conversation.conversation_assignments",
		"UPDATE conversation.message_revisions SET body = 'sửa'",
		"DELETE FROM conversation.message_revisions",
	} {
		err := e.tryApp(e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, q)
			return err //nolint:wrapcheck // test
		})
		if pgCode(err) != "42501" {
			t.Errorf("crm_app %q: %v, mong 42501", q, err)
		}
	}
	e.staff.CheckOwnerAppendOnly(t, e.a,
		"UPDATE conversation.conversation_assignments SET reason = 'self'",
		"DELETE FROM conversation.conversation_assignments",
		"UPDATE conversation.message_revisions SET body = 'sửa'",
		"DELETE FROM conversation.message_revisions",
	)
	err := e.tryApp(e.a, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.retention', 'on', true)"); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(ctx, "UPDATE conversation.message_revisions SET body = 'sửa'")
		return err //nolint:wrapcheck // test
	})
	if pgCode(err) != "42501" {
		t.Errorf("UPDATE khi bật retention: %v, mong 42501", err)
	}
	var deleted int64
	e.app(e.a, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.retention', 'on', true)"); err != nil {
			return err //nolint:wrapcheck // test
		}
		tag, err := tx.Exec(ctx, "DELETE FROM conversation.message_revisions")
		deleted = tag.RowsAffected()
		return err //nolint:wrapcheck // test
	})
	if deleted != 1 {
		t.Errorf("DELETE khi bật retention xóa %d dòng, mong 1", deleted)
	}
}

// [Task 151] Khóa ngoại ghép: hội thoại trỏ khách của tenant khác, tin trỏ hội thoại không có, liên kết nhãn trỏ nhãn
// không có → 23503; người phụ trách phải là nhân viên của tenant. [Task 153] Bài, hội thoại, tin, cài đặt Page trỏ Page
// không có hay Page của tenant khác → 23503.
func TestSchema_ForeignKeys(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	ctx := t.Context()
	var otherCustomer uuid.UUID
	e.staff.Exec(t, e.b, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Anh Nam', 'anh nam') RETURNING id`).Scan(&otherCustomer)
	})
	user := e.staff.User(t, e.a, "nv@elimaz.test", "active")
	pageB := e.staff.ChannelAccount(t, e.b, "facebook", "page-b", "Page B")
	type fkCase struct {
		name, q string
		args    []any
		code    string
	}
	cases := []fkCase{
		{
			"khách tenant khác", `INSERT INTO conversation.conversations
			(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, last_message_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox', 'z-1', now())`,
			[]any{e.page, otherCustomer},
			"23503",
		},
		{
			"tin không hội thoại", `INSERT INTO conversation.messages
			(tenant_id, conversation_id, channel_account_id, direction, kind, origin, sent_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'in', 'message', 'customer', now())`,
			[]any{uuid.New(), e.page},
			"23503",
		},
		{"nhãn không có", `INSERT INTO conversation.conversation_tag_links (tenant_id, conversation_id, tag_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2)`, []any{e.inbox, uuid.New()}, "23503"},
		{
			"người phụ trách lạ", `UPDATE conversation.conversations SET assignee_id = $1 WHERE id = $2`,
			[]any{uuid.New(), e.inbox},
			"23503",
		},
		{
			"người phụ trách đúng", `UPDATE conversation.conversations SET assignee_id = $1 WHERE id = $2`,
			[]any{user, e.inbox},
			"",
		},
		{"cài đặt Page mặc định", `INSERT INTO conversation.page_settings (tenant_id, channel_account_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1)`, []any{e.page}, ""},
		{
			"thời hạn tự động ngoài khoảng", `UPDATE conversation.page_settings SET automation_max_age_minutes = 4`,
			nil, "23514",
		},
		{"bài trùng", `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'post-1')`, []any{e.page}, "23505"},
	}
	for _, page := range []struct {
		name string
		id   uuid.UUID
	}{{"Page không có", uuid.New()}, {"Page tenant khác", pageB}} {
		cases = append(cases,
			fkCase{page.name + ": bài", `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id)
				VALUES (current_setting('app.tenant_id')::uuid, $1, 'post-x')`, []any{page.id}, "23503"},
			fkCase{page.name + ": hội thoại", `INSERT INTO conversation.conversations
				(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, last_message_at)
				VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox', 'z-2', now())`, []any{page.id, e.customer}, "23503"},
			fkCase{page.name + ": tin", `INSERT INTO conversation.messages
				(tenant_id, conversation_id, channel_account_id, direction, kind, origin, sent_at)
				VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'in', 'message', 'customer', now())`, []any{e.inbox, page.id}, "23503"},
			fkCase{page.name + ": cài đặt Page", `INSERT INTO conversation.page_settings (tenant_id, channel_account_id)
				VALUES (current_setting('app.tenant_id')::uuid, $1)`, []any{page.id}, "23503"},
		)
	}
	for _, c := range cases {
		err := e.tryApp(e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, c.q, c.args...)
			return err //nolint:wrapcheck // test
		})
		if pgCode(err) != c.code {
			t.Errorf("%s: %v, mong mã %q", c.name, err, c.code)
		}
	}
}

// [Task 151] UpsertPost: gửi lại cùng mã ngoài trả cùng bài; loại đã biết không bị 'unknown' ghi đè; trường rỗng giữ
// giá trị cũ, trường mới ghi đè.
func TestUpsertPost(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	ctx := t.Context()
	link := "https://facebook.com/p/1"
	var first, second store.UpsertPostRow
	e.app(e.a, func(tx pgx.Tx) error {
		q := store.New(tx)
		var err error
		if first, err = q.UpsertPost(ctx, store.UpsertPostParams{
			ChannelAccountID: e.page, ExternalID: "post-2", Kind: "live", Permalink: pgtype.Text{String: link, Valid: true},
			FetchedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}); err != nil {
			return err //nolint:wrapcheck // test
		}
		second, err = q.UpsertPost(ctx, store.UpsertPostParams{
			ChannelAccountID: e.page, ExternalID: "post-2", Kind: "unknown", FetchedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
		return err //nolint:wrapcheck // test
	})
	if first.ID != second.ID || second.Kind != "live" {
		t.Fatalf("lần 1 %+v, lần 2 %+v", first, second)
	}
	var got string
	e.app(e.a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT permalink FROM conversation.posts WHERE id = $1", first.ID).Scan(&got)
	})
	if got != link {
		t.Errorf("permalink %q, mong giữ %q", got, link)
	}
}
