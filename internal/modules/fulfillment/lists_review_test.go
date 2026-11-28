package fulfillment_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// anomalyOf trả mã anomaly mở loại kind của vận đơn id.
func anomalyOf(e *shipEnv, id uuid.UUID, kind string) uuid.UUID {
	e.t.Helper()
	return readAs(e, func(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
		var a uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM fulfillment.shipment_anomalies WHERE shipment_id = $1 AND kind = $2
			AND status = 'open'`, id, kind).Scan(&a)
		return a, err //nolint:wrapcheck // test
	})
}

// resolveAnomaly đóng anomaly id bằng crm_app (mọi kho được phép).
func resolveAnomaly(e *shipEnv, m *fulfillment.Module, id uuid.UUID, resolution string) (fulfillment.Anomaly, error) {
	var a fulfillment.Anomaly
	err := e.app(func(tx pgx.Tx) (err error) {
		a, err = m.Service.ResolveAnomaly(e.t.Context(), tx, id, fulfillment.AnomalyResolveInput{
			Resolution: resolution, Authorize: func(uuid.UUID) error { return nil },
		}, e.actor)
		return err //nolint:wrapcheck // test
	})
	return a, err
}

func (e *shipEnv) needsReview(id uuid.UUID) bool {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND needs_review`, id) == 1
}

func (e *shipEnv) addAnomaly(id uuid.UUID, kind string) uuid.UUID {
	e.t.Helper()
	var a uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(e.t.Context(), `UPDATE fulfillment.shipments SET needs_review = true WHERE id = $1`, id); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(e.t.Context(), `INSERT INTO fulfillment.shipment_anomalies (tenant_id, shipment_id, kind, detail)
			VALUES ($1, $2, $3, '{"code": "X1"}') RETURNING id`, e.tenant, id, kind).Scan(&a)
	})
	return a
}

// addCallback chèn việc gọi lại lần attempt của vận đơn s, trạng thái status, hạn due.
func (e *shipEnv) addCallback(s fulfillment.Shipment, attempt int, status string, due time.Time) {
	e.t.Helper()
	e.ownerExec(`INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at, status)
		VALUES ($1, $2, $3, $4, $5, $5, $6)`, e.tenant, s.ID, s.OrderID, attempt, due, status)
}

func newListsModule(e *shipEnv, now func() time.Time) *fulfillment.Module {
	return fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &realtimetest.RecordingBus{}, Events: events.NewRegistry(), Now: now,
	})
}

func (e *shipEnv) review(m *fulfillment.Module, f fulfillment.ReviewFilter) fulfillment.ReviewPage {
	e.t.Helper()
	return readAs(e, func(ctx context.Context, tx pgx.Tx) (fulfillment.ReviewPage, error) {
		if f.SellerID == uuid.Nil { // người bán của đơn trong test (ADR-29: bộ lọc bắt buộc có người bán)
			f.SellerID = e.seller
		}
		return m.Service.ListForReview(ctx, tx, f)
	})
}

