package fulfillment_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Đảo "Thành công" (kế hoạch M3, Task M3-27; TD10, TI08): hãng sửa lại (tra API) và nhân viên sửa nhầm (xem trước →
// áp theo token). Tiền COD do cash ghi khi nghe shipment.status_changed (kiểm ở cash và tests/e2e); ở đây sổ COD chỉ
// được đọc (luật chặn), dòng sổ được nạp bằng chủ bảng.

const correctionReason = "Nhân viên bấm nhầm Thành công, gọi 0912345678"

// deliveredD1 dựng D1 (vA ×1, vB ×2) với vận đơn hiện hành đã giao qua tra API (COD = cả đơn, hãng thu đủ).
func deliveredD1(t *testing.T) (*applyEnv, order.Order, uuid.UUID, uuid.UUID, fulfillment.Shipment) {
	t.Helper()
	e, o, vA, vB, s := newD1Shipment(t)
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0), ev("d", fulfillment.CanonDelivered, 15, 0))
	e.orderIs(o.ID, order.StatusDelivered)
	return e, o, vA, vB, e.shipment(s.ID)
}

func (e *applyEnv) preview(id uuid.UUID, in fulfillment.CorrectionInput) (fulfillment.CorrectionPreview, error) {
	var p fulfillment.CorrectionPreview
	err := e.app(func(tx pgx.Tx) (err error) {
		p, err = e.svc.PreviewCorrection(e.t.Context(), tx, id, in)
		return err //nolint:wrapcheck // test
	})
	return p, err
}

func (e *applyEnv) mustPreview(id uuid.UUID) fulfillment.CorrectionPreview {
	e.t.Helper()
	p, err := e.preview(id, fulfillment.CorrectionInput{To: fulfillment.StatusReturning, Reason: correctionReason})
	if err != nil {
		e.t.Fatalf("PreviewCorrection: %v", err)
	}
	return p
}

func (e *applyEnv) correct(id uuid.UUID, token string) (fulfillment.ShipmentView, error) {
	var v fulfillment.ShipmentView
	err := e.app(func(tx pgx.Tx) (err error) {
		v, err = e.svc.ApplyCorrection(e.t.Context(), tx, id, fulfillment.CorrectionInput{
			To: fulfillment.StatusReturning, Reason: correctionReason, PreviewToken: token,
		}, order.Actor{TenantID: e.tenant, UserID: e.actor.UserID, Type: "staff"})
		return err //nolint:wrapcheck // test
	})
	return v, err
}

// seedRemitted nạp bằng chủ bảng bút toán remitted (hãng đã chuyển amount về shop theo bảng kê đã chốt) cho vận đơn
// id; reversed: thêm bút toán đảo trỏ tới nó (số dư shop_received về 0, remitted không còn sống).
func (e *applyEnv) seedRemitted(id uuid.UUID, amount int64, reversed bool) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		post := func(kind, reason string, sign int64, reverses *uuid.UUID) (uuid.UUID, error) {
			var tid uuid.UUID
			if err := tx.QueryRow(e.t.Context(), `INSERT INTO cash.cod_transfers
				(tenant_id, shipment_id, kind, reverses_transfer_id, idempotency_key, actor_type, reason)
				VALUES ($1, $2, $3, $4, $5, 'system', $6) RETURNING id`,
				e.tenant, id, kind, reverses, "seed:"+uuid.NewString(), reason).Scan(&tid); err != nil {
				return uuid.Nil, err //nolint:wrapcheck // test
			}
			_, err := tx.Exec(e.t.Context(), `INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, shipment_id, account, amount)
				VALUES ($1, $2, $5, 'carrier_holding', $3), ($1, $2, $5, 'shop_received', $4)`,
				e.tenant, tid, -sign*amount, sign*amount, id)
			return tid, err //nolint:wrapcheck // test
		}
		r, err := post("remitted", "", 1, nil)
		if err != nil || !reversed {
			return err
		}
		_, err = post("reversal", "Đảo bút toán thử", -1, &r)
		return err
	})
}

