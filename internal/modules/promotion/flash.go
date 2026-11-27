package promotion

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
)

// Đợt flash sale (kế hoạch M5, Task 199 / M5-16; ADR-40): nhân viên soạn đợt và món ở trạng thái nháp. Lên lịch (giữ
// suất, trải đơn vị), mở, đóng ở Task 200; lượt mua ở Task 201. Mọi hàm chạy trong tx tenant của bên gọi; audit do bên
// gọi ghi cùng tx (promotion không nhập identity).

// Trạng thái của đợt (cột flash_sales.status).
const (
	FlashDraft     = "draft"
	FlashScheduled = "scheduled"
	FlashOpen      = "open"
	FlashClosed    = "closed"
)

// Giới hạn của đợt và món (khớp CHECK của bảng, migration 0532).
const (
	FlashMaxLength       = 7 * 24 * time.Hour
	flashMaxQuantity     = 20000
	flashMaxPerCustomer  = 10
	flashMinHoldMinutes  = 5
	flashMaxHoldMinutes  = 30
	flashItemVariantKey  = "flash_sale_items_variant_key"
	flashSaleNotEditable = "đợt flash sale không còn nháp"
)

var (
	// ErrNotEditable báo: đợt (hay món của đợt) không còn ở trạng thái nháp nên không sửa được.
	ErrNotEditable = errors.New("promotion: " + flashSaleNotEditable)
	// ErrNotADiscount báo: giá flash không nhỏ hơn giá bán thường của biến thể (QĐ24).
	ErrNotADiscount = errors.New("promotion: giá flash không thấp hơn giá thường")
	// ErrFlashItemExists báo: biến thể đã có trong đợt.
	ErrFlashItemExists = errors.New("promotion: biến thể đã có trong đợt")
	// ErrFlashWindow báo: giờ kết thúc không sau giờ bắt đầu, hay đợt dài quá 7 ngày.
	ErrFlashWindow = errors.New("promotion: thời gian đợt flash sale không hợp lệ")
	// ErrVariantNotForSale báo: biến thể không có hay không bán được (mẫu, biến thể ngừng bán, mẫu đã xóa).
	ErrVariantNotForSale = errors.New("promotion: biến thể không bán được")
)

// FlashSaleInput là nội dung đợt nhân viên nhập (tạo, sửa).
type FlashSaleInput struct {
	Name             string
	StartsAt, EndsAt time.Time
}

// FlashItemInput là nội dung món nhân viên nhập. VariantID chỉ dùng khi thêm (sửa món không đổi biến thể).
type FlashItemInput struct {
	VariantID                               uuid.UUID
	SalePrice                               int64
	Quantity, PerCustomerLimit, HoldMinutes int
}

// FlashSale là một đợt. ItemCount có ở danh sách và khi đọc một đợt; Items chỉ có khi đọc một đợt (GetFlashSale).
type FlashSale struct {
	ID                   uuid.UUID
	Name                 string
	StartsAt, EndsAt     time.Time
	Status, ClosedReason string
	CreatedBy            uuid.UUID
	CreatedAt, UpdatedAt time.Time
	Version              int
	ItemCount            int
	Items                []FlashItem
}

// FlashItem là một món của đợt. SKU, Name (tên bán "<mẫu> - <màu> / <cỡ>"), ListPrice (giá thường hiện tại) đọc từ
// biến thể.
type FlashItem struct {
	ID, FlashSaleID, SellerID, VariantID, WarehouseID uuid.UUID
	SalePrice                                         int64
	Quantity, PerCustomerLimit, HoldMinutes           int
	Version                                           int
	SKU, Name                                         string
	ListPrice                                         int64
}

// FlashSaleChange là đợt trước và sau một lần sửa (cho audit).
type FlashSaleChange struct{ Before, After FlashSale }

// FlashItemChange là món trước và sau một lần sửa (cho audit).
type FlashItemChange struct{ Before, After FlashItem }

