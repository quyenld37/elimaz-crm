package statement_test

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/cash/internal/statement"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

const (
	csvType  = "text/csv"
	xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// importEnv: database thử, tenant A có tài khoản hãng giả (shiptest), kho tệp trong bộ nhớ, module cash có lệnh đọc
// tệp và worker chạy lệnh đó; log của worker ghi vào logs.
type importEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	user   uuid.UUID
	shop   *shiptest.Shop
	store  *files.MemStore
	mod    *cash.Module
	hs     []commands.Handler
	parser *statement.Parser
	wk     *queue.Worker
	logs   *bytes.Buffer
}

func newImportEnv(t *testing.T) *importEnv {
	t.Helper()
	cash.RegisterFilePurposes()
	e := &importEnv{t: t, staff: testkit.OpenStaff(t), store: testkit.MemFileStore(t), logs: &bytes.Buffer{}}
	e.tenant = e.staff.Tenant(t, "A")
	e.user = e.staff.User(t, e.tenant, "ketoan@elimaz.test", "active")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = shiptest.NewShop(t, tx, e.tenant)
		return nil
	})
	e.mod = cash.New(cash.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Files: e.store, Events: events.NewRegistry()})
	e.hs = e.mod.CommandHandlers()
	for _, h := range e.hs {
		if p, ok := h.(*statement.Parser); ok {
			e.parser = p
		}
	}
	if e.parser == nil {
		t.Fatal("CommandHandlers không có lệnh đọc bảng kê")
	}
	e.wk = e.worker(e.hs)
	return e
}

func (e *importEnv) worker(hs []commands.Handler) *queue.Worker {
	return queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "w-" + uuid.NewString(), Poll: 50 * time.Millisecond, Commands: commands.NewRunner(e.staff.App, hs),
		Logger: slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
}

