package customer_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/xlsx"
	"elimaz.com.vn/crm/internal/testkit"
)

const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// formulaMark mở đầu một ô trong book: phần sau là công thức thật, không phải chữ.
const formulaMark = "\x00f:"

// importEnv: writeEnv có kho tệp trong bộ nhớ, lệnh customer.import và worker chạy lệnh đó.
type importEnv struct {
	*writeEnv
	store *files.MemStore
	imp   *customer.Importer
	wk    *queue.Worker
}

func newImportEnv(t *testing.T) *importEnv {
	t.Helper()
	customer.RegisterFilePurposes()
	e := &importEnv{writeEnv: newWriteEnv(t), store: testkit.MemFileStore(t)}
	e.imp = customer.NewImporter(customer.ImporterOptions{DB: e.staff.App, Store: e.store, Writer: e.w})
	// Không tự dừng theo giờ thật (máy tải nặng làm một lô quá 20 s, lượt dừng giữa chừng và drain kết thúc sớm);
	// test tự dừng đặt ngân sách riêng.
	customer.SetBudget(e.imp, time.Hour)
	e.wk = queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "w-" + uuid.NewString(), Poll: 50 * time.Millisecond,
		Commands: commands.NewRunner(e.staff.App, []commands.Handler{e.imp}),
	})
	return e
}

