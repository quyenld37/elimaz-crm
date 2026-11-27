// Package flashtest dựng đợt flash sale đã lên lịch cho test của lớp gọi (HTTP, e2e; Task 203). Khi nào không dùng:
// mã chạy thật (chỉ test); test của chính module promotion, order (dựng qua API của module ngay trong test để thấy
// từng bước).
package flashtest

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// Sale là một đợt có một món: mã đợt, mã món.
type Sale struct {
	ID, Item uuid.UUID
}

// Item là món của đợt: biến thể, giá flash, số suất, giới hạn mỗi khách, phút giữ.
type Item struct {
	Variant          uuid.UUID
	Price            int64
	Slots, Limit     int
	HoldMinutes      int
	Name             string    // tên đợt; rỗng → "Đợt thử"
	StartsAt         time.Time // giờ mở; đợt dài 2 giờ, lên lịch một giờ trước giờ mở
	TenantID, UserID uuid.UUID // người tạo, lên lịch
}

// Schedule tạo đợt [StartsAt, StartsAt+2h) có một món theo it rồi lên lịch đợt (giữ suất) trong tx của tenant
// (biến thể phải có đủ tồn). Lỗi làm test dừng (chỉ gọi từ goroutine của test).
func Schedule(t testing.TB, tx pgx.Tx, it Item) Sale {
	t.Helper()
	ctx := t.Context()
	name := it.Name
	if name == "" {
		name = "Đợt thử"
	}
	actor := promotion.Actor{TenantID: it.TenantID, UserID: it.UserID}
	sale, err := promotion.CreateFlashSale(ctx, tx, actor, promotion.FlashSaleInput{
		Name: name, StartsAt: it.StartsAt, EndsAt: it.StartsAt.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("flashtest: tạo đợt: %v", err)
	}
	item, err := promotion.AddFlashItem(ctx, tx, sale.ID, promotion.FlashItemInput{
		VariantID: it.Variant, SalePrice: it.Price, Quantity: it.Slots, PerCustomerLimit: it.Limit,
		HoldMinutes: it.HoldMinutes,
	})
	if err != nil {
		t.Fatalf("flashtest: thêm món: %v", err)
	}
	cur, err := promotion.GetFlashSale(ctx, tx, sale.ID)
	if err != nil {
		t.Fatalf("flashtest: đọc đợt: %v", err)
	}
	w := promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
	if _, err := w.ScheduleFlashSale(ctx, tx, sale.ID, cur.Version, actor, it.StartsAt.Add(-time.Hour)); err != nil {
		t.Fatalf("flashtest: lên lịch: %v", err)
	}
	return Sale{ID: sale.ID, Item: item.ID}
}
