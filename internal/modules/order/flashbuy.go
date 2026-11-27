package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
)

// Lõi lượt mua flash sale (kế hoạch M5, Task 201 / M5-18; ADR-40): một transaction của bên gọi (RetryTx với
// db.TxFlashBuy) giữ giới hạn mỗi khách (promotion), lấy đơn vị suất bằng SKIP LOCKED (inventory) và tạo đơn nháp có hạn
// (order). Thứ tự khóa (ADR-46): flash_sale_claims (nhóm 10) → flash_sale_units (nhóm 11) → dòng mới; không khóa
// flash_sales, không sửa stock_balances (phần held của suất đã cộng lúc lên lịch).

// Kết quả của lượt mua (FlashBuyResult.Result).
const (
	FlashBought    = "bought"
	FlashSoldOut   = "sold_out"
	FlashOverLimit = "over_limit"
	FlashNotOpen   = "not_open"

	// maxFlashQty là số lượng tối đa một lượt mua (giới hạn mỗi khách của món tối đa 10, migration 0532).
	maxFlashQty = 10
)

// ErrFlashRetry báo: thiếu đơn vị suất trống vì tx khác đang khóa chúng (lượt mua chưa xong). Bên gọi lùi transaction,
// không lưu gì, và cho thử lại.
var ErrFlashRetry = errors.New("order: đơn vị suất flash sale đang bị khóa, thử lại")

// errFlashRefTaken: câu chèn đơn đụng unique của mã ngoài hay khóa nghiệp vụ (lượt cùng mã ngoài của tx khác vừa
// commit).
var errFlashRefTaken = errors.New("order: mã ngoài của lượt mua đã có đơn")

// FlashBuyInput là một lượt mua: món, khách (đã xác định ở lớp gọi), số lượng, mã đơn của website (khóa nghiệp vụ), giờ
// của lượt.
type FlashBuyInput struct {
	ItemID, CustomerID uuid.UUID
	Qty                int
	ExternalRef        string
	Now                time.Time
}

// FlashBuyResult là kết quả lượt mua: Result là một trong FlashBought, FlashSoldOut, FlashOverLimit, FlashNotOpen; đơn
// nháp (mã, mã đơn, hạn giữ) chỉ có khi bought.
type FlashBuyResult struct {
	Result      string
	OrderID     uuid.UUID
	Code        string
	ExternalRef string
	ExpiresAt   time.Time
}

// BuyFlash chạy một lượt mua trong tx của bên gọi, theo thứ tự:
//
//  1. Kiểm đầu vào (ErrInvalidInput). promotion.GetFlashItemForBuy (không có → promotion.ErrNotFound); đợt không ở
//     scheduled/open hay Now ngoài [StartsAt, EndsAt) → not_open (QĐ8); Qty > giới hạn mỗi khách → over_limit.
//  2. Đã có đơn web của ExternalRef: đơn flash của món này → bought với đơn đó (không ghi gì); đơn khác →
//     ErrDuplicateOrder. Đọc không khóa nguồn web, biến thể (ảnh chụp mã, tên; không bán được → ErrNotForSale).
//  3. promotion.Claim; vượt giới hạn → đọc lại đơn theo ExternalRef (lượt cùng mã vừa commit), không có → over_limit.
//  4. inventory.TakeFlashUnits; thiếu → promotion.Unclaim rồi: không còn đơn vị trống nào khác → sold_out; còn (đang bị
//     tx khác khóa) → ErrFlashRetry.
//  5. Đơn draft (insertFlashDraft, savepoint): mã từ nextCode, khóa nghiệp vụ như đơn web, kênh web, nguồn web, kho của
//     món, hạn Now + HoldMinutes, một dòng đơn giá flash; cùng ExternalRef vừa được tx khác tạo → lùi savepoint,
//     Unclaim, trả đơn đó như bước 2. Lịch sử, order.created, báo hiệu qua recordCreated.
//  6. inventory.HoldFlashUnits (giữ hàng held hạn như đơn, đơn vị → held). 7. bought.
//
// Không giữ trạng thái giữa các lần chạy lại; không gọi mạng. Audit (nếu cần) do bên gọi ghi.
func (w Writer) BuyFlash(ctx context.Context, tx pgx.Tx, in FlashBuyInput, a Actor) (FlashBuyResult, error) {
	if _, err := w.stock(); err != nil {
		return FlashBuyResult{}, err
	}
	in.ExternalRef = strings.TrimSpace(in.ExternalRef)
	if in.ItemID == uuid.Nil || in.CustomerID == uuid.Nil || in.Qty < 1 || in.Qty > maxFlashQty || in.Now.IsZero() ||
		in.ExternalRef == "" || !optText(in.ExternalRef, maxExternalRefRunes) {
		return FlashBuyResult{}, fmt.Errorf("%w: lượt mua flash sale", ErrInvalidInput)
	}
	now := in.Now.UTC()
	it, err := promotion.GetFlashItemForBuy(ctx, tx, in.ItemID)
	if err != nil {
		return FlashBuyResult{}, fmt.Errorf("món flash sale: %w", err)
	}
	if (it.SaleStatus != promotion.FlashScheduled && it.SaleStatus != promotion.FlashOpen) || now.Before(it.StartsAt) ||
		!now.Before(it.EndsAt) {
		return FlashBuyResult{Result: FlashNotOpen, ExternalRef: in.ExternalRef}, nil
	}
	if in.Qty > it.PerCustomerLimit {
		return FlashBuyResult{Result: FlashOverLimit, ExternalRef: in.ExternalRef}, nil
	}
	if res, found, err := flashOrderByRef(ctx, tx, it, in.ExternalRef); found || err != nil {
		return res, err
	}
	src, err := SourceByKey(ctx, tx, SourceKeyWeb)
	if err != nil {
		return FlashBuyResult{}, err
	}
	sale, err := catalog.VariantsForSale(ctx, tx, it.SellerID, []uuid.UUID{it.VariantID})
	if err != nil {
		return FlashBuyResult{}, fmt.Errorf("biến thể của món flash sale: %w", err)
	}
	variant, ok := sale[it.VariantID]
	if !ok {
		return FlashBuyResult{}, ErrNotForSale
	}
	return w.buyFlashLocked(ctx, tx, flashBuy{in: in, it: it, source: src, variant: variant, a: a, now: now})
}

