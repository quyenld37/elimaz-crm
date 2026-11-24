package order_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// Số SĐT dùng trong test M2-16 (dạng nhập và dạng chuẩn hóa).
const (
	shipPhone     = "0901234567"
	shipPhoneNorm = "+84901234567"
	momPhone      = "0912000111"
	momPhoneNorm  = "+84912000111"
)

// newM2Env là orderEnv có danh mục hành chính mẫu (T01 có xã T01001) và cài đặt, nguồn đơn mặc định của tenant.
func newM2Env(t *testing.T) *orderEnv {
	t.Helper()
	e := newOrderEnv(t)
	e.staff.SeedAdminUnits(t)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return order.EnsureDefaults(t.Context(), tx, e.tenant)
	})
	return e
}

// fullAddress là địa chỉ đủ tỉnh, xã.
func fullAddress() *order.ShipAddress {
	return &order.ShipAddress{
		Name: "Chị Lan", Phone: shipPhone, Line: "12 Lê Lợi", ProvinceCode: "T01", WardCode: "T01001",
	}
}

func ln(v uuid.UUID, q int32, discount int64) order.LineInput {
	return order.LineInput{VariantID: v, Quantity: q, LineDiscount: discount}
}

// m2Input: đơn confirmed cho khách mới "Chị Lan" (SĐT người nhận không phải danh tính), địa chỉ đủ.
func (e *orderEnv) m2Input(fee int64, lines ...order.LineInput) order.CreateInput {
	return order.CreateInput{
		IdempotencyKey: uuid.New(), Channel: order.ChannelManual, Intent: order.IntentConfirmed,
		Customer: customer.OrderCustomerInput{FullName: "Chị Lan"}, ShipAddress: fullAddress(), Lines: lines,
		ShippingFee: fee,
	}
}

func (e *orderEnv) createM2(in order.CreateInput) (order.CreateResult, error) {
	var res order.CreateResult
	err := e.app(func(tx pgx.Tx) error {
		var err error
		res, err = e.writer().Create(e.t.Context(), tx, in, e.actor, e.now())
		return err //nolint:wrapcheck // test
	})
	return res, err
}

func (e *orderEnv) mustCreateM2(in order.CreateInput) order.CreateResult {
	e.t.Helper()
	res, err := e.createM2(in)
	if err != nil {
		e.t.Fatalf("Create: %v", err)
	}
	return res
}

// now là giờ thử cố định của test (UTC, làm tròn micro giây như timestamptz).
func (e *orderEnv) now() time.Time {
	return time.Date(2026, 11, 19, 8, 30, 0, 0, time.UTC)
}

// newCustomer tạo khách name có SĐT phone (danh tính) bằng customer.Writer.
func (e *orderEnv) newCustomer(name, phone string) customer.Customer {
	e.t.Helper()
	var c customer.Customer
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		w := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
		c, err = w.CreateCustomer(e.t.Context(), tx, customer.CreateInput{FullName: name, Phone: phone},
			customer.Actor{ID: e.actor.UserID}, e.now())
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo khách: %v", err)
	}
	return c
}

