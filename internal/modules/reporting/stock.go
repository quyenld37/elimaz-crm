package reporting

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/reporting/internal/store"
)

// Báo cáo tồn và xuất nhập tồn (BC03, Task 234 = M6-11). Tồn hôm nay đọc số dư hiện tại (inventory.stock_available_v);
// tồn một ngày đã qua đọc bản chụp của Task 233 (reporting.stock_daily_snapshots), không tính lại từ sổ. Xuất nhập tồn
// đọc ngăn on_hand của sổ (inventory.stock_ledger_v) theo kỳ. Cả hai ẩn dòng mọi số bằng 0, lọc kho theo phạm vi
// report.inventory.read, trang keyset (SKU theo thứ tự byte, biến thể, kho). Câu SQL ở stock_dynsql.go,
// movements_dynsql.go.

// Nguồn số của báo cáo tồn (enum InventoryStock.source của hợp đồng).
const (
	SourceLive     = "live"
	SourceSnapshot = "snapshot"
)

// Giới hạn trang của hai báo cáo tồn (hợp đồng ReportPageLimit: 1..500, mặc định 100).
const (
	DefaultStockLimit = 100
	MaxStockLimit     = 500
)

// maxSKUPrefix là độ dài tối đa của tiền tố SKU (hợp đồng ReportSKUPrefix; SKU tối đa 64 ký tự, CHECK của 0114).
const maxSKUPrefix = 64

// stockCursorIDs là số byte của hai mã trong con trỏ (biến thể, kho) đứng trước SKU.
const stockCursorIDs = 32

// StockCursor là vị trí keyset (SKU, biến thể, kho) của dòng cuối trang trước.
type StockCursor struct {
	SKU                    string
	VariantID, WarehouseID uuid.UUID
}

// String mã hóa con trỏ thành chuỗi mờ: base64url của biến thể (16 byte), kho (16 byte), rồi SKU.
func (c StockCursor) String() string {
	buf := make([]byte, 0, stockCursorIDs+len(c.SKU))
	buf = append(buf, c.VariantID[:]...)
	buf = append(buf, c.WarehouseID[:]...)
	return base64.RawURLEncoding.EncodeToString(append(buf, c.SKU...))
}

// ParseStockCursor đọc chuỗi do StockCursor.String tạo. Hỏng (không phải base64url, thiếu SKU, SKU dài quá 64 byte)
// → ErrBadParams.
func ParseStockCursor(s string) (StockCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) <= stockCursorIDs || len(raw) > stockCursorIDs+maxSKUPrefix {
		return StockCursor{}, fmt.Errorf("con trỏ báo cáo tồn: %w", ErrBadParams)
	}
	return StockCursor{
		VariantID: uuid.UUID(raw[:16]), WarehouseID: uuid.UUID(raw[16:stockCursorIDs]), SKU: string(raw[stockCursorIDs:]),
	}, nil
}

// StockPage là phần lọc và trang chung của hai báo cáo tồn; tên JSON cùng tham số query của GET.
type StockPage struct {
	// WarehouseID: một kho; nil là mọi kho trong phạm vi (ngoài phạm vi → ErrWarehouseForbidden lúc chạy).
	WarehouseID *uuid.UUID `json:"warehouseId,omitempty"`
	// SKUPrefix: tiền tố SKU (bỏ dấu cách đầu cuối, không phân biệt hoa thường vì SKU luôn viết hoa); rỗng là mọi SKU.
	SKUPrefix string `json:"q,omitempty"`
	// Cursor: nextCursor của trang trước (StockCursor.String); rỗng là trang đầu.
	Cursor string `json:"cursor,omitempty"`
	// Limit: số dòng mỗi trang, 1..MaxStockLimit; 0 là DefaultStockLimit.
	Limit int `json:"limit,omitempty"`
}

// stockPage là StockPage đã kiểm.
type stockPage struct {
	warehouse *uuid.UUID
	prefix    string
	cursor    *StockCursor
	limit     int
}

func (p StockPage) resolve() (stockPage, error) {
	a := stockPage{warehouse: p.WarehouseID, prefix: strings.ToUpper(strings.TrimSpace(p.SKUPrefix)), limit: p.Limit}
	if len(a.prefix) > maxSKUPrefix {
		return stockPage{}, fmt.Errorf("tiền tố SKU dài %d, tối đa %d: %w", len(a.prefix), maxSKUPrefix, ErrBadParams)
	}
	if p.Cursor != "" {
		c, err := ParseStockCursor(p.Cursor)
		if err != nil {
			return stockPage{}, err
		}
		a.cursor = &c
	}
	if a.limit == 0 {
		a.limit = DefaultStockLimit
	}
	if a.limit < 1 || a.limit > MaxStockLimit {
		return stockPage{}, fmt.Errorf("limit %d ngoài 1..%d: %w", p.Limit, MaxStockLimit, ErrBadParams)
	}
	return a, nil
}

