package reporting

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// bsDay là ngày của kịch bản chín đơn trong test bán chạy; bsEarlier là ngày lên các đơn chỉ để có hàng trả trong kỳ.
const (
	bsDay     = "2026-11-10"
	bsEarlier = "2026-11-01"
)

// bsEnv là kịch bản chín đơn cộng: hai phiếu khách trả nhận trong kỳ của đơn lên ngoài kỳ (quần B: S1 D0 M1 của dòng 2
// chiếc; váy C: S1 D1 M0), một phiếu chưa nhận (áo A-S, ba cột 0), một sản phẩm của người bán thứ hai bán cùng ngày.
type bsEnv struct {
	setEnv
	sc      reportseed.Scenario
	partner uuid.UUID
	// Mã mẫu Áo A, Quần B, Váy C.
	prodA, prodB, prodC uuid.UUID
}

func newBSEnv(t *testing.T) bsEnv {
	t.Helper()
	e := bsEnv{setEnv: newSetEnv(t)}
	e.sc = reportseed.NineOrders(e.seed, bsDay)
	e.prodA, e.prodB, e.prodC = e.productOf(t, e.sc.ASmall), e.productOf(t, e.sc.PantsB), e.productOf(t, e.sc.DressC)
	vnAt := func(day string, h int) time.Time {
		d, err := time.ParseInLocation(dayLayout, day, VN)
		if err != nil {
			t.Fatal(err)
		}
		return d.Add(time.Duration(h) * time.Hour).UTC()
	}
	placed, received := vnAt(bsEarlier, 9), vnAt(bsDay, 15)
	returned := func(v uuid.UUID, price int64, line reportseed.ReturnLine, receivedAt *time.Time, status string) {
		e.seed.Order(reportseed.OrderSpec{
			Channel: "manual", Items: []reportseed.Item{{Variant: v, Qty: 2, Price: price}},
			History: []reportseed.Step{{To: "new", At: placed}, {To: "confirmed", At: placed.Add(time.Minute)}, {To: "delivered", At: placed.Add(time.Hour)}},
			Returns: []reportseed.ReturnSpec{{Kind: "customer_return", Status: status, ReceivedAt: receivedAt, CreatedAt: placed.Add(2 * time.Hour), Lines: []reportseed.ReturnLine{line}}},
		})
	}
	returned(e.sc.PantsB, 250_000, reportseed.ReturnLine{Qty: 2, Sellable: 1, Missing: 1}, &received, "received")
	returned(e.sc.DressC, 200_000, reportseed.ReturnLine{Qty: 2, Sellable: 1, Damaged: 1}, &received, "received")
	returned(e.sc.ASmall, 150_000, reportseed.ReturnLine{Qty: 1}, nil, "requested")
	e.partner = e.seed.Seller("DOITAC", "partner")
	pv := e.seed.Variant(e.partner, "Túi đối tác", "TUI-DT")
	e.seed.Order(reportseed.OrderSpec{
		Seller: e.partner, Channel: "manual", Items: []reportseed.Item{{Variant: pv, Qty: 9, Price: 500_000}},
		History: []reportseed.Step{{To: "new", At: vnAt(bsDay, 9)}, {To: "confirmed", At: vnAt(bsDay, 10)}, {To: "delivered", At: vnAt(bsDay, 11)}},
	})
	return e
}

// run chạy báo cáo bán chạy (đúng hàm JSON của Def) trong ngữ cảnh tenant bằng crm_app.
func (e bsEnv) run(t *testing.T, s DataScope, p BestSellersParams) (BestSellersReport, error) {
	t.Helper()
	var out BestSellersReport
	err := tenancy.WithTenant(t.Context(), e.st.App, e.tenant, func(tx pgx.Tx) error {
		v, err := bestSellersDef().JSON(t.Context(), tx, s, &p)
		if err != nil {
			return err
		}
		r, ok := v.(BestSellersReport)
		if !ok {
			return fmt.Errorf("kết quả kiểu %T", v)
		}
		out = r
		return nil
	})
	if err != nil {
		return BestSellersReport{}, fmt.Errorf("bán chạy: %w", err)
	}
	return out, nil
}