// ledgerSnapshot: (id, xmin) của mọi dòng sổ tồn của biến thể vs và sổ COD của vận đơn — chỉ được thêm dòng, không
// dòng nào bị sửa hay xóa.
func (e *applyEnv) ledgerSnapshot(shipment uuid.UUID, vs ...uuid.UUID) []string {
	e.t.Helper()
	var out []string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT 'stock:' || id || ':' || xmin FROM inventory.stock_ledger
			WHERE variant_id = ANY($1)
			UNION ALL SELECT 'cod:' || l.id || ':' || l.xmin FROM cash.cod_ledger_entries l
			JOIN cash.cod_transfers t ON t.tenant_id = l.tenant_id AND t.id = l.transfer_id WHERE t.shipment_id = $2`,
			vs, shipment)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	})
	return out
}

func appendOnly(t *testing.T, before, after []string) {
	t.Helper()
	for _, b := range before {
		if !slices.Contains(after, b) {
			t.Fatalf("dòng sổ %s bị sửa hay xóa", b)
		}
	}
	if len(after) <= len(before) {
		t.Fatalf("sổ không thêm dòng: %d → %d", len(before), len(after))
	}
}

// historyReason đếm dòng lịch sử đơn delivered → confirmed với lý do reason.
func (e *applyEnv) historyReason(orderID uuid.UUID, reason string) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status = 'delivered'
		AND to_status = 'confirmed' AND reason = $2`, orderID, reason)
}

func (e *applyEnv) correctedAlerts() []map[string]string {
	e.t.Helper()
	var out []map[string]string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), "SELECT payload FROM platform.jobs WHERE kind = $1", fulfillment.CorrectedAlertKind)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		raws, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
		for _, r := range raws {
			var m map[string]string
			if err := json.Unmarshal(r, &m); err != nil {
				return err //nolint:wrapcheck // test
			}
			out = append(out, m)
		}
		return err //nolint:wrapcheck // test
	})
	return out
}

