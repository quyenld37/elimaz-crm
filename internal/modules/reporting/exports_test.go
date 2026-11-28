package reporting_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/xlsx/xlsxtest"
)

// Xuất Excel chạy nền (Task 237 = M6-14) trên kịch bản chín đơn, kho tệp trong bộ nhớ (filestest.MemFileStore).

// exportEnv là runsEnv cộng kho tệp trong bộ nhớ.
type exportEnv struct {
	*runsEnv
	mem *files.MemStore
}

func newExportEnv(t *testing.T) *exportEnv {
	t.Helper()
	reporting.RegisterFilePurposes()
	return &exportEnv{runsEnv: newRunsEnv(t), mem: filestest.MemFileStore(t)}
}

// exporter là người xem toàn shop có quyền xuất tệp.
func (e *exportEnv) exporter(t *testing.T, email string) identity.AuthUser {
	t.Helper()
	return e.user(t, email, []string{"report.sales.read", "report.inventory.read", "report.export"}, "all", "")
}

// xhandler dựng RunHandler có kho fs (nil: kho bộ nhớ của env), log (nil: bỏ log).
func (e *exportEnv) xhandler(cfg config.ReportConfig, defs map[reporting.Key]reporting.Def, fs files.FileStore, log *slog.Logger) *reporting.RunHandler {
	if fs == nil {
		fs = e.mem
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return reporting.NewRunHandler(reporting.Deps{
		DB: e.st.App, Bus: e.hub, Queue: queue.NewQueue(), Files: fs, Cfg: config.Config{Report: cfg}, Defs: defs, Log: log,
	})
}

func (e *exportEnv) exec(t *testing.T, h *reporting.RunHandler, id uuid.UUID) commands.Outcome {
	t.Helper()
	out, err := h.Execute(t.Context(), e.job(t, id))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func xlsxInput(f reporting.Family, k reporting.Key, params string) reporting.CreateRunInput {
	return reporting.CreateRunInput{Family: f, Report: k, Format: reporting.FormatXLSX, Params: json.RawMessage(params)}
}

const careByParams = `{"from":"` + runDay + `","to":"` + runDay + `","groupBy":"care_by"}`

// exportRow là lần chạy cùng dòng tệp của nó (đọc bằng chủ bảng).
type exportRow struct {
	status                string
	rowCount              *int
	truncated             bool
	fileBytes             *int64
	expiresAt             time.Time
	fileID                *uuid.UUID
	key, bucket, purpose  string
	sha                   []byte
	size                  int64
	fileExpires           *time.Time
	fileDeleted, fileName string
}

func (e *exportEnv) exportRow(t *testing.T, id uuid.UUID) exportRow {
	t.Helper()
	var r exportRow
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		var key, bucket, purpose, deleted, name *string
		var size *int64
		err := tx.QueryRow(t.Context(), `SELECT r.status, r.row_count, r.truncated, r.file_bytes, r.expires_at, r.file_id,
				f.key, f.bucket, f.purpose, f.content_sha256, f.size_bytes, f.expires_at, f.deleted_at::text, f.original_name
			FROM reporting.report_runs r LEFT JOIN platform.files f ON f.id = r.file_id WHERE r.id = $1`, id).
			Scan(&r.status, &r.rowCount, &r.truncated, &r.fileBytes, &r.expiresAt, &r.fileID, &key, &bucket, &purpose, &r.sha,
				&size, &r.fileExpires, &deleted, &name)
		for dst, src := range map[*string]*string{&r.key: key, &r.bucket: bucket, &r.purpose: purpose, &r.fileDeleted: deleted, &r.fileName: name} {
			if src != nil {
				*dst = *src
			}
		}
		if size != nil {
			r.size = *size
		}
		return err //nolint:wrapcheck // test
	})
	return r
}