// FlashSaleFilter lọc danh sách: Status rỗng là mọi trạng thái; After con trỏ trang trước; Limit ngoài 1–100 lấy 50.
type FlashSaleFilter struct {
	Status string
	After  *uuid.UUID
	Limit  int
}

// AuditFields là phần của đợt ghi vào audit.
func (s FlashSale) AuditFields() map[string]any {
	return map[string]any{
		"name": s.Name, "startsAt": s.StartsAt.UTC(), "endsAt": s.EndsAt.UTC(), "status": s.Status, "version": s.Version,
	}
}

// AuditFields là phần của món ghi vào audit.
func (i FlashItem) AuditFields() map[string]any {
	return map[string]any{
		"flashSaleId": i.FlashSaleID.String(), "variantId": i.VariantID.String(), "salePrice": i.SalePrice,
		"quantity": i.Quantity, "perCustomerLimit": i.PerCustomerLimit, "holdMinutes": i.HoldMinutes, "version": i.Version,
	}
}

// CreateFlashSale tạo đợt nháp. Tên sai → ErrInvalidInput; thời gian sai → ErrFlashWindow.
func CreateFlashSale(ctx context.Context, tx pgx.Tx, a Actor, in FlashSaleInput) (FlashSale, error) {
	in, err := checkFlashSaleInput(in)
	if err != nil {
		return FlashSale{}, err
	}
	row, err := store.New(tx).InsertFlashSale(ctx, store.InsertFlashSaleParams{
		Name: in.Name, StartsAt: in.StartsAt, EndsAt: in.EndsAt, CreatedBy: a.UserID,
	})
	if err != nil {
		return FlashSale{}, fmt.Errorf("ghi đợt flash sale: %w", err)
	}
	return flashSaleFrom(store.FlashSaleByIDRow(row)), nil
}

// UpdateFlashSale sửa tên, thời gian của đợt id khi version còn khớp và đợt còn nháp (khóa dòng trước). Không có →
// ErrNotFound; version cũ → ErrVersionConflict; không còn nháp → ErrNotEditable.
func UpdateFlashSale(ctx context.Context, tx pgx.Tx, id uuid.UUID, in FlashSaleInput, version int) (FlashSaleChange, error) {
	in, err := checkFlashSaleInput(in)
	if err != nil {
		return FlashSaleChange{}, err
	}
	q := store.New(tx)
	before, err := lockDraftSale(ctx, q, id)
	if err != nil {
		return FlashSaleChange{}, err
	}
	if before.Version != version {
		return FlashSaleChange{}, ErrVersionConflict
	}
	row, err := q.UpdateFlashSale(ctx, store.UpdateFlashSaleParams{
		Name: in.Name, StartsAt: in.StartsAt, EndsAt: in.EndsAt, ID: id,
		Version: int32(version), //nolint:gosec // G115: version đã khớp dòng (int4)
	})
	if err != nil {
		return FlashSaleChange{}, fmt.Errorf("sửa đợt flash sale: %w", err)
	}
	return FlashSaleChange{Before: before, After: flashSaleFrom(store.FlashSaleByIDRow(row))}, nil
}