// DH01: đơn confirmed hai dòng, giảm dòng, phí ship → tổng, giữ hàng, lịch sử, sự kiện created (seq 1), confirmed
// (seq 2).
func TestCreate_ConfirmedTotals(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v1, v2 := e.variant(250000, 5), e.variant(180000, 1)
	res := e.mustCreateM2(e.m2Input(30000, ln(v1, 2, 20000), ln(v2, 1, 0)))
	o := res.Order
	if res.Replayed || len(res.Warnings) != 0 {
		t.Errorf("Replayed %v, cảnh báo %v", res.Replayed, res.Warnings)
	}
	if o.Status != order.StatusConfirmed || o.Subtotal != 680000 || o.DiscountTotal != 20000 ||
		o.LineDiscountTotal != 20000 || o.GrandTotal != 690000 || o.CODAmount != 690000 || o.ShippingFee != 30000 ||
		o.Version != 2 || !o.AddressComplete || o.Channel != order.ChannelManual || !codeRe.MatchString(o.Code) ||
		o.ShipPhone != shipPhoneNorm || o.ConfirmedAt == nil || o.DraftExpiresAt != nil || !o.CreatedAt.Equal(e.now()) {
		t.Errorf("đơn sai: %+v", o)
	}
	if len(o.Items) != 2 || o.Items[0].VariantID != v1 || o.Items[0].Position != 1 || o.Items[0].LineDiscount != 20000 ||
		o.Items[0].NetAmount != 480000 || o.Items[1].VariantID != v2 || o.Items[1].NetAmount != 180000 {
		t.Errorf("dòng đơn sai: %+v", o.Items)
	}
	if b := e.balance(v1); b.Available != 3 || b.Held != 2 {
		t.Errorf("V1: %+v", b)
	}
	if b := e.balance(v2); b.Available != 0 || b.Held != 1 {
		t.Errorf("V2: %+v", b)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1`, o.ID); n != 1 {
		t.Errorf("%d dòng lịch sử, mong 1", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status IS NULL
		AND to_status = 'confirmed' AND actor_type = 'staff' AND actor_id = $2`, o.ID, e.actor.UserID); n != 1 {
		t.Errorf("dòng lịch sử tạo đơn sai")
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_type = 'order' AND aggregate_id = $1
		AND ((topic = 'order.created' AND seq = 1) OR (topic = 'order.confirmed' AND seq = 2))
		AND payload->>'customerId' = $2 AND payload->>'status' = 'confirmed'`, o.ID.String(), o.CustomerID.String()); n != 2 {
		t.Errorf("sự kiện order.created/confirmed: %d, mong 2", n)
	}
	got := e.get(o.ID)
	if got.GrandTotal != 690000 || got.ShipAddress.WardCode != "T01001" || got.Version != 2 || got.Code != o.Code {
		t.Errorf("Get: %+v", got)
	}
}

// DH04: kênh manual, nguồn "Nhập tay" → đơn ghi đúng nguồn, kênh; nguồn đã tắt → ErrTrafficSourceInactive.
func TestCreate_SourceAndChannel(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(1000, 10)
	var manual uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		manual, err = order.SourceByKey(t.Context(), tx, order.SourceKeyManual)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	in := e.m2Input(0, ln(v, 1, 0))
	in.TrafficSourceID, in.ExternalRef, in.CarrierNote = &manual, "FB-123", "Gọi trước khi giao"
	o := e.mustCreateM2(in).Order
	if o.TrafficSourceID == nil || *o.TrafficSourceID != manual || o.Channel != "manual" || o.ExternalRef == nil ||
		*o.ExternalRef != "FB-123" || o.CarrierNote != "Gọi trước khi giao" {
		t.Errorf("nguồn, kênh: %+v", o)
	}
	if got := e.get(o.ID); got.TrafficSourceID == nil || *got.TrafficSourceID != manual || got.Channel != "manual" {
		t.Errorf("đọc lại: %+v", got)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE ord.traffic_sources SET active = false WHERE id = $1`, manual)
		return err //nolint:wrapcheck // test
	})
	in = e.m2Input(0, ln(v, 1, 0))
	in.TrafficSourceID = &manual
	if _, err := e.createM2(in); !errors.Is(err, order.ErrTrafficSourceInactive) {
		t.Errorf("nguồn tắt: %v, mong ErrTrafficSourceInactive", err)
	}
	in.TrafficSourceID = new(uuid.New())
	if _, err := e.createM2(in); !errors.Is(err, order.ErrTrafficSourceInactive) {
		t.Errorf("nguồn không có: %v, mong ErrTrafficSourceInactive", err)
	}
}

