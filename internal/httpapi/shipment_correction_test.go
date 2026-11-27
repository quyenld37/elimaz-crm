package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Đảo "Thành công" qua HTTP (kế hoạch M3, Task M3-27; TI08): previewShipmentCorrection, applyShipmentCorrection.

const correctUser = "sua-nham@elimaz.test"

// deliveredManual tạo đơn 1 món, vận đơn tay ở tài khoản acc rồi ghi tay shipping → delivered (đơn delivered, hàng đã
// xuất); trả mã vận đơn.
func (e *shipAPIEnv) deliveredManual(acc uuid.UUID, key string) string {
	e.t.Helper()
	orderID := e.confirmedOrder("o-" + key)
	code, s := e.post("/api/v1/orders/"+orderID+"/shipments", "s-"+key,
		fmt.Sprintf(`{"carrierAccountId":"%s","trackingCode":"BEST%s"}`, acc, key))
	if code != http.StatusAccepted {
		e.t.Fatalf("tạo vận đơn tay = %d %v", code, s)
	}
	id := fmt.Sprint(s["id"])
	for i, to := range []string{"shipping", "delivered"} {
		at := time.Now().UTC().Add(time.Duration(i-3) * time.Hour).Format(time.RFC3339)
		if code, b := e.manualStatus(manualUser, id, key+to, fmt.Sprintf(`{"to":"%s","at":"%s","reason":"x"}`, to, at)); code != http.StatusOK {
			e.t.Fatalf("ghi tay %s = %d %v", to, code, b)
		}
	}
	return id
}

