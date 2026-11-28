// Package flashbuytest chạy một lượt mua flash sale (đơn nháp, đơn vị suất đang giữ) cho test của lớp gọi (HTTP, lệnh
// đối chiếu; Task 528). Gom phần lặp ở test httpapi và flashverify.
//
// Khi nào không dùng: mã sản phẩm (chỉ dùng trong test); test của chính module order (gọi Writer.BuyFlash ngay trong
// test để thấy từng bước); đường mua qua HTTP của website (gọi API thật). Nằm ngoài internal/modules vì nhập order mà
// gói dựng đợt promotion/flashtest không được gọi.
package flashbuytest

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Buy chạy một lượt mua flash qty suất của món item cho khách customer như khóa dịch vụ website (order.Writer.BuyFlash,
// transaction tenant riêng trên pool app, db.TxFlashBuy, giờ máy); trả mã đơn nháp. Kết quả khác bought hay lỗi làm
// test dừng (chỉ gọi từ goroutine của test).
func Buy(t testing.TB, app *db.DB, tenantID, item, customer uuid.UUID, qty int, ref string) uuid.UUID {
	t.Helper()
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
	a := order.Actor{TenantID: tenantID, UserID: uuid.New(), Type: order.ActorService}
	var res order.FlashBuyResult
	err := tenancy.WithTenant(t.Context(), app, tenantID, func(tx pgx.Tx) error {
		var err error
		res, err = w.BuyFlash(t.Context(), tx, order.FlashBuyInput{
			ItemID: item, CustomerID: customer, Qty: qty, ExternalRef: ref, Now: time.Now().UTC(),
		}, a)
		return err //nolint:wrapcheck // test
	}, db.TxFlashBuy)
	if err != nil || res.Result != order.FlashBought {
		t.Fatalf("flashbuytest: mua %d: %v %+v", qty, err, res)
	}
	return res.OrderID
}