// flashBuy là một lượt mua đã qua bước đọc không khóa (BuyFlash bước 1–2).
type flashBuy struct {
	in      FlashBuyInput
	it      promotion.FlashItemForBuy
	source  uuid.UUID
	variant catalog.SaleVariant
	a       Actor
	now     time.Time
}

// buyFlashLocked là bước 3–7 của BuyFlash (khóa claim, đơn vị, ghi).
func (w Writer) buyFlashLocked(ctx context.Context, tx pgx.Tx, b flashBuy) (FlashBuyResult, error) {
	in, it := b.in, b.it
	claimed, err := promotion.Claim(ctx, tx, it.ItemID, in.CustomerID, in.Qty, it.PerCustomerLimit)
	if err != nil {
		return FlashBuyResult{}, fmt.Errorf("giới hạn mỗi khách: %w", err)
	}
	if !claimed {
		if res, found, err := flashOrderByRef(ctx, tx, it, in.ExternalRef); found || err != nil {
			return res, err
		}
		return FlashBuyResult{Result: FlashOverLimit, ExternalRef: in.ExternalRef}, nil
	}
	ids, anyAvailable, err := inventory.TakeFlashUnits(ctx, tx, it.ItemID, in.Qty)
	if err != nil {
		return FlashBuyResult{}, fmt.Errorf("lấy suất flash sale: %w", err)
	}
	if len(ids) < in.Qty {
		if err := promotion.Unclaim(ctx, tx, it.ItemID, in.CustomerID, in.Qty); err != nil {
			return FlashBuyResult{}, fmt.Errorf("trả giới hạn mỗi khách: %w", err)
		}
		if anyAvailable {
			return FlashBuyResult{}, ErrFlashRetry
		}
		return FlashBuyResult{Result: FlashSoldOut, ExternalRef: in.ExternalRef}, nil
	}
	// Hạn làm tròn tới micro giây (độ chính xác của timestamptz): lượt lặp cùng mã ngoài đọc lại đúng giá trị này.
	expires := b.now.Add(time.Duration(it.HoldMinutes) * time.Minute).Truncate(time.Microsecond)
	var d flashDraft
	err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		var err error
		d, err = insertFlashDraft(ctx, sp, b, expires)
		return err
	})
	if errors.Is(err, errFlashRefTaken) {
		return sameRefOrder(ctx, tx, b)
	}
	if err != nil {
		return FlashBuyResult{}, fmt.Errorf("tạo đơn nháp flash sale: %w", err)
	}
	o, err := Get(ctx, tx, it.SellerID, d.orderID)
	if err != nil {
		return FlashBuyResult{}, err
	}
	if err := w.recordCreated(ctx, tx, o, b.a, b.now); err != nil {
		return FlashBuyResult{}, err
	}
	if err := inventory.HoldFlashUnits(ctx, tx, ids, d.itemID, it.VariantID, it.WarehouseID, in.Qty, expires); err != nil {
		return FlashBuyResult{}, fmt.Errorf("giữ suất flash sale: %w", err)
	}
	return FlashBuyResult{
		Result: FlashBought, OrderID: o.ID, Code: o.Code, ExternalRef: in.ExternalRef, ExpiresAt: expires,
	}, nil
}