// TI08: xem trước có câu đơn, mỗi dòng một câu đảo phiếu xuất, câu đảo tiền đã thu; không chặn; không đổi dữ liệu.
// Áp với token → như hãng sửa lại (vận đơn returning, đơn confirmed, hàng in_transit, phiếu hoàn), lịch sử đơn lý do
// staff_corrected, sự kiện nguồn staff có lý do đã che SĐT, sổ chỉ thêm dòng, không cảnh báo carrier-corrected.
func TestCorrection_TI08(t *testing.T) {
	t.Parallel()
	e, o, vA, vB, s := deliveredD1(t)
	p := e.mustPreview(s.ID)
	sku := func(v uuid.UUID) string {
		for _, it := range o.Items {
			if it.VariantID == v {
				return it.SKU
			}
		}
		return ""
	}
	want := []string{
		"Vận đơn từ Thành công sang Đang chuyển hoàn; đơn bỏ Thành công, mở phiếu Hãng hoàn chờ kho nhận",
		"Đảo phiếu xuất " + sku(vA) + " ×1: hàng về Đang chuyển, kho nhận lại khi hàng hoàn về",
		"Đảo phiếu xuất " + sku(vB) + " ×2: hàng về Đang chuyển, kho nhận lại khi hàng hoàn về",
		fmt.Sprintf("Đảo bút toán tiền hãng đã thu %s đ", vnd(s.CODAmount)),
	}
	if p.ShipmentID != s.ID || p.From != fulfillment.StatusDelivered || p.To != fulfillment.StatusReturning ||
		!slices.Equal(p.Effects, want) || len(p.Blocking) != 0 || len(p.Token) != 64 {
		t.Fatalf("xem trước = %+v, muốn tác động %q", p, want)
	}
	if again := e.mustPreview(s.ID); again.Token != p.Token {
		t.Fatal("xem trước hai lần ra hai token")
	}
	if got := e.shipment(s.ID); got.Version != s.Version || got.Status != fulfillment.StatusDelivered {
		t.Fatalf("xem trước đổi vận đơn: %+v", got)
	}
	before := e.ledgerSnapshot(s.ID, vA, vB)
	v, err := e.correct(s.ID, p.Token)
	if err != nil {
		t.Fatalf("ApplyCorrection: %v", err)
	}
	if v.Status != string(fulfillment.StatusReturning) {
		t.Fatalf("vận đơn sau áp = %s", v.Status)
	}
	e.returningIs(o, s, vA, vB)
	appendOnly(t, before, e.ledgerSnapshot(s.ID, vA, vB))
	if n := e.historyReason(o.ID, "staff_corrected"); n != 1 {
		t.Fatalf("lịch sử đơn staff_corrected = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1 AND source = 'staff'
		AND canonical = 'returning' AND applied AND actor_id = $2 AND raw->>'reason' LIKE 'Nhân viên bấm nhầm%'
		AND raw->>'reason' NOT LIKE '%0912345678%'`, s.ID, e.actor.UserID); n != 1 {
		t.Fatalf("sự kiện nhân viên sửa nhầm = %d, muốn 1", n)
	}
	if a := e.correctedAlerts(); len(a) != 0 {
		t.Fatalf("cảnh báo carrier-corrected khi nhân viên sửa = %v, muốn không", a)
	}
	// shipment.status_changed của bước đảo mang nguồn staff (sổ COD ghi lý do "Nhân viên sửa…", Task 138).
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'shipment.status_changed'
		AND aggregate_id = $1 AND payload->>'status' = 'returning' AND payload->>'source' = 'staff'`,
		s.ID.String()); n != 1 {
		t.Fatalf("sự kiện đảo có source staff = %d, muốn 1", n)
	}
	if _, err := e.correct(s.ID, p.Token); !errors.Is(err, fulfillment.ErrCorrectionNotAllowed) {
		t.Fatalf("áp lần hai (đã returning) = %v, muốn ErrCorrectionNotAllowed", err)
	}
}

// Token cũ sau khi vận đơn đổi (hãng báo lại tiền đã thu → câu tiền đổi, version mới) → ErrCorrectionStale, không
// đổi gì; xem trước lại rồi áp → được.
func TestCorrection_StaleToken(t *testing.T) {
	t.Parallel()
	e, o, _, _, s := deliveredD1(t)
	old := e.mustPreview(s.ID)
	collected := s.CODAmount - 30000
	if _, err := e.tryApply(fulfillment.ApplyInput{
		ShipmentID: s.ID, Source: fulfillment.SrcCarrierAPI, SyncedAt: testDay().Add(16 * time.Hour),
		Tracking: fulfillment.Tracking{
			Events:       []fulfillment.CarrierEvent{ev("d", fulfillment.CanonDelivered, 15, 0)},
			CODCollected: &collected,
		},
	}); err != nil {
		t.Fatal(err)
	}
	fresh := e.mustPreview(s.ID)
	if fresh.Token == old.Token || !slices.Contains(fresh.Effects,
		fmt.Sprintf("Đảo bút toán tiền hãng đã thu %s đ", vnd(collected))) {
		t.Fatalf("xem trước sau khi hãng báo lại tiền = %+v", fresh)
	}
	if _, err := e.correct(s.ID, old.Token); !errors.Is(err, fulfillment.ErrCorrectionStale) {
		t.Fatalf("token cũ = %v, muốn ErrCorrectionStale", err)
	}
	e.shipmentIs(s.ID, fulfillment.StatusDelivered)
	e.orderIs(o.ID, order.StatusDelivered)
	if _, err := e.correct(s.ID, fresh.Token); err != nil {
		t.Fatalf("token mới: %v", err)
	}
	e.shipmentIs(s.ID, fulfillment.StatusReturning)
}

// Hai lần áp song song cùng token (hai tab) → đúng một lần qua, lần kia ErrCorrectionStale hay ErrCorrectionNotAllowed;
// một phiếu hoàn, một dòng lịch sử đơn.
func TestCorrection_ConcurrentApply(t *testing.T) {
	t.Parallel()
	e, o, _, _, s := deliveredD1(t)
	p := e.mustPreview(s.ID)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.correct(s.ID, p.Token) })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, fulfillment.ErrCorrectionStale), errors.Is(err, fulfillment.ErrCorrectionNotAllowed):
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("số lần áp được = %d (%v), muốn 1", ok, errs)
	}
	if n := e.historyReason(o.ID, "staff_corrected"); n != 1 {
		t.Fatalf("lịch sử đơn = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE shipment_id = $1 AND kind = 'carrier_return'`, s.ID); n != 1 {
		t.Fatalf("phiếu hoàn = %d, muốn 1", n)
	}
}

