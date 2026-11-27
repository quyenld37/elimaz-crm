package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/importer"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// stepError là lỗi của một bước ghi khi áp: mẫu product, dòng row (0: tạo mẫu mới, dòng tìm lại sau khi lùi), cột.
type stepError struct {
	product *ImportPlannedProduct
	row     int
	column  string
	err     error
}

func (e *stepError) Error() string { return fmt.Sprintf("áp dòng %d: %v", e.row, e.err) }
func (e *stepError) Unwrap() error { return e.err }

// write ghi plan (đã khóa, đã Recheck) trong tx (savepoint) theo thứ tự mẫu của tệp. Trước mỗi mẫu: hết ngân sách
// thời gian → errApplyOverBudget (bên gọi lùi savepoint).
func (a *ImportApplier) write(ctx context.Context, tx pgx.Tx, seller uuid.UUID, plan ImportPlan,
	actor identity.AuthUser,
) (applyCounts, error) {
	var c applyCounts
	for i := range plan.Products {
		pp := &plan.Products[i]
		if len(pp.Variants) == 0 {
			continue
		}
		if a.overBudget(ctx) {
			return c, errApplyOverBudget
		}
		if pp.ExistingProductID == nil {
			if err := a.createPlanned(ctx, tx, seller, pp, actor); err != nil {
				return c, err
			}
			c.ProductsCreated++
			c.Create += len(pp.Variants)
			continue
		}
		edited, err := a.updatePlanned(ctx, tx, seller, pp, actor, &c)
		if err != nil {
			return c, err
		}
		if edited {
			c.ProductsEdited++
		}
	}
	return c, nil
}

func (a *ImportApplier) createPlanned(ctx context.Context, tx pgx.Tx, seller uuid.UUID, pp *ImportPlannedProduct,
	actor identity.AuthUser,
) error {
	in := ProductInput{
		SellerID: seller, Code: pp.Code, Name: pp.Name, Description: pp.Description, Status: ProductStatus(pp.Status),
		Measure: MeasureCm{Length: pp.LengthCm, Width: pp.WidthCm, Height: pp.HeightCm},
	}
	for _, v := range pp.Variants {
		in.Variants = append(in.Variants, VariantInput{
			SKU: v.SKU, Barcode: v.Barcode, Color: v.Color, ColorHex: v.ColorHex, Size: v.Size, Price: v.Price,
			CostPrice: v.CostPrice,
		})
	}
	p, err := a.w.CreateProduct(ctx, tx, in, actor)
	if err != nil {
		return &stepError{product: pp, err: err}
	}
	want := Classification{CategoryID: pp.CategoryID, AttributeValueIDs: pp.AttributeValueIDs, TagIDs: pp.TagIDs}
	if want.CategoryID == nil && len(want.AttributeValueIDs) == 0 && len(want.TagIDs) == 0 {
		return nil
	}
	if _, err := a.w.SetClassification(ctx, tx, seller, p.ID, p.Version, want, actor); err != nil {
		return &stepError{product: pp, row: pp.Variants[0].Row, column: importer.ColCategory, err: err}
	}
	return nil
}

// updatePlanned sửa mẫu có sẵn theo pp: tên; mô tả, trạng thái khi ô có giá trị; danh mục, thuộc tính, nhãn khi ô có
// giá trị (ô trống giữ nguyên); biến thể sửa, tạo theo plan. Trả true khi có gì đổi.
func (a *ImportApplier) updatePlanned(ctx context.Context, tx pgx.Tx, seller uuid.UUID, pp *ImportPlannedProduct,
	actor identity.AuthUser, c *applyCounts,
) (bool, error) {
	id, first := *pp.ExistingProductID, pp.Variants[0].Row
	cur, err := GetProduct(ctx, tx, seller, id)
	if err != nil {
		return false, &stepError{product: pp, row: first, column: importer.ColCode, err: err}
	}
	edited, err := a.updateProductFields(ctx, tx, seller, pp, cur, actor)
	if err != nil {
		return false, err
	}
	moved, err := a.moveAside(ctx, tx, seller, pp, cur.Variants, actor)
	if err != nil {
		return false, err
	}
	for _, pv := range pp.Variants {
		switch pv.Action {
		case importer.ActionUnchanged:
			c.Unchanged++
			continue
		case importer.ActionUpdate:
			old := moved[*pv.ExistingID]
			if _, err := a.w.UpdateVariant(ctx, tx, seller, old.ID, old.Version, variantPatch(old, pv), actor); err != nil {
				return false, &stepError{product: pp, row: pv.Row, column: importer.ColSKU, err: err}
			}
			c.Update++
		case importer.ActionCreate:
			v, err := productVersion(ctx, tx, seller, id)
			if err == nil {
				_, err = a.w.AddVariant(ctx, tx, seller, id, v, VariantInput{
					SKU: pv.SKU, Barcode: pv.Barcode, Color: pv.Color, ColorHex: pv.ColorHex, Size: pv.Size,
					Price: pv.Price, CostPrice: pv.CostPrice,
				}, actor)
			}
			if err != nil {
				return false, &stepError{product: pp, row: pv.Row, column: importer.ColSKU, err: err}
			}
			c.Create++
		}
		edited = true
	}
	return edited, nil
}

