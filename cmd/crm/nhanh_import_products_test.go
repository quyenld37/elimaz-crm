package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/nhanh"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Dữ liệu giả hoàn toàn: hai mẫu Nhanh. Mẫu 100 có hai biến thể (Đỏ M tồn 7, Xanh L tồn 3), ảnh chung và ảnh theo màu;
// mẫu 200 là sản phẩm đơn mà CRM chưa có (SKU thiếu).
func nhanhFakeSnapshot() *nhanh.Snapshot {
	r := func(s string) json.RawMessage { return json.RawMessage(s) }
	return &nhanh.Snapshot{
		Version: nhanh.SnapshotVersion, TakenAt: time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC),
		Products: []nhanh.Product{
			{
				ID: 100, List: r(`{"id":100,"parentId":-2,"code":"TEST-AO","name":"Áo thử chuyển","categoryId":55}`),
				Detail: r(`{"id":100,"description":"Mô tả thử","images":{"avatar":"https://cdn.example.test/chung.jpg"}}`),
				Variants: []json.RawMessage{
					r(`{"id":101,"code":"TEST-AO-D-M","prices":{"retail":300000,"retailVat":30000,"import":150000},` +
						`"attributes":[{"name":"Màu","value":"Đỏ"},{"name":"Size","value":"M"}],"images":{"avatar":"https://cdn.example.test/do.jpg"}}`),
					r(`{"id":102,"code":"TEST-AO-X-L","prices":{"retail":300000,"retailVat":30000},` +
						`"attributes":[{"name":"Màu","value":"Xanh"},{"name":"Size","value":"L"}],"images":{"avatar":"https://cdn.example.test/xanh.jpg"}}`),
				},
				Inventory: []json.RawMessage{
					r(`{"productId":101,"inventory":{"remain":7}}`), r(`{"productId":102,"inventory":{"remain":3}}`),
				},
			},
			{
				ID: 200, List: r(`{"id":200,"code":"TEST-DON","name":"Sản phẩm đơn","prices":{"retail":100000,"retailVat":0}}`),
				Variants: []json.RawMessage{}, Inventory: []json.RawMessage{r(`{"productId":200,"inventory":{"remain":4}}`)},
			},
		},
	}
}

type nhanhImportEnv struct {
	t         *testing.T
	s         *testkit.Staff
	tenant    uuid.UUID
	seller    uuid.UUID
	warehouse uuid.UUID
	store     *files.MemStore
	fetches   int
	im        *nhanhProductImporter
}