// Đã có bảng kê chốt (shop_received 598.000) → Blocking có câu; áp → ErrCorrectionBlocked, không đổi gì. Hãng sửa lại
// (tra API, không qua nhân viên) → vận đơn vẫn đảo, cảnh báo carrier-corrected:<mã vận đơn>; tiền do cash (finding
// remitted_then_returned, Task M3-29). Bút toán remitted đã bị đảo (shop_received 0) → không chặn.
func TestCorrection_BlockedByRemittance(t *testing.T) {
	t.Parallel()
	e, o, vA, vB, s := deliveredD1(t)
	e.seedRemitted(s.ID, 598000, false)
	p := e.mustPreview(s.ID)
	want := []string{"Vận đơn đã có tiền về shop 598.000 đ trong bảng kê đã chốt — kế toán điều chỉnh sổ COD trước"}
	if !slices.Equal(p.Blocking, want) {
		t.Fatalf("Blocking = %q, muốn %q", p.Blocking, want)
	}
	if _, err := e.correct(s.ID, p.Token); !errors.Is(err, fulfillment.ErrCorrectionBlocked) {
		t.Fatalf("áp khi bị chặn = %v, muốn ErrCorrectionBlocked", err)
	}
	e.shipmentIs(s.ID, fulfillment.StatusDelivered)
	e.orderIs(o.ID, order.StatusDelivered)

	before := e.ledgerSnapshot(s.ID, vA, vB)
	e.apply(s.ID, ev("r", fulfillment.CanonReturning, 17, 0))
	e.returningIs(o, s, vA, vB)
	appendOnly(t, before, e.ledgerSnapshot(s.ID, vA, vB))
	if n := e.historyReason(o.ID, "carrier_corrected"); n != 1 {
		t.Fatalf("lịch sử đơn carrier_corrected = %d, muốn 1", n)
	}
	if a := e.correctedAlerts(); len(a) != 1 || a[0]["code"] != s.Code || a[0]["shipmentId"] != s.ID.String() {
		t.Fatalf("cảnh báo carrier-corrected = %v, muốn một cho %s", a, s.Code)
	}

	e2, _, _, _, s2 := deliveredD1(t)
	e2.seedRemitted(s2.ID, 598000, true)
	if p := e2.mustPreview(s2.ID); len(p.Blocking) != 0 {
		t.Fatalf("remitted đã đảo: Blocking = %q, muốn rỗng", p.Blocking)
	}
}

// Từ chối: vận đơn chưa giao, đã giao một phần → ErrCorrectionNotAllowed; lý do trống, đích khác returning, áp thiếu
// token → ErrCorrectionInvalid; vận đơn lạ → ErrNotFound. Không đổi gì.
func TestCorrection_Refusals(t *testing.T) {
	t.Parallel()
	e, o, vA, vB, s := newD1Shipment(t)
	in := fulfillment.CorrectionInput{To: fulfillment.StatusReturning, Reason: "x"}
	if _, err := e.preview(s.ID, in); !errors.Is(err, fulfillment.ErrCorrectionNotAllowed) {
		t.Fatalf("vận đơn awaiting_pickup = %v, muốn ErrCorrectionNotAllowed", err)
	}
	for name, bad := range map[string]fulfillment.CorrectionInput{
		"lý do khoảng trắng": {To: fulfillment.StatusReturning, Reason: "  "},
		"đích delivered":     {To: fulfillment.StatusDelivered, Reason: "x"},
		"lý do 1001 ký tự":   {To: fulfillment.StatusReturning, Reason: string(make([]rune, 1001))},
	} {
		if _, err := e.preview(s.ID, bad); !errors.Is(err, fulfillment.ErrCorrectionInvalid) {
			t.Errorf("%s = %v, muốn ErrCorrectionInvalid", name, err)
		}
	}
	if _, err := e.correct(s.ID, ""); !errors.Is(err, fulfillment.ErrCorrectionInvalid) {
		t.Fatalf("áp thiếu token = %v, muốn ErrCorrectionInvalid", err)
	}
	if _, err := e.preview(uuid.New(), in); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("vận đơn lạ = %v, muốn ErrNotFound", err)
	}

	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0))
	if _, err := e.recordPartial(s.ID, td09Collected, td09Lines(o, vA, vB)...); err != nil {
		t.Fatal(err)
	}
	if _, err := e.preview(s.ID, in); !errors.Is(err, fulfillment.ErrCorrectionNotAllowed) {
		t.Fatalf("đã giao một phần = %v, muốn ErrCorrectionNotAllowed", err)
	}
	e.partialUnchanged(o, s.ID, vA, vB)
}

