package conversation_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// chatEnv (Task 169): tenant có hai Page A, B (mã ngẫu nhiên, khóa ngoại tới channel_accounts chưa có — tệp task-151),
// khách gốc ka và hồ sơ kb đã gộp vào ka, hội thoại inbox convA (Page A, khách ka), comment convB (Page B, hồ sơ kb), ba
// nhân viên: sale (đọc, trả lời Page A), lead (đọc, trả lời, giao việc Page A), viewer (chỉ đọc Page A).
type chatEnv struct {
	t                  *testing.T
	staff              *testkit.Staff
	tenant             uuid.UUID
	pageA, pageB       uuid.UUID
	ka, kb             uuid.UUID
	convA, convB, post uuid.UUID
	sale, lead, viewer identity.AuthUser
}

func newChatEnv(t *testing.T) *chatEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &chatEnv{t: t, staff: s, tenant: s.Tenant(t, "A"), pageA: uuid.New(), pageB: uuid.New()}
	ctx := t.Context()
	s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for _, c := range []struct {
			id   *uuid.UUID
			name string
		}{{&e.ka, "Chị Lan"}, {&e.kb, "Lan FB"}} {
			if err := tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name, name_search)
				VALUES (current_setting('app.tenant_id')::uuid, $1, $1) RETURNING id`, c.name).Scan(c.id); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE customer.customers SET root_id = $1 WHERE id = $2`, e.ka, e.kb); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id, kind, snippet)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'post-1', 'post', 'Váy hoa') RETURNING id`, e.pageB).Scan(&e.post); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.conversations
			(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, last_message_at, status, created_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox', 'psid-1', '2026-11-20 08:00Z', 'done',
			        '2026-11-20 08:00Z') RETURNING id`, e.pageA, e.ka).Scan(&e.convA); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO conversation.conversations
			(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, post_id, last_message_at, created_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'comment', 'post-1:psid-2', $3, '2026-11-21 08:00Z',
			        '2026-11-21 08:00Z') RETURNING id`, e.pageB, e.kb, e.post).Scan(&e.convB)
	})
	page := e.pageA.String()
	e.sale = e.user("sale@elimaz.test", page, "conversation.read", "conversation.reply")
	e.lead = e.user("lead@elimaz.test", page, "conversation.read", "conversation.reply", "conversation.assign")
	e.viewer = e.user("xem@elimaz.test", page, "conversation.read")
	return e
}

// user tạo nhân viên có các quyền perms trên Page page (grant thật trong database, để identity.StaffHas đọc được).
func (e *chatEnv) user(email, page string, perms ...string) identity.AuthUser {
	e.t.Helper()
	id := e.staff.User(e.t, e.tenant, email, "active")
	e.staff.Grant(e.t, e.tenant, id, perms, identity.ScopePage, page)
	u := identity.AuthUser{UserID: id, TenantID: e.tenant, Email: email, Kind: identity.KindStaff}
	for _, p := range perms {
		u.Grants = append(u.Grants, identity.Grant{
			Permission: identity.Permission(p), Scope: identity.Scope{Type: identity.ScopePage, ID: page},
		})
	}
	return u
}

// tx chạy fn bằng crm_app trong ngữ cảnh tenant, trả lỗi của fn.
func (e *chatEnv) tx(fn func(pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

// mustTx như tx, lỗi làm test đỏ.
func (e *chatEnv) mustTx(fn func(pgx.Tx) error) {
	e.t.Helper()
	if err := e.tx(fn); err != nil {
		e.t.Fatal(err)
	}
}

func (e *chatEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

// at trả mốc giờ Việt Nam ngày 26/11/2026 lúc hh:mm:ss (đồng hồ giả của test).
func at(hh, mm, ss int) time.Time {
	return time.Date(2026, 11, 26, hh, mm, ss, 0, time.FixedZone("ICT", 7*3600)).UTC()
}
