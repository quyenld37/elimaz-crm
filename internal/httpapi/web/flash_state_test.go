package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion/flashtest"
)

// Trạng thái flash sale và hoàn tất đơn nháp cho website (Task 204, M5-21): máy chủ thật, PostgreSQL thật, Valkey gate
// thật. Ca [FS02], [FS15]; vòng dựng lại ở internal/wiring/flashstate.

const flashCurrentPath = "/api/v1/web/flash-sales/current"

type flashItemState struct {
	ID               uuid.UUID `json:"id"`
	VariantID        uuid.UUID `json:"variantId"`
	ProductID        uuid.UUID `json:"productId"`
	SalePrice        int64     `json:"salePrice"`
	RegularPrice     int64     `json:"regularPrice"`
	Quantity         int       `json:"quantity"`
	PerCustomerLimit int       `json:"perCustomerLimit"`
	HoldMinutes      int       `json:"holdMinutes"`
	Remaining        int       `json:"remaining"`
	SoldOut          bool      `json:"soldOut"`
}

type flashSaleState struct {
	ID       uuid.UUID        `json:"id"`
	Name     string           `json:"name"`
	StartsAt time.Time        `json:"startsAt"`
	EndsAt   time.Time        `json:"endsAt"`
	Status   string           `json:"status"`
	Items    []flashItemState `json:"items"`
}

type flashCurrent struct {
	ServerTime time.Time        `json:"serverTime"`
	Sales      []flashSaleState `json:"sales"`
}

// current đọc webFlashCurrent bằng khóa web.catalog.read (f.env.key; f.key của orderEnv là web.orders.create).
func (f *flashEnv) current() flashCurrent {
	f.t.Helper()
	rec := f.get(f.env.key, flashCurrentPath)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("webFlashCurrent: %d %s", rec.Code, rec.Body.String())
	}
	var out flashCurrent
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func remainingOf(c flashCurrent, item uuid.UUID) (int, bool) {
	for _, s := range c.Sales {
		for _, it := range s.Items {
			if it.ID == item {
				return it.Remaining, it.SoldOut
			}
		}
	}
	return -1, false
}

// laterSale lên lịch một đợt một món (biến thể mới, 4 suất) mở sau giờ mở của f một khoảng after; trả mã đợt.
func (f *flashEnv) laterSale(after time.Duration) uuid.UUID {
	f.t.Helper()
	v := f.product(fmt.Sprintf("FL%d", time.Now().UnixNano()%1_000_000), "[]").Variants[0].ID
	f.stock(v, 10)
	var id uuid.UUID
	f.app(func(tx pgx.Tx) error {
		id = flashtest.ScheduleItems(f.t, tx, flashtest.Item{
			Variant: v, Price: 150000, Slots: 4, Limit: 2, HoldMinutes: 10, StartsAt: f.start.Add(after),
			Name: "Đợt tối", TenantID: f.tenant, UserID: f.owner.UserID,
		}).ID
		return nil
	})
	return id
}

// checkInitial: hai đợt (đang mở hai món 5 suất, chưa tới giờ later một món 4 suất), serverTime = giờ nghiệp vụ.
func (f *flashEnv) checkInitial(c flashCurrent, later uuid.UUID) {
	f.t.Helper()
	if !c.ServerTime.Equal(f.now()) || len(c.Sales) != 2 {
		f.t.Fatalf("serverTime %v (muốn %v), %d đợt", c.ServerTime, f.now(), len(c.Sales))
	}
	open, sched := c.Sales[0], c.Sales[1]
	if open.ID != f.sale || open.Status != "open" || open.Name != "Đợt thử" || !open.StartsAt.Equal(f.start) ||
		!open.EndsAt.Equal(f.start.Add(2*time.Hour)) || len(open.Items) != 2 {
		f.t.Fatalf("đợt đang mở: %+v", open)
	}
	if sched.ID != later || sched.Status != "scheduled" || !sched.StartsAt.Equal(f.start.Add(3*time.Hour)) ||
		len(sched.Items) != 1 || sched.Items[0].Remaining != 4 || sched.Items[0].PerCustomerLimit != 2 {
		f.t.Fatalf("đợt chưa tới giờ: %+v", sched)
	}
	for _, it := range open.Items {
		f.checkItem(it)
	}
}

// checkItem: món của đợt đang mở đúng mẫu của biến thể, giá, số suất, giới hạn, hạn giữ, còn đủ 5.
func (f *flashEnv) checkItem(it flashItemState) {
	f.t.Helper()
	var product uuid.UUID
	f.app(func(tx pgx.Tx) error {
		return tx.QueryRow(f.t.Context(), `SELECT product_id FROM catalog.variants_v WHERE variant_id = $1`,
			it.VariantID).Scan(&product)
	})
	if it.ProductID != product || it.SalePrice != 199000 || it.RegularPrice != 350000 || it.Quantity != 5 ||
		it.PerCustomerLimit != 1 || it.HoldMinutes != 15 || it.Remaining != 5 || it.SoldOut {
		f.t.Fatalf("món: %+v (mẫu %s)", it, product)
	}
}