// VC16: vận đơn in_doubt_create nằm trong Cần xem với câu hướng dẫn; đóng ở đây → ErrAnomalyNotResolvable (409),
// anomaly vẫn mở. counts đúng với dữ liệu thử: 2 cần xem (in_doubt_create, missing_items), 2 kẹt, 3 gọi lại mở
// (open, in_progress, open quá hạn), 1 quá hạn, 1 không rõ; việc done/closed và anomaly đã đóng không đếm. Kho ngoài
// phạm vi → rỗng, 0.
func TestReview_ListCountsAndInDoubt(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	m := newListsModule(e, func() time.Time { return now })
	_, _, s2 := e.shipped(1)
	_, _, s3 := e.shipped(1)
	_, inDoubt := e.createTimedOut()
	missing := e.addAnomaly(s2.ID, "missing_items")
	closed := e.addAnomaly(s3.ID, "tracking_conflict")
	e.ownerExec(`UPDATE fulfillment.shipment_anomalies SET status = 'resolved', resolved_at = now() WHERE id = $1`, closed)
	e.ownerExec(`UPDATE fulfillment.shipments SET stuck_since = $2 WHERE id = ANY ($1)`, []uuid.UUID{s2.ID, s3.ID}, now)
	e.addCallback(s2, 1, "open", now.Add(time.Hour))
	e.addCallback(s2, 2, "in_progress", now.Add(2*time.Hour))
	e.addCallback(s3, 1, "open", now.Add(-time.Minute))
	e.addCallback(s3, 2, "done", now.Add(-time.Hour))
	e.addCallback(s3, 3, "closed", now.Add(-time.Hour))

	p := e.review(m, fulfillment.ReviewFilter{AllWarehouses: true})
	want := fulfillment.ReviewCounts{Review: 2, Stuck: 2, CallbacksOpen: 3, CallbacksOverdue: 1, InDoubt: 1}
	if p.Counts != want {
		t.Fatalf("counts = %+v, muốn %+v", p.Counts, want)
	}
	if len(p.Items) != 2 || p.Next != "" {
		t.Fatalf("items = %+v next %q", p.Items, p.Next)
	}
	first := p.Items[0]
	if first.ShipmentID != inDoubt.ID || first.Kind != "in_doubt_create" || first.OrderCode == "" ||
		first.Label != "Không rõ hãng đã tạo vận đơn chưa — kiểm trên trang hãng rồi bấm Đã tạo / Chưa tạo" {
		t.Fatalf("việc đầu = %+v", first)
	}
	if p.Items[1].AnomalyID != missing || p.Items[1].Detail["code"] != "X1" || p.Items[1].Label == "" {
		t.Fatalf("việc thứ hai = %+v", p.Items[1])
	}
	if p := e.review(m, fulfillment.ReviewFilter{WarehouseIDs: []uuid.UUID{uuid.New()}}); len(p.Items) != 0 ||
		p.Counts != (fulfillment.ReviewCounts{}) {
		t.Fatalf("kho ngoài phạm vi = %+v", p)
	}
	if p := e.review(m, fulfillment.ReviewFilter{WarehouseIDs: []uuid.UUID{e.shop.Warehouse}}); p.Counts != want {
		t.Fatalf("kho MAIN = %+v", p.Counts)
	}

	if _, err := resolveAnomaly(e, m, first.AnomalyID, "đã kiểm"); !errors.Is(err, fulfillment.ErrAnomalyNotResolvable) {
		t.Fatalf("đóng in_doubt_create = %v, muốn ErrAnomalyNotResolvable", err)
	}
	if e.openAnomalies(inDoubt.ID, "in_doubt_create") != 1 || !e.needsReview(inDoubt.ID) {
		t.Fatal("in_doubt_create phải còn mở")
	}
	// Việc hủy tay có đường riêng (Ghi tay trạng thái tự đóng việc): đóng ở đây → ErrAnomalyNotResolvable, vẫn mở.
	for _, kind := range []string{"manual_cancel", "in_doubt_cancel"} {
		a := e.addAnomaly(s3.ID, kind)
		if _, err := resolveAnomaly(e, m, a, "đã hủy trên trang hãng"); !errors.Is(err, fulfillment.ErrAnomalyNotResolvable) {
			t.Fatalf("đóng %s = %v, muốn ErrAnomalyNotResolvable", kind, err)
		}
		if e.openAnomalies(s3.ID, kind) != 1 {
			t.Fatalf("%s phải còn mở", kind)
		}
	}
}

