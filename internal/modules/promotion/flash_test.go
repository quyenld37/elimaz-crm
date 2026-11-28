package promotion_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/testkit"
)

// Đợt flash sale soạn nháp (kế hoạch M5, Task 199; ca FS01 phần soạn).

func flashStart() time.Time { return time.Date(2026, 12, 12, 12, 0, 0, 0, time.UTC) }

func flashInput(name string, length time.Duration) promotion.FlashSaleInput {
	return promotion.FlashSaleInput{Name: name, StartsAt: flashStart(), EndsAt: flashStart().Add(length)}
}

func flashItem(variant uuid.UUID, price int64, qty int) promotion.FlashItemInput {
	return promotion.FlashItemInput{VariantID: variant, SalePrice: price, Quantity: qty, PerCustomerLimit: 1, HoldMinutes: 10}
}

// variant tạo biến thể giá price của người bán seller (chủ bảng).
func (e *env) variant(seller uuid.UUID, price int64) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		id = e.shop.Variant(e.t, tx, seller, price, 0)
		return nil
	})
	return id
}

func (e *env) createFlash(in promotion.FlashSaleInput) (promotion.FlashSale, error) {
	var s promotion.FlashSale
	err := e.app(func(tx pgx.Tx) (err error) {
		s, err = promotion.CreateFlashSale(e.t.Context(), tx, e.actor(), in)
		return err //nolint:wrapcheck // test
	})
	return s, err
}

func (e *env) mustFlash(name string) promotion.FlashSale {
	e.t.Helper()
	s, err := e.createFlash(flashInput(name, 2*time.Hour))
	if err != nil {
		e.t.Fatalf("tạo đợt: %v", err)
	}
	return s
}

func (e *env) addFlashItem(sale uuid.UUID, in promotion.FlashItemInput) (promotion.FlashItem, error) {
	var it promotion.FlashItem
	err := e.app(func(tx pgx.Tx) (err error) {
		it, err = promotion.AddFlashItem(e.t.Context(), tx, sale, in)
		return err //nolint:wrapcheck // test
	})
	return it, err
}

