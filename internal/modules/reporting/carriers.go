package reporting

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Báo cáo hãng vận chuyển (Task 231, M6-08): vận đơn tạo trong kỳ (ngày tạo vận đơn giờ VN), kết quả theo trạng thái
// hiện tại của vận đơn (Q4), một dòng mỗi tài khoản hãng của người bán own; hãng tự giao (self) là một dòng như các hãng
// khác. Danh sách chi tiết theo hãng là báo cáo đơn chiều carrier (Task 230).

// CarriersParams là tham số của báo cáo hãng: kỳ from, to.
type CarriersParams struct {
	PeriodParams
}

// CarrierRow là một dòng của báo cáo hãng (một tài khoản hãng), hay dòng tổng (CarrierKey rỗng, CarrierAccountID
// uuid.Nil). Created: vận đơn chưa hủy (= InProgress + Returning + DeliveryFailed + Delivered + Returned); Cancelled: vận
// đơn đang cancelled; InProgress: creating, awaiting_pickup, shipping, cancel_requested. FeeTotal, CODTotal chỉ cộng
// vận đơn chưa hủy; phí một vận đơn = coalesce(fee_actual, fee, 0). FeeAvg = FeeTotal / Created làm tròn nửa lên, nil
// khi Created = 0. SuccessRateBp = Delivered × 10000 / (Delivered + Returned) làm tròn xuống, nil khi mẫu số 0 (Q5).
type CarrierRow struct {
	CarrierKey                                                                     string
	CarrierAccountID                                                               uuid.UUID
	AccountName                                                                    string
	Created, Cancelled, InProgress, Returning, DeliveryFailed, Delivered, Returned int64
	SuccessRateBp                                                                  *int32
	FeeTotal                                                                       int64
	FeeAvg                                                                         *int64
	CODTotal                                                                       int64
}

// CarriersReport là kết quả của báo cáo hãng: Status "ready", kỳ, các dòng (theo khóa hãng, tên tài khoản, mã tài
// khoản) và dòng tổng (tổng các dòng).
type CarriersReport struct {
	Status string
	Period Period
	Rows   []CarrierRow
	Totals CarrierRow
}

// ReportRows là số dòng cho thuộc tính report.rows của span.
func (r CarriersReport) ReportRows() int { return len(r.Rows) }

func carriersDef() Def {
	d := newDef[CarriersParams](KeyCarriers, FamilySales, permSalesRead)
	d.JSON = func(ctx context.Context, tx pgx.Tx, s DataScope, p Params) (any, error) {
		cp, ok := p.(*CarriersParams)
		if !ok {
			return nil, fmt.Errorf("tham số báo cáo hãng kiểu %T: %w", p, ErrBadParams)
		}
		return carriersReport(ctx, tx, s, *cp)
	}
	d.Rows, d.Columns, d.FileName = carriersRows, carriersColumns, periodFile("hang-van-chuyen")
	return d
}

// carriersReport đọc các dòng (carrierRows) rồi tính tỷ lệ, trung bình và dòng tổng trong Go.
func carriersReport(ctx context.Context, tx pgx.Tx, s DataScope, p CarriersParams) (CarriersReport, error) {
	period, err := p.Period()
	if err != nil {
		return CarriersReport{}, err
	}
	rows, err := carrierRows(ctx, tx, s, period)
	if err != nil {
		return CarriersReport{}, err
	}
	var total CarrierRow
	for i := range rows {
		rows[i].derive()
		total.add(rows[i])
	}
	total.derive()
	return CarriersReport{Status: "ready", Period: period, Rows: rows, Totals: total}, nil
}

// add cộng các số đếm và tiền của o vào r.
func (r *CarrierRow) add(o CarrierRow) {
	r.Created += o.Created
	r.Cancelled += o.Cancelled
	r.InProgress += o.InProgress
	r.Returning += o.Returning
	r.DeliveryFailed += o.DeliveryFailed
	r.Delivered += o.Delivered
	r.Returned += o.Returned
	r.FeeTotal += o.FeeTotal
	r.CODTotal += o.CODTotal
}

// derive tính tỷ lệ thành công và phí trung bình từ các số đếm của r.
func (r *CarrierRow) derive() {
	r.SuccessRateBp = rateBp(r.Delivered, r.Delivered+r.Returned)
	r.FeeAvg = avgHalfUp(r.FeeTotal, r.Created)
}

// rateBp là num × 10000 / den làm tròn xuống (phần vạn, hợp đồng ReportRateBp); nil khi den ≤ 0.
func rateBp(num, den int64) *int32 {
	if den <= 0 {
		return nil
	}
	v := int32(num * 10_000 / den) //nolint:gosec // G115: 0 ≤ num ≤ den nên kết quả trong 0..10000
	return &v
}

// avgHalfUp là sum / n làm tròn nửa lên tới đồng (sum ≥ 0); nil khi n ≤ 0.
func avgHalfUp(sum, n int64) *int64 {
	if n <= 0 {
		return nil
	}
	v := (2*sum + n) / (2 * n)
	return &v
}