func (e *importEnv) tx(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

// upload tải body lên như trình duyệt của kế toán: CreateUpload, PUT tới đường dẫn ký sẵn (đúng header đã ký),
// CompleteUpload; mục đích cod_statement.
func (e *importEnv) upload(name, contentType string, body []byte) uuid.UUID {
	e.t.Helper()
	var f files.File
	var put files.PresignedPut
	if err := e.tx(func(tx pgx.Tx) error {
		var err error
		f, put, err = files.CreateUpload(e.t.Context(), tx, e.store, files.UploadInput{
			TenantID: e.tenant, Purpose: cash.StatementPurposeKey, FileName: name, ContentType: contentType,
			SizeBytes: int64(len(body)),
		}, e.user)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("CreateUpload: %v", err)
	}
	req, err := http.NewRequestWithContext(e.t.Context(), http.MethodPut, put.URL, bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range put.Headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		e.t.Fatalf("PUT tệp: %d", resp.StatusCode)
	}
	done, err := files.CompleteUpload(e.t.Context(), e.staff.App, e.store, queue.NewQueue(), e.tenant, f.ID)
	if err != nil || done.Status != files.StatusUploaded {
		e.t.Fatalf("CompleteUpload: %s, %v", done.Status, err)
	}
	return done.ID
}

// importFile nhập tệp fileID cho tài khoản account như lớp HTTP: đọc, băm ngoài transaction rồi ghi trong một tx.
func (e *importEnv) importFile(fileID, account uuid.UUID, format string) (cash.Statement, error) {
	f, err := e.mod.Service.PrepareStatementImport(e.t.Context(), e.tenant, fileID)
	if err != nil {
		return cash.Statement{}, err //nolint:wrapcheck // test
	}
	var st cash.Statement
	err = e.tx(func(tx pgx.Tx) error {
		var err error
		st, err = e.mod.Service.ImportStatement(e.t.Context(), tx, e.tenant, f, cash.ImportStatementInput{
			CarrierAccountID: account, Format: format, ImportedBy: e.user,
		})
		return err //nolint:wrapcheck // test
	})
	return st, err
}

// run tải body, nhập cho tài khoản của người bán ELIMAZ, chạy hết việc và trả bảng kê sau khi đọc.
func (e *importEnv) run(name, contentType string, body []byte) cash.StatementDetail {
	e.t.Helper()
	st, err := e.importFile(e.upload(name, contentType, body), e.shop.Account, "")
	if err != nil {
		e.t.Fatalf("nhập: %v", err)
	}
	if st.Status != statement.StatusImported || st.Format != "elimaz_v1" || st.CarrierKey != shiptest.CarrierKey {
		e.t.Fatalf("bảng kê mới: %+v", st)
	}
	e.staff.DrainJobs(e.t, e.wk)
	return e.get(st.ID, "", 0, 100)
}

func (e *importEnv) get(id uuid.UUID, match string, after int32, limit int) cash.StatementDetail {
	e.t.Helper()
	var d cash.StatementDetail
	if err := e.tx(func(tx pgx.Tx) error {
		var err error
		d, err = e.mod.Service.GetStatement(e.t.Context(), tx, id, match, after, limit)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("GetStatement: %v", err)
	}
	return d
}

// count chạy câu đếm q trong tenant của e (testkit.Staff.CountIn).
func (e *importEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

// jobStatus là trạng thái lệnh đọc tệp của bảng kê id (platform.jobs không có RLS).
func (e *importEnv) jobStatus(id uuid.UUID) string {
	e.t.Helper()
	var s string
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(),
		`SELECT status FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2`, statement.ParseKind().Name, id.String()).Scan(&s); err != nil {
		e.t.Fatalf("đọc lệnh: %v", err)
	}
	return s
}

// TI02 (phần đọc tệp): elimaz_v1.csv 3 dòng → parsed, 3 dòng đúng số nguyên, số dòng trong tệp, line_count 3, kỳ
// bảng kê theo ngày chuyển; lệnh done; tổng theo kết quả khớp (mọi dòng pending); trang 2 dòng có con trỏ, trang sau
// còn 1 dòng; lọc matched → không dòng nào.
func TestImport_ThreeLinesParsed(t *testing.T) {
	e := newImportEnv(t)
	d := e.run("bang-ke.csv", csvType, testdata(t, "elimaz_v1_ba_dong.csv"))
	st := d.Statement
	if st.Status != statement.StatusParsed || st.LineCount != 3 || st.ParseError != nil ||
		!st.PeriodFrom.Equal(day("2026-11-05")) || !st.PeriodTo.Equal(day("2026-11-06")) || st.Version != 3 {
		t.Fatalf("bảng kê: %+v", st)
	}
	got := make([]string, 0, len(d.Lines))
	for _, l := range d.Lines {
		got = append(got, fmt.Sprintf("%d %s %d %d %d %d %s %s", l.LineNo, l.TrackingCode, l.COD, l.Fee, l.OtherFee, l.Net,
			l.PaidOn.Format("2006-01-02"), l.MatchStatus))
	}
	want := []string{
		"2 FK0001 630000 32000 0 598000 2026-11-05 pending", "3 FK0002 0 25000 0 -25000 2026-11-05 pending",
		"4 NHANH9999 450000 30000 0 420000 2026-11-06 pending",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("dòng %v, mong %v", got, want)
	}
	if fmt.Sprint(d.Totals) != fmt.Sprint([]cash.MatchTotal{{MatchStatus: "pending", Lines: 3, COD: 1080000}}) {
		t.Errorf("tổng %+v", d.Totals)
	}
	if s := e.jobStatus(st.ID); s != "done" {
		t.Errorf("lệnh %s, mong done", s)
	}
	p1 := e.get(st.ID, "", 0, 2)
	if len(p1.Lines) != 2 || p1.Next != 3 {
		t.Fatalf("trang 1: %d dòng, next %d", len(p1.Lines), p1.Next)
	}
	if p2 := e.get(st.ID, "", p1.Next, 2); len(p2.Lines) != 1 || p2.Lines[0].LineNo != 4 || p2.Next != 0 {
		t.Fatalf("trang 2: %+v", p2)
	}
	if m := e.get(st.ID, "matched", 0, 10); len(m.Lines) != 0 || len(m.Totals) != 1 {
		t.Fatalf("lọc matched: %+v", m)
	}
}

// TI04: cùng tệp nhập lại → *DuplicateStatementError kèm mã bảng kê cũ; cùng nội dung tải lên lại với tên khác → vẫn
// trùng (băm nội dung); tài khoản của người bán khác cùng hãng → vẫn trùng (khóa theo hãng); hai lần nhập song song cùng
// nội dung → đúng một bảng kê, một lần trùng. Không thêm bảng kê, không thêm lệnh.
func TestImport_DuplicateRejected(t *testing.T) {
	e := newImportEnv(t)
	body := testdata(t, "elimaz_v1_ba_dong.csv")
	file := e.upload("bang-ke.csv", csvType, body)
	first, err := e.importFile(file, e.shop.Account, "")
	if err != nil {
		t.Fatal(err)
	}
	for name, try := range map[string]func() error{
		"cùng tệp": func() error { _, err := e.importFile(file, e.shop.Account, ""); return err },
		"đổi tên tệp": func() error {
			_, err := e.importFile(e.upload("doi-ten.csv", csvType, body), e.shop.Account, "")
			return err
		},
		"người bán khác": func() error { _, err := e.importFile(file, e.shop.PartnerAccount, ""); return err },
	} {
		err := try()
		de, ok := errors.AsType[*cash.DuplicateStatementError](err)
		if !ok || !errors.Is(err, cash.ErrStatementDuplicate) || de.StatementID != first.ID {
			t.Errorf("%s: %v, mong trùng %s", name, err, first.ID)
		}
	}
	if n := e.count("SELECT count(*) FROM cash.cod_statements"); n != 1 {
		t.Errorf("%d bảng kê, mong 1", n)
	}

	other := testdata(t, "elimaz_v1_thieu_tien_cod.csv")
	a, b := e.upload("a.csv", csvType, other), e.upload("b.csv", csvType, other)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, f := range []uuid.UUID{a, b} {
		wg.Go(func() { _, errs[i] = e.importFile(f, e.shop.Account, "") })
	}
	wg.Wait()
	dups := 0
	for _, err := range errs {
		switch {
		case errors.Is(err, cash.ErrStatementDuplicate):
			dups++
		case err != nil:
			t.Errorf("nhập song song: %v", err)
		}
	}
	var jobs int64
	if err := e.staff.Owner.Pool.QueryRow(t.Context(), `SELECT count(*) FROM platform.jobs WHERE kind = $1`,
		statement.ParseKind().Name).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if dups != 1 || e.count("SELECT count(*) FROM cash.cod_statements") != 2 || jobs != 2 {
		t.Errorf("song song: %d lần trùng, %d lệnh; mong 1 lần trùng, 2 bảng kê, 2 lệnh", dups, jobs)
	}
}

// Bản .xlsx cùng dữ liệu, ô tiền FK0001 là công thức =600000+30000 có giá trị đã tính 630.000 → lưu 630.000.
func TestImport_XlsxFormulaUsesCachedValue(t *testing.T) {
	e := newImportEnv(t)
	d := e.run("bang-ke.xlsx", xlsxType, book(t, [][]string{
		{"ma_van_don", "tien_cod", "phi_van_chuyen", "thuc_nhan"},
		{"FK0001", formulaCell + "600000+30000|630000", "32000", "598000"},
	}))
	if d.Statement.Status != statement.StatusParsed || len(d.Lines) != 1 || d.Lines[0].COD != 630000 {
		t.Fatalf("%+v", d)
	}
}

// Lỗi định dạng → parse_failed với parse_error {code, column, line}, không dòng nào, lệnh dead (rejected) không chạy
// lại: thiếu cột tien_cod; "12,5" ở dòng 17; 50.001 dòng; tệp quá giới hạn đọc (lệnh có MaxBytes nhỏ hơn tệp).
func TestImport_FormatErrorsFailStatement(t *testing.T) {
	e := newImportEnv(t)
	for _, c := range planFormatCases(t) {
		t.Run(c.name, func(t *testing.T) {
			e.t = t
			d := e.run(c.name+".csv", csvType, c.content)
			if d.Statement.Status != statement.StatusParseFailed || d.Statement.ParseError == nil ||
				*d.Statement.ParseError != c.want || len(d.Lines) != 0 || d.Statement.LineCount != 0 {
				t.Fatalf("bảng kê %+v lỗi %+v", d.Statement, d.Statement.ParseError)
			}
			if s := e.jobStatus(d.Statement.ID); s != "dead" {
				t.Errorf("lệnh %s, mong dead", s)
			}
		})
	}
	t.Run("quá MaxBytes của lệnh", func(t *testing.T) {
		e.t = t
		lim := statement.DefaultLimits()
		lim.MaxBytes = 100
		statement.SetLimits(e.parser, lim)
		defer statement.SetLimits(e.parser, statement.DefaultLimits())
		d := e.run("lon.csv", csvType, csvLines(20))
		if d.Statement.Status != statement.StatusParseFailed || d.Statement.ParseError.Code != "too_large" {
			t.Fatalf("%+v %+v", d.Statement, d.Statement.ParseError)
		}
	})
}

// Tệp 11 MiB (ghi thẳng lên kho, vượt giới hạn tải lên) → từ chối ngay khi nhập (files.ErrTooLarge) mà không đọc nội
// dung: cấp phát thêm dưới 1 MiB; không bảng kê nào.
func TestImport_TooLargeRejectedBeforeRead(t *testing.T) {
	e := newImportEnv(t)
	var id uuid.UUID
	var bucket, key string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		id = filestest.Uploaded(t, tx, e.tenant, cash.StatementPurposeKey)
		return tx.QueryRow(t.Context(), `SELECT bucket, key FROM platform.files WHERE id = $1`, id).Scan(&bucket, &key)
	})
	big := bytes.Repeat([]byte("a"), 11<<20)
	if err := e.store.Put(t.Context(), files.Bucket(bucket), key, bytes.NewReader(big), int64(len(big)), csvType); err != nil {
		t.Fatal(err)
	}
	var err error
	alloc := allocDuring(func() { _, err = e.mod.Service.PrepareStatementImport(t.Context(), e.tenant, id) })
	if !errors.Is(err, files.ErrTooLarge) || alloc > 1<<20 {
		t.Fatalf("lỗi %v, cấp %d KB; mong files.ErrTooLarge, dưới 1 MiB", err, alloc>>10)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_statements"); n != 0 {
		t.Errorf("%d bảng kê", n)
	}
}

// allocDuring trả số byte cấp phát thêm (runtime.MemStats.TotalAlloc, kể cả phần đã thu gom) trong lúc chạy f.
func allocDuring(f func()) uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	before := m.TotalAlloc
	f()
	runtime.ReadMemStats(&m)
	return m.TotalAlloc - before
}

