package reporting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Xuất danh sách khách (PQ06, Q13, H11; kế hoạch M6 Task 238 = M6-15, vùng cao). Chỉ xuất tệp (JSON nil), quyền riêng
// customer.export (toàn shop), tệp tiền tố customers/ hạn CUSTOMER_EXPORT_TTL (exports.go). Mỗi dòng là một hồ sơ gốc
// (id = root_id): số liệu của gốc, SĐT và nhãn gom từ mọi hồ sơ cùng root_id; không có địa chỉ. Câu đọc ở
// customers_export_dynsql.go.

// maxCustomerTags là số nhãn tối đa của bộ lọc.
const maxCustomerTags = 50

// maxMinOrders là ngưỡng số đơn tối thiểu lớn nhất nhận được (chặn số vô lý; cột order_count là int).
const maxMinOrders = 1_000_000

// CustomerExportParams là tham số của danh sách khách: khoảng ngày tạo, khoảng ngày mua cuối (ngày giờ VN YYYY-MM-DD,
// tính cả hai đầu, mỗi đầu có thể bỏ), vòng đời (lead, customer), nhãn (có ít nhất một nhãn trong danh sách, xét mọi
// hồ sơ cùng root_id), số đơn tối thiểu. Không trường chữ tự do; trường lạ → ErrBadParams (DecodeParams). Tên trường
// JSON như hợp đồng ReportParams và màn Tệp xuất (Task 241).
type CustomerExportParams struct {
	CreatedFrom   *string     `json:"createdFrom,omitempty"`
	CreatedTo     *string     `json:"createdTo,omitempty"`
	LastOrderFrom *string     `json:"lastOrderFrom,omitempty"`
	LastOrderTo   *string     `json:"lastOrderTo,omitempty"`
	Lifecycle     *string     `json:"lifecycle,omitempty"`
	TagIDs        []uuid.UUID `json:"tagIds,omitempty"`
	MinOrders     *int        `json:"minOrders,omitempty"`
}

// timeRange là khoảng [Start, End) ở UTC của một cặp ngày VN; đầu không đặt là thời điểm rỗng.
type timeRange struct{ Start, End time.Time }

// dayRange đọc cặp ngày from, to (nil: không giới hạn đầu đó): ngày sai → ErrBadDate, from sau to → ErrFromAfterTo.
func dayRange(from, to *string) (timeRange, error) {
	var r timeRange
	if from != nil {
		start, err := dayStart(*from)
		if err != nil {
			return timeRange{}, err
		}
		r.Start = start.UTC()
	}
	if to != nil {
		end, err := DayCutoff(*to)
		if err != nil {
			return timeRange{}, err
		}
		r.End = end
	}
	if !r.Start.IsZero() && !r.End.IsZero() && !r.Start.Before(r.End) {
		return timeRange{}, fmt.Errorf("khoảng %s – %s: %w", *from, *to, ErrFromAfterTo)
	}
	return r, nil
}

// Validate kiểm ngày, vòng đời, số nhãn, số đơn tối thiểu.
func (p *CustomerExportParams) Validate() error {
	if _, err := dayRange(p.CreatedFrom, p.CreatedTo); err != nil {
		return fmt.Errorf("ngày tạo: %w", err)
	}
	if _, err := dayRange(p.LastOrderFrom, p.LastOrderTo); err != nil {
		return fmt.Errorf("ngày mua cuối: %w", err)
	}
	if p.Lifecycle != nil && *p.Lifecycle != "lead" && *p.Lifecycle != "customer" {
		return fmt.Errorf("vòng đời %q: %w", *p.Lifecycle, ErrBadParams)
	}
	if len(p.TagIDs) > maxCustomerTags {
		return fmt.Errorf("%d nhãn, tối đa %d: %w", len(p.TagIDs), maxCustomerTags, ErrBadParams)
	}
	if p.MinOrders != nil && (*p.MinOrders < 0 || *p.MinOrders > maxMinOrders) {
		return fmt.Errorf("số đơn tối thiểu %d: %w", *p.MinOrders, ErrBadParams)
	}
	return nil
}

func customersDef() Def {
	d := newDef[CustomerExportParams](KeyCustomers, FamilyCustomers, permCustomerExport)
	d.Rows, d.Columns, d.FileName = customersExportRows, customersColumns, customersFileName
	return d
}

// customersPage là số khách mỗi lần đọc (keyset, không giữ cả bảng).
const customersPage = 1000

func customersColumns(Params) []xlsx.Column {
	return []xlsx.Column{
		col("Mã khách", xlsx.KindString), col("Tên", xlsx.KindString), col("SĐT", xlsx.KindString),
		col("Số đơn", xlsx.KindInt), col("Thành công", xlsx.KindInt), col("Hoàn", xlsx.KindInt),
		col("Tỷ lệ hoàn", xlsx.KindString), col("Tổng chi", xlsx.KindMoney), col("Mua cuối", xlsx.KindDate),
		col("Nhãn", xlsx.KindString), col("Ngày tạo", xlsx.KindDate),
	}
}

func customersFileName(Params) string { return "danh-sach-khach_" + Today(time.Now()) + ".xlsx" }

// localPhone đổi SĐT dạng chuẩn +84… (khóa danh tính, customer.NormalizePhone) sang dạng quen 0…; dạng khác giữ nguyên.
func localPhone(v string) string {
	if rest, ok := strings.CutPrefix(v, "+84"); ok {
		return "0" + rest
	}
	return v
}

func customersExportRows(ctx context.Context, tx pgx.Tx, _ DataScope, p Params, emit func([]xlsx.Cell) error) error {
	pp, err := typed[CustomerExportParams](p)
	if err != nil {
		return err
	}
	var after *customerKey
	for {
		rows, err := customerRows(ctx, tx, pp, after, customersPage)
		if err != nil {
			return err
		}
		for _, r := range rows {
			phone, last := xlsx.Empty(), xlsx.Empty()
			if r.Phone != nil {
				phone = xlsx.Str(localPhone(*r.Phone))
			}
			if r.LastOrderAt != nil {
				last = xlsx.Date(*r.LastOrderAt)
			}
			if err := emit([]xlsx.Cell{
				xlsx.Str(r.Code), xlsx.Str(r.Name), phone, xlsx.Int(r.Orders), xlsx.Int(r.Delivered), xlsx.Int(r.Returned),
				xlsx.Str(rateText(r.ReturnRateBP)), xlsx.Money(r.Spent), last, xlsx.Str(strings.Join(r.Tags, ", ")),
				xlsx.Date(r.CreatedAt),
			}); err != nil {
				return err
			}
		}
		if len(rows) < customersPage {
			return nil
		}
		last := rows[len(rows)-1]
		after = &customerKey{created: last.CreatedAt, id: last.ID}
	}
}