// book đọc tệp của khóa key trên kho bộ nhớ.
func (e *exportEnv) book(t *testing.T, key string) (*xlsxtest.Book, []byte) {
	t.Helper()
	rc, err := e.mem.Open(t.Context(), files.BucketExports, key, 100<<20)
	if err != nil {
		t.Fatalf("mở tệp %s: %v", key, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := xlsxtest.Read(data)
	if err != nil {
		t.Fatalf("đọc xlsx: %v", err)
	}
	return b, data
}

// sheetRows đọc trang đầu: mỗi dòng là các giá trị ô theo thứ tự.
func sheetRows(t *testing.T, b *xlsxtest.Book) []xlsxtest.Row {
	t.Helper()
	rows, err := b.Rows("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func values(r xlsxtest.Row) []string {
	out := make([]string, 0, len(r.Cells))
	for _, c := range r.Cells {
		out = append(out, c.Value)
	}
	return out
}

func (e *exportEnv) filesCount(t *testing.T) int {
	t.Helper()
	return e.st.CountIn(t, e.tenant, `SELECT count(*) FROM platform.files WHERE purpose = 'report_export'`)
}

// keyPattern là mẫu khóa tệp xuất báo cáo: reports/<tenant>/<run>/<sha256 hex>.xlsx (Task 515).
func keyPattern(tenant, run uuid.UUID) *regexp.Regexp {
	return regexp.MustCompile(`^reports/` + tenant.String() + `/` + run.String() + `/[0-9a-f]{64}\.xlsx$`)
}

// ordersJSON chạy bản xem tại chỗ của báo cáo đơn với phạm vi của u (để so số với tệp).
func (e *exportEnv) ordersJSON(t *testing.T, u identity.AuthUser, params string) *reporting.OrdersReport {
	t.Helper()
	def := reporting.Defs()[reporting.KeyOrders]
	p, err := reporting.DecodeParams(reporting.KeyOrders, json.RawMessage(params))
	if err != nil {
		t.Fatal(err)
	}
	var res any
	err = tenancy.WithTenant(t.Context(), e.st.App, e.tenant, func(tx pgx.Tx) error {
		seller, err := reporting.OwnSeller(t.Context(), tx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		res, err = def.JSON(t.Context(), tx, reporting.ScopeFor(u, seller), p)
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := res.(*reporting.OrdersReport)
	if !ok {
		t.Fatalf("kết quả %T", res)
	}
	return r
}

func i64(v int64) string { return strconv.FormatInt(v, 10) }

func optI64(v *int64) string {
	if v == nil {
		return "—"
	}
	return i64(*v)
}

// Báo cáo đơn theo nhân viên phụ trách ra Excel: ready, một dòng files (bucket exports, mục đích report_export, hạn
// bằng hạn lần chạy, tên tải về theo kỳ), khóa đúng mẫu, mã băm khớp nội dung; tệp có tiêu đề, dòng ghi kỳ/phạm vi,
// 3 dòng An, Bình, Chưa phân công và dòng tổng; mọi số bằng bản xem tại chỗ; ô tiền là số định dạng #,##0; audit
// report.export_ready.
func TestExport_OrdersCareBy(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	u, run := e.ordersRun(t)
	before := time.Now()
	if out := e.exec(t, e.xhandler(config.ReportConfig{}, nil, nil, nil), run); out.Status != commands.StatusDone {
		t.Fatalf("outcome = %+v", out)
	}
	r := e.exportRow(t, run)
	if r.status != "ready" || r.fileID == nil || r.rowCount == nil || *r.rowCount != 4 || r.truncated {
		t.Fatalf("lần chạy = %+v", r)
	}
	checkOrdersFileRow(t, e, run, r, before)
	b, data := e.book(t, r.key)
	if r.fileBytes == nil || *r.fileBytes != int64(len(data)) || r.size != int64(len(data)) || !strings.Contains(r.key, hexSHA(data)) ||
		hexSHA(data) != hexOf(r.sha) {
		t.Errorf("kích thước %v/%d, mã băm %x, khóa %s; tệp %d byte %s", r.fileBytes, r.size, r.sha, r.key, len(data), hexSHA(data))
	}
	checkOrdersSheet(t, b, e.ordersJSON(t, u, careByParams))
	if n := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'report.export_ready'
		AND entity_id = $1 AND actor_type = 'system' AND after->>'rows' = '4'`, run.String()); n != 1 {
		t.Errorf("audit report.export_ready = %d", n)
	}
}

// ordersRun tạo người xuất an@ và lần chạy xlsx báo cáo đơn theo nhân viên phụ trách.
func (e *exportEnv) ordersRun(t *testing.T) (identity.AuthUser, uuid.UUID) {
	t.Helper()
	u := e.exporter(t, "an@elimaz.test")
	return u, e.mustCreate(t, u, xlsxInput(reporting.FamilySales, reporting.KeyOrders, careByParams)).ID
}

// checkOrdersFileRow: dòng files của tệp báo cáo đơn (khóa, bucket, mục đích, hạn, tên, một dòng).
func checkOrdersFileRow(t *testing.T, e *exportEnv, run uuid.UUID, r exportRow, before time.Time) {
	t.Helper()
	if !keyPattern(e.tenant, run).MatchString(r.key) || r.bucket != "exports" || r.purpose != "report_export" {
		t.Errorf("tệp: khóa %q, bucket %q, mục đích %q", r.key, r.bucket, r.purpose)
	}
	if r.fileExpires == nil || !r.fileExpires.Equal(r.expiresAt) {
		t.Errorf("hạn tệp %v, hạn lần chạy %v", r.fileExpires, r.expiresAt)
	}
	if d := r.expiresAt.Sub(before); d < 168*time.Hour || d > 168*time.Hour+time.Minute {
		t.Errorf("hạn lần chạy cách lúc chạy %v, mong 168h", d)
	}
	if r.fileName != "bao-cao-don_"+runDay+"_"+runDay+".xlsx" {
		t.Errorf("tên tệp = %q", r.fileName)
	}
	if n := e.filesCount(t); n != 1 {
		t.Errorf("số dòng files = %d", n)
	}
}

// checkOrdersSheet: tệp báo cáo đơn không công thức, tiêu đề, dòng ghi kỳ, các dòng bằng bản xem tại chỗ, ô tiền số.
func checkOrdersSheet(t *testing.T, b *xlsxtest.Book, want *reporting.OrdersReport) {
	t.Helper()
	if parts := b.FormulaParts(); len(parts) != 0 {
		t.Errorf("có công thức: %v", parts)
	}
	rows := sheetRows(t, b)
	if len(rows) != 2+len(want.Rows)+1 || len(want.Rows) != 3 {
		t.Fatalf("tệp %d dòng, báo cáo %d dòng", len(rows), len(want.Rows))
	}
	if head := values(rows[0]); !slices.Equal(head, []string{
		"Nhân viên phụ trách", "Số đơn", "Chưa hủy", "Hủy", "Tiền hàng", "Phí ship thu khách",
		"Giá trị TB", "Giao thành công", "Hoàn", "Tỷ lệ thành công",
	}) {
		t.Errorf("tiêu đề = %q", head)
	}
	meta := strings.Join(values(rows[1]), "|")
	for _, part := range []string{"Kỳ: 17/11/2026 – 17/11/2026", "Ngày lên đơn", "Tất cả Page", "Tạo lúc"} {
		if len(rows[1].Cells) != 1 || !strings.Contains(meta, part) {
			t.Errorf("dòng ghi kỳ = %q, thiếu %q", meta, part)
		}
	}
	labels := []string{}
	for i, wr := range append(append([]reporting.OrdersRow{}, want.Rows...), want.Totals) {
		got := values(rows[2+i])
		labels = append(labels, got[0])
		exp := []string{
			wr.Label, i64(wr.Orders), optI64(wr.Active), optI64(wr.Cancelled), i64(wr.GoodsAmount),
			i64(wr.ShippingFeeCharged), optI64(wr.AvgGoodsAmount), i64(wr.Delivered), i64(wr.Returned),
			reporting.RateText(wr.SuccessRateBp),
		}
		if !slices.Equal(got, exp) {
			t.Errorf("dòng %d = %q, mong %q", i, got, exp)
		}
	}
	if !slices.Equal(labels, []string{"An", "Bình", "Chưa phân công", "Tổng"}) {
		t.Errorf("nhãn = %q", labels)
	}
	money := rows[2].Cells[4]
	st, err := b.Style(money.Style)
	if err != nil || money.Type != "" || st.NumFmt != "#,##0" {
		t.Errorf("ô tiền: kiểu %q, định dạng %+v %v", money.Type, st, err)
	}
}

// Danh sách chi tiết metric placed ra Excel: mọi đơn của tập (8 đơn O1–O6, O8, O9), không giới hạn trang 200.
func TestExport_DrilldownPlaced(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	r := e.drillExport(t, `,"limit":2`, config.ReportConfig{})
	if r.status != "ready" || r.rowCount == nil || *r.rowCount != 8 {
		t.Fatalf("lần chạy = %+v", r)
	}
	b, _ := e.book(t, r.key)
	rows := sheetRows(t, b)
	got := []string{}
	for _, row := range rows[2:] {
		got = append(got, row.Cells[0].Value)
	}
	want := e.codes(t, "O1", "O2", "O3", "O4", "O5", "O6", "O8", "O9")
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("mã đơn = %v, mong %v", got, want)
	}
}

// codes đọc mã đơn của các đơn theo tên kịch bản, đã sắp.
func (e *exportEnv) codes(t *testing.T, names ...string) []string {
	t.Helper()
	var out []string
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), `SELECT code FROM ord.orders WHERE id = ANY($1) ORDER BY code`, e.sc.IDs(names...))
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	})
	return out
}

// REPORT_MAX_ROWS = 5 với danh sách chi tiết 8 dòng → truncated, 5 dòng dữ liệu rồi dòng ghi chú cắt.
func TestExport_Truncated(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	r := e.drillExport(t, "", config.ReportConfig{MaxRows: 5})
	if r.status != "ready" || r.rowCount == nil || *r.rowCount != 5 || !r.truncated {
		t.Fatalf("lần chạy = %+v", r)
	}
	rows := sheetRows(t, mustBook(t, e, r.key))
	if len(rows) != 1+1+5+1 {
		t.Fatalf("tệp %d dòng, mong tiêu đề + ghi kỳ + 5 + ghi chú", len(rows))
	}
	if last := values(rows[len(rows)-1]); len(last) != 1 || last[0] != "Đã cắt ở 5 dòng, hãy thu hẹp khoảng thời gian" {
		t.Errorf("ghi chú = %q", last)
	}
}

// drillExport chạy xuất danh sách chi tiết metric placed của ngày thử (extra: thêm trường JSON) với cấu hình cfg.
func (e *exportEnv) drillExport(t *testing.T, extra string, cfg config.ReportConfig) exportRow {
	t.Helper()
	u := e.exporter(t, "an@elimaz.test")
	run := e.mustCreate(t, u, xlsxInput(reporting.FamilySales, reporting.KeyOrderDrilldown,
		`{"from":"`+runDay+`","to":"`+runDay+`","metric":"placed"`+extra+`}`))
	e.exec(t, e.xhandler(cfg, nil, nil, nil), run.ID)
	return e.exportRow(t, run.ID)
}

func mustBook(t *testing.T, e *exportEnv, key string) *xlsxtest.Book {
	t.Helper()
	b, _ := e.book(t, key)
	return b
}

// flakyStore là kho bọc: fails lần Put đầu trả lỗi; before chạy một lần trước lần Put đầu (Put lồng trong before chạy thẳng).
type flakyStore struct {
	files.FileStore
	fails  atomic.Int32
	fired  atomic.Bool
	before func()
	puts   atomic.Int32
	exists atomic.Int32
}

func (s *flakyStore) Put(ctx context.Context, b files.Bucket, key string, r io.Reader, size int64, ct string) error {
	if s.before != nil && s.fired.CompareAndSwap(false, true) {
		s.before() // lần Put lồng bên trong before không chạy lại before
	}
	s.puts.Add(1)
	if s.fails.Add(-1) >= 0 {
		return errors.New("kho lỗi giả")
	}
	err := s.FileStore.Put(ctx, b, key, r, size, ct)
	if errors.Is(err, files.ErrObjectExists) {
		s.exists.Add(1)
	}
	return err //nolint:wrapcheck // test
}

// flakyRun tạo lần chạy báo cáo đơn và RunHandler có kho bọc flakyStore.
func (e *exportEnv) flakyRun(t *testing.T) (uuid.UUID, *flakyStore, *reporting.RunHandler) {
	t.Helper()
	_, run := e.ordersRun(t)
	fs := &flakyStore{FileStore: e.mem}
	return run, fs, e.xhandler(config.ReportConfig{}, nil, fs, nil)
}

// Kho lỗi ở lần tải đầu → not_sent, lần chạy vẫn running, không dòng files; lần sau → ready, đúng 1 dòng files.
func TestExport_PutFailsThenRetries(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	run, fs, h := e.flakyRun(t)
	fs.fails.Store(1)
	if out := e.exec(t, h, run); out.Status != commands.StatusNotSent {
		t.Fatalf("lần đầu = %+v, mong not_sent", out)
	}
	if s, _, _ := e.row(t, run); s != "running" || e.filesCount(t) != 0 {
		t.Fatalf("sau lỗi kho: %s, %d dòng files", s, e.filesCount(t))
	}
	if out := e.exec(t, h, run); out.Status != commands.StatusDone {
		t.Fatalf("lần sau = %+v", out)
	}
	if r := e.exportRow(t, run); r.status != "ready" || e.filesCount(t) != 1 {
		t.Errorf("sau khi chạy lại: %s, %d dòng files", r.status, e.filesCount(t))
	}
}

// Lần tải đầu xong nhưng ghi kết quả không thành (giả bằng cách đưa lần chạy về running, bỏ dòng files): lần sau Put
// cùng khóa → ErrObjectExists (khóa theo mã băm nội dung, dữ liệu không đổi) → vẫn ready, đúng 1 dòng files cùng khóa.
func TestExport_ObjectExistsIsSuccess(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	run, fs, h := e.flakyRun(t)
	e.exec(t, h, run)
	first := e.exportRow(t, run)
	e.ownerExec(t, `UPDATE reporting.report_runs SET status = 'running', file_id = NULL, row_count = NULL, file_bytes = NULL,
		finished_at = NULL WHERE id = $1`, run)
	e.ownerExec(t, `DELETE FROM platform.files WHERE id = $1`, *first.fileID)
	if out := e.exec(t, h, run); out.Status != commands.StatusDone {
		t.Fatalf("chạy lại = %+v", out)
	}
	again := e.exportRow(t, run)
	if again.status != "ready" || again.key != first.key || e.filesCount(t) != 1 || fs.exists.Load() != 1 {
		t.Errorf("chạy lại: %s, khóa %s (trước %s), %d dòng files, %d lần ErrObjectExists", again.status, again.key,
			first.key, e.filesCount(t), fs.exists.Load())
	}
	if again.bucket != "exports" || again.purpose != "report_export" || again.fileExpires == nil || !again.fileExpires.Equal(again.expiresAt) {
		t.Errorf("dòng files = %+v", again)
	}
}

// Hai lần executeXLSX cùng một lần chạy, dữ liệu đổi ở giữa: A dựng tệp, trước khi A tải lên thì tên nhân viên đổi và
// B chạy trọn (ready); A tải lên khóa khác rồi thua ở bước ghi → đúng một ready, đúng 1 dòng files, file_id trỏ khóa
// của B (tệp có tên mới).
func TestExport_TwoExecutionsOneWinner(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	run, fs, h := e.flakyRun(t)
	var bOut commands.Outcome
	fs.before = func() {
		e.ownerExec(t, `UPDATE identity.users SET display_name = 'An Mới' WHERE id = $1`, e.sc.An)
		bOut = e.exec(t, h, run)
	}
	aOut := e.exec(t, h, run)
	if aOut.Status != commands.StatusDone || bOut.Status != commands.StatusDone || fs.puts.Load() != 2 {
		t.Fatalf("A = %+v, B = %+v, %d lần Put", aOut, bOut, fs.puts.Load())
	}
	r := e.exportRow(t, run)
	if r.status != "ready" || e.filesCount(t) != 1 || len(e.mem.Keys(files.BucketExports)) != 2 {
		t.Fatalf("lần chạy %s, %d dòng files, %d đối tượng", r.status, e.filesCount(t), len(e.mem.Keys(files.BucketExports)))
	}
	labels := []string{}
	for _, row := range sheetRows(t, mustBook(t, e, r.key))[2:] {
		labels = append(labels, row.Cells[0].Value)
	}
	if !slices.Contains(labels, "An Mới") {
		t.Errorf("tệp được đăng ký không phải của B: %q", labels)
	}
	if n := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'report.export_ready'`); n != 1 {
		t.Errorf("audit export_ready = %d", n)
	}
}

// Tên sản phẩm là công thức trong báo cáo bán chạy → ô chữ bắt đầu bằng "'", không phần tử <f> nào.
func TestExport_FormulaEscaped(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	u := e.exporter(t, "an@elimaz.test")
	evil := `=HYPERLINK("http://evil","x")`
	e.ownerExec(t, `UPDATE catalog.products SET name = $1 WHERE name = 'Áo A'`, evil)
	run := e.mustCreate(t, u, xlsxInput(reporting.FamilySales, reporting.KeyBestSellers, `{"from":"`+runDay+`","to":"`+runDay+`"}`))
	e.exec(t, e.xhandler(config.ReportConfig{}, nil, nil, nil), run.ID)
	b := mustBook(t, e, e.exportRow(t, run.ID).key)
	if parts := b.FormulaParts(); len(parts) != 0 {
		t.Errorf("có công thức: %v", parts)
	}
	found := false
	for _, row := range sheetRows(t, b) {
		for _, c := range row.Cells {
			if strings.Contains(c.Value, "HYPERLINK") {
				found = true
				if c.Value != "'"+evil || c.Formula {
					t.Errorf("ô %s = %q (công thức %v)", c.Ref, c.Value, c.Formula)
				}
			}
		}
	}
	if !found {
		t.Error("không thấy ô tên sản phẩm")
	}
}

// Dọn: tệp xlsx ready quá hạn → expired, dòng files có deleted_at; tệp chưa hạn giữ nguyên.
func TestExport_CleanupExpiresFiles(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	u := e.exporter(t, "an@elimaz.test")
	old := e.mustCreate(t, u, xlsxInput(reporting.FamilySales, reporting.KeyOrders, careByParams))
	fresh := e.mustCreate(t, u, xlsxInput(reporting.FamilySales, reporting.KeyCarriers, `{"from":"`+runDay+`","to":"`+runDay+`"}`))
	h := e.xhandler(config.ReportConfig{}, nil, nil, nil)
	e.exec(t, h, old.ID)
	e.exec(t, h, fresh.ID)
	e.ownerExec(t, `UPDATE reporting.report_runs SET expires_at = now() - interval '1 second' WHERE id = $1`, old.ID)
	out := e.cleanup(t, e.st.App)
	if r := e.exportRow(t, old.ID); r.status != "expired" || r.fileDeleted == "" {
		t.Errorf("quá hạn: %s, deleted_at %q", r.status, r.fileDeleted)
	}
	if r := e.exportRow(t, fresh.ID); r.status != "ready" || r.fileDeleted != "" {
		t.Errorf("chưa hạn: %s, deleted_at %q", r.status, r.fileDeleted)
	}
	if !strings.Contains(string(out), `"expiredFiles":1`) {
		t.Errorf("kết quả dọn = %s", out)
	}
}

// Log của cả lượt xuất (handler slog vào bộ nhớ) chỉ có mã lần chạy, báo cáo, số dòng, số byte, thời gian: không tên
// sản phẩm, tên nhân viên, mã đơn nào của dữ liệu thử.
func TestExport_LogHasNoData(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	u := e.exporter(t, "an@elimaz.test")
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&lockedWriter{w: &buf}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := e.xhandler(config.ReportConfig{}, nil, nil, log)
	for _, in := range []reporting.CreateRunInput{
		xlsxInput(reporting.FamilySales, reporting.KeyOrderDrilldown, `{"from":"`+runDay+`","to":"`+runDay+`","metric":"placed"}`),
		xlsxInput(reporting.FamilySales, reporting.KeyBestSellers, `{"from":"`+runDay+`","to":"`+runDay+`"}`),
		xlsxInput(reporting.FamilySales, reporting.KeyOrders, careByParams),
	} {
		e.exec(t, h, e.mustCreate(t, u, in).ID)
	}
	text := buf.String()
	if !strings.Contains(text, `"run_id"`) || !strings.Contains(text, `"bytes"`) || !strings.Contains(text, `"duration_ms"`) {
		t.Errorf("log thiếu trường: %s", text)
	}
	secrets := append(e.codes(t, "O1", "O2", "O3", "O4", "O5", "O6", "O7", "O8", "O9"), "Áo A", "Quần B", "Váy C", "Bình",
		"Chưa phân công", "AOA-S")
	for _, s := range secrets {
		if strings.Contains(text, s) {
			t.Errorf("log chứa %q", s)
		}
	}
}

// lockedWriter ghi vào w có khóa (handler slog có thể ghi từ nhiều goroutine).
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p) //nolint:wrapcheck // test
}

// sqlLog ghi lại câu SQL của mọi truy vấn trên pool (pgx.QueryTracer).
type sqlLog struct {
	mu   sync.Mutex
	sqls []string
}

func (l *sqlLog) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sqls = append(l.sqls, d.SQL)
	return ctx
}

func (*sqlLog) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (l *sqlLog) index(match func(string) bool) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.IndexFunc(l.sqls, match)
}

// Thứ tự khóa của dọn (ADR-46 R4): câu FOR UPDATE trên report_runs chạy trước mọi câu chạm platform.files. Dọn chạy
// song song với một transaction đang giữ khóa report_runs → files của một lần chạy quá hạn khác (như executeXLSX):
// dọn bỏ qua dòng đang khóa (SKIP LOCKED) rồi xong, không deadlock.
func TestExport_CleanupLockOrder(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	u := e.exporter(t, "an@elimaz.test")
	held := e.mustCreate(t, u, xlsxInput(reporting.FamilySales, reporting.KeyOrders, careByParams))
	free := e.mustCreate(t, u, xlsxInput(reporting.FamilySales, reporting.KeyCarriers, `{"from":"`+runDay+`","to":"`+runDay+`"}`))
	h := e.xhandler(config.ReportConfig{}, nil, nil, nil)
	e.exec(t, h, held.ID)
	e.exec(t, h, free.ID)
	e.ownerExec(t, `UPDATE reporting.report_runs SET expires_at = now() - interval '1 second' WHERE id = ANY($1)`,
		[]uuid.UUID{held.ID, free.ID})
	deadlocks := e.deadlocks(t)

	tx, err := e.st.App.Pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	heldFile := *e.exportRow(t, held.ID).fileID
	for _, q := range []struct {
		sql string
		arg any
	}{
		{`SELECT set_config('app.tenant_id', $1, true)`, e.tenant.String()},
		{`SELECT id FROM reporting.report_runs WHERE id = $1 FOR UPDATE`, held.ID},
		{`SELECT id FROM platform.files WHERE id = $1 FOR UPDATE`, heldFile},
	} {
		if _, err := tx.Exec(t.Context(), q.sql, q.arg); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}

	log := &sqlLog{}
	cfg := e.st.App.Pool.Config()
	cfg.ConnConfig.Tracer = log
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	done := make(chan json.RawMessage, 1)
	go func() { done <- e.cleanupQuiet(t, &db.DB{Pool: pool}) }()
	var out json.RawMessage
	select {
	case out = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("dọn treo khi một lần chạy đang bị khóa")
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"expiredFiles":1`) {
		t.Errorf("kết quả dọn = %s, mong 1 tệp (dòng đang khóa bị bỏ qua)", out)
	}
	if r := e.exportRow(t, free.ID); r.status != "expired" || r.fileDeleted == "" {
		t.Errorf("lần chạy không khóa: %s, deleted_at %q", r.status, r.fileDeleted)
	}
	if r := e.exportRow(t, held.ID); r.status != "ready" {
		t.Errorf("lần chạy đang khóa = %s, mong giữ ready", r.status)
	}
	lockRuns := log.index(func(s string) bool {
		return strings.Contains(s, "reporting.report_runs") && strings.Contains(s, "FOR UPDATE")
	})
	touchFiles := log.index(func(s string) bool { return strings.Contains(s, "platform.files") })
	if lockRuns < 0 || touchFiles < 0 || lockRuns > touchFiles {
		t.Errorf("thứ tự câu: khóa report_runs ở %d, chạm platform.files ở %d", lockRuns, touchFiles)
	}
	if n := e.deadlocks(t); n != deadlocks {
		t.Errorf("deadlocks %d → %d", deadlocks, n)
	}
}

// deadlocks là số deadlock của database thử (pg_stat_database).
func (e *exportEnv) deadlocks(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := e.st.App.Pool.QueryRow(t.Context(), `SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()`).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func hexSHA(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hexOf(b []byte) string { return hex.EncodeToString(b) }
