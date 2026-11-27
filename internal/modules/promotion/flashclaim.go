package promotion

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
)

// Lượt mua flash sale, phần của promotion (kế hoạch M5, Task 201 / M5-18; ADR-40): đọc món để mua và giới hạn mỗi khách
// (flash_sale_claims, ADR-46 nhóm 10). Chạy trong tx tenant của bên gọi (order.Writer.BuyFlash).

// errFlashClaimMissing báo: Unclaim không gặp claim đủ số lượng để trả (bất biến hỏng hay gọi sai).
var errFlashClaimMissing = errors.New("promotion: không có số đã lấy để trả")

// FlashItemForBuy là món flash sale kèm trạng thái và giờ của đợt, đọc lúc mua.
type FlashItemForBuy struct {
	ItemID, SaleID, VariantID, SellerID, WarehouseID uuid.UUID
	SaleStatus                                       string
	StartsAt, EndsAt                                 time.Time
	SalePrice                                        int64
	PerCustomerLimit, HoldMinutes                    int
}

// GetFlashItemForBuy đọc món itemID và đợt của nó (không khóa). Không có → ErrNotFound.
func GetFlashItemForBuy(ctx context.Context, tx pgx.Tx, itemID uuid.UUID) (FlashItemForBuy, error) {
	r, err := store.New(tx).FlashItemForBuy(ctx, itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return FlashItemForBuy{}, ErrNotFound
	}
	if err != nil {
		return FlashItemForBuy{}, fmt.Errorf("đọc món flash sale: %w", err)
	}
	return FlashItemForBuy{
		ItemID: r.ID, SaleID: r.FlashSaleID, VariantID: r.VariantID, SellerID: r.SellerID, WarehouseID: r.WarehouseID,
		SaleStatus: r.Status, StartsAt: r.StartsAt, EndsAt: r.EndsAt, SalePrice: r.SalePrice,
		PerCustomerLimit: int(r.PerCustomerLimit), HoldMinutes: int(r.HoldMinutes),
	}, nil
}

// Claim cộng qty vào số khách customerID đã lấy của món itemID khi tổng mới ≤ limit (một câu upsert có điều kiện); vượt
// → false, không ghi. qty, limit ngoài 1–MaxInt32 → ErrInvalidInput.
func Claim(ctx context.Context, tx pgx.Tx, itemID, customerID uuid.UUID, qty, limit int) (bool, error) {
	if !claimQty(qty) || !claimQty(limit) {
		return false, fmt.Errorf("%w: số lượng lấy flash sale", ErrInvalidInput)
	}
	_, err := store.New(tx).ClaimFlash(ctx, store.ClaimFlashParams{
		ItemID: itemID, CustomerID: customerID, Qty: int32(qty), ClaimLimit: int32(limit), //nolint:gosec // G115: claimQty đã kiểm ≤ MaxInt32
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ghi số đã lấy flash sale: %w", err)
	}
	return true, nil
}

// Unclaim trả qty về số khách customerID đã lấy của món itemID; không có claim đủ qty → errFlashClaimMissing.
func Unclaim(ctx context.Context, tx pgx.Tx, itemID, customerID uuid.UUID, qty int) error {
	if !claimQty(qty) {
		return fmt.Errorf("%w: số lượng trả flash sale", ErrInvalidInput)
	}
	n, err := store.New(tx).UnclaimFlash(ctx, store.UnclaimFlashParams{
		ItemID: itemID, CustomerID: customerID, Qty: int32(qty), //nolint:gosec // G115: claimQty đã kiểm ≤ MaxInt32
	})
	if err != nil {
		return fmt.Errorf("trả số đã lấy flash sale: %w", err)
	}
	if n == 0 {
		return errFlashClaimMissing
	}
	return nil
}

func claimQty(n int) bool { return n >= 1 && n <= math.MaxInt32 }
