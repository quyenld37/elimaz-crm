package inventory_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// agedNow là đồng hồ giả của kế hoạch Task M3-22: 2026-12-21 00:00 +07.
func agedNow() time.Time {
	return time.Date(2026, 12, 21, 0, 0, 0, 0, time.FixedZone("ICT", 7*3600))
}

// agedSeed giữ 1 cái của một biến thể mới cho một dòng đơn thật, đưa sang state (held hoặc in_transit) bằng hàm của
// inventory, rồi đặt state_changed_at = since bằng chủ bảng (giả thời gian đã trôi). Trả mã giữ hàng.
func (e *invEnv) agedSeed(state string, since time.Time) uuid.UUID {
	e.t.Helper()
	v := e.stock(5)
	ids, err := e.reserve(sysActor(), e.line(v, 1))
	if err != nil {
		e.t.Fatal(err)
	}
	if state == inventory.StateInTransit {
		if err := e.app(func(tx pgx.Tx) error {
			return e.writer().StartTransit(e.t.Context(), tx, ids, sysActor())
		}); err != nil {
			e.t.Fatal(err)
		}
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE inventory.stock_reservations SET state_changed_at = $2 WHERE id = $1`, ids[0], since)
		return err //nolint:wrapcheck // test
	})
	return ids[0]
}

// aged gọi AgedReservations bằng crm_app trong ngữ cảnh tenant của e.
func (e *invEnv) aged(held, transit int, warehouses []uuid.UUID, p inventory.AgedPage) ([]inventory.AgedRow, *inventory.AgedCursor) {
	e.t.Helper()
	var rows []inventory.AgedRow
	var next *inventory.AgedCursor
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		p.SellerID = e.seller
		rows, next, err = inventory.AgedReservations(e.t.Context(), tx, agedNow(), held, transit, warehouses, p)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return rows, next
}

func agedIDs(rows []inventory.AgedRow) map[uuid.UUID]inventory.AgedRow {
	out := make(map[uuid.UUID]inventory.AgedRow, len(rows))
	for _, r := range rows {
		out[r.ReservationID] = r
	}
	return out
}

// TestAgedReservations_Boundaries (KH09, AT53): ngưỡng 20/180 ngày, now = 2026-12-21 00:00 +07. held từ 00:00:01 ngày
// 01/12 (thiếu 1 giây) → không; đúng 20 ngày → có; in_transit đúng 180 ngày → có, 179 ngày → không; dòng consumed,
// released cũ cũng không. Held = 25 → dòng 20 ngày biến mất.
func TestAgedReservations_Boundaries(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	now := agedNow()
	day := 24 * time.Hour
	heldShort := e.agedSeed(inventory.StateHeld, time.Date(2026, 12, 1, 0, 0, 1, 0, now.Location()))
	heldExact := e.agedSeed(inventory.StateHeld, time.Date(2026, 12, 1, 0, 0, 0, 0, now.Location()))
	transitExact := e.agedSeed(inventory.StateInTransit, now.Add(-180*day))
	transitShort := e.agedSeed(inventory.StateInTransit, now.Add(-179*day))
	released := e.agedSeed(inventory.StateHeld, now.Add(-400*day))
	if err := e.app(func(tx pgx.Tx) error {
		return e.writer().Release(t.Context(), tx, []uuid.UUID{released}, "test", sysActor())
	}); err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE inventory.stock_reservations SET state_changed_at = $2 WHERE id = $1`,
			released, now.Add(-400*day))
		return err //nolint:wrapcheck // test
	})

	got := agedIDs(func() []inventory.AgedRow { r, _ := e.aged(20, 180, nil, inventory.AgedPage{}); return r }())
	if len(got) != 2 {
		t.Fatalf("20/180: %d dòng, muốn 2 (held đúng 20 ngày, in_transit đúng 180 ngày): %+v", len(got), got)
	}
	for _, id := range []uuid.UUID{heldShort, transitShort, released} {
		if _, ok := got[id]; ok {
			t.Errorf("dòng %s không được vào danh sách", id)
		}
	}
	h, ok := got[heldExact]
	if !ok || h.State != inventory.StateHeld || h.AgeDays != 20 || h.Quantity != 1 || h.WarehouseID != e.warehouse ||
		h.OrderItemID == uuid.Nil || h.VariantID == uuid.Nil || !h.Since.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, now.Location())) {
		t.Errorf("held đúng 20 ngày: %+v", h)
	}
	if tr, ok := got[transitExact]; !ok || tr.State != inventory.StateInTransit || tr.AgeDays != 180 {
		t.Errorf("in_transit đúng 180 ngày: %+v", tr)
	}

	got = agedIDs(func() []inventory.AgedRow { r, _ := e.aged(25, 180, nil, inventory.AgedPage{}); return r }())
	if _, ok := got[heldExact]; ok || len(got) != 1 {
		t.Errorf("Held = 25: dòng 20 ngày phải biến khỏi danh sách, còn %d dòng: %+v", len(got), got)
	}

	onlyHeld, _ := e.aged(20, 180, nil, inventory.AgedPage{State: inventory.StateHeld})
	if len(onlyHeld) != 1 || onlyHeld[0].ReservationID != heldExact {
		t.Errorf("lọc held: %+v", onlyHeld)
	}
	onlyTransit, _ := e.aged(20, 180, nil, inventory.AgedPage{State: inventory.StateInTransit})
	if len(onlyTransit) != 1 || onlyTransit[0].ReservationID != transitExact {
		t.Errorf("lọc in_transit: %+v", onlyTransit)
	}
}

