package catalog_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// apply gọi Writer.ApplyImport cho bản nhập imp ở version của nó (chưa chạy việc).
func (e *importEnv) apply(imp catalog.Import) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := e.writer().ApplyImport(e.t.Context(), tx, e.seller, imp.ID, imp.Version, e.actor)
		return err //nolint:wrapcheck // test
	})
}

// applyNow áp imp, chạy worker tới hết việc và trả bản nhập sau khi áp.
func (e *importEnv) applyNow(imp catalog.Import) catalog.Import {
	e.t.Helper()
	if err := e.apply(imp); err != nil {
		e.t.Fatalf("ApplyImport: %v", err)
	}
	if got := e.getImport(imp.ID); got.Status != catalog.ImportApplying {
		e.t.Fatalf("ngay sau ApplyImport: %s, mong applying", got.Status)
	}
	e.drain()
	return e.getImport(imp.ID)
}

// mustReady dừng test khi bản nhập imp không ready.
func (e *importEnv) mustReady(imp catalog.Import) {
	e.t.Helper()
	if imp.Status != catalog.ImportReady {
		e.t.Fatalf("%s %+v", imp.Status, imp.Errors)
	}
}

// applyExpect áp imp, chạy worker và đòi trạng thái want; trả bản nhập sau khi áp.
func (e *importEnv) applyExpect(imp catalog.Import, want string) catalog.Import {
	e.t.Helper()
	got := e.applyNow(imp)
	if got.Status != want {
		e.t.Fatalf("sau khi áp: %s %+v, mong %s", got.Status, got.Errors, want)
	}
	return got
}

// createOther tạo tay mẫu code với một biến thể v (nhân viên chen ngang giữa lúc kiểm và lúc áp).
func (e *importEnv) createOther(code string, v catalog.VariantInput) {
	e.t.Helper()
	if _, err := e.create(catalog.ProductInput{SellerID: e.seller, Code: code, Name: code, Variants: []catalog.VariantInput{v}}); err != nil {
		e.t.Fatal(err)
	}
}

// applyJob đọc việc catalog.import.apply (duy nhất) của tenant.
func (e *importEnv) applyJob() queue.Job {
	e.t.Helper()
	job := queue.Job{TenantID: &e.tenant}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT id, payload FROM platform.jobs WHERE kind = 'catalog.import.apply'").
			Scan(&job.ID, &job.Payload)
	})
	return job
}

