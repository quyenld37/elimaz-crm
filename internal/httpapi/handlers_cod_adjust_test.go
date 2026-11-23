package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// transferOfKind trả mã bút toán loại kind gần nhất của vận đơn s.
func (e *m3Env) transferOfKind(s uuid.UUID, kind string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT id FROM cash.cod_transfers WHERE shipment_id = $1 AND kind = $2 ORDER BY id DESC LIMIT 1",
			s, kind).Scan(&id)
	})
	return id
}

// previewCodAdjustment, createCodAdjustment qua HTTP (Task M3-33, TI08): Kế toán xem trước đảo bút toán collected →
// 200 số dư trước, sau, bút toán reversal, token; ghi với token → 201 bút toán đảo (lý do, tác nhân có tên), audit
// cash.cod.adjust không mang lý do; token cũ → 409 preview_stale; lý do chỉ khoảng trắng → 422; bút toán của vận đơn
// khác → 404; expected của vận đơn đang giao → 409 invalid_transition; Kho (không có cash.adjust) → 403.
func TestCodAdjustAPI(t *testing.T) {
	t.Parallel()
	e := newM3Env(t)
	shop, user := e.newCodShop()
	s := e.codShipment(shop, user, 630000, []string{"picked_up", "delivered"})
	other := e.codShipment(shop, user, 200000, []string{"picked_up"})
	collected := e.transferOfKind(s, cash.KindCollected)
	body := fmt.Sprintf(`{"shipmentId":%q,"mode":"reverse","reversesTransferId":%q,"reason":"hãng báo nhầm"}`, s, collected)
	code, p := e.send(http.MethodPost, "/api/v1/cod/adjustments/preview", m3Accounter, body)
	if code != http.StatusOK {
		t.Fatalf("xem trước: %d %v", code, p)
	}
	before, after := p["before"].(map[string]any), p["after"].(map[string]any) //nolint:forcetypeassert // test
	tr := p["transfer"].(map[string]any)                                       //nolint:forcetypeassert // test
	if before["carrierHolding"] != float64(630000) || after["carrierHolding"] != float64(0) ||
		after["toCollect"] != float64(630000) || tr["kind"] != "reversal" || len(tr["entries"].([]any)) != 2 || //nolint:forcetypeassert // test
		p["token"] == "" || p["warnings"] == nil {
		t.Fatalf("bản xem trước: %v", p)
	}
	token := p["token"].(string) //nolint:forcetypeassert // test
	withToken := func(tok string) string { return body[:len(body)-1] + fmt.Sprintf(`,"token":%q}`, tok) }
	code, c := e.send(http.MethodPost, "/api/v1/cod/adjustments", m3Accounter, withToken(token))
	actor, _ := c["actor"].(map[string]any)
	if code != http.StatusCreated || c["kind"] != "reversal" || c["reversesTransferId"] != collected.String() ||
		c["reason"] != "hãng báo nhầm" || actor["name"] == "" || actor["name"] == nil {
		t.Fatalf("ghi: %d %v", code, c)
	}
	var audits int
	var after2 []byte
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*), coalesce(max(after::text), '{}') FROM identity.audit_logs
			WHERE action = 'cash.cod.adjust' AND entity_type = 'cash.cod_transfer' AND entity_id = $1`, c["id"]).Scan(&audits, &after2)
	})
	var auditAfter map[string]any
	if err := json.Unmarshal(after2, &auditAfter); err != nil || audits != 1 || auditAfter["shipment_id"] != s.String() ||
		auditAfter["reason"] != nil {
		t.Errorf("audit: %d %s %v", audits, after2, err)
	}
	if code, b := e.send(http.MethodPost, "/api/v1/cod/adjustments", m3Accounter, withToken(token)); code != http.StatusConflict ||
		b["error"] != "preview_stale" {
		t.Errorf("token cũ: %d %v", code, b)
	}
	blank := fmt.Sprintf(`{"shipmentId":%q,"mode":"move","from":"carrier_fee","to":"shop_received","amount":1,"reason":"   "}`, s)
	if code, b := e.send(http.MethodPost, "/api/v1/cod/adjustments/preview", m3Accounter, blank); code != http.StatusUnprocessableEntity ||
		b["error"] != "invalid_input" {
		t.Errorf("thiếu lý do: %d %v", code, b)
	}
	foreign := fmt.Sprintf(`{"shipmentId":%q,"mode":"reverse","reversesTransferId":%q,"reason":"x"}`, other, collected)
	if code, b := e.send(http.MethodPost, "/api/v1/cod/adjustments/preview", m3Accounter, foreign); code != http.StatusNotFound ||
		b["error"] != "not_found" {
		t.Errorf("bút toán vận đơn khác: %d %v", code, b)
	}
	exp := fmt.Sprintf(`{"shipmentId":%q,"mode":"reverse","reversesTransferId":%q,"reason":"x"}`, other,
		e.transferOfKind(other, cash.KindExpected))
	if code, b := e.send(http.MethodPost, "/api/v1/cod/adjustments/preview", m3Accounter, exp); code != http.StatusConflict ||
		b["error"] != "invalid_transition" {
		t.Errorf("expected đang giao: %d %v", code, b)
	}
	if code, _ := e.send(http.MethodPost, "/api/v1/cod/adjustments/preview", m3Warehouse, body); code != http.StatusForbidden {
		t.Errorf("Kho: %d, mong 403", code)
	}
}

// listReconcileFindings qua HTTP (Task M3-33, TI03): chưa chạy → items rỗng, không runId; sau lần chạy có lệch →
// runId, runAt, phát hiện balance_mismatch có shipmentId và detail chuỗi; runId lạ → 404; con trỏ sai dạng → 400; Kho
// (không có cash.cod.read) → 403.
func TestReconcileFindingsAPI(t *testing.T) {
	t.Parallel()
	e := newM3Env(t)
	if code, b := e.send(http.MethodGet, "/api/v1/cod/reconcile-findings", m3Accounter, ""); code != http.StatusOK ||
		b["runId"] != nil || len(b["items"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("chưa chạy: %d %v", code, b)
	}
	shop, user := e.newCodShop()
	s := e.codShipment(shop, user, 450000, []string{"picked_up"}) // vận đơn vẫn creating mà sổ đã có expected → lệch
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {             // đổi trạng thái đã lâu (không bị bỏ qua vì vừa đổi)
		_, err := tx.Exec(t.Context(), "UPDATE fulfillment.shipments SET last_status_at = now() - interval '1 day' WHERE id = $1", s)
		return err //nolint:wrapcheck // test
	})
	var h commands.Handler
	for _, c := range e.cash.CommandHandlers() {
		if c.Kind().Name == cash.ReconcileScheduleName {
			h = c
		}
	}
	slot, _ := json.Marshal(map[string]time.Time{"slot": time.Date(2026, 11, 20, 19, 30, 0, 0, time.UTC)})
	if out, err := h.Execute(t.Context(), queue.Job{TenantID: &e.tenant, Payload: slot}); err != nil || out.Status != commands.StatusDone {
		t.Fatalf("đối chiếu: %+v %v", out, err)
	}
	code, b := e.send(http.MethodGet, "/api/v1/cod/reconcile-findings?limit=10", m3Accounter, "")
	items, _ := b["items"].([]any)
	if code != http.StatusOK || b["runId"] == nil || b["runAt"] == nil || len(items) != 1 {
		t.Fatalf("sau lần chạy: %d %v", code, b)
	}
	f := items[0].(map[string]any) //nolint:forcetypeassert // test
	detail, _ := f["detail"].(map[string]any)
	if f["kind"] != cash.FindingBalanceMismatch || f["shipmentId"] != s.String() || f["runId"] != b["runId"] ||
		detail["toCollect"] != "450000" {
		t.Errorf("phát hiện: %v", f)
	}
	if code, b := e.send(http.MethodGet, "/api/v1/cod/reconcile-findings?runId="+b["runId"].(string), m3Owner, ""); code != http.StatusOK || //nolint:forcetypeassert // test
		len(b["items"].([]any)) != 1 { //nolint:forcetypeassert // test
		t.Errorf("theo runId: %d %v", code, b)
	}
	if code, b := e.send(http.MethodGet, "/api/v1/cod/reconcile-findings?runId="+uuid.NewString(), m3Accounter, ""); code != http.StatusNotFound ||
		b["error"] != "not_found" {
		t.Errorf("runId lạ: %d %v", code, b)
	}
	if code, _ := e.send(http.MethodGet, "/api/v1/cod/reconcile-findings?cursor=x", m3Accounter, ""); code != http.StatusBadRequest {
		t.Errorf("con trỏ sai: %d", code)
	}
	if code, _ := e.send(http.MethodGet, "/api/v1/cod/reconcile-findings", m3Warehouse, ""); code != http.StatusForbidden {
		t.Errorf("Kho: %d, mong 403", code)
	}
}
