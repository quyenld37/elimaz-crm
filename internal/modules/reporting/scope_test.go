package reporting_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

func grant(p identity.Permission, typ, id string) identity.Grant {
	return identity.Grant{Permission: p, Scope: identity.Scope{Type: typ, ID: id}}
}

func TestScopeFor(t *testing.T) {
	t.Parallel()
	tenant, seller := uuid.New(), uuid.New()
	p1, p2, w1 := uuid.New(), uuid.New(), uuid.New()
	u := identity.AuthUser{TenantID: tenant, Grants: []identity.Grant{
		grant("report.sales.read", identity.ScopePage, p1.String()),
		grant("report.sales.read", identity.ScopePage, p2.String()),
		grant("report.sales.read", identity.ScopePage, p1.String()), // trùng: một lần
		grant("audit.read", identity.ScopeAll, ""),
		grant("report.sales.read", identity.ScopeWarehouse, w1.String()), // khác loại phạm vi: bỏ
		grant("report.sales.read", identity.ScopePage, "khong-phai-uuid"),
		grant("report.inventory.read", identity.ScopeWarehouse, w1.String()),
		grant("report.inventory.read", identity.ScopePage, p1.String()), // khác loại phạm vi: bỏ
	}}
	s := reporting.ScopeFor(u, seller)
	if s.TenantID != tenant || s.SellerID != seller {
		t.Fatalf("tenant, seller = %s, %s", s.TenantID, s.SellerID)
	}
	if s.AllPages || !slices.Equal(s.PageIDs, []uuid.UUID{p1, p2}) || s.NoSales() {
		t.Fatalf("Page: all=%v ids=%v", s.AllPages, s.PageIDs)
	}
	if s.AllWarehouses || !slices.Equal(s.WarehouseIDs, []uuid.UUID{w1}) || s.NoInventory() {
		t.Fatalf("kho: all=%v ids=%v", s.AllWarehouses, s.WarehouseIDs)
	}

	all := reporting.ScopeFor(identity.AuthUser{Grants: []identity.Grant{
		grant("report.sales.read", identity.ScopeAll, ""),
		grant("report.sales.read", identity.ScopePage, p1.String()),
	}}, seller)
	if !all.AllPages || all.NoSales() || !all.NoInventory() {
		t.Fatalf("phạm vi all: %+v", all)
	}

	none := reporting.ScopeFor(identity.AuthUser{Grants: []identity.Grant{
		grant("audit.read", identity.ScopeAll, ""),
		grant("report.sales.read", identity.ScopeWarehouse, w1.String()),
		grant("report.sales.read", identity.ScopeAll, "x"), // all có mã: không phải all (identity.isAll)
	}}, seller)
	if !none.NoSales() || !none.NoInventory() || none.AllPages || len(none.PageIDs) != 0 {
		t.Fatalf("không grant hợp lệ: %+v", none)
	}
	if !reporting.ScopeFor(identity.AuthUser{}, seller).NoSales() {
		t.Fatal("không grant nào mà vẫn có phạm vi bán hàng")
	}
}

// OwnSeller: người bán own đang active có id nhỏ nhất (luật catalog.DefaultSeller, G3 sau Task 223); không có →
// ErrNoOwnSeller; người bán partner, own bị ngừng, tenant khác không tính.
func TestOwnSeller(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "a"), s.Tenant(t, "b")
	read := func(tenant uuid.UUID) (uuid.UUID, error) {
		var id uuid.UUID
		err := tenancy.WithTenant(t.Context(), s.App, tenant, func(tx pgx.Tx) error {
			var err error
			if id, err = reporting.OwnSeller(t.Context(), tx); err != nil {
				return fmt.Errorf("OwnSeller: %w", err)
			}
			return nil
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("đọc người bán: %w", err)
		}
		return id, nil
	}
	if _, err := read(a); !errors.Is(err, reporting.ErrNoOwnSeller) {
		t.Fatalf("chưa có người bán: %v", err)
	}
	insert := func(tenant uuid.UUID, code, kind, status string) uuid.UUID {
		var id uuid.UUID
		s.Exec(t, tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `INSERT INTO catalog.sellers (tenant_id, code, name, kind, status)
				VALUES ($1, $2, $2, $3, $4) RETURNING id`, tenant, code, kind, status).Scan(&id)
		})
		return id
	}
	insert(a, "NCC", "partner", "active")
	insert(a, "CU", "own", "suspended")
	if _, err := read(a); !errors.Is(err, reporting.ErrNoOwnSeller) {
		t.Fatalf("chỉ có partner và own ngừng: %v", err)
	}
	first := insert(a, "ELIMAZ", "own", "active")
	insert(a, "ELIMAZ2", "own", "active")
	insert(b, "B", "own", "active")
	got, err := read(a)
	if err != nil || got != first {
		t.Fatalf("OwnSeller = %s, %v; mong %s", got, err, first)
	}
}
