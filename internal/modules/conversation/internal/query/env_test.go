package query_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/convtest"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// env: database thử, tenant A có Page A, Page B (kênh giả); nhân viên đọc theo Page; khách, hội thoại, tin chèn
// thẳng bằng chủ bảng.
type env struct {
	t            *testing.T
	s            *testkit.Staff
	tenant       uuid.UUID
	pageA, pageB uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &env{t: t, s: s}
	e.tenant = s.Tenant(t, "A")
	e.pageA = s.ChannelAccount(t, e.tenant, "fakechat", "page-a", "Page A")
	e.pageB = s.ChannelAccount(t, e.tenant, "fakechat", "page-b", "Page B")
	return e
}

// user tạo nhân viên email có perms ở phạm vi Page page (Nil: toàn shop) và nạp như lớp HTTP.
func (e *env) user(email string, page uuid.UUID, perms ...string) identity.AuthUser {
	e.t.Helper()
	convtest.Staff(e.t, e.s, e.tenant, page, email, perms...)
	return convtest.Load(e.t, e.s, email)
}

// customer tạo khách tên name (phone rỗng: không danh tính SĐT).
func (e *env) customer(name, phone string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.s.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		w := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
		c, err := w.CreateCustomer(e.t.Context(), tx, customer.CreateInput{FullName: name, Phone: phone},
			customer.Actor{Type: customer.ActorSystem}, time.Now().UTC())
		id = c.ID
		return err //nolint:wrapcheck // test
	})
	return id
}

// merge gộp hồ sơ child vào gốc root (như customer.MergeCustomers: chỉ đổi root_id).
func (e *env) merge(root, child uuid.UUID) {
	e.t.Helper()
	e.exec(`UPDATE customer.customers SET root_id = $1 WHERE id = $2`, root, child)
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	e.s.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), sql, args...)
		return err //nolint:wrapcheck // test
	})
}

func (e *env) scan(out any, sql string, args ...any) {
	e.t.Helper()
	e.s.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), sql, args...).Scan(out)
	})
}

// conv là một hội thoại cần chèn; trường rỗng lấy mặc định (inbox, open, không ai nhận, đã đọc).
type conv struct {
	page, customer uuid.UUID
	kind, status   string
	assignee       *uuid.UUID
	unread         int
	at             time.Time
	post           *uuid.UUID
	lastCustomer   *time.Time
}

func (e *env) conv(c conv) uuid.UUID {
	e.t.Helper()
	if c.kind == "" {
		c.kind = "inbox"
	}
	if c.status == "" {
		c.status = "open"
	}
	if c.at.IsZero() {
		c.at = time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	}
	if c.kind == "comment" && c.post == nil {
		c.post = new(e.post(c.page, "post-"+uuid.NewString(), "post", nil))
	}
	var id uuid.UUID
	e.scan(&id, `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
		external_thread_ref, post_id, status, assignee_id, unread_count, last_message_at, last_customer_message_at)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		c.page, c.customer, c.kind, uuid.NewString(), c.post, c.status, c.assignee, c.unread, c.at, c.lastCustomer)
	return id
}

// post chèn bài viết loại kind (post, live) đăng lúc created (nil: chưa biết).
func (e *env) post(page uuid.UUID, ext, kind string, created *time.Time) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.scan(&id, `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id, kind, created_time)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4) RETURNING id`, page, ext, kind, created)
	return id
}

// read chạy fn bằng crm_app trong ngữ cảnh tenant (như lớp HTTP).
func (e *env) read(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.s.App, e.tenant, fn) //nolint:wrapcheck // test
}
