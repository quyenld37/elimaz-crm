package templates_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// tplEnv: tenant A có hai Page thật pageA ("Elimaz Hà Nội"), pageB; khách Lan (tên do nhân viên nhập), khách tạm (tên
// "Khách Facebook", name_source placeholder), khách Mai; hội thoại convLan (Page A, Lan), convTmp (Page A, khách tạm),
// convB (Page B, Lan); nhân viên: owner (mọi quyền hộp chat phạm vi all), pageAdmin (đọc + cài đặt chỉ Page A), reader
// (chỉ đọc Page A), readerB (chỉ đọc Page B). Tenant B trống.
type tplEnv struct {
	t                        *testing.T
	staff                    *testkit.Staff
	tenant, other            uuid.UUID
	pageA, pageB             uuid.UUID
	lan, tmp, mai            uuid.UUID
	convLan, convTmp, convB  uuid.UUID
	owner, pageAdmin, reader identity.AuthUser
	readerB                  identity.AuthUser
	shop                     *shiptest.Shop
	variant                  uuid.UUID
}

func newTplEnv(t *testing.T) *tplEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &tplEnv{t: t, staff: s, tenant: s.Tenant(t, "A"), other: s.Tenant(t, "B")}
	e.pageA = s.ChannelAccount(t, e.tenant, "fakechat", "page-a", "Elimaz Hà Nội")
	e.pageB = s.ChannelAccount(t, e.tenant, "fakechat", "page-b", "Elimaz HCM")
	ctx := t.Context()
	s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for _, c := range []struct {
			id           *uuid.UUID
			name, source string
		}{{&e.lan, "Lan", "staff"}, {&e.tmp, "Khách Facebook", "placeholder"}, {&e.mai, "Mai", "staff"}} {
			if err := tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name, name_search, name_source)
				VALUES (current_setting('app.tenant_id')::uuid, $1, $1, $2) RETURNING id`, c.name, c.source).Scan(c.id); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		for _, c := range []struct {
			id         *uuid.UUID
			page, cust uuid.UUID
			ref        string
		}{{&e.convLan, e.pageA, e.lan, "psid-lan"}, {&e.convTmp, e.pageA, e.tmp, "psid-tmp"}, {&e.convB, e.pageB, e.lan, "psid-lan-b"}} {
			if err := tx.QueryRow(ctx, `INSERT INTO conversation.conversations
				(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, last_message_at)
				VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox', $3, now()) RETURNING id`,
				c.page, c.cust, c.ref).Scan(c.id); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		e.shop = shiptest.NewShop(t, tx, e.tenant)
		e.variant = e.shop.Variant(t, tx, e.shop.Seller, 100000, 50)
		return nil
	})
	settings := []string{"conversation.read", "conversation.reply", "conversation.settings"}
	e.owner = e.user("chu@elimaz.test", identity.ScopeAll, "", settings...)
	e.pageAdmin = e.user("qtv-a@elimaz.test", identity.ScopePage, e.pageA.String(), settings...)
	e.reader = e.user("doc-a@elimaz.test", identity.ScopePage, e.pageA.String(), "conversation.read")
	e.readerB = e.user("doc-b@elimaz.test", identity.ScopePage, e.pageB.String(), "conversation.read")
	return e
}

// user tạo nhân viên (display_name = email) có các quyền perms ở phạm vi (scopeType, scopeID).
func (e *tplEnv) user(email, scopeType, scopeID string, perms ...string) identity.AuthUser {
	e.t.Helper()
	id := e.staff.User(e.t, e.tenant, email, "active")
	e.staff.Grant(e.t, e.tenant, id, perms, scopeType, scopeID)
	u := identity.AuthUser{UserID: id, TenantID: e.tenant, Email: email, Kind: identity.KindStaff}
	for _, p := range perms {
		u.Grants = append(u.Grants, identity.Grant{
			Permission: identity.Permission(p), Scope: identity.Scope{Type: scopeType, ID: scopeID},
		})
	}
	return u
}

// order tạo một đơn (qua order) rồi chuyển sang khách customer; trả mã đơn và mã hiển thị.
func (e *tplEnv) order(customer uuid.UUID) (uuid.UUID, string) {
	e.t.Helper()
	var o order.Order
	testkit.InTenant(e.t, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		o, err = e.shop.Order(e.t.Context(), tx, e.shop.Seller, uuid.Nil, 0, order.ItemInput{VariantID: e.variant, Qty: 1})
		return err //nolint:wrapcheck // test
	})
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE ord.orders SET customer_id = $2 WHERE id = $1`, o.ID, customer)
		return err //nolint:wrapcheck // test
	})
	return o.ID, o.Code
}

// tx chạy fn bằng crm_app trong ngữ cảnh tenant, trả lỗi của fn.
func (e *tplEnv) tx(fn func(pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

func (e *tplEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}