// book dựng tệp .xlsx bằng excelize: dòng 1 là tiêu đề mẫu, rồi rows; ô ghi dạng chữ thô (không qua Sanitize, như tệp
// do Excel hay công cụ khác lưu).
func book(t *testing.T, rows [][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sw, err := f.NewStreamWriter("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	write := func(n int, cells []string) {
		vals := make([]any, len(cells))
		for i, c := range cells {
			vals[i] = c
			if f, ok := strings.CutPrefix(c, formulaMark); ok { // ô công thức thật (phần tử <f>)
				vals[i] = excelize.Cell{Formula: f}
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, n)
		if err := sw.SetRow(cell, vals); err != nil {
			t.Fatal(err)
		}
	}
	write(1, customer.ImportHeader())
	for i, r := range rows {
		write(i+2, r)
	}
	if err := sw.Flush(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// phoneRows trả n dòng hợp lệ, SĐT 0913 + 6 chữ số từ start.
func phoneRows(start, n int) [][]string {
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = []string{fmt.Sprintf("Khách %d", start+i), fmt.Sprintf("0913%06d", start+i)}
	}
	return rows
}

// upload tải body lên tenant như nhân viên (CreateUpload, ghi kho, CompleteUpload) với mục đích purpose.
func (e *importEnv) upload(tenant uuid.UUID, purpose string, body []byte) uuid.UUID {
	e.t.Helper()
	var f files.File
	if err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		f, _, err = files.CreateUpload(e.t.Context(), tx, e.store, files.UploadInput{
			TenantID: tenant, Purpose: purpose, FileName: "khach.xlsx", ContentType: xlsxType, SizeBytes: int64(len(body)),
		}, e.actor.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("CreateUpload: %v", err)
	}
	if err := e.store.Put(e.t.Context(), files.BucketTmp, files.TmpKey(tenant, f.ID), bytes.NewReader(body),
		int64(len(body)), xlsxType); err != nil {
		e.t.Fatal(err)
	}
	if _, err := files.CompleteUpload(e.t.Context(), e.staff.App, e.store, queue.NewQueue(), tenant, f.ID); err != nil {
		e.t.Fatalf("CompleteUpload: %v", err)
	}
	return f.ID
}

func (e *importEnv) start(fileID uuid.UUID) (customer.Import, error) {
	var imp customer.Import
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		imp, err = e.w.StartImport(e.t.Context(), tx, fileID, e.actor)
		return err //nolint:wrapcheck // test
	})
	return imp, err
}

func (e *importEnv) get(id uuid.UUID) customer.Import {
	e.t.Helper()
	var imp customer.Import
	e.inTx(func(tx pgx.Tx) error {
		var err error
		imp, err = customer.GetImport(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return imp
}

// startRows tải tệp có rows lên tenant A và bắt đầu lần nhập (đòi thành công), chưa chạy lệnh.
func (e *importEnv) startRows(rows [][]string) customer.Import {
	e.t.Helper()
	imp, err := e.start(e.upload(e.tenant, customer.ImportPurposeKey, book(e.t, rows)))
	if err != nil {
		e.t.Fatalf("StartImport: %v", err)
	}
	return imp
}

// drain chạy worker tới khi không còn việc pending nào, kể cả việc được hẹn chạy lại sau (Task 429).
func (e *importEnv) drain() {
	e.t.Helper()
	e.staff.DrainJobs(e.t, e.wk)
}

// run tải body, bắt đầu lần nhập, chạy lệnh và trả lần nhập sau khi chạy.
func (e *importEnv) run(body []byte) customer.Import {
	e.t.Helper()
	imp, err := e.start(e.upload(e.tenant, customer.ImportPurposeKey, body))
	if err != nil {
		e.t.Fatalf("StartImport: %v", err)
	}
	if imp.Status != customer.ImportPending {
		e.t.Fatalf("lần nhập mới: %s", imp.Status)
	}
	e.drain()
	return e.get(imp.ID)
}

// Tệp 5 dòng: dòng 3 SĐT sai "77777777", dòng 5 phường không thuộc tỉnh → ok 3, lỗi 2 (row 3, 5), lỗi không chứa giá trị ô;
// dòng đủ cột có địa chỉ mặc định, ngày sinh, nhãn, ghi chú; ô chữ "=HYPERLINK(...)" lưu nguyên; một audit
// customer.import, không audit customer.create; nhập lại cùng tệp → ok 0, skipped 3, không thêm khách.
func TestImport_RowErrorsAndReimport(t *testing.T) {
	e := newImportEnv(t)
	body := book(t, [][]string{
		{"Nguyễn Văn An", "0912000001", "01/02/1990", "Thành phố Thử Một", "Phường Bến Thử", "12 Lê Lợi", "VIP; Mới ;VIP", "Khách quen"},
		{"Trần Bình", "77777777"},
		{`=HYPERLINK("x")`, "0912 000 003"},
		{"Lê Chi", "0912000004", "", "Tỉnh Thử Hai", "Phường Bến Thử", "5 Hàng Bài"},
		{"Phạm Dung", "+84912000005", "", "thu hai", "ben hai", "7 Trần Phú", "Mới"},
	})
	imp := e.run(body)
	if imp.Status != customer.ImportDone || imp.TotalRows != 5 || imp.OKRows != 3 || imp.SkippedRows != 0 ||
		imp.ErrorRows != 2 || imp.FinishedAt == nil {
		t.Fatalf("lần nhập: %+v", imp)
	}
	want := []customer.ImportError{
		{Row: 3, Column: "SĐT*", Code: "invalid_phone"},
		{Row: 5, Column: "Xã/phường", Code: "ward_not_in_province"},
	}
	if fmt.Sprint(imp.Errors) != fmt.Sprint(want) {
		t.Errorf("lỗi dòng: %+v", imp.Errors)
	}
	var raw string
	e.inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT row_to_json(i)::text FROM customer.customer_imports i WHERE id = $1`,
			imp.ID).Scan(&raw)
	})
	// SĐT sai dài 8 chữ số, không phải mẩu ngắn như "123": dòng có UUID ngẫu nhiên và mốc giờ, mẩu ngắn trùng ngẫu nhiên.
	for _, bad := range []string{"77777777", "Bến Thử", "Lê Chi", "Thử Hai", "Hàng Bài"} {
		if strings.Contains(raw, bad) {
			t.Errorf("dòng lần nhập chứa %q: %s", bad, raw)
		}
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 3 {
		t.Errorf("số khách: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers c JOIN customer.customer_identities i ON i.customer_id = c.id
		WHERE c.full_name = '=HYPERLINK("x")' AND i.value = '+84912000003' AND i.source = 'import'`); n != 1 {
		t.Errorf("ô =HYPERLINK phải lưu nguyên chữ: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers c
		JOIN customer.customer_identities i ON i.customer_id = c.id AND i.value = '+84912000001'
		JOIN customer.customer_addresses a ON a.customer_id = c.id AND a.is_default AND a.province_code = 'T01'
			AND a.ward_code = 'T01001' AND a.street = '12 Lê Lợi' AND a.recipient_name = 'Nguyễn Văn An'
			AND a.phone = '+84912000001'
		WHERE c.birthday = DATE '1990-02-01' AND c.name_search = 'nguyen van an' AND c.created_by = $1`, e.actor.ID); n != 1 {
		t.Errorf("khách đủ cột: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_addresses WHERE province_code = 'T02' AND ward_code = 'T02002'`); n != 1 {
		t.Errorf("tên tỉnh, xã không dấu, không tiền tố: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_tags WHERE name IN ('VIP', 'Mới') AND color = 'gray'`); n != 2 {
		t.Errorf("nhãn: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_tag_links l JOIN customer.customer_tags g ON g.id = l.tag_id
		WHERE l.source = 'staff'`); n != 3 {
		t.Errorf("liên kết nhãn (VIP, Mới cho An; Mới cho Dung): %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_notes WHERE body = 'Khách quen' AND author_id = $1`, e.actor.ID); n != 1 {
		t.Errorf("ghi chú: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'customer.created'`); n != 3 {
		t.Errorf("sự kiện customer.created: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.create'`); n != 0 {
		t.Errorf("không audit từng dòng: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.import' AND entity_id = $1
		AND actor_id = $2 AND (after->>'ok')::int = 3 AND (after->>'errors')::int = 2 AND (after->>'total')::int = 5`,
		imp.ID.String(), e.actor.ID.String()); n != 1 {
		t.Errorf("audit customer.import: %d", n)
	}

	again := e.run(body)
	if again.Status != customer.ImportDone || again.OKRows != 0 || again.SkippedRows != 3 || again.ErrorRows != 2 {
		t.Fatalf("nhập lại: %+v", again)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 3 {
		t.Errorf("nhập lại tạo trùng: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.import'`); n != 2 {
		t.Errorf("mỗi lần nhập một audit: %d", n)
	}
}

// Ô trống bắt buộc, ngày sai, địa chỉ thiếu cột, nhãn dài, tên tỉnh lạ, ô công thức thật: mỗi dòng một lỗi đúng cột
// và mã; SĐT trùng trong cùng tệp → dòng sau skipped; ngày dạng số của Excel đọc đúng.
func TestImport_RowCodes(t *testing.T) {
	e := newImportEnv(t)
	imp := e.run(book(t, [][]string{
		{"", "0912100001"},
		{"Không SĐT", ""},
		{"Ngày sai", "0912100003", "31/02/1990"},
		{"Thiếu xã", "0912100004", "", "Tỉnh Thử Hai", "", "1 Đường A"},
		{"Nhãn dài", "0912100005", "", "", "", "", strings.Repeat("x", 51)},
		{"Tỉnh lạ", "0912100006", "", "Tỉnh Không Có", "Phường Bến Hai", "1 Đường A"},
		{"Ngày số", "0912100007", "32874"},
		{"Trùng trong tệp", "0912 100 007"},
		{formulaMark + "1+1", "0912100010"},
		{"Ngày giờ", "0912100011", "32874.75"},
	}))
	want := []customer.ImportError{
		{Row: 2, Column: "Họ tên*", Code: "required"},
		{Row: 3, Column: "SĐT*", Code: "required"},
		{Row: 4, Column: "Ngày sinh (dd/mm/yyyy)", Code: "invalid_date"},
		{Row: 5, Column: "Xã/phường", Code: "required"},
		{Row: 6, Column: "Nhãn (cách nhau bằng ;)", Code: "tag_too_long"},
		{Row: 7, Column: "Tỉnh/thành", Code: "unknown_province"},
		{Row: 10, Column: "Họ tên*", Code: "formula_not_allowed"},
	}
	if imp.OKRows != 2 || imp.SkippedRows != 1 || imp.ErrorRows != 7 || fmt.Sprint(imp.Errors) != fmt.Sprint(want) {
		t.Fatalf("lần nhập: %+v", imp)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE full_name IN ('Ngày số', 'Ngày giờ')
		AND birthday = DATE '1990-01-01'`); n != 2 {
		t.Errorf("ngày dạng số Excel: %d", n)
	}
}

// Việc chết sau lô 1 của tệp 1.200 dòng rồi chạy lại → đúng 1.200 khách, không trùng SĐT, số đếm đúng, một audit.
func TestImport_ResumeAfterCrash(t *testing.T) {
	e := newImportEnv(t)
	imp := e.startRows(phoneRows(0, 1200))
	errCrash := errors.New("tiến trình chết")
	customer.SetAfterBatch(e.imp, func(processed int) error {
		if processed >= customer.ImportBatchSize {
			return errCrash
		}
		return nil
	})
	if _, err := e.imp.Execute(t.Context(), customer.ImportJob(e.tenant, imp.ID)); !errors.Is(err, errCrash) {
		t.Fatalf("lượt đầu phải chết sau lô 1: %v", err)
	}
	mid := e.get(imp.ID)
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != customer.ImportBatchSize || mid.Status != customer.ImportRunning ||
		mid.OKRows != customer.ImportBatchSize {
		t.Fatalf("sau lô 1: %d khách, %+v", n, mid)
	}
	customer.SetAfterBatch(e.imp, nil)
	// Chạy lại thẳng Execute (không qua worker): hạn 30 s của lệnh tính theo giờ thật, máy tải nặng có thể cắt một lô
	// giữa chừng và hẹn lượt sau 30 s; điều cần kiểm là chạy lại đủ, không phải thời gian.
	for range 10 {
		out, err := e.imp.Execute(t.Context(), customer.ImportJob(e.tenant, imp.ID))
		if err != nil {
			t.Fatalf("chạy lại: %v", err)
		}
		if out.Status == commands.StatusDone {
			break
		}
	}
	done := e.get(imp.ID)
	if done.Status != customer.ImportDone || done.TotalRows != 1200 || done.OKRows != 1200 || done.SkippedRows != 0 || done.ErrorRows != 0 {
		t.Fatalf("sau khi chạy lại: %+v", done)
	}
	if n := e.count(`SELECT count(DISTINCT value) FROM customer.customer_identities WHERE kind = 'phone'`); n != 1200 {
		t.Errorf("SĐT khác nhau: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 1200 {
		t.Errorf("tổng khách: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.import'`); n != 1 {
		t.Errorf("audit: %d", n)
	}
}

// Lượt chạy hết ngân sách thời gian → not_sent (yield, hẹn 1 s) sau mỗi lô; các lượt sau làm tiếp đúng chỗ: 1.200 dòng
// xong sau 3 lượt, số đếm đúng, không trùng.
func TestImport_YieldsAndResumes(t *testing.T) {
	e := newImportEnv(t)
	imp := e.startRows(phoneRows(0, 1200))
	customer.SetBudget(e.imp, 0)
	job := customer.ImportJob(e.tenant, imp.ID)
	var statuses []string
	for range 5 {
		out, err := e.imp.Execute(t.Context(), job)
		if err != nil {
			t.Fatal(err)
		}
		statuses = append(statuses, out.Status+"/"+out.Reason)
		if out.Status == commands.StatusDone {
			break
		}
		if out.RetryAfter != time.Second {
			t.Fatalf("hẹn lượt sau: %+v", out)
		}
	}
	if fmt.Sprint(statuses) != "[not_sent/yield not_sent/yield done/]" {
		t.Fatalf("các lượt: %v", statuses)
	}
	done := e.get(imp.ID)
	if done.Status != customer.ImportDone || done.OKRows != 1200 || done.SkippedRows != 0 {
		t.Fatalf("lần nhập: %+v", done)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 1200 {
		t.Errorf("tổng khách: %d", n)
	}
}

// Lệnh hết lượt (dead) → OnTerminal đặt lần nhập chưa xong thành failed với lỗi cả tệp import_failed; lần nhập đã
// done thì giữ nguyên.
func TestImport_TerminalMarksFailed(t *testing.T) {
	e := newImportEnv(t)
	imp := e.startRows(phoneRows(0, 2))
	e.inTx(func(tx pgx.Tx) error {
		return e.imp.OnTerminal(t.Context(), customer.ImportJob(e.tenant, imp.ID), queue.StatusDead, tx)
	})
	got := e.get(imp.ID)
	if got.Status != customer.ImportFailed || got.FinishedAt == nil ||
		fmt.Sprint(got.Errors) != fmt.Sprint([]customer.ImportError{{Row: 0, Code: "import_failed"}}) {
		t.Fatalf("sau dead: %+v", got)
	}
	ok := e.run(book(t, phoneRows(10, 1)))
	e.inTx(func(tx pgx.Tx) error {
		return e.imp.OnTerminal(t.Context(), customer.ImportJob(e.tenant, ok.ID), queue.StatusDead, tx)
	})
	if again := e.get(ok.ID); again.Status != customer.ImportDone || len(again.Errors) != 0 {
		t.Fatalf("lần nhập đã done bị đổi: %+v", again)
	}
}

// Tệp 20.001 dòng → ErrImportTooLarge trước khi tạo lần nhập; tệp sai tiêu đề, không phải xlsx → ErrImportFileInvalid;
// tệp mục đích product_image, tệp của tenant khác → StartImport ErrImportFileInvalid, không ghi gì.
func TestImport_FileChecks(t *testing.T) {
	e := newImportEnv(t)
	ctx := t.Context()
	big := e.upload(e.tenant, customer.ImportPurposeKey, book(t, phoneRows(0, 20_001)))
	if err := customer.CheckImportFile(ctx, e.staff.App, e.store, e.tenant, big); !errors.Is(err, customer.ErrImportTooLarge) {
		t.Fatalf("20.001 dòng: %v", err)
	}
	ok := e.upload(e.tenant, customer.ImportPurposeKey, book(t, phoneRows(0, 20_000)))
	if err := customer.CheckImportFile(ctx, e.staff.App, e.store, e.tenant, ok); err != nil {
		t.Fatalf("20.000 dòng phải nhận: %v", err)
	}
	var wrong bytes.Buffer
	if err := xlsx.Write(&wrong, []xlsx.Sheet{{Name: "Sheet1", Header: []string{"Tên"}, Rows: [][]any{{"A"}}}}); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"sai tiêu đề": wrong.Bytes(), "không phải xlsx": []byte("không phải zip")} {
		id := e.upload(e.tenant, customer.ImportPurposeKey, body)
		if err := customer.CheckImportFile(ctx, e.staff.App, e.store, e.tenant, id); !errors.Is(err, customer.ErrImportFileInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}

	var img files.File
	e.inTx(func(tx pgx.Tx) error {
		var err error
		img, _, err = files.CreateUpload(ctx, tx, e.store, files.UploadInput{
			TenantID: e.tenant, Purpose: "product_image", FileName: "a.jpg", ContentType: "image/jpeg", SizeBytes: 100,
		}, e.actor.ID)
		return err //nolint:wrapcheck // test
	})
	other := e.upload(e.other, customer.ImportPurposeKey, book(t, phoneRows(0, 2)))
	for name, id := range map[string]uuid.UUID{"ảnh sản phẩm": img.ID, "tenant khác": other, "không có": uuid.New()} {
		if _, err := e.start(id); !errors.Is(err, customer.ErrImportFileInvalid) {
			t.Errorf("%s: %v", name, err)
		}
		if err := customer.CheckImportFile(ctx, e.staff.App, e.store, e.tenant, id); !errors.Is(err, customer.ErrImportFileInvalid) {
			t.Errorf("kiểm tệp %s: %v", name, err)
		}
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_imports`); n != 0 {
		t.Errorf("không được tạo lần nhập: %d", n)
	}
}

// Lệnh gặp tệp quá dòng (kiểm lúc bắt đầu bị bỏ qua) → failed với lỗi cả tệp {row 0, import_too_large}, không khách.
func TestImport_JobFailsOnTooLarge(t *testing.T) {
	e := newImportEnv(t)
	imp := e.run(book(t, phoneRows(0, 20_001)))
	if imp.Status != customer.ImportFailed || imp.FinishedAt == nil ||
		fmt.Sprint(imp.Errors) != fmt.Sprint([]customer.ImportError{{Row: 0, Code: "import_too_large"}}) {
		t.Fatalf("lần nhập: %+v", imp)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 0 {
		t.Errorf("không được tạo khách: %d", n)
	}
}

// Mẫu tệp có đúng 8 cột theo thứ tự và đọc lại được bằng xlsx.Read với tiêu đề mẫu (0 dòng).
func TestImportTemplate(t *testing.T) {
	want := []string{
		"Họ tên*", "SĐT*", "Ngày sinh (dd/mm/yyyy)", "Tỉnh/thành", "Xã/phường", "Số nhà, đường",
		"Nhãn (cách nhau bằng ;)", "Ghi chú",
	}
	if fmt.Sprint(customer.ImportHeader()) != fmt.Sprint(want) {
		t.Fatalf("tiêu đề: %v", customer.ImportHeader())
	}
	b, err := customer.ImportTemplate()
	if err != nil {
		t.Fatal(err)
	}
	rows, cellErrs, err := xlsx.Read(bytes.NewReader(b), int64(len(b)), xlsx.Limits{}, want)
	if err != nil || len(rows) != 0 || len(cellErrs) != 0 {
		t.Fatalf("đọc lại mẫu: %v %d %d", err, len(rows), len(cellErrs))
	}
}

// Task 429: lệnh nhập bị hẹn chạy lại sau (như sau một lỗi tạm khi máy bận) → drain vẫn chạy nó tới cuối thay vì dừng
// khi hết việc đến hạn; lần nhập done.
func TestImport_DrainRunsScheduledRetry(t *testing.T) {
	e := newImportEnv(t)
	imp := e.startRows([][]string{{"Nguyễn Văn An", "0912000001"}})
	if _, err := e.staff.Owner.Pool.Exec(t.Context(), `UPDATE platform.jobs SET run_at = now() + interval '1 hour',
		attempts = 1 WHERE status = 'pending'`); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if got := e.get(imp.ID); got.Status != customer.ImportDone || got.OKRows != 1 {
		t.Fatalf("lần nhập: %+v, mong done 1 dòng", got)
	}
}