// sameRefOrder: lượt cùng ExternalRef của tx khác vừa tạo đơn (câu chèn đụng unique, savepoint đã lùi) → trả claim
// của lượt này và trả đơn đó (bước 2). Không đọc thấy đơn (không xảy ra: unique chỉ báo khi tx kia đã commit) →
// ErrFlashRetry.
func sameRefOrder(ctx context.Context, tx pgx.Tx, b flashBuy) (FlashBuyResult, error) {
	if err := promotion.Unclaim(ctx, tx, b.it.ItemID, b.in.CustomerID, b.in.Qty); err != nil {
		return FlashBuyResult{}, fmt.Errorf("trả giới hạn mỗi khách: %w", err)
	}
	res, found, err := flashOrderByRef(ctx, tx, b.it, b.in.ExternalRef)
	if err != nil || found {
		return res, err
	}
	return FlashBuyResult{}, ErrFlashRetry
}

// flashOrderByRef: đơn web đã có của mã ngoài ref → (bought với đơn đó, true) khi là đơn flash của món it; đơn khác →
// ErrDuplicateOrder.
func flashOrderByRef(ctx context.Context, tx pgx.Tx, it promotion.FlashItemForBuy, ref string) (FlashBuyResult, bool, error) {
	r, err := store.New(tx).FlashOrderByExternalRef(ctx, store.FlashOrderByExternalRefParams{
		SellerID: it.SellerID, ExternalRef: pgtype.Text{String: ref, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return FlashBuyResult{}, false, nil
	}
	if err != nil {
		return FlashBuyResult{}, false, fmt.Errorf("đọc đơn flash theo mã ngoài: %w", err)
	}
	if r.FlashSaleItemID != it.ItemID {
		return FlashBuyResult{}, false, fmt.Errorf("%w: mã ngoài đã dùng cho đơn khác", ErrDuplicateOrder)
	}
	res := FlashBuyResult{Result: FlashBought, OrderID: r.ID, Code: r.Code, ExternalRef: ref}
	if r.DraftExpiresAt.Valid {
		res.ExpiresAt = r.DraftExpiresAt.Time.UTC()
	}
	return res, true, nil
}

// flashDraft là đơn nháp vừa chèn của lượt mua: mã đơn, mã dòng đơn.
type flashDraft struct {
	orderID, itemID uuid.UUID
}

// insertFlashDraft chèn đơn draft và một dòng đơn flash của lượt mua b (không qua Create: Create giữ hàng bằng
// inventory.Reserve, làm held tăng lần nữa). Đụng unique mã ngoài hay khóa nghiệp vụ → errFlashRefTaken.
func insertFlashDraft(ctx context.Context, tx pgx.Tx, b flashBuy, expires time.Time) (flashDraft, error) {
	total := b.it.SalePrice * int64(b.in.Qty)
	if b.it.SalePrice <= 0 || !money(total) || total/int64(b.in.Qty) != b.it.SalePrice {
		return flashDraft{}, ErrTotalOverflow
	}
	code, err := nextCode(ctx, tx)
	if err != nil {
		return flashDraft{}, err
	}
	orderID, err := uuid.NewV7()
	if err != nil {
		return flashDraft{}, fmt.Errorf("sinh mã đơn: %w", err)
	}
	itemID, err := uuid.NewV7()
	if err != nil {
		return flashDraft{}, fmt.Errorf("sinh mã dòng đơn: %w", err)
	}
	q := store.New(tx)
	err = q.InsertOrder(ctx, store.InsertOrderParams{
		ID: orderID, SellerID: b.it.SellerID, Code: code, CustomerID: b.in.CustomerID, Channel: ChannelWeb,
		IdempotencyKey: webIdempotencyKey(b.in.ExternalRef), Status: StatusDraft, ShipAddress: []byte("{}"),
		Subtotal: total, CodAmount: total, GrandTotal: total, CreatedBy: b.a.id(), Version: 1,
		WarehouseID: b.it.WarehouseID, TrafficSourceID: pgtype.UUID{Bytes: b.source, Valid: true},
		ExternalRef:    pgtype.Text{String: b.in.ExternalRef, Valid: true},
		DraftExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}, OrderType: OrderTypeShip, Now: b.now,
	})
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == pgUniqueViolate &&
		(pe.ConstraintName == idempotencyIdx || pe.ConstraintName == externalRefKey) {
		return flashDraft{}, errFlashRefTaken
	}
	if err != nil {
		return flashDraft{}, fmt.Errorf("ghi đơn flash sale: %w", err)
	}
	if err := q.InsertFlashOrderItem(ctx, store.InsertFlashOrderItemParams{
		ID: itemID, SellerID: b.it.SellerID, OrderID: orderID, VariantID: b.it.VariantID,
		SkuSnapshot: b.variant.SKU, NameSnapshot: b.variant.Name, Quantity: int32(b.in.Qty), //nolint:gosec // G115: Qty ≤ maxFlashQty
		UnitPrice: b.it.SalePrice, NetAmount: total, FlashSaleItemID: b.it.ItemID,
	}); err != nil {
		return flashDraft{}, fmt.Errorf("ghi dòng đơn flash sale: %w", err)
	}
	return flashDraft{orderID: orderID, itemID: itemID}, nil
}