// AddFlashItem thêm món vào đợt nháp saleID: biến thể bán được (người bán lấy từ biến thể), giá flash nhỏ hơn giá
// thường, kho là kho mặc định của shop. Đợt không có → ErrNotFound; không nháp → ErrNotEditable; biến thể không có hay
// không bán được → ErrVariantNotForSale; giá không giảm → ErrNotADiscount; biến thể đã có → ErrFlashItemExists; số
// ngoài giới hạn → ErrInvalidInput.
func AddFlashItem(ctx context.Context, tx pgx.Tx, saleID uuid.UUID, in FlashItemInput) (FlashItem, error) {
	if err := checkFlashItemInput(in); err != nil {
		return FlashItem{}, err
	}
	q := store.New(tx)
	if _, err := lockDraftSale(ctx, q, saleID); err != nil {
		return FlashItem{}, err
	}
	sv, err := flashVariant(ctx, tx, q, in.VariantID, in.SalePrice)
	if err != nil {
		return FlashItem{}, err
	}
	wh, err := inventory.DefaultWarehouse(ctx, tx)
	if err != nil {
		return FlashItem{}, fmt.Errorf("kho của món flash sale: %w", err)
	}
	row, err := q.InsertFlashItem(ctx, store.InsertFlashItemParams{
		FlashSaleID: saleID, SellerID: sv.SellerID, VariantID: sv.ID, WarehouseID: wh, SalePrice: in.SalePrice,
		Quantity: int32(in.Quantity), PerCustomerLimit: int32(in.PerCustomerLimit), //nolint:gosec // G115: đã kiểm giới hạn
		HoldMinutes: int32(in.HoldMinutes), //nolint:gosec // G115: 5–30
	})
	if isConstraint(err, flashItemVariantKey) {
		return FlashItem{}, ErrFlashItemExists
	}
	if err != nil {
		return FlashItem{}, fmt.Errorf("ghi món flash sale: %w", err)
	}
	it := flashItemFrom(store.FlashItemForUpdateRow(row))
	it.SKU, it.Name, it.ListPrice = sv.SKU, sv.Name, sv.Price
	return it, nil
}

// UpdateFlashItem sửa giá, số suất, giới hạn mỗi khách, phút giữ của món itemID trong đợt nháp saleID khi version của
// món còn khớp (khóa đợt rồi món). Giá kiểm lại với giá thường hiện tại. Lỗi như AddFlashItem; món không có →
// ErrNotFound; version cũ → ErrVersionConflict.
func UpdateFlashItem(ctx context.Context, tx pgx.Tx, saleID, itemID uuid.UUID, in FlashItemInput, version int) (
	FlashItemChange, error,
) {
	if err := checkFlashItemInput(in); err != nil {
		return FlashItemChange{}, err
	}
	q := store.New(tx)
	before, err := lockDraftItem(ctx, q, saleID, itemID)
	if err != nil {
		return FlashItemChange{}, err
	}
	if before.Version != version {
		return FlashItemChange{}, ErrVersionConflict
	}
	sv, err := flashVariant(ctx, tx, q, before.VariantID, in.SalePrice)
	if err != nil {
		return FlashItemChange{}, err
	}
	row, err := q.UpdateFlashItem(ctx, store.UpdateFlashItemParams{
		SalePrice: in.SalePrice, Quantity: int32(in.Quantity), //nolint:gosec // G115: đã kiểm giới hạn
		PerCustomerLimit: int32(in.PerCustomerLimit), HoldMinutes: int32(in.HoldMinutes), //nolint:gosec // G115: đã kiểm
		ID: itemID, Version: int32(version), //nolint:gosec // G115: version đã khớp dòng
	})
	if err != nil {
		return FlashItemChange{}, fmt.Errorf("sửa món flash sale: %w", err)
	}
	after := flashItemFrom(store.FlashItemForUpdateRow(row))
	after.SKU, after.Name, after.ListPrice = sv.SKU, sv.Name, sv.Price
	before.SKU, before.Name, before.ListPrice = sv.SKU, sv.Name, sv.Price
	return FlashItemChange{Before: before, After: after}, nil
}

// RemoveFlashItem bỏ món itemID khỏi đợt nháp saleID (khóa đợt rồi món); trả món đã bỏ. Không có → ErrNotFound; đợt
// không nháp → ErrNotEditable.
func RemoveFlashItem(ctx context.Context, tx pgx.Tx, saleID, itemID uuid.UUID) (FlashItem, error) {
	q := store.New(tx)
	before, err := lockDraftItem(ctx, q, saleID, itemID)
	if err != nil {
		return FlashItem{}, err
	}
	if _, err := q.DeleteFlashItem(ctx, store.DeleteFlashItemParams{ID: itemID, FlashSaleID: saleID}); err != nil {
		return FlashItem{}, fmt.Errorf("bỏ món flash sale: %w", err)
	}
	return before, nil
}