// zipBomb dựng .xlsx khoảng 2 MiB mà trang đầu giải nén ra 2 GiB số 0 (luồng deflate thật: một khối nén 1 MiB số 0
// lặp lại 2.048 lần, header khai đúng kích thước và CRC).
func zipBomb(t *testing.T) []byte {
	t.Helper()
	zero := make([]byte, 1<<20)
	var chunk bytes.Buffer
	fw, _ := flate.NewWriter(&chunk, flate.BestCompression)
	_, _ = fw.Write(zero)
	_ = fw.Flush()
	const n = 2048
	var data bytes.Buffer
	crc := crc32.NewIEEE()
	for range n {
		data.Write(chunk.Bytes())
		_, _ = crc.Write(zero)
	}
	data.Write([]byte{0x03, 0x00}) // khối cuối rỗng
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, body := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"xl/workbook.xml":     `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"/>`,
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name: "xl/worksheets/sheet1.xml", Method: zip.Deflate, CRC32: crc.Sum32(),
		CompressedSize64: uint64(data.Len()), UncompressedSize64: n << 20, //nolint:gosec // độ dài bộ đệm không âm
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(data.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// Tệp .xlsx nén độc (khoảng 2 MiB nén, 2 GiB giải nén) → parse_failed too_large trong dưới 5 s, bộ nhớ cấp thêm của
// worker dưới 100 MiB.
func TestImport_ZipBombFailsFast(t *testing.T) {
	e := newImportEnv(t)
	bomb := zipBomb(t)
	if len(bomb) < 1<<20 || len(bomb) > 4<<20 {
		t.Fatalf("bom %d byte", len(bomb))
	}
	st, err := e.importFile(e.upload("bom.xlsx", xlsxType, bomb), e.shop.Account, "")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	alloc := allocDuring(func() { e.staff.DrainJobs(t, e.wk) })
	d := time.Since(start)
	got := e.get(st.ID, "", 0, 10).Statement
	if got.Status != statement.StatusParseFailed || got.ParseError.Code != "too_large" || d > 5*time.Second || alloc > 100<<20 {
		t.Fatalf("%s %+v sau %v, cấp %d MB", got.Status, got.ParseError, d, alloc>>20)
	}
	t.Logf("bom %d KB: parse_failed sau %v, cấp thêm %d MB", len(bomb)>>10, d, alloc>>20)
}

// Dữ liệu cá nhân: tệp có cột ten_nguoi_nhan, so_dien_thoai, dia_chi → không cột nào của bảng kê, dòng bảng kê, lệnh
// (payload, result, last_error) chứa giá trị đó; log của worker không chứa.
func TestImport_PersonalDataNotStored(t *testing.T) {
	e := newImportEnv(t)
	const name, phone, addr = "Nguyễn Thị Riêng Tư", "0912345678", "12 Ngõ Kín"
	body := "ten_nguoi_nhan,ma_van_don,so_dien_thoai,tien_cod,phi_van_chuyen,thuc_nhan,dia_chi\n" +
		name + ",FK0001," + phone + ",630000,32000,598000," + addr + "\n"
	d := e.run("ca-nhan.csv", csvType, []byte(body))
	if d.Statement.Status != statement.StatusParsed || len(d.Lines) != 1 {
		t.Fatalf("%+v", d)
	}
	var dump string
	if err := e.tx(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT coalesce((SELECT string_agg(row_to_json(s)::text, ' ') FROM cash.cod_statements s), '') ||
			coalesce((SELECT string_agg(row_to_json(l)::text, ' ') FROM cash.cod_statement_lines l), '')`).Scan(&dump)
	}); err != nil {
		t.Fatal(err)
	}
	var jobs string
	if err := e.staff.Owner.Pool.QueryRow(t.Context(), `SELECT coalesce(string_agg(row_to_json(j)::text, ' '), '')
		FROM platform.jobs j WHERE kind = $1`, statement.ParseKind().Name).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{name, phone, addr} {
		for where, text := range map[string]string{"bảng": dump, "lệnh": jobs, "log": e.logs.String()} {
			if strings.Contains(text, v) {
				t.Errorf("%s chứa %q", where, v)
			}
		}
	}
	if !strings.Contains(dump, "FK0001") {
		t.Fatalf("không thấy dòng bảng kê trong %s", dump)
	}
}

// Worker chết giữa lúc ghi lô thứ 2 của tệp 2.500 dòng (lô 1 đã commit, lần chạy trả lỗi → in_doubt): Lookup thấy
// bảng kê chưa parsed → not_sent → chạy lại → đúng 2.500 dòng, line_count 2.500, không dòng trùng.
func TestImport_WorkerDiesMidBatchResumes(t *testing.T) {
	e := newImportEnv(t)
	statement.SetAfterBatch(e.parser, func(written int) error {
		if written == 1000 {
			return errors.New("tiến trình chết")
		}
		return nil
	})
	st, err := e.importFile(e.upload("2500.csv", csvType, csvLines(2500)), e.shop.Account, "")
	if err != nil {
		t.Fatal(err)
	}
	e.staff.DrainJobs(t, e.wk)
	if s := e.jobStatus(st.ID); s != "in_doubt" {
		t.Fatalf("lệnh %s, mong in_doubt", s)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_statement_lines WHERE statement_id = $1", st.ID); n != 1000 {
		t.Fatalf("%d dòng sau lô 1", n)
	}
	if got := e.get(st.ID, "", 0, 1).Statement.Status; got != statement.StatusParsing {
		t.Fatalf("bảng kê %s, mong parsing", got)
	}
	statement.SetAfterBatch(e.parser, nil)
	if _, requeued, _, err := commands.ResolveInDoubt(t.Context(), e.staff.App, e.hs, 10); err != nil || requeued != 1 {
		t.Fatalf("tra lại: %d về hàng chờ, %v", requeued, err)
	}
	e.staff.DrainJobs(t, e.wk)
	d := e.get(st.ID, "", 0, 1)
	if d.Statement.Status != statement.StatusParsed || d.Statement.LineCount != 2500 ||
		e.count("SELECT count(DISTINCT line_no) FROM cash.cod_statement_lines WHERE statement_id = $1", st.ID) != 2500 ||
		e.count("SELECT count(*) FROM cash.cod_statement_lines WHERE statement_id = $1", st.ID) != 2500 {
		t.Fatalf("sau khi chạy lại: %+v", d.Statement)
	}
	if s := e.jobStatus(st.ID); s != "done" {
		t.Errorf("lệnh %s, mong done", s)
	}
}

// Lỗi tạm tới hết lượt (kho tệp chưa nối: not_sent mỗi lượt) → dead → OnTerminal: parse_failed read_failed. Lookup:
// bảng kê chưa đọc → not_sent; đã parsed → done.
func TestImport_RetriesExhaustedAndLookup(t *testing.T) {
	e := newImportEnv(t)
	st, err := e.importFile(e.upload("a.csv", csvType, testdata(t, "elimaz_v1_ba_dong.csv")), e.shop.Account, "")
	if err != nil {
		t.Fatal(err)
	}
	job := queue.Job{Kind: statement.ParseKind().Name, TenantID: &e.tenant, Payload: []byte(`{"statementId":"` + st.ID.String() + `"}`)}
	if s, err := e.parser.Lookup(t.Context(), job); err != nil || s != commands.StatusNotSent {
		t.Fatalf("Lookup trước khi đọc: %s, %v", s, err)
	}
	noFiles := cash.New(cash.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry()})
	e.staff.DrainJobs(t, e.worker(noFiles.CommandHandlers()))
	got := e.get(st.ID, "", 0, 1).Statement
	if s := e.jobStatus(st.ID); s != "dead" || got.Status != statement.StatusParseFailed || got.ParseError.Code != "read_failed" {
		t.Fatalf("lệnh %s, bảng kê %s %+v", s, got.Status, got.ParseError)
	}

	d := e.run("b.csv", csvType, csvLines(3))
	job.Payload = []byte(`{"statementId":"` + d.Statement.ID.String() + `"}`)
	if s, err := e.parser.Lookup(t.Context(), job); err != nil || s != commands.StatusDone {
		t.Fatalf("Lookup sau khi đọc: %s, %v", s, err)
	}
}

// Từ chối không ghi gì: tài khoản lạ (hay của tenant khác), định dạng lạ hoặc của hãng khác, tệp chưa tải xong, tệp
// mục đích khác, tệp không có.
func TestImport_Refusals(t *testing.T) {
	e := newImportEnv(t)
	body := testdata(t, "elimaz_v1_ba_dong.csv")
	file := e.upload("a.csv", csvType, body)
	other := e.staff.Tenant(t, "B")
	var otherAccount uuid.UUID
	e.staff.Exec(t, other, func(tx pgx.Tx) error {
		otherAccount = shiptest.NewShop(t, tx, other).Account
		return nil
	})
	if _, err := e.importFile(file, otherAccount, ""); !errors.Is(err, cash.ErrCarrierAccountNotFound) {
		t.Errorf("tài khoản tenant khác: %v", err)
	}
	if _, err := e.importFile(file, uuid.New(), ""); !errors.Is(err, cash.ErrCarrierAccountNotFound) {
		t.Errorf("tài khoản lạ: %v", err)
	}
	if _, err := e.importFile(file, e.shop.Account, "khong_co"); !errors.Is(err, cash.ErrUnknownStatementFormat) {
		t.Errorf("định dạng lạ: %v", err)
	}
	var pending files.File
	if err := e.tx(func(tx pgx.Tx) error {
		var err error
		pending, _, err = files.CreateUpload(t.Context(), tx, e.store, files.UploadInput{
			TenantID: e.tenant, Purpose: cash.StatementPurposeKey, FileName: "c.csv", ContentType: csvType, SizeBytes: 10,
		}, e.user)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.importFile(pending.ID, e.shop.Account, ""); !errors.Is(err, cash.ErrStatementFileNotReady) {
		t.Errorf("tệp chưa tải xong: %v", err)
	}
	var image uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		image = filestest.Uploaded(t, tx, e.tenant, "product_image")
		return nil
	})
	if _, err := e.importFile(image, e.shop.Account, ""); !errors.Is(err, cash.ErrStatementFileNotReady) {
		t.Errorf("tệp mục đích khác: %v", err)
	}
	if _, err := e.importFile(uuid.New(), e.shop.Account, ""); !errors.Is(err, files.ErrNotFound) {
		t.Errorf("tệp không có: %v", err)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_statements"); n != 0 {
		t.Errorf("%d bảng kê sau các lần từ chối", n)
	}
	if _, err := e.mod.Service.PrepareStatementImport(context.Background(), other, file); !errors.Is(err, files.ErrNotFound) {
		t.Errorf("tệp của tenant khác: %v", err)
	}
}

// ListStatements: mới nhất trước, trang 1 dòng có con trỏ, lọc theo trạng thái, theo tài khoản.
func TestListStatements(t *testing.T) {
	e := newImportEnv(t)
	a := e.run("a.csv", csvType, csvLines(1)).Statement
	b := e.run("b.csv", csvType, testdata(t, "elimaz_v1_thieu_tien_cod.csv")).Statement
	list := func(f cash.StatementListFilter) ([]cash.Statement, *uuid.UUID) {
		var items []cash.Statement
		var next *uuid.UUID
		if err := e.tx(func(tx pgx.Tx) error {
			var err error
			items, next, err = e.mod.Service.ListStatements(t.Context(), tx, f)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return items, next
	}
	p1, next := list(cash.StatementListFilter{Limit: 1})
	if len(p1) != 1 || p1[0].ID != b.ID || next == nil || *next != b.ID {
		t.Fatalf("trang 1: %+v next %v", p1, next)
	}
	if p2, next := list(cash.StatementListFilter{Limit: 1, Before: next}); len(p2) != 1 || p2[0].ID != a.ID || next != nil {
		t.Fatalf("trang 2: %+v next %v", p2, next)
	}
	if got, _ := list(cash.StatementListFilter{Limit: 10, Status: statement.StatusParsed}); len(got) != 1 || got[0].ID != a.ID {
		t.Errorf("lọc parsed: %+v", got)
	}
	acc := e.shop.PartnerAccount
	if got, _ := list(cash.StatementListFilter{Limit: 10, CarrierAccountID: &acc}); len(got) != 0 {
		t.Errorf("lọc tài khoản khác: %+v", got)
	}
}
