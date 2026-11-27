package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
)

// wxMeasure là số đo cm của Nhanh ("Kích thước" của mẫu hay của một SKU): 0, null là trống.
type wxMeasure struct {
	Length *float64 `json:"length"`
	Width  *float64 `json:"width"`
	Height *float64 `json:"height"`
}

// wxProductMeasure là số đo Nhanh của một mẫu website (Task 517): Model là số đo của mẫu (null khi Nhanh không có),
// SKUs là số đo của các SKU theo thứ tự Nhanh (mục null khi SKU không có). ProductID là mã mẫu website (= mã mẫu CRM,
// như productIds của chiến dịch).
type wxProductMeasure struct {
	ProductID uuid.UUID    `json:"productId"`
	Model     *wxMeasure   `json:"model"`
	SKUs      []*wxMeasure `json:"skus"`
}

// has: có ít nhất một số > 0.
func (m *wxMeasure) has() bool {
	if m == nil {
		return false
	}
	for _, v := range []*float64{m.Length, m.Width, m.Height} {
		if v != nil && *v > 0 {
			return true
		}
	}
	return false
}

// measure áp đúng luật nguồn Nhanh của website (measureOf trong src/server/catalog/nhanh-projection.ts): số đo của mẫu
// khi mẫu có ít nhất một số > 0, không thì của SKU đầu tiên có số > 0; chỉ lấy các số > 0. Đổi cm → mm; số có hơn một
// chữ số thập phân hay trên 1000 cm → lỗi bọc catalog.ErrInvalidMeasure (không làm tròn). Không số nào → MeasureMm{}.
func (x wxProductMeasure) measure() (catalog.MeasureMm, error) {
	src := x.Model
	if !src.has() {
		src = nil
		for _, s := range x.SKUs {
			if s.has() {
				src = s
				break
			}
		}
	}
	if src == nil {
		return catalog.MeasureMm{}, nil
	}
	var bad []float64
	conv := func(v *float64) *int {
		if v == nil || *v <= 0 {
			return nil
		}
		mm, ok := catalog.MeasureMmFromCm(*v)
		if !ok {
			bad = append(bad, *v)
			return nil
		}
		return &mm
	}
	out := catalog.MeasureMm{Length: conv(src.Length), Width: conv(src.Width), Height: conv(src.Height)}
	if len(bad) > 0 {
		return catalog.MeasureMm{}, fmt.Errorf("số đo %v cm (CRM nhận 0,1–1000, tối đa một chữ số thập phân): %w", bad,
			catalog.ErrInvalidMeasure)
	}
	return out, nil
}

// measures nhập số đo Nhanh vào mẫu CRM của người bán mặc định (nhóm 4 catalog, trước mọi loại khác): mẫu CRM chưa có
// số đo nào → đặt số đo theo luật của measure (catalog.Writer.UpdateProduct: version, sự kiện, audit); mẫu CRM đã có
// số đo → giữ của CRM (bỏ qua; chạy lại không đổi gì); mục không số đo → bỏ qua. Mỗi mẫu một savepoint. Ghi dòng
// đếm mẫu có / không số đo.
func (im *websiteImporter) measures(ctx context.Context, tx pgx.Tx, list []wxProductMeasure, c *importCount) error {
	if len(list) == 0 {
		return nil
	}
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // catalog đã bọc
	}
	w := catalog.Writer{Events: im.Events, Queue: im.Queue, Realtime: im.Bus, RequestID: importSystemName}
	actor := identity.AuthUser{TenantID: im.Tenant, UserID: importSystemID()}
	with, without := 0, 0
	for _, x := range list {
		m, err := x.measure()
		switch {
		case err != nil:
			with++
			c.fail(x.ProductID.String(), err)
			continue
		case m == (catalog.MeasureMm{}):
			without++
			c.Skipped++
			continue
		}
		with++
		var set bool
		err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
			set = false
			p, err := catalog.GetProduct(ctx, sp, seller, x.ProductID)
			if err != nil || p.Measure != (catalog.MeasureMm{}) {
				return err //nolint:wrapcheck // sentinel của catalog
			}
			if _, err := w.UpdateProduct(ctx, sp, seller, p.ID, p.Version, catalog.ProductPatch{Measure: &m}, actor); err != nil {
				return err //nolint:wrapcheck // sentinel của catalog
			}
			set = true
			return nil
		})
		switch {
		case err != nil:
			c.fail(x.ProductID.String(), err)
		case set:
			c.Added++
		default:
			c.Skipped++
		}
	}
	c.note("có số đo %d, không số đo %d (mẫu CRM đã có số đo: giữ của CRM)", with, without)
	return nil
}