// GetFlashSale đọc đợt id kèm món (theo thứ tự thêm); không có → ErrNotFound.
func GetFlashSale(ctx context.Context, tx pgx.Tx, id uuid.UUID) (FlashSale, error) {
	q := store.New(tx)
	row, err := q.FlashSaleByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return FlashSale{}, ErrNotFound
	}
	if err != nil {
		return FlashSale{}, fmt.Errorf("đọc đợt flash sale: %w", err)
	}
	s := flashSaleFrom(row)
	rows, err := q.FlashItemsBySale(ctx, id)
	if err != nil {
		return FlashSale{}, fmt.Errorf("đọc món flash sale: %w", err)
	}
	s.Items = make([]FlashItem, 0, len(rows))
	for _, r := range rows {
		it := flashItemFrom(store.FlashItemForUpdateRow{
			ID: r.ID, FlashSaleID: r.FlashSaleID, SellerID: r.SellerID, VariantID: r.VariantID, WarehouseID: r.WarehouseID,
			SalePrice: r.SalePrice, Quantity: r.Quantity, PerCustomerLimit: r.PerCustomerLimit, HoldMinutes: r.HoldMinutes,
			Version: r.Version,
		})
		it.SKU, it.Name, it.ListPrice = r.Sku, r.ProductName+" - "+r.Color+" / "+r.Size, r.ListPrice
		s.Items = append(s.Items, it)
	}
	s.ItemCount = len(s.Items)
	return s, nil
}

// ListFlashSales trả một trang đợt (mới nhất trước) và con trỏ trang sau (nil: hết). Status lạ → ErrInvalidInput.
func ListFlashSales(ctx context.Context, tx pgx.Tx, f FlashSaleFilter) ([]FlashSale, *uuid.UUID, error) {
	limit := pageLimit(f.Limit)
	p := store.ListFlashSalesParams{After: f.After, RowLimit: int32(limit + 1)} //nolint:gosec // G115: ≤ 101
	switch f.Status {
	case "":
	case FlashDraft, FlashScheduled, FlashOpen, FlashClosed:
		p.Status.String, p.Status.Valid = f.Status, true
	default:
		return nil, nil, fmt.Errorf("trạng thái đợt: %w", ErrInvalidInput)
	}
	rows, err := store.New(tx).ListFlashSales(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách đợt flash sale: %w", err)
	}
	out := make([]FlashSale, 0, min(len(rows), limit))
	for _, r := range rows[:min(len(rows), limit)] {
		s := flashSaleFrom(store.FlashSaleByIDRow{
			ID: r.ID, Name: r.Name, StartsAt: r.StartsAt, EndsAt: r.EndsAt, Status: r.Status, ClosedReason: r.ClosedReason,
			CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version,
		})
		s.ItemCount = int(r.ItemCount)
		out = append(out, s)
	}
	if len(rows) > limit {
		next := out[len(out)-1].ID
		return out, &next, nil
	}
	return out, nil, nil
}

// checkFlashSaleInput chuẩn hóa (trim tên) và kiểm in.
func checkFlashSaleInput(in FlashSaleInput) (FlashSaleInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if !validName(in.Name) {
		return in, fmt.Errorf("tên đợt flash sale: %w", ErrInvalidInput)
	}
	if !in.StartsAt.Before(in.EndsAt) || in.EndsAt.Sub(in.StartsAt) > FlashMaxLength {
		return in, ErrFlashWindow
	}
	return in, nil
}

func checkFlashItemInput(in FlashItemInput) error {
	switch {
	case in.SalePrice <= 0:
		return fmt.Errorf("giá flash: %w", ErrInvalidInput)
	case in.Quantity < 1 || in.Quantity > flashMaxQuantity:
		return fmt.Errorf("số suất: %w", ErrInvalidInput)
	case in.PerCustomerLimit < 1 || in.PerCustomerLimit > flashMaxPerCustomer:
		return fmt.Errorf("giới hạn mỗi khách: %w", ErrInvalidInput)
	case in.HoldMinutes < flashMinHoldMinutes || in.HoldMinutes > flashMaxHoldMinutes:
		return fmt.Errorf("phút giữ đơn: %w", ErrInvalidInput)
	}
	return nil
}

