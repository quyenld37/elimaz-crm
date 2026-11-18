// Package catalog là mẫu sản phẩm, biến thể màu/size, giá và người bán (kế hoạch M1, Task M1-27; spec 05 mục 2,
// ADR-29). Là module mẫu cho module có chiều người bán: mọi hàm đọc nhận sellerID, sửa có version, module khác đọc qua
// view catalog.*_v.
//
// Phần công khai: hàm đọc (GetProduct, VariantForSale, VariantsForSale, DefaultSeller, EnsureDefaultSeller), bài viết
// (DecodeArticle, RenderArticleHTML — bộ dựng HTML duy nhất của CRM, R7) và Writer (mọi đường ghi: tự ghi sự kiện,
// báo hiệu, audit cùng transaction).
//
// Khi nào không dùng: module khác không đọc bảng catalog.* (đọc view catalog.*_v); không dựng HTML bài viết ở chỗ
// khác (dùng RenderArticleHTML); không ghi mẫu bằng SQL ngoài Writer (mất sự kiện product.updated và audit).
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/article"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// ProductStatus là trạng thái mẫu: draft (đang nhập, ẩn với khách), active, discontinued (ngừng bán, KH12).
type ProductStatus string

// Trạng thái mẫu và biến thể.
const (
	StatusDraft        ProductStatus = "draft"
	StatusActive       ProductStatus = "active"
	StatusDiscontinued ProductStatus = "discontinued"

	VariantActive       = "active"
	VariantDiscontinued = "discontinued"
)

// DefaultSellerCode là mã người bán của chính shop trong dữ liệu khởi tạo (ADR-29).
const DefaultSellerCode = "ELIMAZ"

// MaxMoney là giá lớn nhất (2^53 − 1 đồng, số nguyên lớn nhất JavaScript đọc đúng).
const MaxMoney int64 = 9007199254740991

// ArticleBlock là một khối bài viết (bí danh công khai: gói article là internal).
type ArticleBlock = article.Block

// ArticleImage là các biến thể ảnh của khối image (điền lúc dựng HTML, Task M1-44).
type ArticleImage = article.Image

// ArticleImageVariant là một biến thể ảnh: chiều rộng, định dạng (avif, webp, jpeg), đường dẫn https.
type ArticleImageVariant = article.ImageVariant

// VariantInput là một biến thể khi tạo. Barcode rỗng → CRM sinh EAN-13 tiền tố 2; ColorHex rỗng → không có;
// CostPrice nil → không có giá nhập. SKU chuẩn hóa viết hoa.
type VariantInput struct {
	SKU, Barcode, Color, ColorHex, Size string
	Price                               int64
	CostPrice                           *int64
}

// ProductInput là một mẫu khi tạo. Status rỗng → draft. Article rỗng → bài viết rỗng.
type ProductInput struct {
	SellerID                uuid.UUID
	Code, Name, Description string
	Article                 json.RawMessage
	Status                  ProductStatus
	Variants                []VariantInput
}

// ProductPatch là phần sửa của mẫu; trường nil giữ nguyên.
type ProductPatch struct {
	Name, Description *string
	Article           *json.RawMessage
	Status            *ProductStatus
}

// VariantPatch là phần sửa của biến thể; trường nil giữ nguyên. ColorHex "" bỏ màu hex; ClearCostPrice bỏ giá nhập
// (thắng CostPrice).
type VariantPatch struct {
	SKU, Barcode, Color, ColorHex, Size *string
	Price, CostPrice                    *int64
	ClearCostPrice                      bool
	Status                              *string
}

// Product là một mẫu kèm biến thể (theo thứ tự tạo).
type Product struct {
	ID, SellerID            uuid.UUID
	Code, Name, Description string
	Article                 []ArticleBlock
	Status                  ProductStatus
	Version                 int64
	Variants                []Variant
}

// Variant là một biến thể. ColorHex rỗng là không có.
type Variant struct {
	ID, SellerID, ProductID             uuid.UUID
	SKU, Barcode, Color, ColorHex, Size string
	Price                               int64
	CostPrice                           *int64
	Status                              string
	Version                             int64
}

// SaleVariant là biến thể bán được cho dòng đơn: Name = "<tên mẫu> - <màu> / <size>".
type SaleVariant struct {
	ID, SellerID, ProductID uuid.UUID
	SKU, Name               string
	Price                   int64
}

// Writer là đường ghi của catalog: mỗi lần ghi (trong tx của bên gọi, tenancy.WithTenant hay httpapi.WriteTx) ghi
// kèm sự kiện (Events.Emit qua Queue), báo hiệu realtime (Realtime nil → bỏ qua: báo hiệu được phép mất) và dòng audit
// (RequestID của yêu cầu, rỗng khi không có). Dựng mới cho mỗi yêu cầu; không giữ trạng thái giữa các lần chạy lại.
type Writer struct {
	Events    *events.Registry
	Queue     queue.JobQueue
	Realtime  realtime.Bus
	RequestID string
}

var errWriterNotWired = errors.New("catalog: Writer thiếu Events hoặc Queue")

func (w Writer) check() error {
	if w.Events == nil || w.Queue == nil {
		return errWriterNotWired
	}
	return nil
}

// DefaultSeller trả người bán của chính shop (kind own, active, mã nhỏ nhất) trong ngữ cảnh của tx; không có →
// ErrNotFound.
func DefaultSeller(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	id, err := store.New(tx).GetDefaultSeller(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc người bán mặc định: %w", err)
	}
	return id, nil
}

// EnsureDefaultSeller tạo người bán ELIMAZ (kind own) của tenantID nếu chưa có và trả mã của nó. Chạy bằng chủ bảng
// (seed-dev; crm_app không ghi được sellers), trong tenancy.WithTenant của tenantID. Chạy lại được.
func EnsureDefaultSeller(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (uuid.UUID, error) {
	q := store.New(tx)
	if err := q.InsertSellerIfMissing(ctx, store.InsertSellerIfMissingParams{
		TenantID: tenantID, Code: DefaultSellerCode, Name: DefaultSellerCode,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("ghi người bán mặc định: %w", err)
	}
	id, err := q.GetSellerByCode(ctx, DefaultSellerCode)
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc người bán mặc định: %w", err)
	}
	return id, nil
}