// KH11/AT21: đơn chụp giá lúc tạo; đổi giá sau đó không đổi đơn cũ.
func TestCreate_PriceSnapshot(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(250000, 10)
	o1 := e.mustCreateM2(e.m2Input(0, ln(v, 1, 0))).Order
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET price = 270000 WHERE id = $1`, v)
		return err //nolint:wrapcheck // test
	})
	o2 := e.mustCreateM2(e.m2Input(0, ln(v, 1, 0))).Order
	if got := e.get(o1.ID); got.Items[0].UnitPrice != 250000 || got.Subtotal != 250000 {
		t.Errorf("O1 đổi giá: %+v", got.Items)
	}
	if o2.Items[0].UnitPrice != 270000 || o2.Subtotal != 270000 {
		t.Errorf("O2: %+v", o2.Items)
	}
}

// Khóa nghiệp vụ (Q-T57-4): cùng khóa, cùng nội dung → đơn cũ (Replayed), không giữ thêm; khác dòng →
// ErrDuplicateOrder.
func TestCreate_BusinessKeyReplay(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(250000, 5)
	in := e.m2Input(30000, ln(v, 2, 0))
	first := e.mustCreateM2(in)
	again := e.mustCreateM2(in)
	if !again.Replayed || again.Order.ID != first.Order.ID || again.Order.Code != first.Order.Code ||
		again.Order.GrandTotal != first.Order.GrandTotal || len(again.Order.Items) != 1 {
		t.Errorf("gửi lại: %+v", again)
	}
	other := in
	other.Lines = []order.LineInput{ln(v, 1, 0)}
	if _, err := e.createM2(other); !errors.Is(err, order.ErrDuplicateOrder) {
		t.Errorf("cùng khóa khác dòng: %v, mong ErrDuplicateOrder", err)
	}
	orders, customers := e.count(`SELECT count(*) FROM ord.orders`), e.count(`SELECT count(*) FROM customer.customers`)
	if held := e.balance(v).Held; orders != 1 || customers != 1 || held != 2 {
		t.Errorf("sau gửi lại: %d đơn, %d khách, held %d; mong 1, 1, 2 (gửi lại không ghi gì)", orders, customers, held)
	}
}

// Nguyên tử: dòng 2 thiếu hàng → không đơn, không giữ, không khách mới (kể cả SĐT mới gắn làm danh tính).
func TestCreate_AtomicOnOutOfStock(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v1, v2 := e.variant(250000, 5), e.variant(180000, 1)
	in := e.m2Input(0, ln(v1, 2, 0), ln(v2, 2, 0))
	in.Customer = customer.OrderCustomerInput{FullName: "Chị Mai", Phone: "0987000111", PhoneIsCustomers: true}
	_, err := e.createM2(in)
	var oos *order.OutOfStockError
	if !errors.As(err, &oos) || oos.VariantID != v2 || oos.Available != 1 {
		t.Fatalf("lỗi = %v, mong OutOfStockError V2 (có thể bán 1)", err)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM ord.orders`:                   0,
		`SELECT count(*) FROM customer.customers`:           0,
		`SELECT count(*) FROM customer.customer_identities`: 0,
		`SELECT count(*) FROM inventory.stock_reservations`: 0,
	} {
		if n := e.count(q); n != want {
			t.Errorf("%s = %d, mong %d", q, n, want)
		}
	}
	if b := e.balance(v1); b.Held != 0 || b.Available != 5 {
		t.Errorf("V1: %+v", b)
	}
}

// KH14: biến thể của hai người bán trong một đơn → ErrMixedSellers; đơn chỉ biến thể của người bán kia với SellerID
// của họ → tạo được.
func TestCreate_MixedSellers(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v1 := e.variant(250000, 5)
	w1 := e.variantOf(e.partner, 99000, 3)
	if _, err := e.createM2(e.m2Input(0, ln(v1, 1, 0), ln(w1, 1, 0))); !errors.Is(err, order.ErrMixedSellers) {
		t.Fatalf("lỗi = %v, mong ErrMixedSellers", err)
	}
	in := e.m2Input(0, ln(w1, 1, 0))
	in.SellerID = e.partner
	o := e.mustCreateM2(in).Order
	if o.SellerID != e.partner || o.Subtotal != 99000 {
		t.Errorf("đơn người bán kia: %+v", o)
	}
}

