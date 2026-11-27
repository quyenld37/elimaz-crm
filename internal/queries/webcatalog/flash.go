package webcatalog

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/queries/webcatalog/internal/store"
)

// Trạng thái hiệu lực của đợt flash sale cho website (QĐ-204-1).
const (
	FlashScheduled = "scheduled"
	FlashOpen      = "open"
)

// WebFlashSale là một đợt flash sale đã lên lịch hay đang mở, chưa quá EndsAt, kèm món theo mã tăng dần. Status là
// trạng thái hiệu lực lúc đọc (FlashStatusAt).
type WebFlashSale struct {
	ID               uuid.UUID
	Name             string
	StartsAt, EndsAt time.Time
	Status           string
	Items            []WebFlashItem
}

// WebFlashItem là một món của đợt: biến thể, mẫu, giá flash, giá thường của biến thể, số suất, giới hạn mỗi khách,
// phút giữ đơn nháp.
type WebFlashItem struct {
	ID, VariantID, ProductID                uuid.UUID
	SalePrice, RegularPrice                 int64
	Quantity, PerCustomerLimit, HoldMinutes int
}

// FlashStatusAt là trạng thái hiệu lực của đợt (đã lên lịch hay đang mở trong database) lúc now: trước startsAt →
// FlashScheduled, từ startsAt tới trước endsAt → FlashOpen (lượt mua nhận đợt trong khung giờ dù nhịp mở đợt chưa
// chạy), từ endsAt → "" (không còn trả cho website).
func FlashStatusAt(startsAt, endsAt, now time.Time) string {
	switch {
	case !now.Before(endsAt):
		return ""
	case now.Before(startsAt):
		return FlashScheduled
	}
	return FlashOpen
}

// FlashCurrent trả các đợt scheduled/open chưa quá endsAt lúc now theo giờ mở tăng dần (đợt không có món thì không có),
// món theo mã tăng dần. Không có số còn (FlashAvailable).
func FlashCurrent(ctx context.Context, tx pgx.Tx, now time.Time) ([]WebFlashSale, error) {
	rows, err := store.New(tx).FlashCurrentItems(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("đọc đợt flash sale cho website: %w", err)
	}
	out := []WebFlashSale{}
	for _, r := range rows {
		if len(out) == 0 || out[len(out)-1].ID != r.SaleID {
			out = append(out, WebFlashSale{
				ID: r.SaleID, Name: r.Name, StartsAt: r.StartsAt.UTC(), EndsAt: r.EndsAt.UTC(),
				Status: FlashStatusAt(r.StartsAt, r.EndsAt, now),
			})
		}
		s := &out[len(out)-1]
		s.Items = append(s.Items, WebFlashItem{
			ID: r.ItemID, VariantID: r.VariantID, ProductID: r.ProductID, SalePrice: r.SalePrice,
			RegularPrice: r.RegularPrice, Quantity: int(r.Quantity), PerCustomerLimit: int(r.PerCustomerLimit),
			HoldMinutes: int(r.HoldMinutes),
		})
	}
	return out, nil
}

// FlashAvailable trả số đơn vị suất còn trống của các món trong itemIDs; món không có đơn vị (hay mã lạ) → 0.
func FlashAvailable(ctx context.Context, tx pgx.Tx, itemIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := store.New(tx).FlashAvailableOf(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("đọc suất còn trống cho website: %w", err)
	}
	for _, id := range itemIDs {
		out[id] = 0
	}
	for _, r := range rows {
		out[r.FlashSaleItemID] = int(r.Available)
	}
	return out, nil
}
