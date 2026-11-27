package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Khung chung của các GET báo cáo M6 (Task 228): handler của từng báo cáo dựng tham số rồi gọi runReport; ở tệp của
// vùng chỉ còn việc đổi kết quả sang kiểu của hợp đồng.

// reportDayLayout là dạng ngày YYYY-MM-DD của tham số from, to, date.
const reportDayLayout = "2006-01-02"

// reportDay đổi tham số ngày (ogen đã đọc dạng date thành 00:00 UTC) về chuỗi ngày giờ Việt Nam của module.
func reportDay(t time.Time) string { return t.Format(reportDayLayout) }

// runReport chạy báo cáo k tại chỗ cho nhân viên đang gọi (Deps.Reports). Không chạy tại chỗ được (hết chỗ, quá hạn)
// → bg khác nil (200 run_in_background, màn hình tạo lần chạy nền); lỗi tham số, lỗi khác trả err (bảng errors_reports).
func (h *handler) runReport(ctx context.Context, k reporting.Key, p reporting.Params) (res any, bg *gen.RunInBackground, err error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, nil, errNoAuthUser
	}
	res, err = h.d.Reports.Run(ctx, u, k, p)
	if be, ok := errors.AsType[*reporting.BackgroundError](err); ok {
		return nil, &gen.RunInBackground{
			Status: gen.RunInBackgroundStatusRunInBackground, Reason: gen.RunInBackgroundReason(be.Reason),
		}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("chạy báo cáo: %w", err)
	}
	return res, nil, nil
}
