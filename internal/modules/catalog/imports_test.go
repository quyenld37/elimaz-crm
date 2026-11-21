package catalog_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// importEnv: catalogEnv có phân loại khớp tệp thử (testdata/xlsx/gen), kho tệp trong bộ nhớ và worker chạy lệnh
// catalog.import.validate và việc catalog.import.apply.
type importEnv struct {
	*catalogEnv
	store *files.MemStore
	w     *queue.Worker
}

func newImportEnv(t *testing.T) *importEnv {
	t.Helper()
	e := &importEnv{catalogEnv: newCatalogEnv(t), store: filestest.MemFileStore(t)}
	e.seedClassification()
	v := catalog.NewImportValidator(catalog.ImportValidatorOptions{DB: e.staff.App, Store: e.store})
	e.w = e.newWorker(v)
	return e
}

// newWorker dựng một worker chạy lệnh kiểm v và việc áp dụng (mỗi worker một mã giữ chỗ).
func (e *importEnv) newWorker(v *catalog.ImportValidator) *queue.Worker {
	return queue.NewWorker(e.staff.App, []queue.Handler{catalog.NewImportApplier(e.writer())}, queue.WorkerOptions{
		WorkerID: "w-" + uuid.NewString(), Poll: 50 * time.Millisecond,
		Commands: commands.NewRunner(e.staff.App, []commands.Handler{v}),
	})
}

// seedClassification tạo danh mục, thuộc tính, nhãn mà mau-200.xlsx, loi-tung-dong.xlsx dùng.
func (e *importEnv) seedClassification() {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		return errors.Join(e.seedCategories(tx), e.seedAttributes(tx), e.seedTags(tx))
	}); err != nil {
		e.t.Fatalf("tạo phân loại: %v", err)
	}
}

func (e *importEnv) seedCategories(tx pgx.Tx) error {
	ids := map[string]uuid.UUID{}
	for _, c := range [][2]string{{"", "Váy"}, {"", "Áo"}, {"", "Đầm"}, {"Váy", "Váy dài"}, {"Váy", "Váy ngắn"}, {"Áo", "Áo sơ mi"}} {
		in := catalog.CategoryInput{Name: c[1]}
		if c[0] != "" {
			parent := ids[c[0]]
			in.ParentID = &parent
		}
		cat, err := e.writer().CreateCategory(e.t.Context(), tx, in, e.actor)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		ids[c[1]] = cat.ID
	}
	return nil
}

func (e *importEnv) seedAttributes(tx pgx.Tx) error {
	for _, a := range []struct {
		code, name string
		values     []string
	}{{"chat-lieu", "Chất liệu", []string{"Lụa", "Cotton", "Voan"}}, {"kieu-dang", "Kiểu dáng", []string{"Suông", "Ôm"}}} {
		attr, err := e.writer().CreateAttribute(e.t.Context(), tx, catalog.AttributeInput{Code: a.code, Name: a.name}, e.actor)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, v := range a.values {
			if _, err := e.writer().AddAttributeValue(e.t.Context(), tx, attr.ID, catalog.AttributeValueInput{Value: v}, e.actor); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
	}
	return nil
}

func (e *importEnv) seedTags(tx pgx.Tx) error {
	for _, tg := range []string{"moi-ve", "sale", "ban-chay"} {
		if _, err := e.writer().CreateTag(e.t.Context(), tx, catalog.TagInput{Code: tg, Name: tg}, e.actor); err != nil {
			return err //nolint:wrapcheck // test
		}
	}
	return nil
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "xlsx", name))
	if err != nil {
		t.Fatalf("đọc tệp thử: %v", err)
	}
	return b
}

