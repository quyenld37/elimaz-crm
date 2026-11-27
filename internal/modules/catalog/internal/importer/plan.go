package importer

import "github.com/google/uuid"

// Refs là dữ liệu có sẵn để kiểm tệp, bên gọi đọc trong ngữ cảnh tenant và người bán của lần nhập:
// Categories theo đường dẫn CategoryPath, Attributes theo tên thuộc tính rồi giá trị, Tags theo mã (chữ thường),
// ExistingSKUs theo SKU (viết hoa) của người bán, ExistingProducts theo mã mẫu (viết hoa) của người bán. Bên gọi chỉ
// cần nạp SKU và mã mẫu có trong tệp.
type Refs struct {
	Categories       map[string]uuid.UUID
	Attributes       map[string]map[string]uuid.UUID
	Tags             map[string]uuid.UUID
	ExistingSKUs     map[string]ExistingVariant
	ExistingProducts map[string]ExistingProduct
	// ExistingBarcodes: barcode → SKU của biến thể có sẵn của người bán, cho các barcode gõ tay có trong tệp (kể cả
	// biến thể có SKU không nằm trong tệp). Chỉ bước kiểm dùng; lúc áp unique của database chặn.
	ExistingBarcodes map[string]string
}

// Option là cặp màu, size của một biến thể (đúng chữ như đã lưu).
type Option struct {
	Color, Size string
}

// ExistingProduct là một mẫu đã có trong database: mã và SKU của từng biến thể có sẵn theo màu, size.
type ExistingProduct struct {
	ID      uuid.UUID
	Options map[Option]string
}

// ExistingVariant là một biến thể đã có trong database (ColorHex rỗng là không có; CostPrice nil là không có).
type ExistingVariant struct {
	ID, ProductID            uuid.UUID
	ProductCode              string
	Version                  int64
	Barcode, Color, ColorHex string
	Size                     string
	Price                    int64
	CostPrice                *int64
}

// Action là việc phải làm với một biến thể.
type Action string

// Các Action.
const (
	ActionCreate    Action = "create"    // SKU chưa có
	ActionUpdate    Action = "update"    // SKU đã có ở chính mẫu này, có trường khác
	ActionUnchanged Action = "unchanged" // SKU đã có ở chính mẫu này, mọi trường giống hệt
)

// Plan là kế hoạch ghi của một tệp không có lỗi (lưu thành jsonb ở Task M1-47; không có dữ liệu cá nhân).
type Plan struct {
	Products []PlannedProduct `json:"products"`
}

// PlannedProduct là một mẫu theo thứ tự xuất hiện trong tệp. Status rỗng là cột trống (mẫu mới: draft; mẫu có sẵn:
// giữ nguyên). ExistingProductID có khi mã mẫu có trong Refs.ExistingProducts hay một SKU của mẫu đã có trong
// Refs.ExistingSKUs. AttributeValueIDs, TagIDs
// không trùng, tăng dần. LengthCm, WidthCm, HeightCm (Task 516): nil là ô trống (mẫu mới: không có số đó; mẫu có sẵn:
// giữ nguyên); kế hoạch lưu trước Task 516 không có ba trường (giữ nguyên).
type PlannedProduct struct {
	Code              string           `json:"code"`
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	Status            string           `json:"status,omitempty"`
	CategoryID        *uuid.UUID       `json:"categoryId,omitempty"`
	AttributeValueIDs []uuid.UUID      `json:"attributeValueIds"`
	TagIDs            []uuid.UUID      `json:"tagIds"`
	ExistingProductID *uuid.UUID       `json:"existingProductId,omitempty"`
	LengthCm          *int             `json:"lengthCm,omitempty"`
	WidthCm           *int             `json:"widthCm,omitempty"`
	HeightCm          *int             `json:"heightCm,omitempty"`
	Variants          []PlannedVariant `json:"variants"`
}

// PlannedVariant là một biến thể (một dòng của tệp). Barcode rỗng ở biến thể mới: CRM tự sinh; ở biến thể có sẵn,
// ô trống giữ barcode cũ (đã điền sẵn ở đây). ExistingID, ExistingVersion có khi Action khác create.
type PlannedVariant struct {
	Row             int        `json:"row"`
	Action          Action     `json:"action"`
	ExistingID      *uuid.UUID `json:"existingId,omitempty"`
	ExistingVersion int64      `json:"existingVersion,omitempty"`
	SKU             string     `json:"sku"`
	Barcode         string     `json:"barcode,omitempty"`
	Color           string     `json:"color"`
	ColorHex        string     `json:"colorHex,omitempty"`
	Size            string     `json:"size"`
	Price           int64      `json:"price"`
	CostPrice       *int64     `json:"costPrice,omitempty"`
}