func (e *env) getFlash(id uuid.UUID) promotion.FlashSale {
	e.t.Helper()
	var s promotion.FlashSale
	if err := e.app(func(tx pgx.Tx) (err error) {
		s, err = promotion.GetFlashSale(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("đọc đợt: %v", err)
	}
	return s
}

// Tạo đợt, thêm món giá 199.000 (giá thường 350.000), 500 suất → đọc lại đúng (kho mặc định, người bán của biến thể,
// tên, giá thường); ends_at trước starts_at, bằng starts_at, đợt dài 8 ngày → ErrFlashWindow; đúng 7 ngày được; tên
// rỗng → ErrInvalidInput.
func TestFlashSale_CreateAddRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	v := e.variant(e.shop.Seller, 350000)
	s, err := e.createFlash(flashInput("  Sale 12.12  ", 2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Sale 12.12" || s.Status != promotion.FlashDraft || s.Version != 1 || s.CreatedBy != e.user {
		t.Fatalf("đợt mới: %+v", s)
	}
	it, err := e.addFlashItem(s.ID, promotion.FlashItemInput{
		VariantID: v, SalePrice: 199000, Quantity: 500, PerCustomerLimit: 2, HoldMinutes: 15,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := e.getFlash(s.ID)
	if got.ItemCount != 1 || len(got.Items) != 1 {
		t.Fatalf("món: %+v", got.Items)
	}
	r := got.Items[0]
	if r.ID != it.ID || r.VariantID != v || r.SellerID != e.shop.Seller || r.WarehouseID != e.shop.Warehouse ||
		r.SalePrice != 199000 || r.Quantity != 500 || r.PerCustomerLimit != 2 || r.HoldMinutes != 15 || r.Version != 1 ||
		r.ListPrice != 350000 || r.Name != it.Name || r.SKU != it.SKU || r.SKU == "" {
		t.Fatalf("đọc lại món: %+v (thêm: %+v)", r, it)
	}
	for name, in := range map[string]promotion.FlashSaleInput{
		"kết thúc trước": {Name: "x", StartsAt: flashStart(), EndsAt: flashStart().Add(-time.Minute)},
		"kết thúc bằng":  {Name: "x", StartsAt: flashStart(), EndsAt: flashStart()},
		"dài 8 ngày":     flashInput("x", 8*24*time.Hour),
		"7 ngày 1 giây":  flashInput("x", 7*24*time.Hour+time.Second),
	} {
		if _, err := e.createFlash(in); !errors.Is(err, promotion.ErrFlashWindow) {
			t.Errorf("%s: %v, mong ErrFlashWindow", name, err)
		}
	}
	if _, err := e.createFlash(flashInput("Đúng 7 ngày", 7*24*time.Hour)); err != nil {
		t.Errorf("đúng 7 ngày: %v", err)
	}
	if _, err := e.createFlash(flashInput("   ", time.Hour)); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Errorf("tên rỗng: %v", err)
	}
}

// Giá flash bằng, cao hơn giá thường → ErrNotADiscount; cùng biến thể hai lần → ErrFlashItemExists; biến thể ngừng
// bán, không có → ErrVariantNotForSale; số ngoài giới hạn → ErrInvalidInput; đợt không có → ErrNotFound; biến thể của
// người bán đối tác được thêm, seller_id là của đối tác.
func TestFlashItem_Rejections(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	v := e.variant(e.shop.Seller, 350000)
	s := e.mustFlash("Đợt thử")
	for _, price := range []int64{350000, 360000} {
		if _, err := e.addFlashItem(s.ID, flashItem(v, price, 10)); !errors.Is(err, promotion.ErrNotADiscount) {
			t.Errorf("giá %d: %v, mong ErrNotADiscount", price, err)
		}
	}
	if _, err := e.addFlashItem(s.ID, flashItem(v, 199000, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.addFlashItem(s.ID, flashItem(v, 150000, 5)); !errors.Is(err, promotion.ErrFlashItemExists) {
		t.Errorf("thêm hai lần: %v", err)
	}
	stopped := e.variant(e.shop.Seller, 100000)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET status = 'discontinued' WHERE id = $1`, stopped)
		return err //nolint:wrapcheck // test
	})
	for name, id := range map[string]uuid.UUID{"ngừng bán": stopped, "không có": uuid.New()} {
		if _, err := e.addFlashItem(s.ID, flashItem(id, 50000, 1)); !errors.Is(err, promotion.ErrVariantNotForSale) {
			t.Errorf("%s: %v, mong ErrVariantNotForSale", name, err)
		}
	}
	other := e.variant(e.shop.Seller, 200000)
	for name, in := range map[string]promotion.FlashItemInput{
		"giá 0":        {VariantID: other, SalePrice: 0, Quantity: 1, PerCustomerLimit: 1, HoldMinutes: 10},
		"0 suất":       {VariantID: other, SalePrice: 1000, Quantity: 0, PerCustomerLimit: 1, HoldMinutes: 10},
		"20001 suất":   {VariantID: other, SalePrice: 1000, Quantity: 20001, PerCustomerLimit: 1, HoldMinutes: 10},
		"11 mỗi khách": {VariantID: other, SalePrice: 1000, Quantity: 1, PerCustomerLimit: 11, HoldMinutes: 10},
		"giữ 4 phút":   {VariantID: other, SalePrice: 1000, Quantity: 1, PerCustomerLimit: 1, HoldMinutes: 4},
		"giữ 31 phút":  {VariantID: other, SalePrice: 1000, Quantity: 1, PerCustomerLimit: 1, HoldMinutes: 31},
	} {
		if _, err := e.addFlashItem(s.ID, in); !errors.Is(err, promotion.ErrInvalidInput) {
			t.Errorf("%s: %v, mong ErrInvalidInput", name, err)
		}
	}
	if _, err := e.addFlashItem(uuid.New(), flashItem(other, 1000, 1)); !errors.Is(err, promotion.ErrNotFound) {
		t.Errorf("đợt không có: %v", err)
	}
	pv := e.variant(e.shop.Partner, 300000)
	it, err := e.addFlashItem(s.ID, flashItem(pv, 250000, 3))
	if err != nil || it.SellerID != e.shop.Partner {
		t.Fatalf("biến thể đối tác: %+v %v", it, err)
	}
}

// Sửa đợt, sửa món theo version (version cũ → ErrVersionConflict), bỏ món; đợt đã scheduled (chủ bảng đặt) → sửa đợt,
// thêm, sửa, bỏ món đều ErrNotEditable.
func TestFlashSale_EditVersionAndNotEditable(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	v1, v2 := e.variant(e.shop.Seller, 350000), e.variant(e.shop.Seller, 200000)
	s := e.mustFlash("Sửa")
	it, err := e.addFlashItem(s.ID, flashItem(v1, 199000, 500))
	if err != nil {
		t.Fatal(err)
	}
	update := func(version int) (promotion.FlashSaleChange, error) {
		var ch promotion.FlashSaleChange
		err := e.app(func(tx pgx.Tx) (err error) {
			ch, err = promotion.UpdateFlashSale(t.Context(), tx, s.ID, flashInput("Sửa 2", 3*time.Hour), version)
			return err //nolint:wrapcheck // test
		})
		return ch, err
	}
	updateItem := func(version int, price int64) (promotion.FlashItemChange, error) {
		var ch promotion.FlashItemChange
		err := e.app(func(tx pgx.Tx) (err error) {
			ch, err = promotion.UpdateFlashItem(t.Context(), tx, s.ID, it.ID, flashItem(uuid.Nil, price, 400), version)
			return err //nolint:wrapcheck // test
		})
		return ch, err
	}
	remove := func(item uuid.UUID) error {
		return e.app(func(tx pgx.Tx) error {
			_, err := promotion.RemoveFlashItem(t.Context(), tx, s.ID, item)
			return err //nolint:wrapcheck // test
		})
	}
	ch, err := update(1)
	if err != nil || ch.Before.Name != "Sửa" || ch.After.Name != "Sửa 2" || ch.After.Version != 2 ||
		!ch.After.EndsAt.Equal(flashStart().Add(3*time.Hour)) {
		t.Fatalf("sửa đợt: %+v %v", ch, err)
	}
	if _, err := update(1); !errors.Is(err, promotion.ErrVersionConflict) {
		t.Errorf("sửa đợt version cũ: %v", err)
	}
	if _, err := updateItem(1, 350000); !errors.Is(err, promotion.ErrNotADiscount) {
		t.Errorf("sửa món giá bằng giá thường: %v", err)
	}
	ich, err := updateItem(1, 180000)
	if err != nil || ich.Before.SalePrice != 199000 || ich.After.SalePrice != 180000 || ich.After.Quantity != 400 ||
		ich.After.Version != 2 || ich.After.VariantID != v1 {
		t.Fatalf("sửa món: %+v %v", ich, err)
	}
	if _, err := updateItem(1, 170000); !errors.Is(err, promotion.ErrVersionConflict) {
		t.Errorf("sửa món version cũ: %v", err)
	}
	second, err := e.addFlashItem(s.ID, flashItem(v2, 150000, 5))
	if err != nil {
		t.Fatal(err)
	}
	if err := remove(second.ID); err != nil {
		t.Fatal(err)
	}
	if err := remove(second.ID); !errors.Is(err, promotion.ErrNotFound) {
		t.Errorf("bỏ món đã bỏ: %v", err)
	}
	if got := e.getFlash(s.ID); got.ItemCount != 1 || got.Items[0].ID != it.ID {
		t.Fatalf("sau khi bỏ: %+v", got.Items)
	}

	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.flash_sales SET status = 'scheduled' WHERE id = $1`, s.ID)
		return err //nolint:wrapcheck // test
	})
	if _, err := update(2); !errors.Is(err, promotion.ErrNotEditable) {
		t.Errorf("sửa đợt scheduled: %v", err)
	}
	if _, err := e.addFlashItem(s.ID, flashItem(v2, 150000, 5)); !errors.Is(err, promotion.ErrNotEditable) {
		t.Errorf("thêm món vào đợt scheduled: %v", err)
	}
	if _, err := updateItem(2, 170000); !errors.Is(err, promotion.ErrNotEditable) {
		t.Errorf("sửa món của đợt scheduled: %v", err)
	}
	if err := remove(it.ID); !errors.Is(err, promotion.ErrNotEditable) {
		t.Errorf("bỏ món của đợt scheduled: %v", err)
	}
}

// Danh sách mới nhất trước, lọc trạng thái, trang theo con trỏ, số món; trạng thái lạ → ErrInvalidInput.
func TestFlashSale_List(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	v := e.variant(e.shop.Seller, 350000)
	a, b, c := e.mustFlash("A"), e.mustFlash("B"), e.mustFlash("C")
	if _, err := e.addFlashItem(b.ID, flashItem(v, 100000, 1)); err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.flash_sales SET status = 'closed' WHERE id = $1`, a.ID)
		return err //nolint:wrapcheck // test
	})
	list := func(f promotion.FlashSaleFilter) ([]promotion.FlashSale, *uuid.UUID, error) {
		var out []promotion.FlashSale
		var next *uuid.UUID
		err := e.app(func(tx pgx.Tx) (err error) {
			out, next, err = promotion.ListFlashSales(t.Context(), tx, f)
			return err //nolint:wrapcheck // test
		})
		return out, next, err
	}
	page, next, err := list(promotion.FlashSaleFilter{Limit: 2})
	if err != nil || len(page) != 2 || page[0].ID != c.ID || page[1].ID != b.ID || page[1].ItemCount != 1 || next == nil {
		t.Fatalf("trang 1: %+v %v %v", page, next, err)
	}
	page, next, err = list(promotion.FlashSaleFilter{Limit: 2, After: next})
	if err != nil || len(page) != 1 || page[0].ID != a.ID || next != nil {
		t.Fatalf("trang 2: %+v %v %v", page, next, err)
	}
	drafts, _, err := list(promotion.FlashSaleFilter{Status: promotion.FlashDraft})
	if err != nil || len(drafts) != 2 {
		t.Fatalf("lọc nháp: %d %v", len(drafts), err)
	}
	if _, _, err := list(promotion.FlashSaleFilter{Status: "running"}); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Errorf("trạng thái lạ: %v", err)
	}
}

// Tổng suất, đã bán của đợt và có thể bán của món (Task 528): đợt hai món (tồn 12 và 0, suất 5 và 3) → Quantity 8,
// Sold 0 (nháp chưa trải đơn vị), Sellable 12 và 0 (biến thể chưa có hàng); đợt không món → 0, 0 (sum trên tập rỗng);
// danh sách trả cùng Quantity, Sold; thêm và sửa món cũng trả Sellable.
func TestFlashSale_Counts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var v1 uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		v1 = e.shop.Variant(t, tx, e.shop.Seller, 350000, 12)
		return nil
	})
	v2 := e.variant(e.shop.Seller, 350000)
	full, empty := e.mustFlash("Hai món"), e.mustFlash("Không món")
	it1, err := e.addFlashItem(full.ID, flashItem(v1, 100000, 5))
	if err != nil || it1.Sellable != 12 {
		t.Fatalf("thêm món 1: %+v %v", it1, err)
	}
	if _, err := e.addFlashItem(full.ID, flashItem(v2, 100000, 3)); err != nil {
		t.Fatal(err)
	}
	var upd promotion.FlashItemChange
	if err := e.app(func(tx pgx.Tx) (err error) {
		upd, err = promotion.UpdateFlashItem(t.Context(), tx, full.ID, it1.ID, flashItem(v1, 100000, 4), it1.Version)
		return err //nolint:wrapcheck // test
	}); err != nil || upd.After.Sellable != 12 || upd.After.Quantity != 4 {
		t.Fatalf("sửa món: %+v %v", upd.After, err)
	}
	got := e.getFlash(full.ID)
	if got.Quantity != 7 || got.Sold != 0 || len(got.Items) != 2 || got.Items[0].Sellable != 12 || got.Items[1].Sellable != 0 {
		t.Fatalf("đợt hai món: Quantity %d Sold %d món %+v", got.Quantity, got.Sold, got.Items)
	}
	if g := e.getFlash(empty.ID); g.Quantity != 0 || g.Sold != 0 || g.ItemCount != 0 {
		t.Fatalf("đợt không món: %+v", g)
	}
	var page []promotion.FlashSale
	if err := e.app(func(tx pgx.Tx) (err error) {
		page, _, err = promotion.ListFlashSales(t.Context(), tx, promotion.FlashSaleFilter{})
		return err //nolint:wrapcheck // test
	}); err != nil || len(page) != 2 {
		t.Fatalf("danh sách: %+v %v", page, err)
	}
	for _, s := range page {
		want := map[uuid.UUID][3]int{full.ID: {2, 7, 0}, empty.ID: {0, 0, 0}}[s.ID]
		if [3]int{s.ItemCount, s.Quantity, s.Sold} != want {
			t.Errorf("danh sách %s: món %d suất %d bán %d, mong %v", s.Name, s.ItemCount, s.Quantity, s.Sold, want)
		}
	}
}

// Bốn bảng mới đúng luật schema; CHECK và khóa ngoại của database: đơn vị held không có dòng đơn → CHECK; số đã lấy
// -1 → CHECK; món trỏ biến thể của người bán B với seller_id của A → khóa ngoại (ADR-29); đợt dài 8 ngày ghi thẳng →
// CHECK; crm_app không sửa được created_by của đợt.
func TestFlashSchema_Constraints(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	testkit.CheckSchemaRulesClean(t, e.staff.Owner)
	v := e.variant(e.shop.Seller, 350000)
	pv := e.variant(e.shop.Partner, 300000)
	s := e.mustFlash("Ràng buộc")
	it, err := e.addFlashItem(s.ID, flashItem(v, 199000, 10))
	if err != nil {
		t.Fatal(err)
	}
	cust := e.customer()
	ownerErr := func(q string, args ...any) error {
		return e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q, args...)
			return err //nolint:wrapcheck // test
		})
	}
	code := func(err error) string {
		pg, _ := errors.AsType[*pgconn.PgError](err)
		if pg == nil {
			return ""
		}
		return pg.Code
	}
	tenant := `current_setting('app.tenant_id')::uuid`
	cases := []struct {
		name, want, q string
		args          []any
	}{
		{"held không dòng đơn", "23514", `INSERT INTO inventory.flash_sale_units (tenant_id, flash_sale_item_id, status)
			VALUES (` + tenant + `, $1, 'held')`, []any{it.ID}},
		{"số đã lấy -1", "23514", `INSERT INTO promotion.flash_sale_claims (tenant_id, flash_sale_item_id, customer_id, quantity)
			VALUES (` + tenant + `, $1, $2, -1)`, []any{it.ID, cust}},
		{"biến thể của B với seller A", "23503", `INSERT INTO promotion.flash_sale_items (tenant_id, flash_sale_id,
			seller_id, variant_id, warehouse_id, sale_price, quantity)
			VALUES (` + tenant + `, $1, $2, $3, $4, 1000, 1)`, []any{s.ID, e.shop.Seller, pv, e.shop.Warehouse}},
		{"đợt 8 ngày", "23514", `INSERT INTO promotion.flash_sales (tenant_id, name, starts_at, ends_at, created_by)
			VALUES (` + tenant + `, 'x', $1, $2, $3)`, []any{flashStart(), flashStart().Add(8 * 24 * time.Hour), e.user}},
	}
	for _, c := range cases {
		if got := code(ownerErr(c.q, c.args...)); got != c.want {
			t.Errorf("%s: mã %q, mong %q", c.name, got, c.want)
		}
	}
	if err := ownerErr(`INSERT INTO inventory.flash_sale_units (tenant_id, flash_sale_item_id) VALUES (`+tenant+`, $1)`,
		it.ID); err != nil {
		t.Errorf("đơn vị available không dòng đơn: %v", err)
	}
	err = e.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.flash_sales SET created_by = $1 WHERE id = $2`, uuid.New(), s.ID)
		return err //nolint:wrapcheck // test
	})
	if code(err) != "42501" {
		t.Errorf("crm_app sửa created_by: %v, mong 42501", err)
	}
}