// TestAgedReservations_ReadOnly (KH09 "hệ thống không tự trả hàng", AT53 không tạo việc lặp): chạy 3 lần → số dòng
// stock_reservations (theo trạng thái), stock_ledger, platform.jobs không đổi.
func TestAgedReservations_ReadOnly(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	e.agedSeed(inventory.StateHeld, agedNow().Add(-30*24*time.Hour))
	e.agedSeed(inventory.StateInTransit, agedNow().Add(-200*24*time.Hour))
	snapshot := func() [4]int {
		var c [4]int
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT
				(SELECT count(*) FROM inventory.stock_reservations WHERE state = 'held'),
				(SELECT count(*) FROM inventory.stock_reservations WHERE state = 'in_transit'),
				(SELECT count(*) FROM inventory.stock_ledger),
				(SELECT count(*) FROM platform.jobs)`).Scan(&c[0], &c[1], &c[2], &c[3])
		})
		return c
	}
	before := snapshot()
	for range 3 {
		if rows, _ := e.aged(20, 180, nil, inventory.AgedPage{}); len(rows) != 2 {
			t.Fatalf("%d dòng, muốn 2", len(rows))
		}
	}
	if after := snapshot(); after != before {
		t.Errorf("chạy danh sách mà dữ liệu đổi: trước %v, sau %v (held, in_transit, sổ, việc)", before, after)
	}
}

// TestAgedReservations_Warehouses: danh sách kho rỗng (không nil) → không dòng nào; chỉ kho W2 → không thấy dòng của
// kho chính; nil → mọi kho.
func TestAgedReservations_Warehouses(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	main := e.agedSeed(inventory.StateHeld, agedNow().Add(-30*24*time.Hour))
	w2 := e.secondWarehouse()
	if rows, _ := e.aged(20, 180, []uuid.UUID{w2}, inventory.AgedPage{}); len(rows) != 0 {
		t.Errorf("chỉ kho W2: thấy %+v", rows)
	}
	if rows, _ := e.aged(20, 180, []uuid.UUID{}, inventory.AgedPage{}); len(rows) != 0 {
		t.Errorf("không kho nào: thấy %+v", rows)
	}
	if rows, _ := e.aged(20, 180, []uuid.UUID{e.warehouse, w2}, inventory.AgedPage{}); len(rows) != 1 || rows[0].ReservationID != main {
		t.Errorf("kho chính + W2: %+v", rows)
	}
}

// TestAgedReservations_Pages: cũ nhất trước; trang 2 dòng lần lượt hết 5 dòng (kể cả hai dòng cùng mốc), không lặp,
// không sót; trang cuối không có con trỏ; con trỏ qua String/Parse vẫn đúng.
func TestAgedReservations_Pages(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	base := agedNow().Add(-50 * 24 * time.Hour)
	var seeded []uuid.UUID
	for _, off := range []int{0, 1, 1, 2, 3} {
		seeded = append(seeded, e.agedSeed(inventory.StateHeld, base.Add(time.Duration(off)*time.Hour)))
	}
	var got []uuid.UUID
	var after *inventory.AgedCursor
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("không dừng phân trang")
		}
		rows, next := e.aged(20, 180, nil, inventory.AgedPage{After: after, Limit: 2})
		for _, r := range rows {
			got = append(got, r.ReservationID)
		}
		if next == nil {
			break
		}
		c, err := inventory.ParseAgedCursor(next.String())
		if err != nil || !c.Since.Equal(next.Since) || c.ID != next.ID {
			t.Fatalf("con trỏ qua chuỗi: %v %+v != %+v", err, c, *next)
		}
		after = &c
	}
	if len(got) != len(seeded) {
		t.Fatalf("phân trang: %d dòng, muốn %d", len(got), len(seeded))
	}
	seen := map[uuid.UUID]bool{}
	for i, id := range got {
		if seen[id] {
			t.Errorf("dòng %s lặp", id)
		}
		seen[id] = true
		if i == 0 && id != seeded[0] || i == 4 && id != seeded[4] {
			t.Errorf("thứ tự: vị trí %d là %s", i, id)
		}
	}
}

// TestAgedReservations_InvalidInput: ngưỡng ≤ 0, trạng thái lạ, con trỏ hỏng → ErrInvalidInput.
func TestAgedReservations_InvalidInput(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	for _, c := range []struct {
		name          string
		held, transit int
		p             inventory.AgedPage
	}{
		{"held 0", 0, 180, inventory.AgedPage{}},
		{"transit âm", 20, -1, inventory.AgedPage{}},
		{"trạng thái lạ", 20, 180, inventory.AgedPage{State: "consumed"}},
	} {
		err := e.app(func(tx pgx.Tx) error {
			_, _, err := inventory.AgedReservations(t.Context(), tx, agedNow(), c.held, c.transit, nil, c.p)
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("%s: %v, muốn ErrInvalidInput", c.name, err)
		}
	}
	for _, s := range []string{"", "!!", "AAAA"} {
		if _, err := inventory.ParseAgedCursor(s); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("con trỏ %q: %v", s, err)
		}
	}
}
