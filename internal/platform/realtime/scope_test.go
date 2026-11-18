package realtime_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// WarehouseScope dựng đúng phạm vi kho mà identity.Can so với grant warehouse:<id>.
func TestWarehouseScope(t *testing.T) {
	s := realtime.WarehouseScope("w1")
	if s == nil || *s != (identity.Scope{Type: identity.ScopeWarehouse, ID: "w1"}) {
		t.Fatalf("WarehouseScope sai: %+v", s)
	}
	u := identity.AuthUser{Grants: []identity.Grant{{Permission: "inventory.read", Scope: identity.Scope{Type: "warehouse", ID: "w1"}}}}
	if !identity.Can(u, "inventory.read", s) || identity.Can(u, "inventory.read", realtime.WarehouseScope("w2")) {
		t.Fatal("grant warehouse:w1 phải thấy w1, không thấy w2")
	}
}