func newNhanhImportEnv(t *testing.T) *nhanhImportEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &nhanhImportEnv{t: t, s: s, tenant: s.Tenant(t, "ELIMAZ"), store: files.NewMemStore()}
	s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.seller, _ = catalogtest.Sellers(t, tx, e.tenant)
		var err error
		e.warehouse, err = inventory.EnsureDefaultWarehouse(t.Context(), tx, e.tenant, e.seller)
		return err //nolint:wrapcheck // test
	})
	e.im = &nhanhProductImporter{
		DB: s.App, Tenant: e.tenant, Files: e.store, Queue: queue.NewQueue(),
		Writer: catalog.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()},
		Fetch: func(_ context.Context, _ string) ([]byte, string, error) {
			e.fetches++
			return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...), "image/jpeg", nil
		},
		// Giả worker của CRM: đặt tệp ảnh vừa tải lên thành ready (đường xử lý ảnh thật có test riêng ở gói files).
		WaitImage: func(ctx context.Context, id uuid.UUID) error {
			sum := make([]byte, 32)
			_, _ = rand.Read(sum)
			return tenancy.WithTenant(ctx, s.Owner, e.tenant, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE platform.files SET status = 'ready', detected_type = 'image/jpeg',
					width = 800, height = 800, content_sha256 = $2, preset_version = 1001 WHERE id = $1`, id, sum)
				return err //nolint:wrapcheck // test
			})
		},
	}
	return e
}

// applyLikeStaff tạo trực tiếp mẫu và biến thể mà nhân viên sẽ có sau khi áp bản nhập (đường áp thật có test ở catalog).
func (e *nhanhImportEnv) applyLikeStaff(withSecond bool) {
	e.t.Helper()
	e.s.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		p := catalogtest.Product(e.t, tx, e.tenant, e.seller, "TEST-AO")
		for _, v := range [][3]string{{"TEST-AO-D-M", "Đỏ", "M"}, {"TEST-AO-X-L", "Xanh", "L"}} {
			if _, err := tx.Exec(e.t.Context(), `INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color,
				size, price) VALUES ($1, $2, $3, $4, $4, $5, $6, 330000)`, e.tenant, e.seller, p, v[0], v[1], v[2]); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		if withSecond {
			q := catalogtest.Product(e.t, tx, e.tenant, e.seller, "TEST-DON")
			_, err := tx.Exec(e.t.Context(), `INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color,
				size, price) VALUES ($1, $2, $3, 'TEST-DON', 'TEST-DON', 'Mặc định', 'Free', 100000)`, e.tenant, e.seller, q)
			return err //nolint:wrapcheck // test
		}
		return nil
	})
}

func (e *nhanhImportEnv) drafts(only []int64) []nhanh.Draft {
	e.t.Helper()
	picked, err := nhanh.Select(nhanhFakeSnapshot().Products, only, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return nhanh.BuildDrafts(picked)
}

func (e *nhanhImportEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.s.CountIn(e.t, e.tenant, q, args...)
}

// prepare: tạo đúng một tệp Excel (mục đích catalog_import, riêng tư) và một bản nhập validating; chưa tạo mẫu nào
// (nhân viên áp trên màn Nhập Excel); danh mục chưa ánh xạ được đếm; chưa có ảnh nào bị tải.
func TestNhanhImportProducts_PrepareCreatesImportOnly(t *testing.T) {
	t.Parallel()
	e := newNhanhImportEnv(t)
	e.s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO catalog.categories (tenant_id, name, slug, depth)
			VALUES (current_setting('app.tenant_id')::uuid, 'Áo', 'ao', 1)`)
		return err //nolint:wrapcheck // test
	})
	rep, err := e.im.prepare(t.Context(), e.drafts(nil), map[int64][]string{55: {"Áo"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Models != 2 || rep.Variants != 3 || rep.MissingCategories != 0 || rep.ImportID == uuid.Nil {
		t.Errorf("báo cáo = %+v", rep)
	}
	if n := e.count(`SELECT count(*) FROM platform.files WHERE purpose = 'catalog_import' AND status = 'uploaded'`); n != 1 {
		t.Errorf("tệp catalog_import = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM catalog.imports WHERE id = $1 AND status = 'validating'`, rep.ImportID); n != 1 {
		t.Errorf("bản nhập validating = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM catalog.products`); n != 0 {
		t.Errorf("prepare đã tạo %d mẫu; chỉ nhân viên áp mới tạo", n)
	}
	if e.fetches != 0 {
		t.Errorf("prepare tải %d ảnh, muốn 0", e.fetches)
	}
}

// finish: ghi mã cũ, gắn ảnh đúng màu (ảnh chung không màu), ghi tồn dự kiến; SKU chưa có thành dòng lỗi mà phần khác vẫn
// chạy; chạy lại không tải, không gắn, không ghi thêm.
func TestNhanhImportProducts_FinishLinksImagesStockAndIsRerunnable(t *testing.T) {
	t.Parallel()
	e := newNhanhImportEnv(t)
	e.applyLikeStaff(false)
	taken := nhanhFakeSnapshot().TakenAt
	rep, err := e.im.finish(t.Context(), e.drafts(nil), taken)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ImagesAttached != 3 || rep.ImagesFailed != 0 || rep.VariantsMissing != 1 || rep.RefsNew != 3 || rep.Expected != 2 {
		t.Errorf("báo cáo = %+v", rep)
	}
	if len(rep.Rows) != 1 || rep.Rows[0] != (nhanhProblemRow{Kind: "variant", ID: 200, Code: "sku_missing"}) {
		t.Errorf("dòng lỗi = %+v, muốn một dòng sku_missing của biến thể 200", rep.Rows)
	}
	for color, want := range map[string]int{"Đỏ": 1, "Xanh": 1} {
		if n := e.count(`SELECT count(*) FROM catalog.product_images WHERE color = $1 AND deleted_at IS NULL`, color); n != want {
			t.Errorf("ảnh màu %s = %d, muốn %d", color, n, want)
		}
	}
	if n := e.count(`SELECT count(*) FROM catalog.product_images WHERE color IS NULL AND deleted_at IS NULL`); n != 1 {
		t.Errorf("ảnh chung = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM catalog.legacy_refs WHERE kind = 'image'`); n != 3 {
		t.Errorf("mã cũ ảnh = %d, muốn 3", n)
	}
	if n := e.count(`SELECT count(*) FROM catalog.legacy_refs WHERE kind = 'variant' AND legacy_id IN ('101','102')`); n != 2 {
		t.Errorf("mã cũ biến thể = %d, muốn 2", n)
	}
	// Tồn dự kiến: 7 và 3 (cho 2 biến thể có trong CRM), KHÔNG có sổ tồn.
	if n := e.count(`SELECT count(*) FROM inventory.opening_expected e JOIN catalog.variants v ON v.id = e.variant_id
		WHERE (v.sku = 'TEST-AO-D-M' AND e.expected = 7) OR (v.sku = 'TEST-AO-X-L' AND e.expected = 3)`); n != 2 {
		t.Errorf("số dự kiến đúng = %d, muốn 2", n)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger`) + e.count(`SELECT count(*) FROM inventory.stock_balances`); n != 0 {
		t.Errorf("tồn dự kiến làm đổi sổ hay số dư tồn (%d dòng)", n)
	}
	fetched := e.fetches
	if fetched != 3 {
		t.Errorf("tải %d ảnh, muốn 3", fetched)
	}
	// Chạy lại: không tải, không gắn, không mã cũ mới, số dòng giữ nguyên.
	again, err := e.im.finish(t.Context(), e.drafts(nil), taken)
	if err != nil {
		t.Fatal(err)
	}
	if e.fetches != fetched || again.ImagesAttached != 0 || again.ImagesSkipped != 3 || again.RefsNew != 0 {
		t.Errorf("chạy lại: tải %d→%d, báo cáo %+v", fetched, e.fetches, again)
	}
	if n := e.count(`SELECT count(*) FROM catalog.product_images`); n != 3 {
		t.Errorf("sau khi chạy lại có %d ảnh, muốn 3", n)
	}
	if n := e.count(`SELECT count(*) FROM inventory.opening_expected`); n != 2 {
		t.Errorf("sau khi chạy lại có %d dòng dự kiến, muốn 2", n)
	}
}

// Ảnh theo màu mà mẫu CRM không có màu đó vẫn gắn được, không màu (không vỡ cả mẫu).
func TestNhanhImportProducts_UnknownColorImageStillAttached(t *testing.T) {
	t.Parallel()
	e := newNhanhImportEnv(t)
	e.s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		p := catalogtest.Product(e.t, tx, e.tenant, e.seller, "TEST-AO")
		_, err := tx.Exec(e.t.Context(), `INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price)
			VALUES ($1, $2, $3, 'TEST-AO-D-M', 'TEST-AO-D-M', 'Hồng', 'M', 1)`, e.tenant, e.seller, p)
		return err //nolint:wrapcheck // test
	})
	rep, err := e.im.finish(t.Context(), e.drafts([]int64{100}), nhanhFakeSnapshot().TakenAt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ImagesFailed != 0 || e.count(`SELECT count(*) FROM catalog.product_images WHERE color IS NULL`) != 3 {
		t.Errorf("báo cáo = %+v", rep)
	}
}

// Lỗi tải ảnh chỉ thành dòng lỗi (mã, không có địa chỉ); ảnh khác và tồn dự kiến vẫn chạy.
func TestNhanhImportProducts_ImageFailureIsRowNotAbort(t *testing.T) {
	t.Parallel()
	e := newNhanhImportEnv(t)
	e.applyLikeStaff(true)
	e.im.Fetch = func(_ context.Context, u string) ([]byte, string, error) {
		if strings.Contains(u, "xanh") {
			return nil, "", nhanh.ErrImageTooLarge
		}
		return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...), "image/jpeg", nil
	}
	rep, err := e.im.finish(t.Context(), e.drafts(nil), nhanhFakeSnapshot().TakenAt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ImagesFailed != 1 || rep.ImagesAttached != 2 || rep.Expected != 3 || rep.VariantsMissing != 0 {
		t.Errorf("báo cáo = %+v", rep)
	}
	for _, r := range rep.Rows {
		if r.Code != "image_fetch" || r.ID != 100 {
			t.Errorf("dòng lỗi lạ: %+v", r)
		}
	}
	// Chạy lại sau khi sửa lỗi: chỉ ảnh lỗi được tải.
	e.im.Fetch = func(_ context.Context, _ string) ([]byte, string, error) {
		e.fetches++
		return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...), "image/jpeg", nil
	}
	if rep, err = e.im.finish(t.Context(), e.drafts(nil), nhanhFakeSnapshot().TakenAt); err != nil || e.fetches != 1 || rep.ImagesAttached != 1 {
		t.Errorf("chạy lại: tải %d, %+v, %v", e.fetches, rep, err)
	}
}

// Tenant khác không thấy mã cũ, ảnh hay số dự kiến.
func TestNhanhImportProducts_OtherTenantSeesNothing(t *testing.T) {
	t.Parallel()
	e := newNhanhImportEnv(t)
	other := e.s.Tenant(t, "KHAC")
	e.applyLikeStaff(false)
	if _, err := e.im.finish(t.Context(), e.drafts([]int64{100}), nhanhFakeSnapshot().TakenAt); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"catalog.legacy_refs", "inventory.opening_expected", "catalog.product_images"} {
		if n := e.s.CountIn(t, other, "SELECT count(*) FROM "+q); n != 0 {
			t.Errorf("tenant khác thấy %d dòng của %s", n, q)
		}
	}
}

// Cờ: thiếu bước, sai bước, tenant không phải UUID, mã sản phẩm sai → lỗi cách gọi (không đụng database).
func TestNhanhImportProducts_FlagsAndDryRun(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "" }
	tenant := uuid.NewString()
	for name, args := range map[string][]string{
		"không bước":    {},
		"bước lạ":       {"run", "--tenant", tenant},
		"thiếu tenant":  {"prepare"},
		"tenant sai":    {"prepare", "--tenant", "abc"},
		"kho sai":       {"finish", "--tenant", tenant, "--warehouse", "abc"},
		"mã sai":        {"prepare", "--tenant", tenant, "--only", "1,x"},
		"cờ thừa":       {"prepare", "--tenant", tenant, "thừa"},
		"chờ ảnh không": {"finish", "--tenant", tenant, "--image-wait", "0s"},
	} {
		if _, err := parseNhanhImportFlags(args, getenv); !errors.Is(err, errNhanhImportUsage) {
			t.Errorf("%s: err = %v, muốn errNhanhImportUsage", name, err)
		}
	}
	fl, err := parseNhanhImportFlags([]string{"prepare", "--tenant", tenant}, getenv)
	if err != nil || fl.Dir != defaultNhanhDir || fl.Step != "prepare" {
		t.Errorf("mặc định = %+v, %v", fl, err)
	}

	dir := t.TempDir()
	if _, err := nhanh.WriteSnapshot(dir, nhanhFakeSnapshot()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	run := func(extra ...string) error {
		out.Reset()
		return runNhanhImportProducts(t.Context(), config.Config{}, append([]string{"prepare", "--dry-run", "--tenant", tenant, "--dir", dir}, extra...),
			getenv, &out, slog.New(slog.DiscardHandler))
	}
	if err := run(); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "mẫu 2, biến thể 3, ảnh 3, tồn dự kiến 3") {
		t.Errorf("dry-run in %q", got)
	}
	if err := run("--remove", "200"); err != nil || !strings.Contains(out.String(), "mẫu 1, biến thể 2") {
		t.Errorf("--remove 200: %v, %q", err, out.String())
	}
	if err := run("--only", "999"); !errors.Is(err, nhanh.ErrSelection) || nhanhImportExit(err, &bytes.Buffer{}) != exitConfig {
		t.Errorf("--only 999: %v", err)
	}
	if err := run("--dir", t.TempDir()); !errors.Is(err, nhanh.ErrNoSnapshot) {
		t.Errorf("thư mục rỗng: %v", err)
	}
	// Không --dry-run mà không có kho tệp: báo trước khi mở database.
	err = runNhanhImportProducts(t.Context(), config.Config{}, []string{"finish", "--tenant", tenant, "--dir", dir}, getenv, &out, slog.New(slog.DiscardHandler))
	if !errors.Is(err, errNhanhImportNoFiles) {
		t.Errorf("không kho tệp: %v", err)
	}
	// Ánh xạ danh mục: tệp JSON.
	mapFile := filepath.Join(t.TempDir(), "cat.json")
	if err := os.WriteFile(mapFile, []byte(`{"55":"Áo > Áo sơ mi"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if m, err := readCategoryMap(mapFile); err != nil || len(m[55]) != 2 || m[55][1] != "Áo sơ mi" {
		t.Errorf("ánh xạ = %v, %v", m, err)
	}
}