// checkWarehouse: lọc một kho ngoài phạm vi report.inventory.read của người xem → ErrWarehouseForbidden.
func (a stockPage) checkWarehouse(s DataScope) error {
	if a.warehouse == nil || s.AllWarehouses || slices.Contains(s.WarehouseIDs, *a.warehouse) {
		return nil
	}
	return fmt.Errorf("kho %s: %w", *a.warehouse, ErrWarehouseForbidden)
}

// StockParams là tham số của báo cáo tồn: ngày (rỗng là hôm nay) cùng kho, tiền tố SKU, trang.
type StockParams struct {
	// Date là ngày giờ Việt Nam (YYYY-MM-DD); rỗng là hôm nay.
	Date string `json:"date,omitempty"`
	StockPage
}

// Validate kiểm ngày (đúng dạng, không phải ngày mai trở đi theo giờ VN lúc gọi), tiền tố, con trỏ, limit.
func (p *StockParams) Validate() error {
	_, _, err := p.resolve(time.Now())
	return err
}

// resolve trả ngày của báo cáo (Date, hay hôm nay khi rỗng) và phần trang đã kiểm. Ngày sai dạng hay sau hôm nay của
// now → ErrBadDate.
func (p *StockParams) resolve(now time.Time) (string, stockPage, error) {
	today := Today(now)
	day := p.Date
	if day == "" {
		day = today
	}
	if _, err := dayStart(day); err != nil {
		return "", stockPage{}, err
	}
	if day > today { // YYYY-MM-DD so theo chuỗi đúng thứ tự ngày
		return "", stockPage{}, fmt.Errorf("ngày %s sau hôm nay %s: %w", day, today, ErrBadDate)
	}
	a, err := p.StockPage.resolve()
	return day, a, err
}

// StockRow là một dòng của báo cáo tồn: một biến thể ở một kho; Available = OnHand − Damaged − Held − InTransit.
type StockRow struct {
	VariantID                        uuid.UUID
	SKU, ProductName, Color, Size    string
	WarehouseID                      uuid.UUID
	WarehouseName                    string
	OnHand, Held, InTransit, Damaged int64
	Available                        int64
}

// StockReport là một trang của báo cáo tồn: Status "ready", ngày, nguồn (SourceLive hôm nay, SourceSnapshot ngày đã
// qua), ngày chụp đầu tiên (nil khi chưa chụp ngày nào), các dòng, con trỏ trang sau (nil khi hết).
type StockReport struct {
	Status, Date, Source string
	FirstSnapshotDay     *string
	Rows                 []StockRow
	Next                 *StockCursor
}

// ReportRows là số dòng cho thuộc tính report.rows của span.
func (r StockReport) ReportRows() int { return len(r.Rows) }

func stockDef() Def {
	d := newDef[StockParams](KeyStock, FamilyInventory, permInventoryRead)
	d.JSON = func(ctx context.Context, tx pgx.Tx, s DataScope, p Params) (any, error) {
		sp, ok := p.(*StockParams)
		if !ok {
			return nil, fmt.Errorf("tham số báo cáo tồn kiểu %T: %w", p, ErrBadParams)
		}
		return stockReport(ctx, tx, s, sp, time.Now())
	}
	d.Rows, d.Columns, d.FileName = stockExportRows, stockColumns, stockFileName
	return d
}

// stockReport đọc một trang báo cáo tồn ngày của p (giờ VN của now): hôm nay → số dư; ngày đã qua đã chụp → bản chụp;
// ngày đã qua chưa chụp (kể cả trước bản chụp đầu) → ErrNoSnapshot.
func stockReport(ctx context.Context, tx pgx.Tx, s DataScope, p *StockParams, now time.Time) (StockReport, error) {
	day, a, err := p.resolve(now)
	if err != nil {
		return StockReport{}, err
	}
	if err := a.checkWarehouse(s); err != nil {
		return StockReport{}, err
	}
	info, err := store.New(tx).SnapshotDayInfo(ctx, day)
	if err != nil {
		return StockReport{}, fmt.Errorf("đọc ngày chụp tồn: %w", err)
	}
	rep := StockReport{Status: "ready", Date: day, Source: SourceLive, Rows: []StockRow{}}
	if info.FirstDay != "" {
		rep.FirstSnapshotDay = &info.FirstDay
	}
	snapshot := day != Today(now)
	if snapshot {
		if !info.Taken {
			return StockReport{}, fmt.Errorf("tồn ngày %s: %w", day, ErrNoSnapshot)
		}
		rep.Source = SourceSnapshot
	}
	if s.NoInventory() {
		return rep, nil
	}
	rows, err := stockRows(ctx, tx, s, a, snapshotDay(snapshot, day))
	if err != nil {
		return StockReport{}, err
	}
	rep.Rows, rep.Next = cutPage(rows, a.limit, func(r StockRow) StockCursor {
		return StockCursor{SKU: r.SKU, VariantID: r.VariantID, WarehouseID: r.WarehouseID}
	})
	return rep, nil
}

