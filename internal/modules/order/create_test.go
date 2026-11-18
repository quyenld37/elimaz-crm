package order_test

import (
	"errors"
	"regexp"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/logging"
)

var codeRe = regexp.MustCompile(`^DH[0-9]{7,}$`)

func TestCreateManual_Totals(t *testing.T) {
	e := newOrderEnv(t)
	v := e.variant(250000, 5)
	o := e.mustCreate(e.input(30000, item(v, 2)))

	if o.Subtotal != 500000 || o.GrandTotal != 530000 || o.CODAmount != 530000 || o.ShippingFee != 30000 ||
		o.DiscountTotal != 0 || o.Status != order.StatusConfirmed {
		t.Errorf("đơn tạo sai: %+v", o)
	}
	if !codeRe.MatchString(o.Code) {
		t.Errorf("mã đơn %q không khớp %s", o.Code, codeRe)
	}
	if len(o.Items) != 1 || o.Items[0].Position != 1 || o.Items[0].Qty != 2 || o.Items[0].UnitPrice != 250000 ||
		o.Items[0].NetAmount != 500000 || o.Items[0].SKU == "" || o.Items[0].Name != "Áo thử - Đỏ / S1" {
		t.Errorf("dòng đơn sai: %+v", o.Items)
	}
	if b := e.balance(v); b.Held != 2 || b.OnHand != 5 || b.Available != 3 {
		t.Errorf("số dư sau giữ: %+v", b)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers_v WHERE id = $1 AND root_id = id`, o.CustomerID); n != 1 {
		t.Errorf("khách mới phải có root_id = id, có %d dòng", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status IS NULL
		AND to_status = 'confirmed' AND actor_type = 'staff' AND actor_id = $2`, o.ID, e.actor.UserID); n != 1 {
		t.Errorf("lịch sử tạo đơn: %d dòng", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_type = 'order' AND aggregate_id = $1
		AND ((topic = 'order.created' AND seq = 1) OR (topic = 'order.confirmed' AND seq = 2))`, o.ID.String()); n != 2 {
		t.Errorf("sự kiện order.created, order.confirmed: %d", n)
	}
	var got order.Order
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		got, err = order.Get(t.Context(), tx, e.seller, o.ID)
		return err //nolint:wrapcheck // test
	}); err != nil || got.Code != o.Code || len(got.Items) != 1 || got.GrandTotal != 530000 {
		t.Errorf("Get: %+v, %v", got, err)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := order.Get(t.Context(), tx, e.partner, o.ID)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("Get bằng người bán khác: %v, mong ErrNotFound", err)
	}
}

func TestCreateManual_CodesUnique(t *testing.T) {
	e := newOrderEnv(t)
	v := e.variant(1000, 100)
	codes := make([]string, 100)
	errs := make([]error, 100)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() {
			o, err := e.create(e.input(0, item(v, 1)))
			codes[i], errs[i] = o.Code, err
		})
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, c := range codes {
		if errs[i] != nil {
			t.Fatalf("đơn %d: %v", i, errs[i])
		}
		if seen[c] || !codeRe.MatchString(c) {
			t.Fatalf("mã %q trùng hoặc sai dạng", c)
		}
		seen[c] = true
	}
}

func TestCreateManual_SecondLineShort(t *testing.T) {
	e := newOrderEnv(t)
	a, b := e.variant(1000, 5), e.variant(1000, 1)
	_, err := e.create(e.input(0, item(a, 2), item(b, 2)))
	var oos *order.OutOfStockError
	if !errors.As(err, &oos) || !errors.Is(err, order.ErrOutOfStock) || oos.VariantID != b || oos.Available != 1 {
		t.Fatalf("lỗi = %v, mong OutOfStockError của dòng 2 (có thể bán 1)", err)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		t.Errorf("còn %d đơn", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_items`) + e.count(`SELECT count(*) FROM inventory.stock_reservations`); n != 0 {
		t.Errorf("còn %d dòng đơn hay giữ hàng", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 0 {
		t.Errorf("còn %d khách (transaction phải lùi cả khách)", n)
	}
	if ba := e.balance(a); ba.Held != 0 || ba.OnHand != 5 {
		t.Errorf("số dư dòng 1 đổi: %+v", ba)
	}
}

// TestOrderItems_SellerFK (KH14): biến thể của người bán P không vào được đơn của ELIMAZ — ở Go và ở database.
func TestOrderItems_SellerFK(t *testing.T) {
	e := newOrderEnv(t)
	own := e.variant(1000, 5)
	p := e.variantOf(e.partner, 1000, 0)
	if _, err := e.create(e.input(0, item(own, 1), item(p, 1))); !errors.Is(err, order.ErrMixedSellers) {
		t.Fatalf("lỗi = %v, mong ErrMixedSellers", err)
	}
	if _, err := e.create(e.input(0, item(uuid.New(), 1))); !errors.Is(err, order.ErrMixedSellers) {
		t.Errorf("biến thể không có: %v, mong ErrMixedSellers", err)
	}
	o := e.mustCreate(e.input(0, item(own, 1)))
	err := e.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO ord.order_items (tenant_id, seller_id, order_id, position, variant_id,
			sku_snapshot, name_snapshot, quantity, unit_price, net_amount)
			VALUES ($1, $2, $3, 2, $4, 'X', 'X', 1, 1000, 1000)`, e.tenant, e.seller, o.ID, p)
		return err //nolint:wrapcheck // test
	})
	if !isPgCode(err, "23503") {
		t.Errorf("chèn dòng đơn trỏ biến thể người bán khác: %v, mong lỗi khóa ngoại", err)
	}
}

// TestCreateOrder_DiscontinuedVariant (KH12): biến thể ngừng bán không lên đơn mới; đơn cũ vẫn đọc được, tên chụp
// lúc đặt giữ nguyên.
func TestCreateOrder_DiscontinuedVariant(t *testing.T) {
	e := newOrderEnv(t)
	v := e.variant(1000, 5)
	old := e.mustCreate(e.input(0, item(v, 1)))
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET status = 'discontinued' WHERE id = $1`, v); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `UPDATE catalog.products SET name = 'Tên mới' WHERE id = $1`, e.shop.Product(e.seller))
		return err //nolint:wrapcheck // test
	})
	if _, err := e.create(e.input(0, item(v, 1))); !errors.Is(err, order.ErrNotForSale) {
		t.Fatalf("lỗi = %v, mong ErrNotForSale", err)
	}
	var got order.Order
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		got, err = order.Get(t.Context(), tx, e.seller, old.ID)
		return err //nolint:wrapcheck // test
	}); err != nil || got.Items[0].Name != "Áo thử - Đỏ / S1" {
		t.Errorf("đơn cũ: %+v, %v", got, err)
	}
}

