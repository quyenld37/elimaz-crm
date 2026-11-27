package web

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// WebCustomerName là tên của khách mới tạo từ lượt mua flash sale (website không gửi tên; customer đòi tên 1–120 ký
// tự; không chứa SĐT). Kế hoạch M5, bảng đối chiếu dòng B1.
const WebCustomerName = "Khách website"

// Flash là lượt mua flash sale của website (Task 203, M5-20): tìm hay tạo khách theo SĐT, rồi lượt mua (order.BuyFlash)
// trong transaction của lớp HTTP.
type Flash struct {
	db        *db.DB
	orders    order.Writer
	customers customer.Writer
	now       func() time.Time
}

// NewFlash trả Flash trên d; now nil → time.Now.
func NewFlash(d *db.DB, orders order.Writer, customers customer.Writer, now func() time.Time) *Flash {
	return &Flash{db: d, orders: orders, customers: customers, now: now}
}

func (f *Flash) clock() time.Time { return nowUTC(f.now) }

// FlashBuyInput là một lượt mua: đợt và món trên đường dẫn, khách (đã tìm hay tạo), số lượng, mã đơn của website.
type FlashBuyInput struct {
	SaleID, ItemID, CustomerID uuid.UUID
	Qty                        int
	ExternalRef                string
}

// Customer tìm hay tạo khách của SĐT phone (đã chuẩn hóa) trong một transaction riêng của tenant (db.TxAPI): SĐT là
// danh tính của khách, nguồn web, khách mới tên WebCustomerName. Transaction commit trước lượt mua (khóa dòng khách nhả
// ngay, không nằm trong transaction mua).
func (f *Flash) Customer(ctx context.Context, tenant uuid.UUID, phone logging.Phone, a customer.Actor) (uuid.UUID, error) {
	now := f.clock()
	var id uuid.UUID
	err := tenancy.WithTenant(ctx, f.db, tenant, func(tx pgx.Tx) error {
		c, err := f.customers.FindOrCreateForOrder(ctx, tx, customer.OrderCustomerInput{
			FullName: WebCustomerName, Phone: string(phone), PhoneIsCustomers: true, Source: customer.SourceWeb,
		}, a, now)
		id = c.CustomerID // gán lại mỗi lần chạy
		return err        //nolint:wrapcheck // bọc bên dưới
	}, db.TxAPI)
	if err != nil {
		return uuid.Nil, fmt.Errorf("khách của lượt mua flash sale: %w", err)
	}
	return id, nil
}

// Buy chạy lượt mua in trong tx của bên gọi (db.TxFlashBuy): món không thuộc đợt in.SaleID → promotion.ErrNotFound;
// rồi order.Writer.BuyFlash với giờ của Flash. Lỗi trả nguyên (sentinel của order, promotion) để lớp HTTP phân loại.
func (f *Flash) Buy(ctx context.Context, tx pgx.Tx, in FlashBuyInput, a order.Actor) (order.FlashBuyResult, error) {
	it, err := promotion.GetFlashItemForBuy(ctx, tx, in.ItemID)
	if err != nil {
		return order.FlashBuyResult{}, fmt.Errorf("món flash sale: %w", err)
	}
	if it.SaleID != in.SaleID {
		return order.FlashBuyResult{}, fmt.Errorf("món không thuộc đợt: %w", promotion.ErrNotFound)
	}
	res, err := f.orders.BuyFlash(ctx, tx, order.FlashBuyInput{
		ItemID: in.ItemID, CustomerID: in.CustomerID, Qty: in.Qty, ExternalRef: in.ExternalRef, Now: f.clock(),
	}, a)
	if err != nil {
		return order.FlashBuyResult{}, err //nolint:wrapcheck // sentinel của order giữ nguyên cho errors_web.go
	}
	return res, nil
}

// FlashResultOut là kết quả lượt mua theo hình dạng WebFlashBuyResult; mã đơn, mã ngoài, hạn giữ chỉ khi bought.
func FlashResultOut(r order.FlashBuyResult) *gen.WebFlashBuyResult {
	out := &gen.WebFlashBuyResult{Result: gen.WebFlashBuyResultResult(r.Result)}
	if r.Result == order.FlashBought {
		out.Code = gen.NewOptString(r.Code)
		out.ExternalRef = gen.NewOptString(r.ExternalRef)
		out.ExpiresAt = gen.NewOptDateTime(r.ExpiresAt.UTC())
	}
	return out
}