// updateProductFields sửa tên, mô tả, trạng thái, số đo (ô có số; ô trống giữ số cũ) và phân loại của mẫu có sẵn
// cur khi khác tệp.
func (a *ImportApplier) updateProductFields(ctx context.Context, tx pgx.Tx, seller uuid.UUID, pp *ImportPlannedProduct,
	cur Product, actor identity.AuthUser,
) (bool, error) {
	first, edited, version := pp.Variants[0].Row, false, cur.Version
	var patch ProductPatch
	if pp.Name != cur.Name {
		patch.Name = &pp.Name
	}
	if pp.Description != "" && pp.Description != cur.Description {
		patch.Description = &pp.Description
	}
	if s := ProductStatus(pp.Status); s != "" && s != cur.Status {
		patch.Status = &s
	}
	if m, changed := plannedMeasure(cur.Measure, pp); changed {
		patch.Measure = &m
	}
	if patch != (ProductPatch{}) {
		p, err := a.w.UpdateProduct(ctx, tx, seller, cur.ID, version, patch, actor)
		if err != nil {
			return false, &stepError{product: pp, row: first, column: importer.ColName, err: err}
		}
		version, edited = p.Version, true
	}
	have, err := GetClassification(ctx, tx, seller, cur.ID)
	if err != nil {
		return false, &stepError{product: pp, row: first, column: importer.ColCode, err: err}
	}
	want := have.Classification
	if pp.CategoryID != nil {
		want.CategoryID = pp.CategoryID
	}
	if len(pp.AttributeValueIDs) > 0 {
		want.AttributeValueIDs = pp.AttributeValueIDs
	}
	if len(pp.TagIDs) > 0 {
		want.TagIDs = pp.TagIDs
	}
	if sameClassification(have.Classification, want) {
		return edited, nil
	}
	if _, err := a.w.SetClassification(ctx, tx, seller, cur.ID, version, want, actor); err != nil {
		return false, &stepError{product: pp, row: first, column: importer.ColCategory, err: err}
	}
	return true, nil
}

// plannedMeasure: số đo cur với các ô có số của pp thay vào; changed khi khác cur.
func plannedMeasure(cur MeasureCm, pp *ImportPlannedProduct) (MeasureCm, bool) {
	out, changed := cur, false
	for _, f := range []struct {
		dst  **int
		cell *int
	}{{&out.Length, pp.LengthCm}, {&out.Width, pp.WidthCm}, {&out.Height, pp.HeightCm}} {
		if f.cell != nil && (*f.dst == nil || **f.dst != *f.cell) {
			*f.dst, changed = f.cell, true
		}
	}
	return out, changed
}

func sameClassification(a, b Classification) bool {
	return (a.CategoryID == nil) == (b.CategoryID == nil) && (a.CategoryID == nil || *a.CategoryID == *b.CategoryID) &&
		slices.Equal(sortedUnique(a.AttributeValueIDs), sortedUnique(b.AttributeValueIDs)) &&
		slices.Equal(sortedUnique(a.TagIDs), sortedUnique(b.TagIDs))
}

// moveAside: mọi biến thể có sẵn đổi màu+size được dời tạm sang size giả trước, để mọi chỗ cũ đều trống khi các bước
// sửa, tạo chạy theo thứ tự dòng (đổi chỗ cho nhau, đổi dây chuyền A S→M rồi B M→L, tạo mới vào chỗ vừa trống:
// unique màu+size không chặn). Bước kiểm đã bảo đảm không dòng nào lấy chỗ của biến thể đứng yên. Trả biến thể hiện
// tại theo mã (version sau khi dời).
func (a *ImportApplier) moveAside(ctx context.Context, tx pgx.Tx, seller uuid.UUID, pp *ImportPlannedProduct,
	variants []Variant, actor identity.AuthUser,
) (map[uuid.UUID]Variant, error) {
	byID := make(map[uuid.UUID]Variant, len(variants))
	for _, v := range variants {
		byID[v.ID] = v
	}
	for _, pv := range pp.Variants {
		if pv.Action != importer.ActionUpdate {
			continue
		}
		old := byID[*pv.ExistingID]
		if old.Color == pv.Color && old.Size == pv.Size {
			continue
		}
		tmp := fmt.Sprintf("~nhập dòng %d", pv.Row)
		moved, err := a.w.UpdateVariant(ctx, tx, seller, old.ID, old.Version, VariantPatch{Size: &tmp}, actor)
		if err != nil {
			return nil, &stepError{product: pp, row: pv.Row, column: importer.ColSize, err: err}
		}
		byID[old.ID] = moved
	}
	return byID, nil
}