// upload tải body lên như nhân viên (CreateUpload, ghi kho, CompleteUpload) với mục đích purpose; trả mã tệp.
func (e *importEnv) upload(purpose string, body []byte) uuid.UUID {
	e.t.Helper()
	var f files.File
	err := e.app(func(tx pgx.Tx) error {
		var err error
		f, _, err = files.CreateUpload(e.t.Context(), tx, e.store, files.UploadInput{
			TenantID: e.tenant, Purpose: purpose, FileName: "san-pham.xlsx", ContentType: xlsxType, SizeBytes: int64(len(body)),
		}, e.actor.UserID)
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		e.t.Fatalf("CreateUpload: %v", err)
	}
	if err := e.store.Put(e.t.Context(), files.BucketTmp, files.TmpKey(e.tenant, f.ID), bytes.NewReader(body),
		int64(len(body)), xlsxType); err != nil {
		e.t.Fatal(err)
	}
	if _, err := files.CompleteUpload(e.t.Context(), e.staff.App, e.store, queue.NewQueue(), e.tenant, f.ID); err != nil {
		e.t.Fatalf("CompleteUpload: %v", err)
	}
	return f.ID
}

func (e *importEnv) createImport(fileID uuid.UUID) (catalog.Import, error) {
	var imp catalog.Import
	err := e.app(func(tx pgx.Tx) error {
		var err error
		imp, err = e.writer().CreateImport(e.t.Context(), tx, e.seller, fileID, e.actor)
		return err //nolint:wrapcheck // test
	})
	return imp, err
}

func (e *importEnv) getImport(id uuid.UUID) catalog.Import {
	e.t.Helper()
	var imp catalog.Import
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		imp, err = catalog.GetImport(e.t.Context(), tx, e.seller, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("GetImport: %v", err)
	}
	return imp
}

// drain chạy worker tới khi không còn việc pending nào, kể cả việc được hẹn chạy lại sau (Task 429).
func (e *importEnv) drain() {
	e.t.Helper()
	e.staff.DrainJobs(e.t, e.w)
}

// validate tải body lên, tạo bản nhập, chạy lệnh kiểm và trả bản nhập sau khi kiểm.
func (e *importEnv) validate(body []byte) catalog.Import {
	e.t.Helper()
	imp, err := e.createImport(e.upload("catalog_import", body))
	if err != nil {
		e.t.Fatalf("CreateImport: %v", err)
	}
	if imp.Status != catalog.ImportValidating {
		e.t.Fatalf("bản nhập mới: %s, mong validating", imp.Status)
	}
	e.drain()
	return e.getImport(imp.ID)
}

func (e *importEnv) products() int {
	return e.count("SELECT count(*) FROM catalog.products")
}

// TestApply_200Models (phần kiểm, KH01): mau-200.xlsx → ready, summary {models 200, create 1000}, plan đủ 200 mẫu.
func TestApply_200Models(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(fixture(t, "mau-200.xlsx"))
	if imp.Status != catalog.ImportReady {
		t.Fatalf("trạng thái %s, lỗi %v", imp.Status, imp.Errors[:min(5, len(imp.Errors))])
	}
	if want := (catalog.ImportSummary{Models: 200, Create: 1000}); imp.Summary != want {
		t.Fatalf("summary %+v, mong %+v", imp.Summary, want)
	}
	if imp.Plan == nil || len(imp.Plan.Products) != 200 || len(imp.Errors) != 0 || imp.DuplicateOf != nil {
		t.Fatalf("plan/lỗi: %+v", imp)
	}
	n := 0
	for _, p := range imp.Plan.Products {
		n += len(p.Variants)
		if p.CategoryID == nil || len(p.TagIDs)+len(p.AttributeValueIDs) == 0 {
			t.Fatalf("mẫu %s thiếu phân loại: %+v", p.Code, p)
		}
	}
	if n != 1000 || e.products() != 0 {
		t.Fatalf("%d biến thể trong plan, %d mẫu trong database", n, e.products())
	}
	if e.count("SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.import.create'") != 1 {
		t.Fatal("thiếu audit catalog.import.create")
	}
	if got := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'catalog.import.validate' AND status = 'done'"); got != 1 {
		t.Fatalf("%d lệnh done", got)
	}

	// Phần áp (Task M1-47b): 200 mẫu, 1.000 biến thể, đủ danh mục, nhãn, thuộc tính như plan.
	start := time.Now()
	applied := e.applyNow(imp)
	t.Logf("áp 200 mẫu, 1.000 biến thể (cả xếp việc và worker): %s", time.Since(start))
	if applied.Status != catalog.ImportApplied || len(applied.Errors) != 0 {
		t.Fatalf("sau khi áp: %s %+v", applied.Status, applied.Errors)
	}
	if p, v := e.products(), e.count("SELECT count(*) FROM catalog.variants"); p != 200 || v != 1000 {
		t.Fatalf("%d mẫu, %d biến thể", p, v)
	}
	for _, pp := range imp.Plan.Products {
		e.checkClassification(pp)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.import.applied'
		AND entity_id = $1 AND after->>'create' = '1000' AND after->>'update' = '0'
		AND after->>'unchanged' = '0'`, imp.ID.String()); n != 1 {
		t.Fatalf("%d audit tổng catalog.import.applied đúng số", n)
	}
}

// loi-tung-dong.xlsx → invalid, đúng 7 lỗi đúng dòng, cột, mã; không có plan; 0 mẫu được tạo.
func TestImport_SevenRowErrors(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(fixture(t, "loi-tung-dong.xlsx"))
	if imp.Status != catalog.ImportInvalid || imp.Plan != nil {
		t.Fatalf("trạng thái %s, plan %v", imp.Status, imp.Plan != nil)
	}
	want := []string{
		"5:Tên mẫu*:missing_required", "9:Giá bán*:invalid_price", "12:SKU*:duplicate_sku_in_file",
		"20:Danh mục:unknown_category", "31:Mã màu:invalid_color_hex", "44:Trạng thái:invalid_status",
		"50:Tên mẫu*:inconsistent_product_fields",
	}
	got := make([]string, 0, len(imp.Errors))
	for _, x := range imp.Errors {
		got = append(got, strconv.Itoa(x.Row)+":"+x.Column+":"+x.Code)
		if x.Message == "" {
			t.Errorf("dòng %d thiếu lời báo", x.Row)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("lỗi %v\nmong %v", got, want)
	}
	if imp.Summary != (catalog.ImportSummary{Errors: 7}) || e.products() != 0 {
		t.Fatalf("summary %+v, %d mẫu", imp.Summary, e.products())
	}
}

// Việc treo (1) của Task 38: lỗi ô của xlsx.Read (ô Mô tả quá 2.000 ký tự: đọc thành rỗng, Validate không thấy lỗi)
// được gộp với lỗi dòng trước khi ghi → invalid, không có plan.
func TestImport_CellErrorsMerged(t *testing.T) {
	e := newImportEnv(t)
	rows := readRows(t, fixture(t, "mau-200.xlsx"))
	rows[6].Cells["Mô tả"] = strings.Repeat("ộ", 2001)
	imp := e.validate(writeRows(t, mauHeader(), rows))
	if imp.Status != catalog.ImportInvalid || imp.Plan != nil || len(imp.Errors) != 1 ||
		imp.Errors[0].Row != 8 || imp.Errors[0].Column != "Mô tả" || imp.Errors[0].Code != "cell_too_long" {
		t.Fatalf("%s %+v", imp.Status, imp.Errors)
	}
}

// Lỗi cấp tệp: không phải xlsx → invalid, một lỗi dòng 0 file_not_xlsx.
func TestImport_NotXlsx(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(fixture(t, "khong-phai-xlsx.xlsx"))
	if imp.Status != catalog.ImportInvalid || len(imp.Errors) != 1 || imp.Errors[0].Code != "file_not_xlsx" ||
		imp.Errors[0].Row != 0 || imp.Errors[0].Message == "" {
		t.Fatalf("%s %+v", imp.Status, imp.Errors)
	}
}

// Phần kiểm của TestApply_SameFileTwice (KH01): đã có bản nhập applied cùng file_sha256 → duplicate, duplicateOf,
// không plan.
func TestApply_SameFileTwice(t *testing.T) {
	e := newImportEnv(t)
	body := fixture(t, "mau-200.xlsx")
	sum := sha256.Sum256(body)
	oldFile := e.upload("catalog_import", body)
	var applied uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO catalog.imports
			(tenant_id, file_id, file_sha256, status, seller_id, created_by, applied_by, applied_at)
			VALUES ($1, $2, $3, 'applied', $4, $5, $5, now()) RETURNING id`,
			e.tenant, oldFile, sum[:], e.seller, e.actor.UserID).Scan(&applied)
	})
	imp := e.validate(body)
	if imp.Status != catalog.ImportDuplicate || imp.DuplicateOf == nil || *imp.DuplicateOf != applied || imp.Plan != nil {
		t.Fatalf("%s duplicateOf %v plan %v", imp.Status, imp.DuplicateOf, imp.Plan != nil)
	}
}

