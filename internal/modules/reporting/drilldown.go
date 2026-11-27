package reporting

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Danh sách chi tiết của một ô số (BC02, Task 230): cùng orderSet, cùng ô và cột số với báo cáo, nên Total bằng đúng
// số trong ô.

// Giới hạn trang của danh sách chi tiết (hợp đồng: limit 1..200, mặc định 50; Q23).
const (
	DefaultDrillLimit = 50
	MaxDrillLimit     = 200
)

// drillCursorLen là số byte của con trỏ trước khi mã hóa: micro giây Unix (8), order_id (16), Total (8).
const drillCursorLen = 32

// DrillCursor là vị trí keyset (mốc, mã đơn) của dòng cuối trang trước, kèm Total của trang đầu.
type DrillCursor struct {
	At    time.Time
	ID    uuid.UUID
	Total int64
}

// String mã hóa con trỏ thành chuỗi mờ: base64url của 32 byte (micro giây Unix, order_id, total; big-endian).
func (c DrillCursor) String() string {
	buf := binary.BigEndian.AppendUint64(make([]byte, 0, drillCursorLen), uint64(c.At.UnixMicro()))
	buf = append(buf, c.ID[:]...)
	buf = binary.BigEndian.AppendUint64(buf, uint64(c.Total)) //nolint:gosec // G115: Total không âm
	return base64.RawURLEncoding.EncodeToString(buf)
}

// ParseDrillCursor đọc chuỗi do DrillCursor.String tạo. Hỏng (không phải base64url, sai độ dài, Total âm) →
// ErrBadParams.
func ParseDrillCursor(s string) (DrillCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != drillCursorLen {
		return DrillCursor{}, fmt.Errorf("con trỏ danh sách chi tiết: %w", ErrBadParams)
	}
	c := DrillCursor{
		At:    time.UnixMicro(int64(binary.BigEndian.Uint64(raw[:8]))).UTC(), //nolint:gosec // G115: đảo của String
		ID:    uuid.UUID(raw[8:24]),
		Total: int64(binary.BigEndian.Uint64(raw[24:])), //nolint:gosec // G115: kiểm âm ngay dưới
	}
	if c.Total < 0 {
		return DrillCursor{}, fmt.Errorf("con trỏ danh sách chi tiết: %w", ErrBadParams)
	}
	return c, nil
}

// DrillItem là một dòng của danh sách chi tiết: chỉ mã, trạng thái, tiền, kênh, nguồn, nhân viên, hãng; không SĐT,
// địa chỉ, tên khách (Global Constraints M6). EventAt là mốc của cột (khóa sắp xếp); PlacedAt là mốc lên đơn,
// CompletedAt là dòng giao xong hay hoàn muộn nhất (nil khi chưa có; QĐ-230-6).
type DrillItem struct {
	OrderID                         uuid.UUID
	Code                            string
	CustomerCode                    *string
	EventAt                         time.Time
	PlacedAt, CompletedAt           *time.Time
	StatusLabel                     orderview.LabelKey
	Channel                         string
	SourceName, CareByName          *string
	CarrierKey                      *string
	GoodsAmount, ShippingFeeCharged int64
}

// DrillPage là một trang của danh sách chi tiết; Next nil khi hết.
type DrillPage struct {
	Total int64
	Items []DrillItem
	Next  *DrillCursor
}

// ReportRows là số dòng cho thuộc tính report.rows của span.
func (p *DrillPage) ReportRows() int { return len(p.Items) }