// [FS02] Một đợt trong giờ mở hai món và một đợt chưa tới giờ: đủ trường, trạng thái hiệu lực, serverTime để đếm
// ngược; remaining giảm ngay sau 3 lượt mua (cổng chưa biết → đếm ở PostgreSQL); cổng có số → dùng số của cổng; quá
// endsAt → đợt không còn; khóa không có web.catalog.read → 403.
func TestWebFlashCurrent_Shape(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 5, limit: 1, phoneMax: 1000, extra: 1})
	later := f.laterSale(3 * time.Hour)
	f.checkInitial(f.current(), later)
	for i := range 3 {
		if r := f.send(f.buyer(i), 1); r.result != "bought" {
			t.Fatalf("mua %d: %+v", i, r)
		}
	}
	if n, s := remainingOf(f.current(), f.items[0]); n != 2 || s {
		t.Fatalf("sau 3 lượt mua: còn %d, hết %v", n, s)
	}
	if err := f.gate.SetState(t.Context(), f.tenant, f.items[1], 0, true); err != nil {
		t.Fatal(err)
	}
	if n, s := remainingOf(f.current(), f.items[1]); n != 0 || !s {
		t.Fatalf("món có số của cổng: còn %d, hết %v", n, s)
	}
	f.shift.Store(int64(2 * time.Hour)) // quá endsAt của đợt đầu, trước giờ mở đợt sau
	c := f.current()
	if len(c.Sales) != 1 || c.Sales[0].ID != later || c.Sales[0].Status != "scheduled" {
		t.Fatalf("sau endsAt của đợt đầu: %+v", c.Sales)
	}
	f.shift.Store(int64(3 * time.Hour))
	if c = f.current(); len(c.Sales) != 1 || c.Sales[0].Status != "open" {
		t.Fatalf("tới giờ mở đợt sau: %+v", c.Sales)
	}
	if rec := f.get(f.buyKey, flashCurrentPath); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa web.flash.buy đọc trạng thái: %d", rec.Code)
	}
}

// Không có đợt nào → 200, danh sách rỗng (không null).
func TestWebFlashCurrent_Empty(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 1, limit: 1, phoneMax: 10})
	f.shift.Store(int64(3 * time.Hour)) // đợt duy nhất đã quá endsAt
	rec := f.get(f.env.key, flashCurrentPath)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"sales":[]`) {
		t.Fatalf("không có đợt: %d %s", rec.Code, rec.Body.String())
	}
}

// complete gửi webFlashComplete cho mã ngoài ref với SĐT ph, Idempotency-Key key.
func (f *flashEnv) complete(ref, ph, key string) flashReply {
	f.t.Helper()
	body := fmt.Sprintf(`{"phone":%q,"shipAddress":{"name":"Chị Mai","phone":%q,"line":"12 Lê Lợi",`+
		`"provinceCode":"T01","wardCode":"T01001"},"shippingFee":30000,"note":"Giao giờ hành chính"}`, ph, ph)
	req := httptest.NewRequestWithContext(f.t.Context(), http.MethodPost,
		"/api/v1/web/flash-orders/"+ref+"/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := f.orderEnv.send(req, f.buyKey, "Idempotency-Key", key)
	r := parseReply(rec.Code, rec.Header(), rec.Body.Bytes())
	if rec.Code == http.StatusOK {
		var b struct {
			Code       string `json:"code"`
			Status     string `json:"status"`
			GrandTotal int64  `json:"grandTotal"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &b)
		r.code, r.result = b.Code, fmt.Sprintf("%s %d", b.Status, b.GrandTotal)
	}
	return r
}

func (f *flashEnv) orderOf(ref string) (id uuid.UUID, status, reason string) {
	f.t.Helper()
	f.app(func(tx pgx.Tx) error {
		return tx.QueryRow(f.t.Context(), `SELECT id, status, coalesce(cancel_reason, '') FROM ord.orders_v
			WHERE external_ref = $1`, ref).Scan(&id, &status, &reason)
	})
	return id, status, reason
}

// [FS15] Từ chối không lưu theo khóa: SĐT khác → 404 not_found (không lộ đơn có tồn tại); SĐT sai dạng → 400; mã ngoài
// không có → 404.
func TestWebFlashComplete_Refusals(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 3, limit: 1, phoneMax: 1000})
	b := f.buyer(1)
	if r := f.send(b, 1); r.result != "bought" {
		t.Fatalf("mua: %+v", r)
	}
	if r := f.complete(b.ref, phone(999), "C-other"); r.status != http.StatusNotFound || r.errCode != "not_found" {
		t.Fatalf("SĐT khác: %+v", r)
	}
	if n := f.storedKeys("C-other"); n != 0 {
		t.Fatalf("404 được lưu theo khóa (%d dòng)", n)
	}
	if r := f.complete(b.ref, "12", "C-bad"); r.status != http.StatusBadRequest {
		t.Fatalf("SĐT sai dạng: %+v", r)
	}
	if r := f.complete("FS-KHONG-CO", b.phone, "C-none"); r.status != http.StatusNotFound {
		t.Fatalf("mã ngoài không có: %+v", r)
	}
	if _, status, _ := f.orderOf(b.ref); status != "draft" {
		t.Fatalf("đơn sau các lần từ chối: %s", status)
	}
}