func TestCreateManual_TotalOverflow(t *testing.T) {
	e := newOrderEnv(t)
	v := e.variant(catalog.MaxMoney, 5)
	cases := map[string]order.CreateManualInput{
		"giá tối đa × 2":        e.input(0, item(v, 2)),
		"giá tối đa + phí ship": e.input(1, item(v, 1)),
		"hai dòng cộng tràn":    e.input(0, item(v, 1), item(v, 1)),
	}
	for name, in := range cases {
		if _, err := e.create(in); !errors.Is(err, order.ErrTotalOverflow) {
			t.Errorf("%s: lỗi = %v, mong ErrTotalOverflow", name, err)
		}
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		t.Errorf("còn %d đơn", n)
	}
	if b := e.balance(v); b.Held != 0 {
		t.Errorf("held = %d sau đơn tràn", b.Held)
	}
}

func TestCreateManual_InvalidInput(t *testing.T) {
	e := newOrderEnv(t)
	v := e.variant(1000, 5)
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'a'
	}
	bad := map[string]func(in *order.CreateManualInput){
		"không dòng":          func(in *order.CreateManualInput) { in.Items = nil },
		"số lượng 0":          func(in *order.CreateManualInput) { in.Items[0].Qty = 0 },
		"phí âm":              func(in *order.CreateManualInput) { in.ShippingFee = -1 },
		"thiếu khóa":          func(in *order.CreateManualInput) { in.IdempotencyKey = uuid.Nil },
		"SĐT sai":             func(in *order.CreateManualInput) { in.ShipAddress.Phone = "12ab" },
		"tên người nhận rỗng": func(in *order.CreateManualInput) { in.ShipAddress.Name = " " },
		"địa chỉ quá dài":     func(in *order.CreateManualInput) { in.ShipAddress.Line = logging.Address(long) },
	}
	for name, mutate := range bad {
		in := e.input(0, item(v, 1))
		mutate(&in)
		if _, err := e.create(in); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("%s: lỗi = %v, mong ErrInvalidInput", name, err)
		}
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := customer.Create(t.Context(), tx, "  ")
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, customer.ErrInvalidInput) {
		t.Errorf("tên khách rỗng: %v", err)
	}
}

func TestCreateManual_DuplicateBusinessKey(t *testing.T) {
	e := newOrderEnv(t)
	v := e.variant(1000, 5)
	in := e.input(0, item(v, 1))
	e.mustCreate(in)
	if _, err := e.create(in); !errors.Is(err, order.ErrDuplicateOrder) {
		t.Fatalf("cùng khóa nghiệp vụ: %v, mong ErrDuplicateOrder", err)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 1 {
		t.Errorf("%d đơn, mong 1", n)
	}
	if b := e.balance(v); b.Held != 1 {
		t.Errorf("held = %d, mong 1", b.Held)
	}
}

func isPgCode(err error, code string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == code
}

func TestOrderStatusHistory_AppendOnly(t *testing.T) {
	e := newOrderEnv(t)
	v := e.variant(1000, 5)
	e.mustCreate(e.input(0, item(v, 1)))
	for _, q := range []string{`UPDATE ord.order_status_history SET reason = 'x'`, `DELETE FROM ord.order_status_history`} {
		err := e.app(func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q)
			return err //nolint:wrapcheck // test
		})
		if !isPgCode(err, "42501") {
			t.Errorf("%s: %v, mong bị chặn", q, err)
		}
	}
}