// Biến thể ngừng bán → ErrNotForSale; biến thể của tenant khác (hay không có) → ErrVariantNotFound.
func TestCreate_VariantErrors(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	gone := e.variant(1000, 5)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET status = 'discontinued' WHERE id = $1`, gone)
		return err //nolint:wrapcheck // test
	})
	if _, err := e.createM2(e.m2Input(0, ln(gone, 1, 0))); !errors.Is(err, order.ErrNotForSale) {
		t.Errorf("ngừng bán: %v, mong ErrNotForSale", err)
	}
	tenantB := e.staff.Tenant(t, "B")
	var foreign uuid.UUID
	e.staff.Exec(t, tenantB, func(tx pgx.Tx) error {
		shopB := invtest.NewShop(t, tx, tenantB)
		foreign = shopB.Variant(t, tx, shopB.Seller, 1000, 5)
		return nil
	})
	for name, v := range map[string]uuid.UUID{"tenant B": foreign, "không có": uuid.New()} {
		if _, err := e.createM2(e.m2Input(0, ln(v, 1, 0))); !errors.Is(err, order.ErrVariantNotFound) {
			t.Errorf("%s: %v, mong ErrVariantNotFound", name, err)
		}
	}
}

// Ý định: draft giữ hàng, hạn = now + 24 h; confirmed thiếu xã → ErrAddressIncomplete; new không địa chỉ → tạo được.
func TestCreate_Intents(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(1000, 10)
	in := e.m2Input(0, ln(v, 2, 0))
	in.Intent, in.ShipAddress = order.IntentDraft, nil
	d := e.mustCreateM2(in).Order
	if d.Status != order.StatusDraft || d.DraftExpiresAt == nil || !d.DraftExpiresAt.Equal(e.now().Add(24*time.Hour)) ||
		d.Version != 1 || d.ConfirmedAt != nil || d.AddressComplete {
		t.Errorf("đơn nháp: %+v", d)
	}
	if b := e.balance(v); b.Held != 2 {
		t.Errorf("đơn nháp phải giữ hàng: %+v", b)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_id = $1`, d.ID.String()); n != 1 {
		t.Errorf("đơn nháp: %d sự kiện, mong 1 (order.created)", n)
	}

	in = e.m2Input(0, ln(v, 1, 0))
	in.ShipAddress.WardCode = ""
	if _, err := e.createM2(in); !errors.Is(err, order.ErrAddressIncomplete) {
		t.Errorf("confirmed thiếu xã: %v, mong ErrAddressIncomplete", err)
	}
	in.Intent = order.IntentNew
	o := e.mustCreateM2(in).Order
	if o.Status != order.StatusNew || o.AddressComplete || o.ShipAddress.ProvinceCode != "T01" {
		t.Errorf("new thiếu xã: %+v", o)
	}
	in = e.m2Input(0, ln(v, 1, 0))
	in.Intent, in.ShipAddress = order.IntentNew, nil
	if o := e.mustCreateM2(in).Order; o.Status != order.StatusNew || o.AddressComplete || o.ShipPhone != "" {
		t.Errorf("new không địa chỉ: %+v", o)
	}
	in = e.m2Input(0, ln(v, 1, 0))
	in.ShipAddress.WardCode = "T99999"
	if _, err := e.createM2(in); !errors.Is(err, customer.ErrUnknownAdminUnit) {
		t.Errorf("mã xã lạ: %v, mong customer.ErrUnknownAdminUnit", err)
	}
	in = e.m2Input(0, ln(v, 1, 0))
	in.Intent, in.ShipAddress = "", nil
	if _, err := e.createM2(in); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("intent rỗng không địa chỉ: %v, mong ErrInvalidInput (luật M1)", err)
	}
}

// Tiền: giảm dòng vượt tiền dòng, giảm đơn vượt tạm tính, phí vượt giới hạn.
func TestCreate_MoneyErrors(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(250000, 10)
	if _, err := e.createM2(e.m2Input(0, ln(v, 2, 500001))); !errors.Is(err, order.ErrLineDiscountExceeds) {
		t.Errorf("giảm dòng: %v, mong ErrLineDiscountExceeds", err)
	}
	in := e.m2Input(0, ln(v, 1, 0))
	in.OrderDiscount = 250001
	if _, err := e.createM2(in); !errors.Is(err, order.ErrOrderDiscountExceeds) {
		t.Errorf("giảm đơn: %v, mong ErrOrderDiscountExceeds", err)
	}
	in = e.m2Input(1<<53, ln(v, 1, 0))
	if _, err := e.createM2(in); !errors.Is(err, order.ErrInvalidInput) && !errors.Is(err, order.ErrTotalOverflow) {
		t.Errorf("phí 2^53: %v", err)
	}
	in = e.m2Input(0, ln(v, 4, 1000))
	in.OrderDiscount = 3
	o := e.mustCreateM2(in).Order
	if o.OrderDiscount != 3 || o.DiscountTotal != 1003 || o.GrandTotal != 1000000-1003 ||
		o.Items[0].AllocatedOrderDiscount != 3 || o.Items[0].NetAmount != 1000000-1003 {
		t.Errorf("giảm đơn chia về dòng: %+v", o)
	}
	in = e.m2Input(0, ln(v, 1, 0))
	in.CouponCode = "GIAM10"
	if _, err := e.createM2(in); !errors.Is(err, promotion.ErrCouponRejected) {
		t.Errorf("mã giảm giá không có (từ Task M2-17 dùng mã thật): %v, mong ErrCouponRejected", err)
	}
}