// snapshotDay là ngày đọc bản chụp, rỗng khi đọc số dư hiện tại.
func snapshotDay(snapshot bool, day string) string {
	if snapshot {
		return day
	}
	return ""
}

// MovementsParams là tham số của xuất nhập tồn: kỳ from, to cùng kho, tiền tố SKU, trang.
type MovementsParams struct {
	PeriodParams
	StockPage
}

// Validate kiểm kỳ (tối đa MaxDays ngày), tiền tố, con trỏ, limit.
func (p *MovementsParams) Validate() error {
	_, _, err := p.resolve()
	return err
}

func (p *MovementsParams) resolve() (Period, stockPage, error) {
	period, err := p.Period()
	if err != nil {
		return Period{}, stockPage{}, err
	}
	a, err := p.StockPage.resolve()
	return period, a, err
}

// MovementRow là một dòng của xuất nhập tồn (ngăn on_hand của một biến thể ở một kho trong kỳ). Sold, ExportedOther
// là số dương khi hàng ra; Adjusted có dấu. Closing = Opening + Imported + ReturnedIn − Sold − ExportedOther +
// Adjusted; DamagedClosing là ngăn damaged tới cuối kỳ.
type MovementRow struct {
	VariantID                                                             uuid.UUID
	SKU, ProductName, Color, Size                                         string
	WarehouseID                                                           uuid.UUID
	Opening, Imported, ReturnedIn, Sold, ExportedOther, Adjusted, Closing int64
	DamagedClosing                                                        int64
}

// MovementsReport là một trang của xuất nhập tồn: Status "ready", kỳ, các dòng, con trỏ trang sau (nil khi hết).
type MovementsReport struct {
	Status string
	Period Period
	Rows   []MovementRow
	Next   *StockCursor
}

// ReportRows là số dòng cho thuộc tính report.rows của span.
func (r MovementsReport) ReportRows() int { return len(r.Rows) }

func movementsDef() Def {
	d := newDef[MovementsParams](KeyStockMovements, FamilyInventory, permInventoryRead)
	d.JSON = func(ctx context.Context, tx pgx.Tx, s DataScope, p Params) (any, error) {
		mp, ok := p.(*MovementsParams)
		if !ok {
			return nil, fmt.Errorf("tham số xuất nhập tồn kiểu %T: %w", p, ErrBadParams)
		}
		return movementsReport(ctx, tx, s, mp)
	}
	d.Rows, d.Columns, d.FileName = movementsExportRows, movementsColumns, periodFile("xuat-nhap-ton")
	return d
}

// movementsReport đọc một trang xuất nhập tồn của kỳ p.
func movementsReport(ctx context.Context, tx pgx.Tx, s DataScope, p *MovementsParams) (MovementsReport, error) {
	period, a, err := p.resolve()
	if err != nil {
		return MovementsReport{}, err
	}
	if err := a.checkWarehouse(s); err != nil {
		return MovementsReport{}, err
	}
	rep := MovementsReport{Status: "ready", Period: period, Rows: []MovementRow{}}
	if s.NoInventory() {
		return rep, nil
	}
	rows, err := movementRows(ctx, tx, s, a, period)
	if err != nil {
		return MovementsReport{}, err
	}
	rep.Rows, rep.Next = cutPage(rows, a.limit, func(r MovementRow) StockCursor {
		return StockCursor{SKU: r.SKU, VariantID: r.VariantID, WarehouseID: r.WarehouseID}
	})
	return rep, nil
}

// cutPage cắt rows (đọc limit + 1 dòng) còn limit dòng và trả con trỏ của dòng cuối khi còn trang sau; rows rỗng (nil)
// thành mảng rỗng.
func cutPage[T any](rows []T, limit int, key func(T) StockCursor) ([]T, *StockCursor) {
	if len(rows) <= limit {
		return append([]T{}, rows...), nil
	}
	rows = rows[:limit]
	c := key(rows[limit-1])
	return rows, &c
}
