package inventory_test

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

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Nhập số đếm bằng Excel (kế hoạch M7, Task 263 = M7-18): tải tệp mục đích inventory_count_import, PrepareCountImport
// (đọc, băm, phân tích ngoài transaction), CreateCountImport (ghi bản nhập + việc nền), việc inventory.count_import áp
// các dòng hợp lệ trong một transaction.

const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// formulaCell mở đầu một ô của countBook: phần sau là công thức thật.
const formulaCell = "="

type countImportEnv struct {
	*invEnv
	store *files.MemStore
	wk    *queue.Worker
	actor inventory.Actor
}

func newCountImportEnv(t *testing.T) *countImportEnv {
	t.Helper()
	inventory.RegisterCountImportPurpose()
	e := &countImportEnv{
		invEnv: newInvEnv(t), store: filestest.MemFileStore(t),
		actor: inventory.Actor{Type: inventory.ActorStaff, ID: uuid.New()},
	}
	e.wk = queue.NewWorker(e.staff.App, inventory.CountImportJobs(inventory.CountImportDeps{Now: time.Now}),
		queue.WorkerOptions{WorkerID: "w-" + uuid.NewString()})
	return e
}

// countBook dựng tệp .xlsx: dòng 1 là tiêu đề "Mã", "Số đếm", rồi rows; ô bắt đầu bằng "=" là công thức thật.
func countBook(t *testing.T, rows [][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	writeCountRows(t, f, append([][]string{inventory.CountImportHeader()}, rows...))
	b, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// writeCountRows ghi all vào trang đầu của f, dòng 1 trở đi; ô ngoài dòng 1 bắt đầu bằng formulaCell là công thức thật
// (phần tử <f>).
func writeCountRows(t *testing.T, f *excelize.File, all [][]string) {
	t.Helper()
	sw, err := f.NewStreamWriter("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	for r, cells := range all {
		vals := make([]any, 0, len(cells))
		for _, v := range cells {
			if fm, ok := strings.CutPrefix(v, formulaCell); ok && r > 0 {
				vals = append(vals, excelize.Cell{Formula: fm})
				continue
			}
			vals = append(vals, v)
		}
		ref, _ := excelize.CoordinatesToCellName(1, r+1)
		if err := sw.SetRow(ref, vals); err != nil {
			t.Fatal(err)
		}
	}
	if err := sw.Flush(); err != nil {
		t.Fatal(err)
	}
}

// upload tải body (loại ct) với mục đích inventory_count_import như nhân viên; trả mã tệp hoặc lỗi của CreateUpload.
func (e *countImportEnv) upload(body []byte, ct string, size int64) (uuid.UUID, error) {
	var f files.File
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		f, _, err = files.CreateUpload(e.t.Context(), tx, e.store, files.UploadInput{
			TenantID: e.tenant, Purpose: inventory.CountImportPurposeKey, FileName: "dem.xlsx", ContentType: ct,
			SizeBytes: size,
		}, e.actor.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		return uuid.Nil, err
	}
	e.finishUpload(f.ID, body, ct)
	return f.ID, nil
}

// finishUpload đặt body vào chỗ tạm của tệp id rồi gọi CompleteUpload.
func (e *countImportEnv) finishUpload(id uuid.UUID, body []byte, ct string) {
	ctx, key := e.t.Context(), files.TmpKey(e.tenant, id)
	if err := e.store.Put(ctx, files.BucketTmp, key, bytes.NewReader(body), int64(len(body)), ct); err != nil {
		e.t.Fatalf("đặt nội dung tệp: %v", err)
	}
	if _, err := files.CompleteUpload(ctx, e.staff.App, e.store, queue.NewQueue(), e.tenant, id); err != nil {
		e.t.Fatalf("CompleteUpload: %v", err)
	}
}

func (e *countImportEnv) mustUpload(body []byte) uuid.UUID {
	e.t.Helper()
	id, err := e.upload(body, xlsxType, int64(len(body)))
	if err != nil {
		e.t.Fatalf("tải tệp: %v", err)
	}
	return id
}

// start chuẩn bị tệp rồi tạo bản nhập (như handler: đọc ngoài transaction, ghi trong transaction).
func (e *countImportEnv) start(stocktakeID, fileID uuid.UUID, mode string) (inventory.CountImport, bool, error) {
	f, err := inventory.PrepareCountImport(e.t.Context(), e.staff.App, e.store, e.tenant, fileID)
	if err != nil {
		return inventory.CountImport{}, false, err //nolint:wrapcheck // test
	}
	var imp inventory.CountImport
	var created bool
	err = e.app(func(tx pgx.Tx) error {
		var err error
		imp, created, err = inventory.CreateCountImport(e.t.Context(), tx, queue.NewQueue(), stocktakeID, f, mode, e.actor)
		return err //nolint:wrapcheck // test
	})
	return imp, created, err
}

// run chạy hàng đợi tới khi bản nhập id kết thúc (done hay failed) và trả bản nhập.
func (e *countImportEnv) run(id uuid.UUID) inventory.CountImport {
	e.t.Helper()
	deadline := time.Now().Add(60 * time.Second) // chốt chống treo
	for {
		if _, err := e.wk.RunOnce(e.t.Context(), 10); err != nil {
			e.t.Fatalf("RunOnce: %v", err)
		}
		imp := e.getImport(id)
		if imp.Status == inventory.CountImportDone || imp.Status == inventory.CountImportFailed {
			return imp
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("bản nhập %s chưa xong: %+v", id, imp)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *countImportEnv) getImport(id uuid.UUID) inventory.CountImport {
	e.t.Helper()
	var imp inventory.CountImport
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		imp, err = inventory.GetCountImport(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("GetCountImport: %v", err)
	}
	return imp
}

// Tệp 6 dòng: A 3, A 4, X 1 (mã lạ), B abc, C =1+1, B 10 → replace: A = 7, B = 10; lỗi theo số dòng Excel (tiêu đề
// là dòng 1): {4 unknown_code}, {5 not_integer}, {6 formula}; không lưu nội dung ô. Nhập lại cùng tệp replace → bản cũ,
// không đổi số; add hai lần cùng tệp → lần hai ErrImportAlreadyApplied, lần đầu cộng thêm.
func TestCountImport_ReplaceAddAndRowErrors(t *testing.T) {
	t.Parallel()
	e := newCountImportEnv(t)
	vs := e.openingFixture()
	s := e.mustCreateOpening(e.warehouse)
	body := countBook(t, [][]string{
		{"MS1-1", "3"}, {"ms1-1", "4"}, {"KHONGCO", "1"}, {"MS1-2", "abc"}, {"MS1-3", "=1+1"}, {"MS1-2", "10"},
	})
	fid := e.mustUpload(body)
	imp, created, err := e.start(s.ID, fid, inventory.CountImportReplace)
	if err != nil || !created || imp.Status != inventory.CountImportQueued || imp.Mode != inventory.CountImportReplace {
		t.Fatalf("tạo bản nhập: %+v %v %v", imp, created, err)
	}
	done := e.run(imp.ID)
	wantErrs := []inventory.CountImportRowError{{Row: 4, Code: "unknown_code"}, {Row: 5, Code: "not_integer"}, {Row: 6, Code: "formula"}}
	if done.Status != inventory.CountImportDone || done.AppliedRows != 3 || fmt.Sprint(done.ErrorRows) != fmt.Sprint(wantErrs) ||
		done.FinishedAt == nil {
		t.Fatalf("bản nhập xong: %+v", done)
	}
	if a, b, c := e.countedOf(s.ID, vs[0]), e.countedOf(s.ID, vs[1]), e.countedOf(s.ID, vs[2]); a != 7 || b != 10 || c != -1 {
		t.Fatalf("A=%d B=%d C=%d, mong 7, 10, chưa có", a, b, c)
	}
	raw := e.rawImport(imp.ID)
	for _, cell := range []string{"KHONGCO", "abc", "1+1"} {
		if strings.Contains(raw, cell) {
			t.Fatalf("bản nhập lưu nội dung ô %q: %s", cell, raw)
		}
	}

	again, created, err := e.start(s.ID, fid, inventory.CountImportReplace)
	if err != nil || created || again.ID != imp.ID || again.Status != inventory.CountImportDone {
		t.Fatalf("nhập lại replace: %+v %v %v", again, created, err)
	}
	if a := e.countedOf(s.ID, vs[0]); a != 7 {
		t.Fatalf("nhập lại replace đổi số: %d", a)
	}

	add, created, err := e.start(s.ID, fid, inventory.CountImportAdd)
	if err != nil || !created {
		t.Fatalf("add lần một: %v %v", created, err)
	}
	if r := e.run(add.ID); r.Status != inventory.CountImportDone || r.AppliedRows != 3 {
		t.Fatalf("add lần một: %+v", r)
	}
	if a, b := e.countedOf(s.ID, vs[0]), e.countedOf(s.ID, vs[1]); a != 14 || b != 20 {
		t.Fatalf("sau add: A=%d B=%d, mong 14, 20", a, b)
	}
	if _, _, err := e.start(s.ID, fid, inventory.CountImportAdd); !errors.Is(err, inventory.ErrImportAlreadyApplied) {
		t.Fatalf("add lần hai: %v", err)
	}
}

// rawImport đọc dòng count_imports như JSON (mọi cột).
func (e *countImportEnv) rawImport(id uuid.UUID) string {
	e.t.Helper()
	var raw string
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT to_jsonb(c)::text FROM inventory.count_imports c WHERE id = $1`, id).Scan(&raw)
	}); err != nil {
		e.t.Fatal(err)
	}
	return raw
}

// Phiên đã đăng: tạo bản nhập → ErrStocktakeNotOpen; bản nhập xếp trước khi đăng, chạy sau khi đăng → failed, không đổi
// dòng nào; số âm → negative; ô trống mã → unknown_code; mã trùng hai người bán → ambiguous_code.
func TestCountImport_ClosedStocktakeAndCodes(t *testing.T) {
	t.Parallel()
	e := newCountImportEnv(t)
	vs, s := e.countedOpening()
	fid := e.mustUpload(countBook(t, [][]string{{"MS1-1", "5"}, {"MS1-2", "-1"}, {"", "2"}}))
	imp, _, err := e.start(s.ID, fid, inventory.CountImportAdd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.postOpening(s.ID); err != nil {
		t.Fatalf("PostOpening: %v", err)
	}
	if r := e.run(imp.ID); r.Status != inventory.CountImportFailed || r.AppliedRows != 0 {
		t.Fatalf("chạy sau khi đăng: %+v", r)
	}
	if a := e.countedOf(s.ID, vs[0]); a != 0 {
		t.Fatalf("dòng đổi sau khi đăng: %d", a)
	}
	if _, _, err := e.start(s.ID, e.mustUpload(countBook(t, [][]string{{"MS1-1", "1"}})), inventory.CountImportReplace); !errors.Is(err, inventory.ErrStocktakeNotOpen) {
		t.Fatalf("tạo trên phiên đã đăng: %v", err)
	}

	// Kho W2, mã trùng giữa hai người bán.
	w2 := e.secondWarehouse()
	sellers := e.sellers()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for i, seller := range sellers[:2] {
			p, err := e.fixtureProduct(tx, seller, fmt.Sprintf("TRUNG%d", i), false)
			if err != nil {
				return err
			}
			if _, err := e.fixtureVariant(tx, seller, p, "TRUNG-1", "active"); err != nil {
				return err
			}
		}
		return nil
	})
	s2 := e.mustCreateOpening(w2)
	imp2, _, err := e.start(s2.ID, e.mustUpload(countBook(t, [][]string{{"TRUNG-1", "1"}, {"MS1-1", "-2"}, {"", "3"}, {"MS1-4", "2"}})), inventory.CountImportReplace)
	if err != nil {
		t.Fatal(err)
	}
	r := e.run(imp2.ID)
	want := []inventory.CountImportRowError{{Row: 2, Code: "ambiguous_code"}, {Row: 3, Code: "negative"}, {Row: 4, Code: "unknown_code"}}
	if r.Status != inventory.CountImportDone || r.AppliedRows != 1 || fmt.Sprint(r.ErrorRows) != fmt.Sprint(want) {
		t.Fatalf("lỗi mã: %+v", r)
	}
	if n := e.countedOf(s2.ID, vs[3]); n != 2 {
		t.Fatalf("MS1-4 = %d, mong 2", n)
	}
}

// Tệp 3.000 dòng (1.000 mã khác nhau, mỗi mã 3 dòng) áp trong một transaction; ghi thời gian đo (không lấy giờ làm tiêu
// chí đúng/sai, Task 400).
func TestCountImport_ThreeThousandRows(t *testing.T) {
	t.Parallel()
	e := newCountImportEnv(t)
	vs := e.variants(1000)
	codes := make([]string, len(vs))
	if err := e.app(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT variant_id, sku FROM catalog.variants_v WHERE variant_id = ANY($1)`, vs)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		idx := map[uuid.UUID]int{}
		for i, v := range vs {
			idx[v] = i
		}
		for rows.Next() {
			var v uuid.UUID
			var sku string
			if err := rows.Scan(&v, &sku); err != nil {
				return err //nolint:wrapcheck // test
			}
			codes[idx[v]] = sku
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	s := e.mustCreateOpening(e.warehouse)
	var rows [][]string
	for rep := range 3 {
		for i, c := range codes {
			rows = append(rows, []string{c, fmt.Sprint((i + rep) % 7)})
		}
	}
	started := time.Now()
	imp, _, err := e.start(s.ID, e.mustUpload(countBook(t, rows)), inventory.CountImportReplace)
	if err != nil {
		t.Fatal(err)
	}
	r := e.run(imp.ID)
	t.Logf("3.000 dòng: chuẩn bị + áp %s", time.Since(started))
	if r.Status != inventory.CountImportDone || r.AppliedRows != 3000 || len(r.ErrorRows) != 0 {
		t.Fatalf("bản nhập: status %s, %d dòng, %d lỗi", r.Status, r.AppliedRows, len(r.ErrorRows))
	}
	for _, i := range []int{0, 1, 999} {
		want := i%7 + (i+1)%7 + (i+2)%7
		if got := e.countedOf(s.ID, vs[i]); got != want {
			t.Fatalf("biến thể %d: %d, mong %d", i, got, want)
		}
	}
}

// Mục đích tệp (R5): inventory_count_import bucket private, chỉ .xlsx, tối đa 5 MiB, tải lên được; .xlsx 6 MiB →
// files.ErrTooLarge; .csv → files.ErrTypeNotAllowed. Tệp không phải .xlsx hay sai tiêu đề → ErrInvalidInput; lỗi không
// chứa nội dung ô.
func TestCountImport_PurposeAndBadFiles(t *testing.T) {
	t.Parallel()
	e := newCountImportEnv(t)
	p, ok := files.PurposeOf(inventory.CountImportPurposeKey)
	if !ok || p.Bucket != files.BucketPrivate || p.MaxBytes != 5<<20 || !p.Upload || p.Image ||
		len(p.AllowedTypes) != 1 || p.AllowedTypes[0] != xlsxType {
		t.Fatalf("mục đích: %+v %v", p, ok)
	}
	if n := e.count(`SELECT count(*)::int FROM platform.file_purposes WHERE key = 'inventory_count_import' AND bucket = 'private'`); n != 1 {
		t.Fatalf("platform.file_purposes: %d dòng", n)
	}
	if _, err := e.upload(nil, xlsxType, 6<<20); !errors.Is(err, files.ErrTooLarge) {
		t.Fatalf(".xlsx 6 MiB: %v", err)
	}
	if _, err := e.upload([]byte("Mã,Số đếm\n"), "text/csv", 10); !errors.Is(err, files.ErrTypeNotAllowed) {
		t.Fatalf(".csv: %v", err)
	}
	e.openingFixture()
	s := e.mustCreateOpening(e.warehouse)
	bad := e.mustUpload([]byte("BIMAT không phải zip"))
	if _, _, err := e.start(s.ID, bad, inventory.CountImportReplace); !errors.Is(err, inventory.ErrInvalidInput) || strings.Contains(err.Error(), "BIMAT") {
		t.Fatalf("không phải xlsx: %v", err)
	}
	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]any{"SKU", "Số"})
	_ = f.SetSheetRow("Sheet1", "A2", &[]any{"BIMAT", "1"})
	var buf bytes.Buffer
	_ = f.Write(&buf)
	_ = f.Close()
	if _, _, err := e.start(s.ID, e.mustUpload(buf.Bytes()), inventory.CountImportReplace); !errors.Is(err, inventory.ErrInvalidInput) || strings.Contains(err.Error(), "BIMAT") {
		t.Fatalf("sai tiêu đề: %v", err)
	}
	if _, err := inventory.PrepareCountImport(e.t.Context(), e.staff.App, e.store, e.tenant, uuid.New()); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("tệp không có: %v", err)
	}
}