// checkClassification: mẫu pp của plan có trong database với đúng danh mục, giá trị thuộc tính, nhãn của plan.
func (e *importEnv) checkClassification(pp catalog.ImportPlannedProduct) {
	e.t.Helper()
	var c catalog.ProductClassification
	if err := e.app(func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(e.t.Context(), "SELECT id FROM catalog.products WHERE code = $1", pp.Code).Scan(&id); err != nil {
			return err //nolint:wrapcheck // test
		}
		var err error
		c, err = catalog.GetClassification(e.t.Context(), tx, e.seller, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("đọc phân loại %s: %v", pp.Code, err)
	}
	if !equalPtr(c.CategoryID, pp.CategoryID) || !slices.Equal(c.AttributeValueIDs, pp.AttributeValueIDs) ||
		!slices.Equal(c.TagIDs, pp.TagIDs) {
		e.t.Fatalf("phân loại %s = %+v, plan %+v", pp.Code, c.Classification, pp)
	}
}

func equalPtr[T comparable](a, b *T) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// catalogFingerprint tóm tắt mọi mẫu, biến thể (số dòng, tổng version, tổng giá) để so "không đổi gì".
func (e *importEnv) catalogFingerprint() string {
	e.t.Helper()
	var s string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT format('%s mẫu v%s / %s biến thể v%s giá %s',
			(SELECT count(*) FROM catalog.products), (SELECT coalesce(sum(version), 0) FROM catalog.products),
			(SELECT count(*) FROM catalog.variants), (SELECT coalesce(sum(version), 0) FROM catalog.variants),
			(SELECT coalesce(sum(price), 0) FROM catalog.variants))`).Scan(&s)
	})
	return s
}

// oneProduct là tệp một mẫu code, các size sizes (SKU code-size), giá price, tên name.
func oneProduct(t *testing.T, code, name string, price string, sizes ...string) []byte {
	t.Helper()
	rows := make([]xlsx.Row, 0, len(sizes))
	for i, s := range sizes {
		rows = append(rows, xlsx.Row{Number: i + 2, Cells: map[string]string{
			"Mã mẫu*": code, "Tên mẫu*": name, "Danh mục": "Áo > Áo sơ mi", "Nhãn": "sale", "Màu*": "Đỏ", "Size*": s,
			"SKU*": code + "-" + s, "Giá bán*": price,
		}})
	}
	return writeRows(t, basicHeader(), rows)
}

// basicHeader là tiêu đề đủ cột bắt buộc của mẫu, không cột thuộc tính.
func basicHeader() []string {
	return []string{
		"Mã mẫu*", "Tên mẫu*", "Danh mục", "Mô tả", "Trạng thái", "Nhãn", "Màu*", "Mã màu", "Size*",
		"SKU*", "Barcode", "Giá bán*", "Giá nhập",
	}
}

// Bản nhập không ready (invalid) → ErrImportNotReady, không xếp việc; version cũ → ErrVersionConflict; 0 mẫu.
func TestApply_Refused(t *testing.T) {
	e := newImportEnv(t)
	bad := e.validate(fixture(t, "loi-tung-dong.xlsx"))
	if bad.Status != catalog.ImportInvalid {
		t.Fatalf("loi-tung-dong: %s", bad.Status)
	}
	if err := e.apply(bad); !errors.Is(err, catalog.ErrImportNotReady) {
		t.Fatalf("áp bản invalid: %v", err)
	}
	ready := e.validate(oneProduct(t, "AO01", "Áo sơ mi", "250000", "S"))
	stale := ready
	stale.Version--
	if err := e.apply(stale); !errors.Is(err, catalog.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	other := ready
	other.ID = uuid.New()
	if err := e.apply(other); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("bản nhập không có: %v", err)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'catalog.import.apply'"); n != 0 || e.products() != 0 {
		t.Fatalf("%d việc áp, %d mẫu", n, e.products())
	}
	// Áp lần hai sau khi đã applying → ErrImportNotReady (không xếp việc thứ hai).
	if err := e.apply(ready); err != nil {
		t.Fatal(err)
	}
	if err := e.apply(e.getImport(ready.ID)); !errors.Is(err, catalog.ErrImportNotReady) {
		t.Fatalf("áp lần hai: %v", err)
	}
}

// Tệp sửa giá 3 biến thể trên 200 mẫu có sẵn → áp → đúng 3 price.changed, giá mới đúng, 997 biến thể giữ version.
func TestApply_ThreePriceChanges(t *testing.T) {
	e := newImportEnv(t)
	rows := readRows(t, fixture(t, "mau-200.xlsx"))
	e.seedFromFile(rows)
	skus := []string{rows[0].Cells["SKU*"], rows[499].Cells["SKU*"], rows[999].Cells["SKU*"]}
	for _, i := range []int{0, 499, 999} {
		rows[i].Cells["Giá bán*"] = "420000"
	}
	imp := e.validate(writeRows(t, mauHeader(), rows))
	if imp.Summary.Update != 3 {
		t.Fatalf("summary %+v", imp.Summary)
	}
	e.applyExpect(imp, catalog.ImportApplied)
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'price.changed'"); n != 3 {
		t.Fatalf("%d price.changed, mong 3", n)
	}
	if n := e.count("SELECT count(*) FROM catalog.variants WHERE price = 420000 AND sku = ANY($1)", skus); n != 3 {
		t.Fatalf("%d biến thể có giá mới", n)
	}
	if n := e.count("SELECT count(*) FROM catalog.variants WHERE version = 1"); n != 997 {
		t.Fatalf("%d biến thể giữ version 1, mong 997", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.import.applied'
		AND after->>'update' = '3' AND after->>'unchanged' = '997' AND after->>'create' = '0'`); n != 1 {
		t.Fatalf("%d audit tổng đúng số", n)
	}
}

