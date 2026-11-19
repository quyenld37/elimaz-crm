package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Task 406: tồn thực không đủ cho giữ hàng (đã bọc %w) → 409 stock_short kèm variantId, available = phần trên kệ; đúng
// bốn trường, không dữ liệu cá nhân.
func TestDomainErrors_StockShort(t *testing.T) {
	v := uuid.New()
	err := fmt.Errorf("trừ tồn khi giao xong: %w", &inventory.StockShortError{
		ReservationID: uuid.New(), VariantID: v, WarehouseID: uuid.New(), Requested: 2, OnShelf: 1,
	})
	rec := domainErrorRecorder(t, err)
	var body map[string]any
	if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != http.StatusConflict || body["error"] != "stock_short" ||
		body["variantId"] != v.String() || body["available"] != float64(1) || body["request_id"] == nil || len(body) != 4 {
		t.Fatalf("mong 409 stock_short kèm variantId, available 1: %d %s", rec.Code, rec.Body.String())
	}
	if rec := domainErrorRecorder(t, fmt.Errorf("việc thử: %w", inventory.ErrStockShort)); rec.Code != http.StatusConflict {
		t.Fatalf("sentinel ErrStockShort: %d %s", rec.Code, rec.Body.String())
	}
	if !errors.Is(&inventory.StockShortError{}, inventory.ErrStockShort) {
		t.Fatal("StockShortError phải Is ErrStockShort")
	}
}

// holdByOrder tạo một đơn nhập tay thật giữ qty cái biến thể v ở kho wh; trả mã đơn và mã hiển thị.
func (e *invAPIEnv) holdByOrder(v, wh uuid.UUID, qty int32) (uuid.UUID, string) {
	e.t.Helper()
	var id uuid.UUID
	var code string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		ctx := e.t.Context()
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		c, err := customer.Create(ctx, tx, "Khách thử")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		o, err := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}.CreateManual(ctx, tx,
			order.CreateManualInput{
				IdempotencyKey: uuid.New(), SellerID: seller, CustomerID: c, WarehouseID: wh,
				Items:       []order.ItemInput{{VariantID: v, Qty: qty}},
				ShipAddress: order.ShipAddress{Name: "Khách thử", Phone: testPhone, Line: "1 Lê Lợi"},
			}, order.Actor{TenantID: e.tenant})
		id, code = o.ID, o.Code
		return err //nolint:wrapcheck // test
	})
	return id, code
}

// Áp dụng phiên đếm thiếu dưới phần giữ qua API: shortages kèm orders — các đơn đang giữ (chưa đi giao) theo thứ tự
// giữ hàng, mã đơn, số lượng; biến thể không thiếu không có dòng; chỉ mã đơn và số, không thông tin khách.
func TestStocktakeAPI_ShortageListsOrders(t *testing.T) {
	e := newInvAPIEnv(t)
	v, _, id := e.openW1() // on_hand 5 ở w1
	o1, c1 := e.holdByOrder(v, e.w1, 2)
	o2, c2 := e.holdByOrder(v, e.w1, 1)
	mustOK(t, "đếm", e.countStk(stkApply, id, v, `{"counted":1}`, "c1"), http.StatusOK)
	g := mustOK(t, "getStocktake", e.do(http.MethodGet, "/api/v1/inventory/stocktakes/"+id, stkApply, "", ""), http.StatusOK)
	rec := e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/apply", stkApply, "a1",
		fmt.Sprintf(`{"version":%d}`, version(t, g)))
	if rec.Code != http.StatusOK {
		t.Fatalf("áp dụng: %d %s", rec.Code, rec.Body.String())
	}
	var r struct {
		Shortages []struct {
			VariantID string `json:"variantId"`
			Available int32  `json:"available"`
			Orders    []struct {
				OrderID   string `json:"orderId"`
				OrderCode string `json:"orderCode"`
				Quantity  int32  `json:"quantity"`
			} `json:"orders"`
		} `json:"shortages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Shortages) != 1 || r.Shortages[0].VariantID != v.String() || r.Shortages[0].Available != -2 {
		t.Fatalf("shortages: %s", rec.Body.String())
	}
	os := r.Shortages[0].Orders
	if len(os) != 2 || os[0].OrderID != o1.String() || os[0].OrderCode != c1 || os[0].Quantity != 2 ||
		os[1].OrderID != o2.String() || os[1].OrderCode != c2 || os[1].Quantity != 1 {
		t.Fatalf("orders: %s (mong %s ×2, %s ×1)", rec.Body.String(), c1, c2)
	}
}
