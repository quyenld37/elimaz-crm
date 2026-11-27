package reporting_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Quy luật (vùng cao scope.go, kế hoạch M6 Task 227): với tập grant ngẫu nhiên (1–4 quyền, phạm vi all/page/warehouse,
// có mã không phải uuid, uuid viết hoa), một Page p thấy được ⇔ identity.Can(u, report.sales.read, page p); một kho w
// thấy được ⇔ identity.Can(u, report.inventory.read, warehouse w). NoSales ⇔ không Page nào và không AllPages.
func TestScopeForMatchesCan(t *testing.T) {
	t.Parallel()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	idText := []string{"", "khong-uuid", strings.ToUpper(ids[0].String()), "{" + ids[1].String() + "}"}
	for _, id := range ids {
		idText = append(idText, id.String())
	}
	perms := []identity.Permission{"report.sales.read", "report.inventory.read", "report.export", "audit.read"}
	types := []string{identity.ScopeAll, identity.ScopePage, identity.ScopeWarehouse}
	rapid.Check(t, func(rt *rapid.T) {
		grants := rapid.SliceOfN(rapid.Custom(func(rt *rapid.T) identity.Grant {
			typ := rapid.SampledFrom(types).Draw(rt, "type")
			id := rapid.SampledFrom(idText).Draw(rt, "id")
			if typ == identity.ScopeAll && rapid.Bool().Draw(rt, "allNoID") {
				id = ""
			}
			return identity.Grant{Permission: rapid.SampledFrom(perms).Draw(rt, "perm"), Scope: identity.Scope{Type: typ, ID: id}}
		}), 1, 4).Draw(rt, "grants")
		checkScopeMatchesCan(rt, identity.AuthUser{TenantID: uuid.New(), Grants: grants}, append(slices.Clone(ids), uuid.New()))
	})
}

// checkScopeMatchesCan so ScopeFor(u) với identity.Can trên từng đích trong targets (Page và kho).
func checkScopeMatchesCan(rt *rapid.T, u identity.AuthUser, targets []uuid.UUID) {
	s := reporting.ScopeFor(u, uuid.New())
	for _, p := range targets {
		sees := s.AllPages || slices.Contains(s.PageIDs, p)
		can := identity.Can(u, "report.sales.read", &identity.Scope{Type: identity.ScopePage, ID: p.String()})
		if sees != can {
			rt.Fatalf("Page %s: thấy = %v, Can = %v (grants %v, scope %+v)", p, sees, can, u.Grants, s)
		}
		seesW := s.AllWarehouses || slices.Contains(s.WarehouseIDs, p)
		canW := identity.Can(u, "report.inventory.read", &identity.Scope{Type: identity.ScopeWarehouse, ID: p.String()})
		if seesW != canW {
			rt.Fatalf("kho %s: thấy = %v, Can = %v (grants %v, scope %+v)", p, seesW, canW, u.Grants, s)
		}
	}
	if s.NoSales() != (!s.AllPages && len(s.PageIDs) == 0) || s.NoInventory() != (!s.AllWarehouses && len(s.WarehouseIDs) == 0) {
		rt.Fatalf("NoSales/NoInventory lệch: %+v", s)
	}
	if s.AllPages && len(s.PageIDs) != 0 {
		rt.Fatalf("AllPages mà còn PageIDs: %+v", s)
	}
}
