package webcatalog

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Giới hạn của các lời đọc (kế hoạch M5 Task 190): một trang tối đa MaxPage mẫu, đọc theo mã tối đa MaxIDs mã, luồng
// thay đổi tối đa MaxChanges mã.
const (
	MaxPage    = 200
	MaxIDs     = 100
	MaxChanges = 1000
)

// DefaultWarehouseCode là mã kho mặc định có tồn hiện trên website; trùng inventory.DefaultWarehouseCode (gói này không
// nhập module, test của internal/httpapi/web so hai hằng).
const DefaultWarehouseCode = "MAIN"

// WebProduct là một mẫu cho website. Không có giá nhập. ArticleHTML do bên gọi dựng từ Article bằng
// catalog.ArticleHTML của M1 (gói này không nhập catalog, không tự dựng HTML).
type WebProduct struct {
	ID, SellerID     uuid.UUID
	Code, Name       string
	ShortDescription string // cột description
	ArticleHTML      string
	Article          json.RawMessage `json:"-"` // cột article nguyên dạng, chỉ để dựng ArticleHTML
	Status           string          // draft | active | discontinued
	CategoryIDs      []uuid.UUID
	Tags             []string // mã nhãn
	Attributes       []WebAttr
	Images           []WebImage
	Variants         []WebVariant
	UpdatedAt        time.Time
	Version          int64
}

// WebAttr là một giá trị thuộc tính của mẫu; Position là vị trí của thuộc tính.
type WebAttr struct {
	Name, Value string
	Position    int
}

// WebImage là một ảnh đã xử lý xong của mẫu: các biến thể (chiều rộng, định dạng, đường dẫn) của bộ kích thước hiện
// hành. Color nil: ảnh chung mọi màu.
type WebImage struct {
	Position int
	Color    *string
	Variants []WebImageVariant
}

// WebImageVariant là một biến thể của ảnh.
type WebImageVariant struct {
	Width  int
	Format string
	URL    string
}

// WebVariant là một biến thể cho website. Available: còn hàng có thể bán ở kho mặc định; WeightGrams nil là chưa nhập.
type WebVariant struct {
	ID                        uuid.UUID
	SKU, Barcode, Color, Size string
	Price                     int64
	Status                    string // active | discontinued
	Sellable, Available       bool
	WeightGrams               *int
}

// ImageView là cách dựng đường dẫn ảnh: BaseURL là FILES_PUBLIC_BASE_URL, Preset là IMAGE_PRESET_VERSION (0 → 1).
type ImageView struct {
	BaseURL string
	Preset  int
}

// WebTaxonomy là phân loại cho website: danh mục đang dùng (theo cấp, vị trí), thuộc tính kèm giá trị (theo vị trí),
// nhãn.
type WebTaxonomy struct {
	Categories []WebCategory
	Attributes []WebAttribute
	Tags       []WebTag
}

// WebCategory là một danh mục đang dùng; ParentID nil ở cấp 1.
type WebCategory struct {
	ID              uuid.UUID
	ParentID        *uuid.UUID
	Name, Slug      string
	Depth, Position int
}

// WebAttribute là một thuộc tính kèm giá trị.
type WebAttribute struct {
	ID         uuid.UUID
	Code, Name string
	Position   int
	Values     []WebAttributeValue
}

// WebAttributeValue là một giá trị của thuộc tính.
type WebAttributeValue struct {
	ID       uuid.UUID
	Value    string
	Position int
}

// WebTag là một nhãn.
type WebTag struct {
	Code, Name string
}
