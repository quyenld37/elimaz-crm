package httpapi

// Bảng đổi lỗi của vùng báo cáo M6 sang mã HTTP (Task 228; Task 441: mỗi vùng một tệp errors_<vùng>.go; thứ tự kiểm
// giữa các vùng ở domainErrorMappers, errors.go). Mã đã khai ở x-error-codes của openapi.yaml (Task 225).

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ogen-go/ogen/ogenerrors"

	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Mã lỗi chỉ vùng báo cáo trả.
const (
	codeBadParams     = "bad_params"
	codeBadDate       = "bad_date"
	codeRangeTooLong  = "range_too_long"
	codeUnknownReport = "unknown_report"
	codeNoSnapshot    = "no_snapshot"
	codeExportForbid  = "export_forbidden"
	codeRunNotFound   = "run_not_found"
	codeTooManyRuns   = "too_many_runs"
)

// reportsError đổi lỗi nghiệp vụ của báo cáo sang mã HTTP. ErrNotImplemented: phần của báo cáo mà task của nó chưa
// làm → 501 như operation chưa làm.
func reportsError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{reporting.ErrBadDate, http.StatusBadRequest, codeBadDate},
		{reporting.ErrFromAfterTo, http.StatusBadRequest, codeBadDate},
		{reporting.ErrRangeTooLong, http.StatusBadRequest, codeRangeTooLong},
		{reporting.ErrBadParams, http.StatusBadRequest, codeBadParams},
		{reporting.ErrUnknownReport, http.StatusBadRequest, codeUnknownReport},
		{reporting.ErrPageForbidden, http.StatusForbidden, codeForbidden},
		{reporting.ErrWarehouseForbidden, http.StatusForbidden, codeForbidden},
		{reporting.ErrNoSnapshot, http.StatusNotFound, codeNoSnapshot},
		{reporting.ErrExportForbidden, http.StatusForbidden, codeExportForbid},
		{reporting.ErrForbidden, http.StatusForbidden, codeForbidden},
		{reporting.ErrTooManyRuns, http.StatusTooManyRequests, codeTooManyRuns},
		{reporting.ErrRunNotFound, http.StatusNotFound, codeRunNotFound},
		{reporting.ErrNotImplemented, http.StatusNotImplemented, codeNotImplemented},
	})
}

// reportDateParams là tham số ngày (format: date) của các GET báo cáo.
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var reportDateParams = map[string]bool{"from": true, "to": true, "date": true}

// reportParamError: tham số query của route báo cáo mà ogen không đọc được → 400 với mã của hợp đồng M6: tham số ngày
// (sai dạng, không có trên lịch, thiếu) → bad_date như ngày sai mà module bắt ("sai dạng, from > to → 400 bad_date");
// tham số khác (enum groupBy, metric, basis, limit ngoài miền, thiếu metric…) → bad_params ("trường lạ hay sai → 400
// bad_params", QĐ-230-4), thay cho bad_request chung.
func reportParamError(path string, err error) (code string, ok bool) {
	if !strings.HasPrefix(path, "/api/v1/reports/") {
		return "", false
	}
	var pe *ogenerrors.DecodeParamError
	if !errors.As(err, &pe) || pe.In != "query" {
		return "", false
	}
	if reportDateParams[pe.Name] {
		return codeBadDate, true
	}
	return codeBadParams, true
}
