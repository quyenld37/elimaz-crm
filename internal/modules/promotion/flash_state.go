package promotion

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
)

// FlashItemRemaining là số suất còn trống (đơn vị available) của một món thuộc đợt đã lên lịch hay đang mở.
type FlashItemRemaining struct {
	ItemID, SaleID uuid.UUID
	Available      int
}

// FlashRemaining trả số suất còn trống của các món thuộc đợt scheduled/open theo mã món tăng dần (Task 204: vòng dựng
// lại cờ hết suất và số còn trên cổng flash sale). saleID, itemID khác uuid.Nil thì chỉ lấy món của đợt đó, đúng món đó
// (món của đợt nháp hay đã đóng không có dòng). Chỉ đọc, không khóa; số có thể cũ ngay sau khi đọc.
//
// Khi nào không dùng: quyết định bán (lượt mua chốt trong transaction của order.BuyFlash); số liệu màn quản lý (Live).
func FlashRemaining(ctx context.Context, tx pgx.Tx, saleID, itemID uuid.UUID) ([]FlashItemRemaining, error) {
	var p store.FlashActiveRemainingParams // mã rỗng: không lọc (NULL)
	if saleID != uuid.Nil {
		p.SaleID = &saleID
	}
	if itemID != uuid.Nil {
		p.ItemID = &itemID
	}
	rows, err := store.New(tx).FlashActiveRemaining(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("đọc số suất còn trống: %w", err)
	}
	out := make([]FlashItemRemaining, len(rows))
	for i, r := range rows {
		out[i] = FlashItemRemaining{ItemID: r.ID, SaleID: r.FlashSaleID, Available: int(r.Available)}
	}
	return out, nil
}