// variantPatch là phần sửa đưa biến thể old về đúng dòng pv (ô Mã màu, Giá nhập trống: bỏ; barcode trống: giữ).
func variantPatch(old Variant, pv importer.PlannedVariant) VariantPatch {
	p := VariantPatch{Color: &pv.Color, Size: &pv.Size, ColorHex: &pv.ColorHex, Price: &pv.Price}
	if pv.Barcode != "" && pv.Barcode != old.Barcode {
		p.Barcode = &pv.Barcode
	}
	if pv.CostPrice == nil {
		p.ClearCostPrice = true
	} else {
		p.CostPrice = pv.CostPrice
	}
	return p
}

func productVersion(ctx context.Context, tx pgx.Tx, seller, id uuid.UUID) (int64, error) {
	row, err := store.New(tx).GetProductCategory(ctx, store.GetProductCategoryParams{SellerID: seller, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("đọc version mẫu: %w", err)
	}
	return row.Version, nil
}

// describeStep đổi lỗi nghiệp vụ của một bước ghi thành lỗi dòng (tx đã lùi về trước lần áp). ok = false: không phải
// lỗi do dữ liệu đổi (hết hạn, lỗi database, lỗi lập trình) → bên gọi trả lỗi để việc chạy lại.
func describeStep(ctx context.Context, tx pgx.Tx, seller uuid.UUID, s *stepError) (ImportError, bool, error) {
	var e ImportError
	switch {
	case errors.Is(s.err, ErrSKUTaken):
		e = ImportError{
			Column: importer.ColSKU, Code: importer.CodeSKUBelongsToOtherProduct,
			Message: "SKU vừa được dùng cho mẫu khác sau khi kiểm tệp: hãy tải tệp lên lại để kiểm",
		}
	case errors.Is(s.err, ErrBarcodeTaken):
		e = ImportError{
			Column: importer.ColBarcode, Code: importer.CodeBarcodeTaken,
			Message: "Barcode vừa được dùng cho biến thể khác sau khi kiểm tệp: hãy đổi barcode hay để trống",
		}
	case errors.Is(s.err, ErrDuplicateOption):
		e = ImportError{
			Column: importer.ColSize, Code: importer.CodeOptionTaken,
			Message: "Màu và size này vừa thuộc biến thể khác của mẫu sau khi kiểm tệp: hãy tải tệp lên lại để kiểm",
		}
	case errors.Is(s.err, ErrCodeTaken):
		e = ImportError{
			Column: importer.ColCode, Code: importer.CodeCodeTaken,
			Message: "Mã mẫu" + " vừa được tạo sau khi kiểm tệp (hoặc thuộc mẫu đã xóa): hãy đổi mã hay tải tệp lên lại",
		}
	case errors.Is(s.err, ErrNotFound), errors.Is(s.err, ErrVersionConflict):
		e = ImportError{
			Column: s.column, Code: importer.CodeChangedSinceValidation,
			Message: "Mẫu, biến thể hay phân loại vừa bị sửa hay xóa sau khi kiểm tệp: hãy tải tệp lên lại để kiểm",
		}
	default:
		return e, false, nil
	}
	e.Row = s.row
	if e.Row != 0 {
		return e, true, nil
	}
	row, err := culpritRow(ctx, tx, seller, s.product, e.Code)
	e.Row = row
	return e, true, err
}

// culpritRow tìm dòng của mẫu mới p có SKU hay barcode vừa bị lấy (CreateProduct không nói dòng nào); không thấy →
// dòng đầu của mẫu.
func culpritRow(ctx context.Context, tx pgx.Tx, seller uuid.UUID, p *ImportPlannedProduct, code string) (int, error) {
	q := store.New(tx)
	for _, v := range p.Variants {
		var taken bool
		switch code {
		case importer.CodeSKUBelongsToOtherProduct:
			rows, err := q.ListVariantsBySKUs(ctx, store.ListVariantsBySKUsParams{SellerID: seller, Skus: []string{v.SKU}})
			if err != nil {
				return 0, fmt.Errorf("tìm SKU bị lấy: %w", err)
			}
			taken = len(rows) > 0
		case importer.CodeBarcodeTaken:
			if v.Barcode == "" {
				continue
			}
			var err error
			if taken, err = q.BarcodeExists(ctx, store.BarcodeExistsParams{SellerID: seller, Barcode: v.Barcode}); err != nil {
				return 0, fmt.Errorf("tìm barcode bị lấy: %w", err)
			}
		}
		if taken {
			return v.Row, nil
		}
	}
	return p.Variants[0].Row, nil
}

func applyResult(status string) (json.RawMessage, error) {
	b, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả áp: %w", err)
	}
	return b, nil
}
