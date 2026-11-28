package catalog

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/importer"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Chuyển sản phẩm từ hệ cũ (kế hoạch M7, Task 541 = M7-41; ADR-48): tệp Excel sinh từ bản chụp đi qua đúng đường nhập
// Excel của M1 (CreateImport, kiểm, nhân viên bấm áp), nên mẫu mới là draft và mọi luật nhập giữ nguyên. Phần này thêm:
// sinh tệp (LegacyWorkbook), sổ mã cũ để chạy lại không trùng (RecordLegacyRefs, LegacyRefs) và tra biến thể theo SKU
// sau khi áp (VariantsBySKU).

// Nguồn và loại của mã cũ (catalog.legacy_refs).
const (
	LegacySourceNhanh = "nhanh"
	LegacyKindProduct = "product"
	LegacyKindVariant = "variant"
	LegacyKindImage   = "image"
)

// LegacyVariant là một dòng biến thể của tệp sinh ra. CostPrice nil: ô Giá nhập để trống. Barcode rỗng: CRM tự sinh.
type LegacyVariant struct {
	SKU, Barcode, Color, Size string
	Price                     int64
	CostPrice                 *int64
}

// LegacyProduct là một mẫu của tệp sinh ra. CategoryPath là đường danh mục CRM từ gốc ("Áo > Áo sơ mi"); rỗng hay
// không có trong CRM thì để trống ô Danh mục.
type LegacyProduct struct {
	Code, Name, Description string
	CategoryPath            []string
	Variants                []LegacyVariant
}

// LegacyWorkbook sinh tệp .xlsx đúng cột của mẫu nhập (cùng cột với ImportTemplate, gồm cột thuộc tính hiện có) cho
// products, trạng thái để trống (mẫu mới là draft). Trả tệp và đường danh mục không có trong CRM (không trùng, theo
// thứ tự): ô Danh mục của các mẫu đó để trống để tệp vẫn qua bước kiểm.
func LegacyWorkbook(ctx context.Context, tx pgx.Tx, products []LegacyProduct) (data []byte, missingCategories []string, err error) {
	refs, err := classificationRefs(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	header := importer.Columns(refs)
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[h] = i
	}
	var rows [][]any
	for _, p := range products {
		category := ""
		if len(p.CategoryPath) > 0 {
			path := importer.CategoryPath(p.CategoryPath...)
			if _, ok := refs.Categories[path]; ok {
				category = path
			} else if !slices.Contains(missingCategories, path) {
				missingCategories = append(missingCategories, path)
			}
		}
		for _, v := range p.Variants {
			r := make([]any, len(header))
			set := func(name string, val any) { r[col[name]] = val }
			set(importer.ColCode, p.Code)
			set(importer.ColName, p.Name)
			set(importer.ColCategory, orNil(category))
			set(importer.ColDescription, orNil(p.Description))
			set(importer.ColColor, v.Color)
			set(importer.ColSize, v.Size)
			set(importer.ColSKU, v.SKU)
			set(importer.ColBarcode, orNil(v.Barcode))
			set(importer.ColPrice, strconv.FormatInt(v.Price, 10))
			if v.CostPrice != nil {
				set(importer.ColCostPrice, strconv.FormatInt(*v.CostPrice, 10))
			}
			rows = append(rows, r)
		}
	}
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, []xlsx.Sheet{{Name: "Sản phẩm", Header: header, Rows: rows}}); err != nil {
		return nil, nil, fmt.Errorf("ghi tệp chuyển sản phẩm: %w", err)
	}
	return buf.Bytes(), missingCategories, nil
}

func orNil(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// LegacyRef là một mã cũ (nguồn Nhanh) và thực thể CRM tương ứng: Kind là LegacyKind*, LegacyID là mã số của nguồn
// (hay địa chỉ ảnh), EntityID là mã mẫu, biến thể hay tệp ảnh của CRM.
type LegacyRef struct {
	Kind, LegacyID string
	EntityID       uuid.UUID
}

// RecordLegacyRefs ghi các mã cũ (nguồn Nhanh) của tenantID trong tx của bên gọi; mã đã có thì bỏ qua (chạy lại không
// trùng, không đổi thực thể đã ghi). Trả số mã mới ghi.
func RecordLegacyRefs(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, refs []LegacyRef) (int, error) {
	q := store.New(tx)
	n := 0
	for _, r := range refs {
		rows, err := q.InsertLegacyRef(ctx, store.InsertLegacyRefParams{
			TenantID: tenantID, Source: LegacySourceNhanh, Kind: r.Kind, LegacyID: r.LegacyID, EntityID: r.EntityID,
		})
		if err != nil {
			return 0, fmt.Errorf("ghi mã cũ: %w", err)
		}
		n += int(rows)
	}
	return n, nil
}

// LegacyRefs trả thực thể CRM của các mã cũ ids (nguồn Nhanh, loại kind); mã chưa ghi không có trong kết quả.
func LegacyRefs(ctx context.Context, tx pgx.Tx, kind string, ids []string) (map[string]uuid.UUID, error) {
	rows, err := store.New(tx).ListLegacyRefs(ctx, store.ListLegacyRefsParams{
		Source: LegacySourceNhanh, Kind: kind, LegacyIds: slices.Sorted(maps.Keys(setOf(ids))),
	})
	if err != nil {
		return nil, fmt.Errorf("đọc mã cũ: %w", err)
	}
	out := make(map[string]uuid.UUID, len(rows))
	for _, r := range rows {
		out[r.LegacyID] = r.EntityID
	}
	return out, nil
}

func setOf(s []string) map[string]struct{} {
	m := make(map[string]struct{}, len(s))
	for _, v := range s {
		m[v] = struct{}{}
	}
	return m
}

// SKUVariant là biến thể tìm được theo SKU, kèm mẫu cha và phiên bản của mẫu (để gắn ảnh).
type SKUVariant struct {
	ID, ProductID  uuid.UUID
	SKU, Color     string
	ProductVersion int64
}

// VariantsBySKU trả các biến thể của người bán sellerID theo SKU (khóa kết quả là SKU viết hoa); SKU không có thì
// vắng mặt. Mẫu đã xóa không tính.
func VariantsBySKU(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, skus []string) (map[string]SKUVariant, error) {
	upper := make([]string, 0, len(skus))
	for _, s := range skus {
		upper = append(upper, strings.ToUpper(strings.TrimSpace(s)))
	}
	rows, err := store.New(tx).ListVariantsBySKU(ctx, store.ListVariantsBySKUParams{SellerID: sellerID, Skus: upper})
	if err != nil {
		return nil, fmt.Errorf("đọc biến thể theo SKU: %w", err)
	}
	out := make(map[string]SKUVariant, len(rows))
	for _, r := range rows {
		out[strings.ToUpper(r.Sku)] = SKUVariant{ID: r.ID, ProductID: r.ProductID, SKU: r.Sku, Color: r.Color, ProductVersion: r.ProductVersion}
	}
	return out, nil
}