// Giữa lúc kiểm và lúc áp, nhân viên tạo tay SKU của plan cho mẫu khác → áp thành failed, lỗi dòng
// sku_belongs_to_other_product, 0 thay đổi; việc done (không thử lại).
func TestApply_SKUTakenMeanwhile(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(oneProduct(t, "AO01", "Áo sơ mi", "250000", "S", "M", "L"))
	e.mustReady(imp)
	e.createOther("VAY09", catalog.VariantInput{SKU: "AO01-M", Color: "Xanh", Size: "M", Price: 1})
	before := e.catalogFingerprint()
	got := e.applyExpect(imp, catalog.ImportFailed)
	if len(got.Errors) != 1 || got.Errors[0].Row != 3 ||
		got.Errors[0].Code != "sku_belongs_to_other_product" || got.Errors[0].Column != "SKU*" {
		t.Fatalf("%s %+v", got.Status, got.Errors)
	}
	if after := e.catalogFingerprint(); after != before {
		t.Fatalf("dữ liệu đổi: %s → %s", before, after)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'catalog.import.apply' AND status = 'done'"); n != 1 {
		t.Fatalf("%d việc done", n)
	}
}

// Lỗi ở giữa lúc ghi (barcode gõ tay vừa bị biến thể khác lấy: Recheck không thấy, unique của database báo) → lùi
// toàn bộ phần đã ghi, failed với lỗi đúng dòng.
func TestApply_WriteErrorRollsBackEverything(t *testing.T) {
	e := newImportEnv(t)
	var rows []xlsx.Row
	for i := range 6 {
		rows = append(rows, xlsx.Row{Number: i + 2, Cells: map[string]string{
			"Mã mẫu*": fmt.Sprintf("M%02d", i/2), "Tên mẫu*": "Mẫu", "Màu*": "Đỏ", "Size*": []string{"S", "M"}[i%2],
			"SKU*": fmt.Sprintf("M%02d-%d", i/2, i), "Barcode": fmt.Sprintf("BC%04d", i), "Giá bán*": "1000",
		}})
	}
	imp := e.validate(writeRows(t, basicHeader(), rows))
	e.mustReady(imp)
	e.createOther("KHAC", catalog.VariantInput{SKU: "KHAC-1", Barcode: "BC0005", Color: "Đen", Size: "S", Price: 1})
	before := e.catalogFingerprint()
	got := e.applyExpect(imp, catalog.ImportFailed)
	if len(got.Errors) != 1 || got.Errors[0].Row != 7 ||
		got.Errors[0].Code != "barcode_taken" {
		t.Fatalf("%s %+v", got.Status, got.Errors)
	}
	if after := e.catalogFingerprint(); after != before {
		t.Fatalf("còn phần ghi dở: %s → %s", before, after)
	}
}