// TestApply_SameFileTwice (KH01, đường thật): áp mau-200.xlsx, tải lại đúng tệp đó → duplicate, duplicateOf = lần một,
// không mẫu, biến thể nào đổi (version, số dòng giữ nguyên); bản nhập thứ hai không áp được.
func TestApply_SameFileTwiceEndToEnd(t *testing.T) {
	e := newImportEnv(t)
	body := fixture(t, "mau-200.xlsx")
	first := e.applyNow(e.validate(body))
	if first.Status != catalog.ImportApplied {
		t.Fatalf("lần một: %s %+v", first.Status, first.Errors)
	}
	before := e.catalogFingerprint()
	second := e.validate(body)
	if second.Status != catalog.ImportDuplicate || second.DuplicateOf == nil || *second.DuplicateOf != first.ID ||
		second.Plan != nil {
		t.Fatalf("lần hai: %s duplicateOf %v", second.Status, second.DuplicateOf)
	}
	if err := e.apply(second); !errors.Is(err, catalog.ErrImportNotReady) {
		t.Fatalf("áp bản duplicate: %v", err)
	}
	e.drain()
	if after := e.catalogFingerprint(); after != before {
		t.Fatalf("dữ liệu đổi sau lần hai: %s → %s", before, after)
	}
}

// seedFromFile tạo các mẫu, biến thể của rows (đã đọc từ tệp) bằng Writer.CreateProduct.
func (e *importEnv) seedFromFile(rows []xlsx.Row) {
	e.t.Helper()
	byCode := map[string]*catalog.ProductInput{}
	var order []string
	for _, r := range rows {
		code := r.Cells["Mã mẫu*"]
		in, ok := byCode[code]
		if !ok {
			in = &catalog.ProductInput{SellerID: e.seller, Code: code, Name: r.Cells["Tên mẫu*"], Status: catalog.StatusActive}
			byCode[code] = in
			order = append(order, code)
		}
		price, _ := strconv.ParseInt(strings.ReplaceAll(r.Cells["Giá bán*"], ".", ""), 10, 64)
		v := catalog.VariantInput{
			SKU: r.Cells["SKU*"], Color: r.Cells["Màu*"], ColorHex: strings.ToUpper(r.Cells["Mã màu"]), Size: r.Cells["Size*"],
			Price: price,
		}
		if c := r.Cells["Giá nhập"]; c != "" {
			cost, _ := strconv.ParseInt(c, 10, 64)
			v.CostPrice = &cost
		}
		in.Variants = append(in.Variants, v)
	}
	err := e.app(func(tx pgx.Tx) error {
		for _, code := range order {
			if _, err := e.writer().CreateProduct(e.t.Context(), tx, *byCode[code], e.actor); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	if err != nil {
		e.t.Fatalf("tạo dữ liệu sẵn: %v", err)
	}
}

func readRows(t *testing.T, body []byte) []xlsx.Row {
	t.Helper()
	rows, cellErrs, err := xlsx.Read(bytes.NewReader(body), int64(len(body)), xlsx.Limits{}, nil)
	if err != nil || len(cellErrs) > 0 {
		t.Fatalf("xlsx.Read: %v %v", err, cellErrs)
	}
	return rows
}

// mauHeader là tiêu đề của mau-200.xlsx (testdata/xlsx/gen).
func mauHeader() []string {
	return []string{
		"Mã mẫu*", "Tên mẫu*", "Danh mục", "Mô tả", "Trạng thái", "Nhãn", "Thuộc tính: Chất liệu", "Thuộc tính: Kiểu dáng",
		"Màu*", "Mã màu", "Size*", "SKU*", "Barcode", "Giá bán*", "Giá nhập",
	}
}

// writeRows ghi rows với tiêu đề header bằng xlsx.Write.
func writeRows(t *testing.T, header []string, rows []xlsx.Row) []byte {
	t.Helper()
	s := xlsx.Sheet{Name: "Sản phẩm", Header: header}
	for _, r := range rows {
		line := make([]any, len(header))
		for i, h := range header {
			line[i] = r.Cells[h]
		}
		s.Rows = append(s.Rows, line)
	}
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, []xlsx.Sheet{s}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Tệp sửa giá 3 biến thể trên dữ liệu sẵn 200 mẫu, 1.000 biến thể → summary {models 200, update 3, unchanged 997}.
func TestImport_ThreePriceChanges(t *testing.T) {
	e := newImportEnv(t)
	body := fixture(t, "mau-200.xlsx")
	rows := readRows(t, body)
	e.seedFromFile(rows)
	for _, i := range []int{0, 499, 999} {
		rows[i].Cells["Giá bán*"] = "420000"
	}
	imp := e.validate(writeRows(t, mauHeader(), rows))
	if imp.Status != catalog.ImportReady {
		t.Fatalf("trạng thái %s, lỗi %v", imp.Status, imp.Errors[:min(5, len(imp.Errors))])
	}
	if want := (catalog.ImportSummary{Models: 200, Update: 3, Unchanged: 997}); imp.Summary != want {
		t.Fatalf("summary %+v, mong %+v", imp.Summary, want)
	}
	for _, p := range imp.Plan.Products {
		if p.ExistingProductID == nil {
			t.Fatalf("mẫu %s thiếu ExistingProductID", p.Code)
		}
	}
}

// Biến thể mới trùng màu và size với biến thể có sẵn không có trong tệp → invalid option_taken (việc treo (2) của
// Task 38, đọc từ database thật).
func TestImport_OptionTakenInDatabase(t *testing.T) {
	e := newImportEnv(t)
	if _, err := e.create(dam01(e.seller)); err != nil {
		t.Fatal(err)
	}
	body := writeRows(t, importHeader(), []xlsx.Row{{Number: 2, Cells: map[string]string{
		"Mã mẫu*": "DAM01", "Tên mẫu*": "Đầm lụa xòe", "Mô tả": "Đầm lụa dáng xòe", "Màu*": "Đỏ", "Size*": "S",
		"SKU*": "DAM01-MOI-S", "Giá bán*": "350000",
	}}})
	e.expectOneRowError(body, "option_taken")
}

// importHeader là dòng tiêu đề đủ cột của mẫu tệp nhập.
func importHeader() []string {
	return []string{
		"Mã mẫu*", "Tên mẫu*", "Danh mục", "Mô tả", "Trạng thái", "Nhãn", "Màu*", "Mã màu", "Size*",
		"SKU*", "Barcode", "Giá bán*", "Giá nhập",
	}
}

// expectOneRowError kiểm body thành invalid với đúng một lỗi mã code ở dòng 2.
func (e *importEnv) expectOneRowError(body []byte, code string) {
	e.t.Helper()
	imp := e.validate(body)
	if imp.Status != catalog.ImportInvalid || len(imp.Errors) != 1 || imp.Errors[0].Code != code || imp.Errors[0].Row != 2 {
		e.t.Fatalf("%s %+v", imp.Status, imp.Errors)
	}
}

// B-03 (review độc lập M1): barcode gõ tay trùng barcode của biến thể có sẵn mà SKU không có trong tệp → bước kiểm
// báo invalid_barcode đúng dòng (không để tới lúc áp mới failed với câu "vừa được dùng sau khi kiểm tệp").
func TestImport_BarcodeTakenInDatabase(t *testing.T) {
	e := newImportEnv(t)
	in := dam01(e.seller)
	in.Variants[0].Barcode = "8930000000017"
	if _, err := e.create(in); err != nil {
		t.Fatal(err)
	}
	body := writeRows(t, importHeader(), []xlsx.Row{{Number: 2, Cells: map[string]string{
		"Mã mẫu*": "DAM02", "Tên mẫu*": "Đầm mới", "Màu*": "Đỏ", "Size*": "S",
		"SKU*": "DAM02-DO-S", "Barcode": "8930000000017", "Giá bán*": "350000",
	}}})
	e.expectOneRowError(body, "invalid_barcode")
}

// Tạo bản nhập: tệp sai mục đích, chưa tải xong → ErrFileNotReady; tệp không có, của tenant khác → ErrNotFound;
// không ghi dòng nào.
func TestCreateImport_Refused(t *testing.T) {
	e := newImportEnv(t)
	other := e.staff.Tenant(t, "B")
	var waiting files.File
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		waiting, _, err = files.CreateUpload(t.Context(), tx, e.store, files.UploadInput{
			TenantID: e.tenant, Purpose: "catalog_import", FileName: "a.xlsx", ContentType: xlsxType, SizeBytes: 10,
		}, e.actor.UserID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	var foreign uuid.UUID
	e.staff.Exec(t, other, func(tx pgx.Tx) error {
		f, _, err := files.CreateUpload(t.Context(), tx, e.store, files.UploadInput{
			TenantID: other, Purpose: "catalog_import", FileName: "b.xlsx", ContentType: xlsxType, SizeBytes: 10,
		}, uuid.Nil)
		foreign = f.ID
		return err //nolint:wrapcheck // test
	})
	var image uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		image = filestest.Uploaded(t, tx, e.tenant, "product_image")
		return nil
	})
	for name, c := range map[string]struct {
		id   uuid.UUID
		want error
	}{
		"chưa tải xong": {waiting.ID, catalog.ErrFileNotReady},
		"mục đích ảnh":  {image, catalog.ErrFileNotReady},
		"không có":      {uuid.Must(uuid.NewV7()), catalog.ErrNotFound},
		"tenant khác":   {foreign, catalog.ErrNotFound},
	} {
		if _, err := e.createImport(c.id); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, mong %v", name, err, c.want)
		}
	}
	if n := e.count("SELECT count(*) FROM catalog.imports"); n != 0 {
		t.Fatalf("%d bản nhập", n)
	}
}

// Tệp mất trên kho → lệnh dead, bản nhập failed (không kẹt ở validating).
func TestImport_SourceMissingFails(t *testing.T) {
	e := newImportEnv(t)
	id := e.upload("catalog_import", fixture(t, "mau-200.xlsx"))
	imp, err := e.createImport(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.Delete(t.Context(), files.BucketTmp, files.TmpKey(e.tenant, id)); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if got := e.getImport(imp.ID); got.Status != catalog.ImportFailed {
		t.Fatalf("trạng thái %s, mong failed", got.Status)
	}
}

// Chạy lại lệnh (in_doubt rồi tra lại, hay lease hết) không đổi bản nhập đã kiểm xong; Lookup: đã xong → done.
func TestImport_RerunKeepsResult(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(fixture(t, "mau-200.xlsx"))
	v := catalog.NewImportValidator(catalog.ImportValidatorOptions{DB: e.staff.App, Store: e.store})
	var j queue.Job
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id, kind, tenant_id, payload FROM platform.jobs
			WHERE kind = 'catalog.import.validate'`).Scan(&j.ID, &j.Kind, &j.TenantID, &j.Payload)
	})
	if got, err := v.Lookup(t.Context(), j); err != nil || got != commands.StatusDone {
		t.Fatalf("Lookup: %s %v", got, err)
	}
	out, err := v.Execute(context.WithoutCancel(t.Context()), j)
	if err != nil || out.Status != commands.StatusDone {
		t.Fatalf("chạy lại: %+v %v", out, err)
	}
	if again := e.getImport(imp.ID); again.Status != catalog.ImportReady || again.Version != imp.Version {
		t.Fatalf("chạy lại đổi bản nhập: %s v%d → v%d", again.Status, imp.Version, again.Version)
	}
}

// ImportTemplate → tệp .xlsx đọc lại được bằng xlsx.Read với cột của mẫu Task M1-38, kèm cột thuộc tính đang có.
func TestImportTemplate(t *testing.T) {
	e := newImportEnv(t)
	var body []byte
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		body, err = catalog.ImportTemplate(t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	rows, cellErrs, err := xlsx.Read(bytes.NewReader(body), int64(len(body)), xlsx.Limits{}, mauHeader())
	if err != nil || len(cellErrs) > 0 || len(rows) != 0 {
		t.Fatalf("đọc lại mẫu: %v %v %d dòng", err, cellErrs, len(rows))
	}
}

// B-06 (review độc lập M1): tên danh mục có ">" (dấu tách cấp của cột Danh mục) và giá trị thuộc tính có ";" (dấu tách
// nhiều giá trị) không tạo, không sửa được — tệp nhập không bao giờ chọn đúng được chúng. Dữ liệu cũ (ghi thẳng, trước
// luật này) cho hai danh mục cùng đường dẫn "Áo > Áo sơ mi": tệp chọn đường dẫn đó → lỗi dòng, không gán bừa một cái.
func TestClassification_ImportSeparators(t *testing.T) {
	e := newImportEnv(t)
	if _, err := e.createCategory(catalog.CategoryInput{Name: "Váy > Váy xòe"}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Errorf("tạo danh mục có '>': mong ErrInvalidInput, có %v", err)
	}
	c := e.mustCategory("Quần", nil)
	if _, err := e.updateCategory(c.ID, c.Version, catalog.CategoryPatch{Name: ptr("Quần>Jean")}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Errorf("đổi tên danh mục có '>': mong ErrInvalidInput, có %v", err)
	}
	var attr catalog.Attribute
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		attr, err = e.writer().CreateAttribute(t.Context(), tx, catalog.AttributeInput{Code: "vai", Name: "Vải"}, e.actor)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = e.writer().AddAttributeValue(t.Context(), tx, attr.ID, catalog.AttributeValueInput{Value: "Cotton; lụa"}, e.actor)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Errorf("giá trị thuộc tính có ';': mong ErrInvalidInput, có %v", err)
	}

	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO catalog.categories (tenant_id, name, slug, depth, position)
			VALUES ($1, 'Áo > Áo sơ mi', 'ao-ao-so-mi-cu', 1, 99)`, e.tenant)
		return err //nolint:wrapcheck // test
	})
	imp := e.validate(oneProduct(t, "AO01", "Áo", "1", "S"))
	if imp.Status != catalog.ImportInvalid || len(imp.Errors) != 1 || imp.Errors[0].Column != "Danh mục" {
		t.Fatalf("đường dẫn danh mục trùng: %s %+v, mong invalid với một lỗi cột Danh mục", imp.Status, imp.Errors)
	}
}