// [FS15] Hoàn tất trong hạn → 200 new, tổng = giá flash + phí ship, đơn vị sold, một dòng audit order.create; cùng khóa
// → bản đã lưu; khóa khác → cùng kết quả, không audit lần hai.
func TestWebFlashComplete(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 3, limit: 1, phoneMax: 1000})
	b := f.buyer(1)
	won := f.send(b, 1)
	if won.result != "bought" {
		t.Fatalf("mua: %+v", won)
	}
	done := f.complete(b.ref, b.phone, "C-1")
	if done.status != http.StatusOK || done.code != won.code || done.result != "new 229000" {
		t.Fatalf("hoàn tất: %+v", done)
	}
	if r := f.complete(b.ref, b.phone, "C-1"); r.status != http.StatusOK || r.replayed != "true" || r.code != won.code {
		t.Fatalf("gửi lại cùng khóa: %+v", r)
	}
	if r := f.complete(b.ref, b.phone, "C-2"); r.status != http.StatusOK || r.code != won.code || r.result != "new 229000" {
		t.Fatalf("khóa khác sau khi đã hoàn tất: %+v", r)
	}
	id, status, _ := f.orderOf(b.ref)
	if status != "new" {
		t.Fatalf("đơn %s", status)
	}
	if n := f.staff.CountIn(t, f.tenant, `SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1
		AND action = 'order.create' AND actor_type = 'service' AND (after->>'grandTotal')::bigint = 229000`,
		id.String()); n != 1 {
		t.Fatalf("mong 1 dòng audit order.create, có %d", n)
	}
	if n := f.staff.CountIn(t, f.tenant, `SELECT count(*) FROM inventory.flash_sale_units u
		JOIN ord.order_items i ON i.id = u.order_item_id WHERE i.order_id = $1 AND u.status = 'sold'`, id); n != 1 {
		t.Fatalf("đơn vị sold của đơn: %d", n)
	}
}

// [FS15] Hoàn tất sau hạn giữ → 422 hold_expired, lưu theo khóa (lần hai Idempotent-Replayed); đơn cancelled
// draft_expired, đơn vị trả về đợt (đợt còn mở).
func TestWebFlashComplete_HoldExpired(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 3, limit: 1, phoneMax: 1000})
	late := f.buyer(2)
	if r := f.send(late, 1); r.result != "bought" {
		t.Fatalf("mua: %+v", r)
	}
	f.shift.Store(int64(16 * time.Minute)) // quá hạn giữ 15 phút
	for i, replayed := range []string{"", "true"} {
		r := f.complete(late.ref, late.phone, "C-late")
		if r.status != http.StatusUnprocessableEntity || r.errCode != "hold_expired" || r.replayed != replayed {
			t.Fatalf("hoàn tất sau hạn, lần %d: %+v", i+1, r)
		}
	}
	if _, status, reason := f.orderOf(late.ref); status != "cancelled" || reason != "draft_expired" {
		t.Fatalf("đơn quá hạn: %s %s", status, reason)
	}
	if n := f.staff.CountIn(t, f.tenant, `SELECT count(*) FROM inventory.flash_sale_units
		WHERE flash_sale_item_id = $1 AND status = 'available'`, f.item); n != 3 {
		t.Fatalf("đơn vị còn trống sau khi trả suất: %d, muốn 3", n)
	}
}

// Mã ngoài của lượt mua flash đã dùng cho đơn của món khác → 422 idempotency_key_reused, khóa chống trùng trả lại
// ngay (không lưu; QĐ-204-5).
func TestWebFlashBuy_ExternalRefTakenReleasesKey(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 3, limit: 1, phoneMax: 1000, extra: 1})
	first := f.buyer(1)
	if r := f.send(first, 1); r.result != "bought" {
		t.Fatalf("mua: %+v", r)
	}
	f.item = f.items[1] // lượt sau mua món thứ hai với cùng mã ngoài
	other := f.buyer(2)
	other.ref = first.ref
	r := f.sendBody("K-reuse", other.body(), 1)
	if r.status != http.StatusUnprocessableEntity || r.errCode != "idempotency_key_reused" {
		t.Fatalf("mã ngoài đã dùng: %+v", r)
	}
	if n := f.storedKeys("K-reuse"); n != 0 {
		t.Fatalf("khóa chống trùng còn giữ (%d dòng)", n)
	}
}