// resolveShipmentAnomaly: resolution bắt buộc; đóng anomaly cuối → needs_review false; còn anomaly khác mở → vẫn true;
// đóng lần hai → ErrAnomalyNotResolvable; không có / tenant khác → ErrAnomalyNotFound; Authorize từ chối → lỗi của nó,
// không đổi gì.
func TestReview_Resolve(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	now := time.Now().UTC()
	m := newListsModule(e, func() time.Time { return now })
	_, _, s := e.shipped(1)
	a1 := e.addAnomaly(s.ID, "missing_items")
	a2 := e.addAnomaly(s.ID, "tracking_conflict")

	if _, err := resolveAnomaly(e, m, a1, "  "); !errors.Is(err, fulfillment.ErrShipInvalid) {
		t.Fatalf("resolution rỗng = %v", err)
	}
	denied := errors.New("không có quyền")
	if err := e.app(func(tx pgx.Tx) error {
		_, err := m.Service.ResolveAnomaly(t.Context(), tx, a1, fulfillment.AnomalyResolveInput{
			Resolution: "x", Authorize: func(wh uuid.UUID) error {
				if wh != e.shop.Warehouse {
					t.Errorf("Authorize nhận kho %s, muốn %s", wh, e.shop.Warehouse)
				}
				return denied
			},
		}, e.actor)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, denied) {
		t.Fatalf("Authorize từ chối = %v", err)
	}
	got, err := resolveAnomaly(e, m, a1, "đã gọi khách")
	if err != nil || got.ID != a1 || got.Status != "resolved" || got.Resolution != "đã gọi khách" || got.ResolvedAt == nil ||
		got.Kind != "missing_items" || got.ShipmentID != s.ID {
		t.Fatalf("đóng a1 = %+v, %v", got, err)
	}
	if !e.needsReview(s.ID) {
		t.Fatal("còn a2 mở mà needs_review false")
	}
	if _, err := resolveAnomaly(e, m, a1, "lần hai"); !errors.Is(err, fulfillment.ErrAnomalyNotResolvable) {
		t.Fatalf("đóng lần hai = %v", err)
	}
	if _, err := resolveAnomaly(e, m, a2, "đã xử lý"); err != nil {
		t.Fatal(err)
	}
	if e.needsReview(s.ID) {
		t.Fatal("đóng anomaly cuối mà needs_review vẫn true")
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_anomalies WHERE id = $1 AND resolved_by = $2`, a2, e.actor.UserID); n != 1 {
		t.Fatal("resolved_by phải là người đóng")
	}
	if _, err := resolveAnomaly(e, m, uuid.New(), "x"); !errors.Is(err, fulfillment.ErrAnomalyNotFound) {
		t.Fatalf("anomaly lạ = %v", err)
	}
	other := e.staff.Tenant(t, "B")
	if err := tenancy.WithTenant(t.Context(), e.staff.App, other, func(tx pgx.Tx) error {
		_, err := m.Service.ResolveAnomaly(t.Context(), tx, a1, fulfillment.AnomalyResolveInput{
			Resolution: "x", Authorize: func(uuid.UUID) error { return nil },
		}, fulfillment.Actor{TenantID: other})
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrAnomalyNotFound) {
		t.Fatalf("tenant khác = %v", err)
	}
}

// Hai người đóng cùng anomaly cùng lúc → đúng một thành công, người kia ErrAnomalyNotResolvable.
func TestReview_ResolveConcurrent(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	m := newListsModule(e, time.Now)
	_, _, s := e.shipped(1)
	a := e.addAnomaly(s.ID, "missing_items")
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() { _, errs[i] = resolveAnomaly(e, m, a, "song song") })
	}
	wg.Wait()
	ok := slices.IndexFunc(errs, func(err error) bool { return err == nil })
	for i, err := range errs {
		if i != ok && !errors.Is(err, fulfillment.ErrAnomalyNotResolvable) {
			t.Fatalf("lần %d = %v", i, err)
		}
	}
	if ok < 0 || slices.ContainsFunc(errs[ok+1:], func(err error) bool { return err == nil }) {
		t.Fatalf("số lần thành công khác 1: %v", errs)
	}
}

// Cần xem phân trang theo con trỏ (cũ nhất trước), counts như nhau mọi trang; con trỏ hỏng → ErrInvalidPage.
func TestReview_Pages(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	m := newListsModule(e, time.Now)
	var want []uuid.UUID
	for range 3 {
		_, _, s := e.shipped(1)
		want = append(want, e.addAnomaly(s.ID, "missing_items"))
	}
	var got []uuid.UUID
	cursor := ""
	for range 4 {
		p := e.review(m, fulfillment.ReviewFilter{AllWarehouses: true, Limit: 2, Cursor: cursor})
		if p.Counts.Review != 3 {
			t.Fatalf("counts.review = %d", p.Counts.Review)
		}
		for _, it := range p.Items {
			got = append(got, it.AnomalyID)
		}
		if p.Next == "" {
			break
		}
		cursor = p.Next
	}
	if !slices.Equal(got, want) {
		t.Fatalf("các trang = %v, muốn %v", got, want)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := m.Service.ListForReview(t.Context(), tx, fulfillment.ReviewFilter{AllWarehouses: true, Cursor: "!"})
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrInvalidPage) {
		t.Fatalf("con trỏ hỏng = %v", err)
	}
}
