package reporting_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// retryDef là báo cáo đơn thử: lần chạy đầu đẩy first dòng (bỏ qua lỗi của emit, như câu đọc còn trả dòng) rồi trả
// lỗi 40001 giả (RetryTx chạy lại hàm); các lần sau đẩy then dòng.
func retryDef(first, then int, calls *atomic.Int32) map[reporting.Key]reporting.Def {
	d := reporting.Defs()[reporting.KeyOrders]
	d.Columns = func(reporting.Params) []xlsx.Column { return []xlsx.Column{{Title: "Số", Kind: xlsx.KindInt}} }
	d.Rows = func(_ context.Context, _ pgx.Tx, _ reporting.DataScope, _ reporting.Params, emit func([]xlsx.Cell) error) error {
		n := then
		attempt := calls.Add(1)
		if attempt == 1 {
			n = first
		}
		for i := range n {
			_ = emit([]xlsx.Cell{xlsx.Int(int64(attempt*100) + int64(i))})
		}
		if attempt == 1 {
			return fmt.Errorf("câu cuối: %w", &pgconn.PgError{Code: "40001", Message: "giả xung đột tuần tự"})
		}
		return nil
	}
	return map[reporting.Key]reporting.Def{reporting.KeyOrders: d}
}

// Thử lại giữa chừng (C19): lần đầu đẩy 7 dòng rồi 40001 → RetryTx chạy lại; tệp có đúng 7 dòng dữ liệu của lần sau
// (không 14), mã băm trong khóa và dòng files bằng mã băm của tệp trên kho.
func TestExportRetry_FileRebuiltFromScratch(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	_, run := e.ordersRun(t)
	var calls atomic.Int32
	if out := e.exec(t, e.xhandler(config.ReportConfig{}, retryDef(7, 7, &calls), nil, nil), run); out.Status != commands.StatusDone {
		t.Fatalf("outcome = %+v", out)
	}
	r := e.exportRow(t, run)
	if calls.Load() != 2 || r.status != "ready" || r.rowCount == nil || *r.rowCount != 7 || r.truncated {
		t.Fatalf("%d lần chạy hàm, lần chạy %+v", calls.Load(), r)
	}
	b, data := e.book(t, r.key)
	rows := sheetRows(t, b)
	var nums []string
	for _, row := range rows[2:] {
		nums = append(nums, row.Cells[0].Value)
	}
	if strings.Join(nums, ",") != "200,201,202,203,204,205,206" {
		t.Errorf("dòng dữ liệu = %v", nums)
	}
	if sha := hexSHA(data); hexOf(r.sha) != sha || !strings.HasSuffix(r.key, "/"+sha+".xlsx") || r.size != int64(len(data)) {
		t.Errorf("mã băm dòng files %x, khóa %s; tệp %s, %d byte (dòng files %d)", r.sha, r.key, sha, len(data), r.size)
	}
}

// Lần đầu đã ghi quá REPORT_MAX_ROWS (7 > 5) rồi 40001; lần sau chỉ 3 dòng → truncated tính lại là false, 3 dòng, không
// dòng ghi chú cắt.
func TestExportRetry_TruncatedRecomputed(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	_, run := e.ordersRun(t)
	var calls atomic.Int32
	e.exec(t, e.xhandler(config.ReportConfig{MaxRows: 5}, retryDef(7, 3, &calls), nil, nil), run)
	r := e.exportRow(t, run)
	if r.status != "ready" || r.rowCount == nil || *r.rowCount != 3 || r.truncated {
		t.Fatalf("lần chạy %+v", r)
	}
	rows := sheetRows(t, mustBook(t, e, r.key))
	if len(rows) != 2+3 {
		t.Errorf("tệp %d dòng, mong tiêu đề + ghi kỳ + 3", len(rows))
	}
}