// DH12: khách A đặt hộ mẹ — SĐT người nhận không phải của khách → đơn có ship_phone đó, A không có danh tính mới.
// Cảnh báo: SĐT đánh dấu là của khách nhưng thuộc khách B → tạo đơn cho A, cảnh báo phone_belongs_to_other_customer.
func TestCreate_PhoneOwnership(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(1000, 10)
	a := e.newCustomer("Anh A", "")
	in := e.m2Input(0, ln(v, 1, 0))
	in.Customer = customer.OrderCustomerInput{CustomerID: &a.ID}
	in.ShipAddress.Phone = momPhone
	o := e.mustCreateM2(in).Order
	if o.CustomerID != a.ID || o.ShipPhone != momPhoneNorm {
		t.Errorf("DH12: %+v", o)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1`, a.ID); n != 0 {
		t.Errorf("A có %d danh tính, mong 0", n)
	}

	b := e.newCustomer("Chị B", momPhone)
	in = e.m2Input(0, ln(v, 1, 0))
	in.Customer = customer.OrderCustomerInput{CustomerID: &a.ID, Phone: momPhone, PhoneIsCustomers: true}
	res := e.mustCreateM2(in)
	if res.Order.CustomerID != a.ID || len(res.Warnings) != 1 ||
		res.Warnings[0].Code != order.WarningPhoneBelongsToOtherCustomer || res.Warnings[0].RefID != b.RootID {
		t.Errorf("cảnh báo: %+v", res)
	}
}

// R2: Create và CreateManual dùng chung một chỗ sinh mã: 100 đơn xen kẽ hai đường → 100 mã DH… khác nhau.
func TestCreate_CodesSharedWithCreateManual(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(1000, 200)
	type result struct {
		code string
		err  error
	}
	results := make(chan result, 100)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			if i%2 == 0 {
				o, err := e.create(e.input(0, item(v, 1)))
				results <- result{o.Code, err}
				return
			}
			r, err := e.createM2(e.m2Input(0, ln(v, 1, 0)))
			results <- result{r.Order.Code, err}
		})
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for r := range results {
		if r.err != nil || !codeRe.MatchString(r.code) || seen[r.code] {
			t.Fatalf("mã %q (lỗi %v): trùng, sai dạng hoặc lỗi", r.code, r.err)
		}
		seen[r.code] = true
	}
	if len(seen) != 100 {
		t.Fatalf("%d mã, mong 100", len(seen))
	}
}

// PQ02: sau các lần tạo đơn, audit, việc nền, sự kiện không chứa SĐT, tên khách, địa chỉ.
func TestCreate_NoPersonalDataInEventsAndJobs(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(1000, 10)
	in := e.m2Input(0, ln(v, 1, 0))
	in.Customer = customer.OrderCustomerInput{FullName: "Chị Lan", Phone: shipPhone, PhoneIsCustomers: true}
	e.mustCreateM2(in)
	e.mustCreate(e.input(0, item(v, 1)))
	e.assertNoPII(nil, "0901234567", "901234567", "Chị Lan", "Lê Lợi")
}

// assertNoPII kiểm sự kiện, việc, audit (và các câu extra, mỗi câu trả một chuỗi) không chứa chuỗi nào trong pii.
func (e *orderEnv) assertNoPII(extra []string, pii ...string) {
	e.t.Helper()
	qs := append([]string{
		`SELECT coalesce(string_agg(payload::text, ' '), '') FROM platform.domain_events`,
		`SELECT coalesce(string_agg(payload::text, ' '), '') FROM platform.jobs`,
		`SELECT coalesce(string_agg(coalesce(before::text, '') || coalesce(after::text, ''), ' '), '') FROM identity.audit_logs`,
	}, extra...)
	for _, q := range qs {
		var s string
		e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error { return tx.QueryRow(e.t.Context(), q).Scan(&s) })
		for _, x := range pii {
			if strings.Contains(s, x) {
				e.t.Errorf("%s chứa %q", q, x)
			}
		}
	}
}