// Thiếu fulfillment.correct → 403; xem trước → 200 ba câu tác động, không chặn, token; bấm lại cùng khóa → cùng thân;
// áp với token sai → 409 preview_stale; áp đúng → 200 returning, cùng khóa → cùng thân, một audit có lý do đã che SĐT;
// khóa khác → 409 invalid_transition; lý do khoảng trắng → 422 invalid_input.
func TestShipmentCorrectionAPI(t *testing.T) {
	t.Parallel()
	e, acc := correctionEnv(t)
	id := e.deliveredManual(acc, "100001")
	preview := "/api/v1/shipments/" + id + "/corrections/preview"
	apply := "/api/v1/shipments/" + id + "/corrections"
	body := `{"to":"returning","reason":"Bấm nhầm, khách gọi 0912345678"}`

	if rec := serveAs(e.srv, e.keys, http.MethodPost, preview, manualUser, "p0", body); rec.Code != http.StatusForbidden {
		t.Fatalf("thiếu fulfillment.correct = %d %s, muốn 403", rec.Code, rec.Body.String())
	}
	if rec := serveAs(e.srv, e.keys, http.MethodPost, preview, correctUser, "p1",
		`{"to":"returning","reason":"   "}`); rec.Code != http.StatusUnprocessableEntity || decodeJSON(t, rec)["error"] != "invalid_input" {
		t.Fatalf("lý do khoảng trắng = %d %s, muốn 422 invalid_input", rec.Code, rec.Body.String())
	}
	rec1 := serveAs(e.srv, e.keys, http.MethodPost, preview, correctUser, "p2", body)
	rec2 := serveAs(e.srv, e.keys, http.MethodPost, preview, correctUser, "p2", body)
	if rec1.Code != http.StatusOK || rec1.Body.String() != rec2.Body.String() {
		t.Fatalf("xem trước hai lần cùng khóa: %d %d\n%s\n%s", rec1.Code, rec2.Code, rec1.Body.String(), rec2.Body.String())
	}
	p := decodeJSON(t, rec1)
	effects, _ := p["effects"].([]any)
	blocking, _ := p["blocking"].([]any)
	token := fmt.Sprint(p["token"])
	if p["shipmentId"] != id || p["from"] != "delivered" || p["to"] != "returning" || len(effects) != 3 ||
		len(blocking) != 0 || len(token) != 64 {
		t.Fatalf("xem trước = %v", p)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND status = 'delivered'", id); n != 1 {
		t.Fatal("xem trước đổi vận đơn")
	}

	applyBody := func(tok string) string {
		return fmt.Sprintf(`{"to":"returning","reason":"Bấm nhầm, khách gọi 0912345678","previewToken":"%s"}`, tok)
	}
	rec := serveAs(e.srv, e.keys, http.MethodPost, apply, correctUser, "a0", applyBody("khac"))
	if b := decodeJSON(t, rec); rec.Code != http.StatusConflict || b["error"] != "preview_stale" {
		t.Fatalf("token sai = %d %v, muốn 409 preview_stale", rec.Code, b)
	}
	rec1 = serveAs(e.srv, e.keys, http.MethodPost, apply, correctUser, "a1", applyBody(token))
	rec2 = serveAs(e.srv, e.keys, http.MethodPost, apply, correctUser, "a1", applyBody(token))
	if rec1.Code != http.StatusOK || rec1.Body.String() != rec2.Body.String() {
		t.Fatalf("áp hai lần cùng khóa: %d %d\n%s\n%s", rec1.Code, rec2.Code, rec1.Body.String(), rec2.Body.String())
	}
	if s := decodeJSON(t, rec1); s["status"] != "returning" {
		t.Fatalf("sau áp = %v", s)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.correction'
		AND entity_id = $1 AND after->>'reason' LIKE 'Bấm nhầm%' AND after->>'reason' NOT LIKE '%0912345678%'
		AND after->>'from' = 'delivered' AND after->>'to' = 'returning'`, id); n != 1 {
		t.Fatalf("audit đảo Thành công (đã che SĐT) = %d, muốn 1", n)
	}
	rec = serveAs(e.srv, e.keys, http.MethodPost, apply, correctUser, "a2", applyBody(token))
	if b := decodeJSON(t, rec); rec.Code != http.StatusConflict || b["error"] != "invalid_transition" {
		t.Fatalf("khóa khác sau khi đã đảo = %d %v, muốn 409 invalid_transition", rec.Code, b)
	}
}

// correctionEnv: môi trường HTTP có nhân viên ghi tay, nhân viên correctUser (fulfillment.correct), tài khoản tay.
func correctionEnv(t *testing.T) (*shipAPIEnv, uuid.UUID) {
	t.Helper()
	e := newShipAPIEnv(t)
	e.grantManual()
	tenant := e.shop.Tenant
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, correctUser, "active"), []string{"fulfillment.correct"}, "all", "")
	return e, e.manualAccount("best", "best-tay-sua")
}

// Tiền đã về shop (bút toán remitted) → xem trước có câu chặn, áp → 409 correction_blocked; vận đơn lạ → 404.
func TestShipmentCorrectionAPI_Blocked(t *testing.T) {
	t.Parallel()
	e, acc := correctionEnv(t)
	tenant := e.shop.Tenant
	body := `{"to":"returning","reason":"Bấm nhầm"}`
	applyBody := func(tok string) string {
		return fmt.Sprintf(`{"to":"returning","reason":"Bấm nhầm","previewToken":"%s"}`, tok)
	}
	blocked := e.deliveredManual(acc, "100002")
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		var tid uuid.UUID
		if err := tx.QueryRow(t.Context(), `INSERT INTO cash.cod_transfers (tenant_id, shipment_id, kind, idempotency_key,
			actor_type) VALUES ($1, $2, 'remitted', $3, 'system') RETURNING id`, tenant, blocked, "seed:"+blocked).Scan(&tid); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, shipment_id, account,
			amount) VALUES ($1, $2, $3, 'carrier_holding', -250000), ($1, $2, $3, 'shop_received', 250000)`, tenant, tid, blocked)
		return err //nolint:wrapcheck // test
	})
	rec := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/shipments/"+blocked+"/corrections/preview", correctUser, "b1", body)
	p := decodeJSON(t, rec)
	if blocking, _ := p["blocking"].([]any); rec.Code != http.StatusOK || len(blocking) != 1 {
		t.Fatalf("xem trước khi tiền đã về = %d %v", rec.Code, p)
	}
	rec = serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/shipments/"+blocked+"/corrections", correctUser, "b2",
		applyBody(fmt.Sprint(p["token"])))
	if b := decodeJSON(t, rec); rec.Code != http.StatusConflict || b["error"] != "correction_blocked" {
		t.Fatalf("áp khi tiền đã về = %d %v, muốn 409 correction_blocked", rec.Code, b)
	}

	rec = serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/shipments/"+uuid.NewString()+"/corrections/preview",
		correctUser, "n1", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("vận đơn lạ = %d %s, muốn 404", rec.Code, rec.Body.String())
	}
}