// Drilldown đọc một trang danh sách đơn của ô cell (nil: cả bảng), cột số m, bộ lọc f, phạm vi s: tập dựng bằng
// orderSet (cùng hàm với báo cáo), keyset (mốc, order_id) giảm dần, limit 1..MaxDrillLimit. Trang đầu (cursor nil) đếm
// Total bằng count(*) OVER () trên cả tập; trang sau giữ Total của con trỏ. Tham số lạ → ErrBadParams; lọc Page ngoài
// phạm vi → ErrPageForbidden.
func Drilldown(ctx context.Context, tx pgx.Tx, s DataScope, f OrderFilter, m Metric, cell *Cell, cursor *DrillCursor,
	limit int,
) (DrillPage, error) {
	if limit < 1 || limit > MaxDrillLimit {
		return DrillPage{}, fmt.Errorf("limit %d ngoài 1..%d: %w", limit, MaxDrillLimit, ErrBadParams)
	}
	if err := checkPageFilter(s, f); err != nil {
		return DrillPage{}, err
	}
	b := &sqlBuilder{}
	q, err := drilldownSQL(s, f, m, cell, cursor, limit, b)
	if err != nil {
		return DrillPage{}, err
	}
	items, total, err := drillRows(ctx, tx, q, b.args)
	if err != nil {
		return DrillPage{}, err
	}
	page := DrillPage{Total: total, Items: items}
	if cursor != nil {
		page.Total = cursor.Total
	}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		page.Next = &DrillCursor{At: last.EventAt, ID: last.OrderID, Total: page.Total}
	}
	if page.Items == nil {
		page.Items = []DrillItem{}
	}
	return page, nil
}

// DrilldownParams là tham số của danh sách chi tiết: bộ lọc chung, ô (cellDimension, cellKey; bỏ trống cả hai là cả
// bảng), cột số, con trỏ, limit (0 là DefaultDrillLimit); tên JSON cùng tham số query của GET.
type DrilldownParams struct {
	SalesFilterParams
	CellDimension Dimension `json:"cellDimension,omitempty"`
	CellKey       string    `json:"cellKey,omitempty"`
	Metric        Metric    `json:"metric"`
	Cursor        string    `json:"cursor,omitempty"`
	Limit         int       `json:"limit,omitempty"`
}

// Validate kiểm bộ lọc, ô (đủ cả chiều và khóa, hoặc bỏ cả hai), cột số, con trỏ, limit.
func (p DrilldownParams) Validate() error {
	_, err := p.resolve()
	return err
}

type drillArgs struct {
	f      OrderFilter
	cell   *Cell
	cursor *DrillCursor
	limit  int
}

func (p DrilldownParams) resolve() (drillArgs, error) {
	f, err := p.Filter()
	if err != nil {
		return drillArgs{}, err
	}
	a := drillArgs{f: f, limit: p.Limit}
	if err := checkEnum(p.Metric, metricSet, "cột số"); err != nil {
		return drillArgs{}, err
	}
	switch {
	case p.CellDimension == "" && p.CellKey == "":
	case p.CellDimension == "" || p.CellKey == "":
		return drillArgs{}, fmt.Errorf("ô cần đủ chiều và khóa: %w", ErrBadParams)
	default:
		if err := checkEnum(p.CellDimension, dimensionSet, "chiều của ô"); err != nil {
			return drillArgs{}, err
		}
		a.cell = &Cell{Dimension: p.CellDimension, Key: p.CellKey}
	}
	if p.Cursor != "" {
		c, err := ParseDrillCursor(p.Cursor)
		if err != nil {
			return drillArgs{}, err
		}
		a.cursor = &c
	}
	if a.limit == 0 {
		a.limit = DefaultDrillLimit
	}
	if a.limit < 1 || a.limit > MaxDrillLimit {
		return drillArgs{}, fmt.Errorf("limit %d ngoài 1..%d: %w", p.Limit, MaxDrillLimit, ErrBadParams)
	}
	return a, nil
}

func drilldownDef() Def {
	d := newDef[DrilldownParams](KeyOrderDrilldown, FamilySales, permSalesRead)
	d.JSON = func(ctx context.Context, tx pgx.Tx, s DataScope, p Params) (any, error) {
		dp, ok := p.(*DrilldownParams)
		if !ok {
			return nil, fmt.Errorf("tham số danh sách chi tiết %T: %w", p, ErrBadParams)
		}
		a, err := dp.resolve()
		if err != nil {
			return nil, err
		}
		page, err := Drilldown(ctx, tx, s, a.f, dp.Metric, a.cell, a.cursor, a.limit)
		if err != nil {
			return nil, err
		}
		return &page, nil
	}
	return d
}