func (e bsEnv) mustRun(t *testing.T, s DataScope, p BestSellersParams) BestSellersReport {
	t.Helper()
	r, err := e.run(t, s, p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func bsParams(level, basis string, limit int) BestSellersParams {
	return BestSellersParams{PeriodParams: PeriodParams{From: bsDay, To: bsDay}, Level: level, Basis: basis, Limit: limit}
}

// bsWant là phần của một dòng mà test so.
type bsWant struct {
	key                                  uuid.UUID // biến thể (sku) hay mẫu (product)
	sku                                  string    // "" ở mức product
	name                                 string
	qty, orders, goods, returnedQuantity int64
}

func checkRows(t *testing.T, name string, got BestSellersReport, total int64, want ...bsWant) {
	t.Helper()
	if got.Status != "ready" || got.TotalRows != total || len(got.Rows) != len(want) {
		t.Fatalf("%s: status %q, totalRows %d (mong %d), %d dòng (mong %d): %+v", name, got.Status, got.TotalRows, total,
			len(got.Rows), len(want), got.Rows)
	}
	for i, w := range want {
		r := got.Rows[i]
		key := r.ProductID
		if w.sku != "" {
			if r.VariantID == nil || r.SKU == nil || *r.SKU != w.sku || r.Color == nil || r.Size == nil || *r.Size != w.sku {
				t.Errorf("%s dòng %d: biến thể %v, sku %v, màu %v, size %v; mong sku %s", name, i, r.VariantID, r.SKU, r.Color, r.Size, w.sku)
				continue
			}
			key = *r.VariantID
		} else if r.VariantID != nil || r.SKU != nil || r.Color != nil || r.Size != nil {
			t.Errorf("%s dòng %d: mức mẫu có biến thể/sku/màu/size: %+v", name, i, r)
		}
		if key != w.key || r.Name != w.name || r.Quantity != w.qty || r.Orders != w.orders || r.GoodsAmount != w.goods ||
			r.ReturnedQuantity != w.returnedQuantity {
			t.Errorf("%s dòng %d = {%v %q SL %d đơn %d tiền %d trả %d}, mong {%v %q SL %d đơn %d tiền %d trả %d}", name, i,
				key, r.Name, r.Quantity, r.Orders, r.GoodsAmount, r.ReturnedQuantity,
				w.key, w.name, w.qty, w.orders, w.goods, w.returnedQuantity)
		}
	}
}

// productOf đọc mã mẫu của biến thể v.
func (e bsEnv) productOf(t *testing.T, v uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tenancy.WithTenant(t.Context(), e.st.App, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT product_id FROM catalog.variants_v WHERE variant_id = $1", v).Scan(&id)
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// Kịch bản chín đơn, mức SKU, cơ sở placed: A-M 4 (O3, O9), A-S 3 (O1, O8; O7 nháp không tính), quần B 2, váy C 2 (O6
// hủy không tính, tiền 360.000). Hàng khách trả theo ngày nhận, theo biến thể, không theo tập đơn: A-M 1 (O9), quần B
// 1 (S1 D0 M1: chiếc thiếu không tính), váy C 2 (S1 D1 M0), A-S 0 (phiếu chưa nhận). Sản phẩm của người bán thứ hai
// không xuất hiện.
func TestBestSellers_SKUPlaced(t *testing.T) {
	t.Parallel()
	e := newBSEnv(t)
	sc := e.sc
	got := e.mustRun(t, e.all, bsParams("sku", "placed", 0))
	checkRows(t, "sku placed", got, 4,
		bsWant{sc.AMedium, "AOA-M", "Áo A", 4, 2, 600_000, 1},
		bsWant{sc.ASmall, "AOA-S", "Áo A", 3, 2, 450_000, 0},
		bsWant{sc.PantsB, "QUANB", "Quần B", 2, 2, 500_000, 1},
		bsWant{sc.DressC, "VAYC", "Váy C", 2, 1, 360_000, 2},
	)
	// Mặc định (level, basis bỏ trống) là sku, placed.
	def := e.mustRun(t, e.all, bsParams("", "", 0))
	if len(def.Rows) != 4 || def.Rows[0].VariantID == nil || *def.Rows[0].VariantID != sc.AMedium {
		t.Errorf("mặc định: %+v", def.Rows)
	}
}

// Mức mẫu: Áo A 7 chiếc, 4 đơn, 1.050.000; quần B 2, 500.000 đứng trước váy C 2, 360.000 (bằng số lượng, hơn tiền).
// Limit 2 → 2 dòng, TotalRows 3.
func TestBestSellers_ProductPlaced(t *testing.T) {
	t.Parallel()
	e := newBSEnv(t)
	a, b, c := e.prodA, e.prodB, e.prodC
	got := e.mustRun(t, e.all, bsParams("product", "placed", 0))
	checkRows(t, "product placed", got, 3,
		bsWant{a, "", "Áo A", 7, 4, 1_050_000, 1},
		bsWant{b, "", "Quần B", 2, 2, 500_000, 1},
		bsWant{c, "", "Váy C", 2, 1, 360_000, 2},
	)
	got = e.mustRun(t, e.all, bsParams("product", "placed", 2))
	checkRows(t, "product placed limit 2", got, 3,
		bsWant{a, "", "Áo A", 7, 4, 1_050_000, 1},
		bsWant{b, "", "Quần B", 2, 2, 500_000, 1},
	)
}

// Cơ sở delivered (đơn giao thành công trong kỳ: O1, O9): Áo A 5 chiếc, 750.000; quần B, váy C không giao trong kỳ
// nhưng có hàng trả nhận trong kỳ nên vẫn có dòng số lượng 0 (QĐ-232-3). Mức SKU: A-M trả 1.
func TestBestSellers_Delivered(t *testing.T) {
	t.Parallel()
	e := newBSEnv(t)
	sc, a, b, c := e.sc, e.prodA, e.prodB, e.prodC
	got := e.mustRun(t, e.all, bsParams("product", "delivered", 0))
	checkRows(t, "product delivered", got, 3,
		bsWant{a, "", "Áo A", 5, 2, 750_000, 1},
		bsWant{b, "", "Quần B", 0, 0, 0, 1}, // bằng số lượng, bằng tiền: theo tên tăng
		bsWant{c, "", "Váy C", 0, 0, 0, 2},
	)
	got = e.mustRun(t, e.all, bsParams("sku", "delivered", 0))
	checkRows(t, "sku delivered", got, 4,
		bsWant{sc.AMedium, "AOA-M", "Áo A", 3, 1, 450_000, 1},
		bsWant{sc.ASmall, "AOA-S", "Áo A", 2, 1, 300_000, 0},
		bsWant{sc.PantsB, "QUANB", "Quần B", 0, 0, 0, 1},
		bsWant{sc.DressC, "VAYC", "Váy C", 0, 0, 0, 2},
	)
}

// Phạm vi Page: người chỉ xem Page P2 chỉ thấy đơn của P2 (O3: A-M 1; O6 hủy), không thấy hàng trả của đơn P1 (O9) hay
// đơn không gắn Page; không Page nào → không dòng nào.
func TestBestSellers_PageScope(t *testing.T) {
	t.Parallel()
	e := newBSEnv(t)
	p2 := DataScope{TenantID: e.tenant, SellerID: e.all.SellerID, PageIDs: []uuid.UUID{e.sc.P2}}
	checkRows(t, "Page P2", e.mustRun(t, p2, bsParams("sku", "placed", 0)), 1,
		bsWant{e.sc.AMedium, "AOA-M", "Áo A", 1, 1, 150_000, 0})
	none := DataScope{TenantID: e.tenant, SellerID: e.all.SellerID}
	checkRows(t, "không Page", e.mustRun(t, none, bsParams("sku", "placed", 0)), 0)
}

// Tham số sai → ErrBadParams (hay lỗi ngày) trước khi có câu nào: level, basis lạ; Limit âm hay quá 500; ngày sai.
func TestBestSellers_BadParams(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		p    BestSellersParams
		want error
	}{
		{"level lạ", bsParams("color", "placed", 0), ErrBadParams},
		{"basis lạ", bsParams("sku", "completed", 0), ErrBadParams},
		{"limit 501", bsParams("sku", "placed", 501), ErrBadParams},
		{"limit âm", bsParams("sku", "placed", -1), ErrBadParams},
		{"ngày sai", BestSellersParams{PeriodParams: PeriodParams{From: "2026-11-31", To: bsDay}}, ErrBadDate},
	} {
		if err := c.p.Validate(); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, mong %v", c.name, err, c.want)
		}
	}
	for _, ok := range []BestSellersParams{bsParams("sku", "placed", 500), bsParams("product", "delivered", 1), bsParams("", "", 0)} {
		if err := ok.Validate(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	p, err := DecodeParams(KeyBestSellers, []byte(`{"from":"2026-11-01","to":"2026-11-02","level":"product","basis":"delivered","limit":501}`))
	if !errors.Is(err, ErrBadParams) {
		t.Errorf("DecodeParams limit 501: %v, %v", p, err)
	}
}

// EXPLAIN bằng crm_app của câu bán chạy kỳ 31 ngày trên 100.000 đơn (hai mức, hai cơ sở, phạm vi Page): không Seq Scan.
func TestBestSellers_ExplainNoSeqScan(t *testing.T) {
	t.Parallel()
	rnd := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // G404: dữ liệu thử lặp lại được, không phải bí mật
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	e := newSetEnv(t)
	e.seed.Bulk(100_000, rnd, start, start.AddDate(2, 0, 0))
	page := DataScope{TenantID: e.tenant, SellerID: e.all.SellerID, PageIDs: e.seed.Pages()[:1]}
	for _, c := range []struct {
		s            DataScope
		level, basis string
	}{
		{e.all, LevelSKU, SalesBasisPlaced}, {e.all, LevelProduct, SalesBasisDelivered}, {page, LevelProduct, SalesBasisPlaced},
	} {
		p := BestSellersParams{PeriodParams: PeriodParams{From: "2026-03-01", To: "2026-03-31"}, Level: c.level, Basis: c.basis}
		sql, args, err := bestSellersSQL(c.s, &p, DefaultBestSellersLimit)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := testkit.ExplainAsApp(t.Context(), e.st.App, e.tenant, sql, args...)
		if err != nil {
			t.Fatalf("%s/%s: %v", c.level, c.basis, err)
		}
		testkit.RequireNoSeqScan(t, nodes)
	}
}
