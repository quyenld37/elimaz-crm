package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// auditByAction xếp các dòng nhật ký của một trang theo action.
func auditByAction(t *testing.T, items []any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, it := range items {
		row, _ := it.(map[string]any)
		out[fmt.Sprint(row["action"])] = row
	}
	return out
}

// [Task 488] Cột "Mục" của nhật ký: yêu cầu đơn → mã YC (dòng audit thật của lệnh tạo), phiếu kho → mã phiếu; mã không
// còn → không có entityName; loại không có tên → không có. GET audit-actors: người có audit.read đọc được nhân viên (chỉ
// tên, không email riêng ngoài tên hiển thị), người không có → 403.
func TestAuditLogsAPI_EntityNamesAndActors(t *testing.T) {
	t.Parallel()
	e := newReqAPIEnv(t, nil)
	sku := e.variant(e.shop.Seller, 199000, 3)
	_ = e.webOrder("W-NK", sku, 1, true)
	ref, _ := e.ok("tạo yêu cầu", e.createRequest("W-NK", exchangeBody(sku, "FIT")), http.StatusCreated)["ref"].(string)
	var doc inventory.Document
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.shop.Tenant, func(tx pgx.Tx) (err error) {
		doc, err = inventory.CreateDocument(t.Context(), tx, inventory.DocumentInput{
			WarehouseID: e.shop.Warehouse, Kind: inventory.KindImport,
		}, inventory.Actor{Type: "system"})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, en := range []identity.AuditEntry{
			{Action: "t.doc", EntityType: "inventory.stock_document", EntityID: doc.ID.String()},
			{Action: "t.doc-gone", EntityType: "inventory.stock_document", EntityID: uuid.NewString()},
			{Action: "t.req-gone", EntityType: "order_request", EntityID: uuid.NewString()},
			{Action: "t.other", EntityType: "order", EntityID: doc.ID.String()},
		} {
			en.TenantID, en.ActorType = e.shop.Tenant, "system"
			if err := identity.RecordAudit(t.Context(), tx, en); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got := e.ok("nhật ký", e.get(reqOwner, auditPath), http.StatusOK)
	items, _ := got["items"].([]any)
	rows := auditByAction(t, items)
	for action, want := range map[string]any{
		"order.request.create": ref, "t.doc": doc.Code, "t.doc-gone": nil, "t.req-gone": nil, "t.other": nil,
	} {
		row, ok := rows[action]
		if !ok || row["entityName"] != want {
			t.Errorf("%s: %v, muốn entityName %v", action, row, want)
		}
	}
	if doc.Code == "" || ref == "" {
		t.Fatalf("thiếu mã: phiếu %q, yêu cầu %q", doc.Code, ref)
	}

	expect(t, "không có audit.read", e.get(reqNobody, "/api/v1/identity/audit-actors"),
		http.StatusForbidden, "forbidden")
	actors := e.ok("người làm", e.get(reqOwner, "/api/v1/identity/audit-actors"), http.StatusOK)
	list, _ := actors["items"].([]any)
	names := map[string]map[string]any{}
	for _, it := range list {
		a, _ := it.(map[string]any)
		names[fmt.Sprint(a["name"])] = a
	}
	if a := names[reqSale]; a == nil || a["actorType"] != "staff" || a["active"] != true {
		t.Fatalf("người làm: %v", actors)
	}
	for _, a := range names {
		if a["email"] != nil {
			t.Fatalf("trả email: %v", a)
		}
	}
}

// [Task 488] Dòng nhật ký về tài khoản hãng có entityName là tên tài khoản (đọc qua fulfillment).
func TestAuditLogsAPI_CarrierAccountName(t *testing.T) {
	t.Parallel()
	e, a, _, _ := newWebCarrierEnv(t)
	reader := "doc-nhat-ky@elimaz.test"
	e.staff.Grant(t, e.shop.Tenant, e.staff.User(t, e.shop.Tenant, reader, "active"), []string{"audit.read"}, "all", "")
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		return identity.RecordAudit(t.Context(), tx, identity.AuditEntry{
			TenantID: e.shop.Tenant, ActorType: "system", Action: "t.acc", EntityType: "carrier_account", EntityID: a.String(),
		})
	})
	code, got, raw := e.send(http.MethodGet, auditPath+"?action=t.acc", reader, "")
	items, _ := got["items"].([]any)
	if code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["entityName"] != "Hãng a" { //nolint:forcetypeassert // test
		t.Fatalf("tên tài khoản hãng: %d %s", code, raw)
	}
}
