package reporting

import (
	"cmp"
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Sản phẩm bán chạy theo SKU hay theo mẫu (Task 232, M6-09). Phần xuất tệp (Def.Rows, cột XLSX) là Task 237; tệp xuất
// đọc mọi dòng bằng bestSellerRows với limit 0.

// Mức gộp của báo cáo bán chạy.
const (
	LevelSKU     = "sku"
	LevelProduct = "product"
)

// Cơ sở của báo cáo bán chạy (Q6): đơn lên trong kỳ và chưa hủy, hay đơn giao thành công trong kỳ.
const (
	SalesBasisPlaced    = "placed"
	SalesBasisDelivered = "delivered"
)

// Số dòng của bản xem tại chỗ (Q23): mặc định 50, tối đa 500; tệp xuất không giới hạn.
const (
	DefaultBestSellersLimit = 50
	MaxBestSellersLimit     = 500
)

// BestSellersParams là tham số của báo cáo bán chạy. Level, Basis bỏ trống là sku, placed; Limit 0 là 50.
type BestSellersParams struct {
	PeriodParams
	Level string `json:"level,omitempty"`
	Basis string `json:"basis,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// Validate kiểm kỳ, mức, cơ sở và Limit (0–500); sai → ErrBadParams (kỳ: lỗi ngày của ParsePeriod).
func (p BestSellersParams) Validate() error {
	if err := p.PeriodParams.Validate(); err != nil {
		return err
	}
	if l := p.level(); l != LevelSKU && l != LevelProduct {
		return fmt.Errorf("mức %q: %w", p.Level, ErrBadParams)
	}
	if b := p.basis(); b != SalesBasisPlaced && b != SalesBasisDelivered {
		return fmt.Errorf("cơ sở %q: %w", p.Basis, ErrBadParams)
	}
	if p.Limit < 0 || p.Limit > MaxBestSellersLimit {
		return fmt.Errorf("limit %d, tối đa %d: %w", p.Limit, MaxBestSellersLimit, ErrBadParams)
	}
	return nil
}

func (p BestSellersParams) level() string { return cmp.Or(p.Level, LevelSKU) }

func (p BestSellersParams) basis() string { return cmp.Or(p.Basis, SalesBasisPlaced) }

// viewLimit là số dòng của bản xem tại chỗ (0 → mặc định).
func (p BestSellersParams) viewLimit() int { return cmp.Or(p.Limit, DefaultBestSellersLimit) }

// BestSellerRow là một dòng: một biến thể (mức sku) hay một mẫu (mức product: VariantID, SKU, Color, Size là nil).
// Quantity, Orders, GoodsAmount (Σ net_amount, đồng) tính trên tập đơn của cơ sở; ReturnedQuantity là số chiếc khách
// trả nhận trong kỳ (sellable + damaged, chiếc thiếu không tính), không phụ thuộc tập đơn.
type BestSellerRow struct {
	ProductID                                       uuid.UUID
	VariantID                                       *uuid.UUID
	SKU                                             *string
	Name                                            string
	Color, Size                                     *string
	Quantity, Orders, GoodsAmount, ReturnedQuantity int64
}

// BestSellersReport là kết quả: các dòng đã sắp (số lượng giảm, tiền giảm, SKU/tên tăng) và tổng số dòng trước khi
// cắt theo Limit.
type BestSellersReport struct {
	Status    string
	Rows      []BestSellerRow
	TotalRows int64
}

// ReportRows là số dòng của kết quả (thuộc tính report.rows của span).
func (r BestSellersReport) ReportRows() int { return len(r.Rows) }

func bestSellersDef() Def {
	d := newDef[BestSellersParams](KeyBestSellers, FamilySales, permSalesRead)
	d.JSON = bestSellersJSON
	d.Rows, d.Columns, d.FileName = bestSellersRows, bestSellersColumns, periodFile("ban-chay")
	return d
}

// bestSellersJSON là bản xem tại chỗ: tối đa viewLimit dòng.
func bestSellersJSON(ctx context.Context, tx pgx.Tx, s DataScope, p Params) (any, error) {
	bp, ok := p.(*BestSellersParams)
	if !ok {
		return nil, fmt.Errorf("tham số bán chạy kiểu %T: %w", p, ErrBadParams)
	}
	rows, total, err := bestSellerRows(ctx, tx, s, bp, bp.viewLimit())
	if err != nil {
		return nil, err
	}
	return BestSellersReport{Status: "ready", Rows: rows, TotalRows: total}, nil
}