// flashVariant đọc biến thể variantID bán được (người bán của chính biến thể) và kiểm price nhỏ hơn giá thường.
func flashVariant(ctx context.Context, tx pgx.Tx, q *store.Queries, variantID uuid.UUID, price int64) (catalog.SaleVariant, error) {
	seller, err := q.VariantSeller(ctx, variantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.SaleVariant{}, ErrVariantNotForSale
	}
	if err != nil {
		return catalog.SaleVariant{}, fmt.Errorf("đọc người bán của biến thể: %w", err)
	}
	sv, err := catalog.VariantForSale(ctx, tx, seller, variantID)
	if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, catalog.ErrNotForSale) {
		return catalog.SaleVariant{}, ErrVariantNotForSale
	}
	if err != nil {
		return catalog.SaleVariant{}, fmt.Errorf("đọc biến thể của món flash sale: %w", err)
	}
	if price >= sv.Price {
		return catalog.SaleVariant{}, ErrNotADiscount
	}
	return sv, nil
}

// lockDraftSale khóa dòng đợt id; không có → ErrNotFound, không nháp → ErrNotEditable.
func lockDraftSale(ctx context.Context, q *store.Queries, id uuid.UUID) (FlashSale, error) {
	return lockSale(ctx, q, id, ErrNotEditable, FlashDraft)
}

// lockSale khóa dòng đợt id; không có → ErrNotFound, trạng thái ngoài statuses → errState.
func lockSale(ctx context.Context, q *store.Queries, id uuid.UUID, errState error, statuses ...string) (FlashSale, error) {
	row, err := q.FlashSaleByIDForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return FlashSale{}, ErrNotFound
	}
	if err != nil {
		return FlashSale{}, fmt.Errorf("khóa đợt flash sale: %w", err)
	}
	s := flashSaleFrom(store.FlashSaleByIDRow(row))
	if !slices.Contains(statuses, s.Status) {
		return FlashSale{}, errState
	}
	return s, nil
}

// lockDraftItem khóa đợt nháp saleID rồi món itemID của nó (ADR-46 nhóm 10); món không có → ErrNotFound.
func lockDraftItem(ctx context.Context, q *store.Queries, saleID, itemID uuid.UUID) (FlashItem, error) {
	if _, err := lockDraftSale(ctx, q, saleID); err != nil {
		return FlashItem{}, err
	}
	row, err := q.FlashItemForUpdate(ctx, store.FlashItemForUpdateParams{ID: itemID, FlashSaleID: saleID})
	if errors.Is(err, pgx.ErrNoRows) {
		return FlashItem{}, ErrNotFound
	}
	if err != nil {
		return FlashItem{}, fmt.Errorf("khóa món flash sale: %w", err)
	}
	return flashItemFrom(row), nil
}

func isConstraint(err error, name string) bool {
	pg, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pg.Code == uniqueViolation && pg.ConstraintName == name
}

func flashSaleFrom(r store.FlashSaleByIDRow) FlashSale {
	return FlashSale{
		ID: r.ID, Name: r.Name, StartsAt: r.StartsAt, EndsAt: r.EndsAt, Status: r.Status, ClosedReason: r.ClosedReason.String,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: int(r.Version),
	}
}

func flashItemFrom(r store.FlashItemForUpdateRow) FlashItem {
	return FlashItem{
		ID: r.ID, FlashSaleID: r.FlashSaleID, SellerID: r.SellerID, VariantID: r.VariantID, WarehouseID: r.WarehouseID,
		SalePrice: r.SalePrice, Quantity: int(r.Quantity), PerCustomerLimit: int(r.PerCustomerLimit),
		HoldMinutes: int(r.HoldMinutes), Version: int(r.Version),
	}
}