// Hai lần áp song song của hai bản nhập khác nhau cùng chạm SKU AO01-S → không trùng SKU, không deadlock; mỗi lần
// applied hoặc failed, đúng một applied.
func TestApply_TwoImportsSameSKUConcurrently(t *testing.T) {
	e := newImportEnv(t)
	a := e.validate(oneProduct(t, "AO01", "Áo sơ mi trắng", "250000", "S", "M"))
	b := e.validate(oneProduct(t, "AO01", "Áo sơ mi kẻ", "260000", "S", "L"))
	if a.Status != catalog.ImportReady || b.Status != catalog.ImportReady {
		t.Fatalf("%s %s", a.Status, b.Status)
	}
	for _, imp := range []catalog.Import{a, b} {
		if err := e.apply(imp); err != nil {
			t.Fatal(err)
		}
	}
	v := catalog.NewImportValidator(catalog.ImportValidatorOptions{DB: e.staff.App, Store: e.store})
	workers := []*queue.Worker{e.newWorker(v), e.newWorker(v)}
	var wg sync.WaitGroup
	errs := make([]error, len(workers))
	for i, w := range workers {
		wg.Go(func() { _, errs[i] = w.RunOnce(t.Context(), 1) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	e.drain()
	statuses := []string{e.getImport(a.ID).Status, e.getImport(b.ID).Status}
	slices.Sort(statuses)
	if !slices.Equal(statuses, []string{catalog.ImportApplied, catalog.ImportFailed}) {
		t.Fatalf("trạng thái %v", statuses)
	}
	if n := e.count("SELECT count(*) FROM catalog.variants WHERE sku = 'AO01-S'"); n != 1 || e.products() != 1 {
		t.Fatalf("%d biến thể AO01-S, %d mẫu", n, e.products())
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'catalog.import.apply' AND status <> 'done'"); n != 0 {
		t.Fatalf("%d việc áp chưa done", n)
	}
}

// Mẫu có sẵn: tệp đổi tên, thêm phân loại, đổi size của hai biến thể cho nhau (S ↔ M) và thêm biến thể mới → applied,
// đúng dữ liệu (đổi chỗ không vướng unique màu+size).
func TestApply_ExistingProductSwapAndClassify(t *testing.T) {
	e := newImportEnv(t)
	if _, err := e.create(catalog.ProductInput{SellerID: e.seller, Code: "AO01", Name: "Áo cũ", Variants: []catalog.VariantInput{
		{SKU: "AO01-S", Color: "Đỏ", Size: "S", Price: 1}, {SKU: "AO01-M", Color: "Đỏ", Size: "M", Price: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	body := writeRows(t, basicHeader(), []xlsx.Row{
		{Number: 2, Cells: map[string]string{"Mã mẫu*": "AO01", "Tên mẫu*": "Áo mới", "Danh mục": "Áo", "Nhãn": "sale", "Màu*": "Đỏ", "Size*": "M", "SKU*": "AO01-S", "Giá bán*": "1"}},
		{Number: 3, Cells: map[string]string{"Mã mẫu*": "AO01", "Tên mẫu*": "Áo mới", "Danh mục": "Áo", "Nhãn": "sale", "Màu*": "Đỏ", "Size*": "S", "SKU*": "AO01-M", "Giá bán*": "1"}},
		{Number: 4, Cells: map[string]string{"Mã mẫu*": "AO01", "Tên mẫu*": "Áo mới", "Danh mục": "Áo", "Nhãn": "sale", "Màu*": "Đỏ", "Size*": "L", "SKU*": "AO01-L", "Giá bán*": "1"}},
	})
	imp := e.validate(body)
	e.mustReady(imp)
	e.applyExpect(imp, catalog.ImportApplied)
	if n := e.count(`SELECT count(*) FROM catalog.variants v JOIN catalog.products p ON p.id = v.product_id
		WHERE p.name = 'Áo mới' AND ((v.sku = 'AO01-S' AND v.size = 'M') OR (v.sku = 'AO01-M' AND v.size = 'S')
		OR (v.sku = 'AO01-L' AND v.size = 'L'))`); n != 3 {
		t.Fatalf("%d biến thể đúng", n)
	}
	e.checkClassification(imp.Plan.Products[0])
}

// Việc áp hết lượt (dead) → OnTerminal đặt bản nhập applying thành failed (không kẹt applying).
func TestApply_DeadJobFailsImport(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(oneProduct(t, "AO01", "Áo", "1", "S"))
	if err := e.apply(imp); err != nil {
		t.Fatal(err)
	}
	job := e.applyJob()
	if err := e.app(func(tx pgx.Tx) error {
		return catalog.NewImportApplier(e.writer()).OnTerminal(t.Context(), job, queue.StatusDead, tx)
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.getImport(imp.ID); got.Status != catalog.ImportFailed {
		t.Fatalf("%s", got.Status)
	}
}

// Việc hết hạn giữa lúc ghi (lỗi hạ tầng, không phải dữ liệu đổi) → Handle trả lỗi để việc chạy lại; bản nhập vẫn
// applying, không mẫu nào được ghi (không bị coi là lỗi vĩnh viễn rồi failed).
func TestApply_TimeoutMidWriteRetries(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(fixture(t, "mau-200.xlsx"))
	if err := e.apply(imp); err != nil {
		t.Fatal(err)
	}
	job := e.applyJob()
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	err := tenancy.WithTenant(ctx, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := catalog.NewImportApplier(e.writer()).Handle(ctx, job, tx)
		return err //nolint:wrapcheck // test
	})
	if err == nil || errors.Is(err, queue.ErrPermanent) {
		t.Fatalf("Handle hết hạn: %v, mong lỗi chạy lại được", err)
	}
	if got := e.getImport(imp.ID); got.Status != catalog.ImportApplying || e.products() != 0 {
		t.Fatalf("%s, %d mẫu", got.Status, e.products())
	}
}

// Đổi dây chuyền và tạo vào chỗ vừa trống (reviewer): A S→M đứng trước B M→L; biến thể mới C lấy S (chỗ của A) đứng
// trước dòng A → applied, đúng màu+size.
func TestApply_ChainedOptionMoves(t *testing.T) {
	e := newImportEnv(t)
	if _, err := e.create(catalog.ProductInput{SellerID: e.seller, Code: "AO01", Name: "Áo", Variants: []catalog.VariantInput{
		{SKU: "AO01-A", Color: "Đỏ", Size: "S", Price: 1}, {SKU: "AO01-B", Color: "Đỏ", Size: "M", Price: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	row := func(n int, sku, size string) xlsx.Row {
		return xlsx.Row{Number: n, Cells: map[string]string{
			"Mã mẫu*": "AO01", "Tên mẫu*": "Áo", "Màu*": "Đỏ", "Size*": size, "SKU*": sku, "Giá bán*": "1",
		}}
	}
	imp := e.validate(writeRows(t, basicHeader(), []xlsx.Row{
		row(2, "AO01-C", "S"), row(3, "AO01-A", "M"), row(4, "AO01-B", "L"),
	}))
	e.mustReady(imp)
	e.applyExpect(imp, catalog.ImportApplied)
	if n := e.count(`SELECT count(*) FROM catalog.variants WHERE (sku, size) IN
		(('AO01-C', 'S'), ('AO01-A', 'M'), ('AO01-B', 'L'))`); n != 3 {
		t.Fatalf("%d biến thể đúng size", n)
	}
}

// Quyết định 6: hai bản nhập cùng một tệp đều ready (chưa bản nào áp) → áp cả hai: bản đầu applied, bản sau duplicate
// (duplicateOf = bản đầu, bỏ plan), không ghi gì thêm.
func TestApply_SameFileBothReady(t *testing.T) {
	e := newImportEnv(t)
	body := oneProduct(t, "AO01", "Áo", "1", "S", "M")
	first, second := e.validate(body), e.validate(body)
	if first.Status != catalog.ImportReady || second.Status != catalog.ImportReady {
		t.Fatalf("%s %s", first.Status, second.Status)
	}
	if got := e.applyNow(first); got.Status != catalog.ImportApplied {
		t.Fatalf("%s %+v", got.Status, got.Errors)
	}
	before := e.catalogFingerprint()
	got := e.applyNow(second)
	if got.Status != catalog.ImportDuplicate || got.DuplicateOf == nil || *got.DuplicateOf != first.ID || got.Plan != nil {
		t.Fatalf("%s duplicateOf %v plan %v", got.Status, got.DuplicateOf, got.Plan != nil)
	}
	if after := e.catalogFingerprint(); after != before {
		t.Fatalf("dữ liệu đổi: %s → %s", before, after)
	}
}

// B-04 (review độc lập M1): việc áp đã ghi xong mà transaction còn mở (tệp lớn: tới 30 s) không giữ khóa bộ đếm
// barcode — CreateProduct barcode trống với lock_timeout 300 ms vẫn qua; sau khi cả hai commit, mọi barcode khác nhau.
func TestApply_DoesNotHoldBarcodeCounter(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(oneProduct(t, "AO01", "Áo", "1", "S", "M", "L"))
	e.mustReady(imp)
	if err := e.apply(imp); err != nil {
		t.Fatal(err)
	}
	job := e.applyJob()
	handled, release := make(chan error, 1), make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
			_, err := catalog.NewImportApplier(e.writer()).Handle(t.Context(), job, tx)
			handled <- err
			<-release
			return err //nolint:wrapcheck // test
		})
	}()
	if err := <-handled; err != nil {
		close(release)
		t.Fatalf("Handle: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	err := tenancy.WithTenant(ctx, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '300ms'"); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := e.writer().CreateProduct(ctx, tx, catalog.ProductInput{
			SellerID: e.seller, Code: "TAY01", Name: "Tay",
			Variants: []catalog.VariantInput{{SKU: "TAY01-S", Color: "Đỏ", Size: "S", Price: 1}},
		}, e.actor)
		return err //nolint:wrapcheck // test
	})
	close(release)
	if herr := <-held; herr != nil {
		t.Fatalf("commit việc áp: %v", herr)
	}
	if err != nil {
		t.Fatalf("CreateProduct trong lúc việc áp còn mở: %v", err)
	}
	if n, d := e.count("SELECT count(*) FROM catalog.variants"), e.count("SELECT count(DISTINCT barcode) FROM catalog.variants"); n != 4 || d != 4 {
		t.Fatalf("%d biến thể, %d barcode khác nhau; mong 4, 4", n, d)
	}
}

// Việc áp xếp trước khi có dải barcode cấp sẵn (payload không có dải) vẫn áp được, barcode tự sinh hợp lệ, khác nhau.
func TestApply_PayloadWithoutBarcodeRange(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(oneProduct(t, "AO01", "Áo", "1", "S", "M"))
	e.mustReady(imp)
	if err := e.apply(imp); err != nil {
		t.Fatal(err)
	}
	job := e.applyJob()
	job.Payload = []byte(fmt.Sprintf(`{"importId":%q,"actorId":%q}`, imp.ID, e.actor.UserID))
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := catalog.NewImportApplier(e.writer()).Handle(t.Context(), job, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.getImport(imp.ID); got.Status != catalog.ImportApplied {
		t.Fatalf("%s %+v", got.Status, got.Errors)
	}
	if d := e.count("SELECT count(DISTINCT barcode) FROM catalog.variants WHERE barcode LIKE '2%' AND length(barcode) = 13"); d != 2 {
		t.Fatalf("%d barcode tự sinh khác nhau, mong 2", d)
	}
}

// handleWithClock chạy Handle của việc áp (lượt attempts) trong một tx tenant có hạn 1 phút; đồng hồ của việc áp trả
// giờ thật cho `realCalls` lần đọc đầu rồi nhảy tới sau hạn (hết ngân sách giữa lúc ghi, sau khi đã ghi vài mẫu).
func (e *importEnv) handleWithClock(attempts, realCalls int) error {
	e.t.Helper()
	job := e.applyJob()
	job.Attempts = attempts
	var calls int
	clock := func() time.Time {
		calls++
		if calls > realCalls {
			return time.Now().Add(time.Hour)
		}
		return time.Now()
	}
	ctx, cancel := context.WithTimeout(e.t.Context(), time.Minute)
	defer cancel()
	err := tenancy.WithTenant(ctx, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := catalog.NewImportApplierWithClock(e.writer(), clock).Handle(ctx, job, tx)
		return err //nolint:wrapcheck // test
	})
	if calls <= realCalls {
		e.t.Fatalf("đồng hồ chỉ được đọc %d lần, mong hơn %d (không tới chỗ hết ngân sách)", calls, realCalls)
	}
	return err //nolint:wrapcheck // test
}

// applyingBig kiểm và yêu cầu áp mau-200.xlsx (chưa chạy việc); trả bản nhập và dấu dữ liệu catalog trước khi áp.
func (e *importEnv) applyingBig() (catalog.Import, string) {
	e.t.Helper()
	imp := e.validate(fixture(e.t, "mau-200.xlsx"))
	e.mustReady(imp)
	if err := e.apply(imp); err != nil {
		e.t.Fatal(err)
	}
	return imp, e.catalogFingerprint()
}

// B-01 (Task 407): lần áp hết ngân sách thời gian giữa lúc ghi ở lượt 1, 2 → Handle trả lỗi chạy lại được (máy bận
// nhất thời); bản nhập vẫn applying; các mẫu đã ghi trước đó bị lùi hết.
func TestApply_OverTimeBudgetRetries(t *testing.T) {
	e := newImportEnv(t)
	imp, before := e.applyingBig()
	for _, attempt := range []int{1, 2} {
		err := e.handleWithClock(attempt, 20)
		if err == nil || errors.Is(err, queue.ErrPermanent) {
			t.Fatalf("lượt %d hết ngân sách: %v, mong lỗi chạy lại được", attempt, err)
		}
		if got := e.getImport(imp.ID); got.Status != catalog.ImportApplying || len(got.Errors) != 0 {
			t.Fatalf("lượt %d: %s %+v, mong applying không lỗi", attempt, got.Status, got.Errors)
		}
		if e.products() != 0 || e.catalogFingerprint() != before {
			t.Fatalf("lượt %d: còn %d mẫu sau khi lùi", attempt, e.products())
		}
	}
}

// B-01 (Task 407): từ lượt 3 lần áp vẫn hết ngân sách → bản nhập failed với đúng một lỗi cả tệp (dòng 0)
// apply_too_long, tóm tắt 1 lỗi, việc xong (Handle không lỗi: không còn chạy lại tới hàng giờ rồi dead); không mẫu,
// biến thể nào được ghi (tất cả hoặc không). Tệp nhỏ vẫn áp được bằng worker sau đó (không ảnh hưởng tệp khác).
func TestApply_OverTimeBudgetFailsWithReason(t *testing.T) {
	e := newImportEnv(t)
	imp, before := e.applyingBig()
	if err := e.handleWithClock(3, 20); err != nil {
		t.Fatalf("lượt 3 hết ngân sách: %v, mong xong (failed)", err)
	}
	got := e.getImport(imp.ID)
	if got.Status != catalog.ImportFailed || len(got.Errors) != 1 || got.Summary.Errors != 1 {
		t.Fatalf("%s %+v tóm tắt %d lỗi, mong failed với 1 lỗi", got.Status, got.Errors, got.Summary.Errors)
	}
	if ie := got.Errors[0]; ie.Row != 0 || ie.Column != "" || ie.Code != catalog.CodeApplyTooLong ||
		!strings.Contains(ie.Message, "chia") {
		t.Fatalf("lỗi %+v, mong dòng 0 %s bảo chia tệp", ie, catalog.CodeApplyTooLong)
	}
	if e.products() != 0 || e.count("SELECT count(*) FROM catalog.variants") != 0 || e.catalogFingerprint() != before {
		t.Fatalf("còn %d mẫu sau khi failed", e.products())
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.import.apply_failed'"); n != 1 {
		t.Fatalf("%d audit apply_failed, mong 1", n)
	}
	small := e.validate(oneProduct(t, "AO01", "Áo", "100000", "S", "M"))
	e.mustReady(small)
	e.applyExpect(small, catalog.ImportApplied)
}

// Task 429: việc áp bị hẹn chạy lại sau (như sau một lần hết ngân sách thời gian khi máy bận) → drain vẫn chạy nó tới
// cuối thay vì dừng khi hết việc đến hạn; bản nhập applied, việc done.
func TestApply_DrainRunsScheduledRetry(t *testing.T) {
	e := newImportEnv(t)
	imp := e.validate(oneProduct(t, "AO01", "Áo", "100000", "S", "M"))
	e.mustReady(imp)
	if err := e.apply(imp); err != nil {
		t.Fatal(err)
	}
	if _, err := e.staff.Owner.Pool.Exec(t.Context(), `UPDATE platform.jobs SET run_at = now() + interval '1 hour',
		attempts = 1 WHERE kind = 'catalog.import.apply'`); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if got := e.getImport(imp.ID); got.Status != catalog.ImportApplied {
		t.Fatalf("%s %+v, mong applied", got.Status, got.Errors)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'catalog.import.apply' AND status = 'done'"); n != 1 {
		t.Fatalf("%d việc áp done, mong 1", n)
	}
}