// QĐ-118-2 (chồng Task 106): ghi tay returning lên vận đơn tay delivered cũng theo luật chặn — tiền đã về shop →
// ErrCorrectionBlocked, không đổi gì; chưa có tiền về → được (như trước).
func TestCorrection_ManualStatusBlocked(t *testing.T) {
	t.Parallel()
	e := newApplyEnv(t)
	acc := e.manualAccount("best", "best-tay-dao")
	delivered := func() fulfillment.ShipmentView {
		o, _ := e.order(1)
		sv := e.manualShipping(o, acc)
		return e.mustSetManual(sv.ID, fulfillment.ManualStatusInput{
			To: fulfillment.StatusDelivered, At: time.Now().UTC().Add(-2 * time.Hour), Reason: "Giao",
		})
	}
	back := fulfillment.ManualStatusInput{To: fulfillment.StatusReturning, At: time.Now().UTC().Add(-time.Hour), Reason: "Nhầm"}
	blocked := delivered()
	e.seedRemitted(blocked.ID, 280000, false)
	if _, err := e.setManual(blocked.ID, back); !errors.Is(err, fulfillment.ErrCorrectionBlocked) {
		t.Fatalf("ghi tay returning khi tiền đã về = %v, muốn ErrCorrectionBlocked", err)
	}
	e.shipmentIs(blocked.ID, fulfillment.StatusDelivered)
	free := delivered()
	if v := e.mustSetManual(free.ID, back); v.Status != string(fulfillment.StatusReturning) {
		t.Fatalf("ghi tay returning chưa có tiền về = %s", v.Status)
	}
}

// manualShipping tạo vận đơn tay của đơn o ở tài khoản tay acc rồi ghi tay shipping (3 giờ trước).
func (e *applyEnv) manualShipping(o order.Order, acc uuid.UUID) fulfillment.ShipmentView {
	e.t.Helper()
	sv, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: acc, TrackingCode: trackingCode()})
	if err != nil {
		e.t.Fatalf("tạo vận đơn tay: %v", err)
	}
	return e.mustSetManual(sv.ID, fulfillment.ManualStatusInput{
		To: fulfillment.StatusShipping, At: time.Now().UTC().Add(-3 * time.Hour), Reason: "Đã lấy hàng",
	})
}

// Lệnh CorrectedAlertKind: Notify(key "carrier-corrected:<mã vận đơn>", normal, văn bản chỉ có mã); payload hỏng →
// rejected; không Notifier → rejected; Notify lỗi → not_sent.
func TestCorrectedAlertCommand(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"shipmentId":"` + uuid.NewString() + `","code":"VD0001"}`)
	n := &recordingNotifier{}
	out, err := fulfillment.NewCorrectedAlertCommand(n, nil).Execute(t.Context(), queue.Job{Payload: payload})
	if err != nil || out.Status != "done" || len(n.keys) != 1 || n.keys[0] != "carrier-corrected:VD0001" ||
		n.severity[0] != "normal" {
		t.Fatalf("gửi = %+v %v %+v", out, err, n)
	}
	for name, c := range map[string]struct {
		n       fulfillment.Notifier
		payload []byte
		want    string
	}{
		"payload hỏng":   {n, []byte(`{"code":""}`), "rejected"},
		"không Notifier": {nil, payload, "rejected"},
		"Notify lỗi":     {&recordingNotifier{err: errors.New("telegram")}, payload, "not_sent"},
	} {
		out, err := fulfillment.NewCorrectedAlertCommand(c.n, nil).Execute(t.Context(), queue.Job{Payload: c.payload})
		if err != nil || string(out.Status) != c.want {
			t.Errorf("%s = %+v %v, muốn %s", name, out, err, c.want)
		}
	}
}

type recordingNotifier struct {
	mu             sync.Mutex
	keys, severity []string
	err            error
}

func (r *recordingNotifier) Notify(_ context.Context, key, severity, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys, r.severity = append(r.keys, key), append(r.severity, severity)
	return r.err
}

// vnd định dạng tiền như câu tác động ("630.000").
func vnd(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}
