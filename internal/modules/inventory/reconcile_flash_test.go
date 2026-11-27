package inventory_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// Đối chiếu đêm cho đơn vị flash sale (kế hoạch M5, Task 202 / M5-19; 05 mục 7.4).

// flashSaleItem dùng flashItem (đợt nháp, n suất) rồi giữ n suất cho món bằng ReserveForFlash (held + n có sổ
// flash_hold, n đơn vị trống) và đặt trạng thái đợt là status bằng chủ bảng. Trả mã món.
func (e *invEnv) flashSaleItem(v uuid.UUID, status string, n int) uuid.UUID {
	e.t.Helper()
	ref := e.flashItem(v, n)
	if err := e.app(func(tx pgx.Tx) error {
		return e.writer().ReserveForFlash(e.t.Context(), tx, ref, inventory.Actor{Type: inventory.ActorStaff, ID: uuid.New()})
	}); err != nil {
		e.t.Fatal(err)
	}
	e.setSaleStatus(ref.ItemID, status)
	return ref.ItemID
}

// setSaleStatus đặt trạng thái đợt của món item bằng chủ bảng.
func (e *invEnv) setSaleStatus(item uuid.UUID, status string) {
	e.t.Helper()
	e.owner(`UPDATE promotion.flash_sales SET status = $2 WHERE id =
		(SELECT flash_sale_id FROM promotion.flash_sale_items WHERE id = $1)`, item, status)
}

func flashMismatches(r inventory.Run, item uuid.UUID) int {
	n := 0
	for _, m := range r.Mismatches {
		if m.Check == inventory.CheckFlashUnits && m.FlashItemID == item {
			n++
		}
	}
	return n
}

// held_vs_reservations của M1 cộng đơn vị trống của đợt scheduled/open: đợt đang mở, đợt đã lên lịch không lệch.
// Sửa tay: hai đơn vị trống → held gắn một dòng đơn số lượng 1 → flash_units báo đúng mã món (và held lệch giữ hàng
// ở biến thể); đợt đóng mà còn đơn vị trống → flash_units báo mã món đó. Món khác không bị báo.
func TestReconcile_FlashUnits(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vOpen, vSched, vClosed := e.stock(10), e.stock(10), e.stock(10)
	open := e.flashSaleItem(vOpen, "open", 4)
	sched := e.flashSaleItem(vSched, "scheduled", 3)
	closed := e.flashSaleItem(vClosed, "open", 2)
	if r := e.reconcile(0); !r.OK {
		t.Fatalf("đợt mở, đã lên lịch: lệch %+v", r.Mismatches)
	}
	item, err := e.orderItem(vOpen)
	if err != nil {
		t.Fatal(err)
	}
	e.owner(`UPDATE inventory.flash_sale_units SET status = 'held', order_item_id = $2 WHERE id IN
		(SELECT id FROM inventory.flash_sale_units WHERE flash_sale_item_id = $1 ORDER BY id LIMIT 2)`, open, item)
	e.setSaleStatus(closed, "closed")
	r := e.reconcile(0)
	if flashMismatches(r, open) != 1 || flashMismatches(r, closed) != 1 || flashMismatches(r, sched) != 0 {
		t.Fatalf("mục flash_units: %+v", r.Mismatches)
	}
	if !hasMismatch(r, inventory.Mismatch{
		VariantID: vOpen, WarehouseID: e.warehouse,
		Check: inventory.CheckHeldVsReservations, Expected: 2, Actual: 4,
	}) {
		t.Errorf("held lệch giữ hàng của biến thể món mở: %+v", r.Mismatches)
	}
	for _, m := range r.Mismatches {
		if m.Check == inventory.CheckFlashUnits && (m.VariantID != uuid.Nil || m.WarehouseID != uuid.Nil) {
			t.Errorf("mục flash_units chỉ mang mã món: %+v", m)
		}
	}
}

// Việc đối chiếu: có mục flash_units → ghi đúng một lệnh FlashMismatchAlertKind (payload chỉ số món và mã món). Lệnh gửi khóa reconcile:flash:<tenant>, mức normal, văn bản chỉ có số
// món và mã món.
func TestReconcile_FlashAlert(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	item := e.flashSaleItem(e.stock(5), "open", 2)
	e.setSaleStatus(item, "closed")
	w := queue.NewWorker(e.staff.App, []queue.Handler{inventory.NewReconcileHandler(queue.NewQueue())},
		queue.WorkerOptions{WorkerID: "w", Concurrency: 1})
	e.runReconcileJob(w)
	var payload json.RawMessage
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = $1", inventory.FlashMismatchAlertKind); n != 1 {
		t.Fatalf("%d lệnh cảnh báo flash, mong 1", n)
	}
	if err := e.staff.Owner.Pool.QueryRow(t.Context(), "SELECT payload FROM platform.jobs WHERE kind = $1",
		inventory.FlashMismatchAlertKind).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(payload, &p); err != nil || len(p) != 2 {
		t.Fatalf("payload %s", payload)
	}
	n := &fakeNotifier{}
	cmd := inventory.NewFlashMismatchCommand(n, nil)
	if cmd.Kind().Name != inventory.FlashMismatchAlertKind || cmd.Kind().Policy.Mode != queue.ModeCommand {
		t.Fatalf("Kind %+v", cmd.Kind())
	}
	out, err := cmd.Execute(t.Context(), queue.Job{Kind: cmd.Kind().Name, Payload: payload, TenantID: &e.tenant})
	if err != nil || out.Status != commands.StatusDone {
		t.Fatalf("Execute: %+v, %v", out, err)
	}
	if n.key != "reconcile:flash:"+e.tenant.String() || n.severity != "normal" ||
		!strings.HasPrefix(n.text, "1 món flash sale lệch đơn vị suất: ") || !strings.Contains(n.text, item.String()) {
		t.Fatalf("cảnh báo %+v", n)
	}
	if out, err := cmd.Execute(t.Context(), queue.Job{Kind: cmd.Kind().Name, Payload: payload}); err != nil ||
		out.Status != commands.StatusRejected {
		t.Fatalf("không tenant: %+v, %v", out, err)
	}
}

// runReconcileJob ghi một việc đối chiếu cho tenant của e và chạy nó bằng w.
func (e *invEnv) runReconcileJob(w *queue.Worker) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		_, _, err := queue.NewQueue().Enqueue(e.t.Context(), tx, inventory.ReconcileKind(),
			queue.NewJob{TenantID: &e.tenant, DedupeKey: uuid.NewString()})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	testkit.MakeDueSoon(e.t, e.staff.Owner)
	if _, err := w.RunOnce(e.t.Context(), 5); err != nil {
		e.t.Fatal(err)
	}
	if n := e.staff.Count(e.t, "SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'done'",
		inventory.ReconcileKind().Name); n != 1 {
		e.t.Fatalf("%d việc đối chiếu done", n)
	}
}
