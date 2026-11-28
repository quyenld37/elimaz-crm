package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/nhanh"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// nhanhImportProductsCommand là lệnh vận hành `crm nhanh-import-products prepare|finish` (kế hoạch M7, Task 541 = M7-41;
// ADR-48): chuyển tối đa 50 mẫu sản phẩm mới và tồn dự kiến từ bản chụp của `crm nhanh-snapshot` (Task 540). Không gọi
// Nhanh (chỉ đọc tệp bản chụp trên đĩa và tải ảnh công khai về), không đụng đơn, khách, giá hay tồn thật.
//
//   - prepare: sinh tệp Excel đúng mẫu nhập của catalog từ bản chụp, tải lên kho tệp riêng tư và tạo bản nhập
//     (CreateImport). Nhân viên mở màn Nhập Excel sẵn có, xem kết quả kiểm rồi bấm áp; mẫu mới là draft (ẩn với khách).
//   - finish (sau khi bản nhập đã áp): ghi mã cũ của mẫu, biến thể; tải ảnh về kho tệp rồi gắn vào mẫu đúng màu; ghi tồn
//     Nhanh làm số dự kiến của phiên kiểm kho mở sổ. Chạy lại không trùng (mã cũ là khóa).
const nhanhImportProductsCommand = "nhanh-import-products"

const (
	stepPrepare = "prepare"
	stepFinish  = "finish"
)

var (
	errNhanhImportUsage = errors.New("cách gọi: `crm nhanh-import-products prepare|finish --tenant <mã tenant (UUID)> " +
		"[--dry-run] [--snapshot <tệp> | --dir <thư mục bản chụp>] [--only 1,2,3] [--remove 4,5] " +
		"[--category-map <tệp JSON {mã danh mục Nhanh: \"Áo > Áo sơ mi\"}>] [--warehouse <mã kho (UUID)>] [--image-wait 3m]`")
	errNhanhImportNoFiles = errors.New("không --dry-run thì cần kho tệp (S3_*) và worker xử lý ảnh: chạy lệnh trong container " +
		"worker (CRM_ROLE=worker)")
	errNhanhImportIncomplete = errors.New("có dòng lỗi, xem bảng")
)

func isNhanhImportProductsCommand(args []string) bool {
	return len(args) > 0 && args[0] == nhanhImportProductsCommand
}

type nhanhImportFlags struct {
	Step                  string
	Tenant, Warehouse     uuid.UUID
	Dir, Snapshot, CatMap string
	Only, Remove          []int64
	DryRun                bool
	ImageWait             time.Duration
}

func parseNhanhImportFlags(args []string, getenv func(string) string) (nhanhImportFlags, error) {
	fs := flag.NewFlagSet(nhanhImportProductsCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tenant := fs.String("tenant", "", "")
	wh := fs.String("warehouse", "", "")
	dir := fs.String("dir", "", "")
	snap := fs.String("snapshot", "", "")
	catMap := fs.String("category-map", "", "")
	only := fs.String("only", "", "")
	remove := fs.String("remove", "", "")
	dry := fs.Bool("dry-run", false, "")
	wait := fs.Duration("image-wait", defaultImageWait, "")
	if len(args) < 1 || (args[0] != stepPrepare && args[0] != stepFinish) {
		return nhanhImportFlags{}, errNhanhImportUsage
	}
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *wait <= 0 {
		return nhanhImportFlags{}, errNhanhImportUsage
	}
	fl := nhanhImportFlags{Step: args[0], Dir: *dir, Snapshot: *snap, CatMap: *catMap, DryRun: *dry, ImageWait: *wait}
	var err error
	if fl.Tenant, err = uuid.Parse(*tenant); err != nil {
		return nhanhImportFlags{}, errNhanhImportUsage
	}
	if *wh != "" {
		if fl.Warehouse, err = uuid.Parse(*wh); err != nil {
			return nhanhImportFlags{}, errNhanhImportUsage
		}
	}
	if fl.Only, err = parseIDs(*only); err != nil {
		return nhanhImportFlags{}, errNhanhImportUsage
	}
	if fl.Remove, err = parseIDs(*remove); err != nil {
		return nhanhImportFlags{}, errNhanhImportUsage
	}
	if fl.Dir == "" {
		if fl.Dir = getenv(envNhanhDir); fl.Dir == "" {
			fl.Dir = defaultNhanhDir
		}
	}
	return fl, nil
}

// nhanhImportExit in lỗi ra stderr và trả mã thoát: sai cách gọi, danh sách chọn sai, chưa có bản chụp → exitConfig.
func nhanhImportExit(err error, stderr io.Writer) int {
	if err == nil {
		return exitOK
	}
	_, _ = fmt.Fprintln(stderr, nhanhImportProductsCommand+":", err)
	for _, cfgErr := range []error{
		errNhanhImportUsage, errNhanhImportNoFiles, errImportTenant, nhanh.ErrSelection,
		nhanh.ErrNoSnapshot, nhanh.ErrSnapshotVersion,
	} {
		if errors.Is(err, cfgErr) {
			return exitConfig
		}
	}
	return exitFail
}

// nhanhProblemRow là một dòng của bảng kết quả: loại việc, mã Nhanh và mã lỗi; không có giá trị nào của sản phẩm.
type nhanhProblemRow struct {
	Kind string
	ID   int64
	Code string
}

// nhanhImportReport là kết quả in ra: chỉ số đếm và dòng lỗi.
type nhanhImportReport struct {
	Step                                        string
	Models, Variants, Images, Expected          int
	MissingCategories                           int
	ImportID                                    uuid.UUID
	ImagesAttached, ImagesSkipped, ImagesFailed int
	VariantsMissing, RefsNew                    int
	Gaps                                        map[string]int
	Rows                                        []nhanhProblemRow
}

func (r *nhanhImportReport) fail(kind string, id int64, code string) {
	r.Rows = append(r.Rows, nhanhProblemRow{Kind: kind, ID: id, Code: code})
}

func (r *nhanhImportReport) print(w io.Writer) {
	_, _ = fmt.Fprintf(w, "nhanh-import-products %s: mẫu %d, biến thể %d, ảnh %d, tồn dự kiến %d\n", r.Step, r.Models, r.Variants, r.Images, r.Expected)
	if len(r.Gaps) > 0 {
		keys := make([]string, 0, len(r.Gaps))
		for k := range r.Gaps {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+" "+strconv.Itoa(r.Gaps[k]))
		}
		_, _ = fmt.Fprintf(w, "trường thiếu hay đã sửa (số mẫu/biến thể): %s\n", strings.Join(parts, ", "))
	}
	switch r.Step {
	case stepPrepare:
		_, _ = fmt.Fprintf(w, "bản nhập: %s; danh mục chưa ánh xạ: %d\n", r.ImportID, r.MissingCategories)
	case stepFinish:
		_, _ = fmt.Fprintf(w, "mã cũ mới: %d; SKU chưa có trong CRM: %d; ảnh gắn: %d, đã có: %d, lỗi: %d\n",
			r.RefsNew, r.VariantsMissing, r.ImagesAttached, r.ImagesSkipped, r.ImagesFailed)
	}
	for _, row := range r.Rows {
		_, _ = fmt.Fprintf(w, "LỖI %s %d %s\n", row.Kind, row.ID, row.Code)
	}
}

// nhanhProductImporter làm hai bước của lệnh trên một tenant. Fetch tải ảnh; WaitImage chờ worker xử lý ảnh (test thay).
type nhanhProductImporter struct {
	DB        *db.DB
	Tenant    uuid.UUID
	Files     files.FileStore
	Queue     queue.JobQueue
	Writer    catalog.Writer
	Fetch     func(ctx context.Context, url string) ([]byte, string, error)
	WaitImage func(ctx context.Context, fileID uuid.UUID) error
	// Warehouse: kho nhận số dự kiến; uuid.Nil là kho mặc định của tenant.
	Warehouse uuid.UUID
}

func (im *nhanhProductImporter) actor() identity.AuthUser {
	return identity.AuthUser{TenantID: im.Tenant, UserID: importSystemID()}
}

func newNhanhReport(step string, drafts []nhanh.Draft) *nhanhImportReport {
	r := &nhanhImportReport{Step: step, Models: len(drafts), Gaps: map[string]int{}}
	for _, d := range drafts {
		r.Variants += len(d.Variants)
		r.Images += len(d.Images)
		for _, v := range d.Variants {
			if v.Stock != nil {
				r.Expected++
			}
		}
		seen := map[string]bool{}
		for _, p := range d.Problems {
			if !seen[p.Field] {
				seen[p.Field] = true
				r.Gaps[p.Field]++
			}
		}
	}
	return r
}

// prepare sinh tệp Excel từ drafts, tải lên và tạo bản nhập (nhân viên áp trên màn Nhập Excel). catMap: mã danh mục
// Nhanh → đường danh mục CRM.
func (im *nhanhProductImporter) prepare(ctx context.Context, drafts []nhanh.Draft, catMap map[int64][]string) (*nhanhImportReport, error) {
	rep := newNhanhReport(stepPrepare, drafts)
	products := make([]catalog.LegacyProduct, 0, len(drafts))
	for _, d := range drafts {
		p := catalog.LegacyProduct{Code: d.Code, Name: d.Name, Description: d.Description, CategoryPath: catMap[d.CategoryLegacyID]}
		for _, v := range d.Variants {
			p.Variants = append(p.Variants, catalog.LegacyVariant{
				SKU: v.SKU, Barcode: v.Barcode, Color: v.Color, Size: v.Size, Price: v.Price, CostPrice: v.CostPrice,
			})
		}
		products = append(products, p)
	}
	var data []byte
	var missing []string
	var seller uuid.UUID
	if err := tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
		var err error
		if seller, err = catalog.DefaultSeller(ctx, tx); err != nil {
			return err //nolint:wrapcheck // catalog đã bọc
		}
		data, missing, err = catalog.LegacyWorkbook(ctx, tx, products)
		return err //nolint:wrapcheck // catalog đã bọc
	}); err != nil {
		return nil, fmt.Errorf("sinh tệp nhập: %w", err)
	}
	rep.MissingCategories = len(missing)
	fileID, err := uploadFile(ctx, im.DB, im.Files, im.Queue, im.Tenant, "catalog_import", "chuyen-tu-nhanh.xlsx", xlsxContentType, data)
	if err != nil {
		return nil, err
	}
	if err := tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
		imp, err := im.Writer.CreateImport(ctx, tx, seller, fileID, im.actor())
		rep.ImportID = imp.ID // gán lại mỗi lượt (WithTenant có thể chạy lại thân hàm)
		return err            //nolint:wrapcheck // catalog đã bọc
	}); err != nil {
		return nil, fmt.Errorf("tạo bản nhập: %w", err)
	}
	return rep, nil
}

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// finish chạy sau khi bản nhập đã áp: ghi mã cũ, gắn ảnh, ghi tồn dự kiến. Lỗi của một mẫu hay ảnh chỉ thành dòng lỗi
// (phần còn lại vẫn chạy); lỗi trả về là lỗi hạ tầng (database).
func (im *nhanhProductImporter) finish(ctx context.Context, drafts []nhanh.Draft, takenAt time.Time) (*nhanhImportReport, error) {
	rep := newNhanhReport(stepFinish, drafts)
	var seller, warehouse uuid.UUID
	var skus []string
	for _, d := range drafts {
		for _, v := range d.Variants {
			skus = append(skus, v.SKU)
		}
	}
	var found map[string]catalog.SKUVariant
	if err := tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
		var err error
		if seller, err = catalog.DefaultSeller(ctx, tx); err != nil {
			return err //nolint:wrapcheck // catalog đã bọc
		}
		if warehouse = im.Warehouse; warehouse == uuid.Nil {
			if warehouse, err = inventory.DefaultWarehouse(ctx, tx); err != nil {
				return err //nolint:wrapcheck // inventory đã bọc
			}
		}
		found, err = catalog.VariantsBySKU(ctx, tx, seller, skus)
		return err //nolint:wrapcheck // catalog đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc biến thể theo SKU: %w", err)
	}
	var expected []inventory.ExpectedStock
	for _, d := range drafts {
		prod, vars := im.matchDraft(d, found, rep)
		if len(vars) == 0 {
			continue
		}
		refs := []catalog.LegacyRef{{Kind: catalog.LegacyKindProduct, LegacyID: strconv.FormatInt(d.LegacyID, 10), EntityID: prod}}
		for _, dv := range d.Variants {
			if sv, ok := found[strings.ToUpper(dv.SKU)]; ok {
				refs = append(refs, catalog.LegacyRef{Kind: catalog.LegacyKindVariant, LegacyID: strconv.FormatInt(dv.LegacyID, 10), EntityID: sv.ID})
				if dv.Stock != nil {
					expected = append(expected, inventory.ExpectedStock{VariantID: sv.ID, SellerID: seller, Expected: *dv.Stock})
				}
			}
		}
		n, err := im.recordRefs(ctx, refs)
		if err != nil {
			return nil, err
		}
		rep.RefsNew += n
		im.attachImages(ctx, d, prod, seller, vars, rep)
	}
	rep.Expected = len(expected)
	if err := tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
		_, err := inventory.SetOpeningExpected(ctx, tx, im.Tenant, warehouse, takenAt, expected)
		return err //nolint:wrapcheck // inventory đã bọc
	}); err != nil {
		return nil, fmt.Errorf("ghi tồn dự kiến: %w", err)
	}
	return rep, nil
}

// matchDraft trả mã mẫu CRM (của biến thể đầu tiên tìm được) và các biến thể CRM của d; SKU chưa có thành dòng lỗi.
func (im *nhanhProductImporter) matchDraft(d nhanh.Draft, found map[string]catalog.SKUVariant, rep *nhanhImportReport) (uuid.UUID, []catalog.SKUVariant) {
	var prod uuid.UUID
	var vars []catalog.SKUVariant
	for _, dv := range d.Variants {
		sv, ok := found[strings.ToUpper(dv.SKU)]
		if !ok {
			rep.VariantsMissing++
			rep.fail("variant", dv.LegacyID, "sku_missing")
			continue
		}
		if prod == uuid.Nil {
			prod = sv.ProductID
		}
		vars = append(vars, sv)
	}
	return prod, vars
}

func (im *nhanhProductImporter) recordRefs(ctx context.Context, refs []catalog.LegacyRef) (int, error) {
	n := 0
	if err := tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
		var err error
		n, err = catalog.RecordLegacyRefs(ctx, tx, im.Tenant, refs) // gán lại mỗi lượt
		return err                                                  //nolint:wrapcheck // catalog đã bọc
	}); err != nil {
		return 0, fmt.Errorf("ghi mã cũ: %w", err)
	}
	return n, nil
}

// imageRefID là mã cũ của một ảnh: băm địa chỉ (địa chỉ có thể mang mã truy cập, không lưu nguyên).
func imageRefID(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

// attachImages tải, tải lên và gắn từng ảnh của d chưa có mã cũ vào mẫu prod. Ảnh theo màu chỉ gắn màu khi mẫu có
// biến thể màu đó. Lỗi một ảnh thành dòng lỗi, không dừng.
func (im *nhanhProductImporter) attachImages(ctx context.Context, d nhanh.Draft, prod, seller uuid.UUID, vars []catalog.SKUVariant, rep *nhanhImportReport) {
	ids := make([]string, 0, len(d.Images))
	for _, img := range d.Images {
		ids = append(ids, imageRefID(img.URL))
	}
	var have map[string]uuid.UUID
	if err := tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
		var err error
		have, err = catalog.LegacyRefs(ctx, tx, catalog.LegacyKindImage, ids)
		return err //nolint:wrapcheck // catalog đã bọc
	}); err != nil {
		rep.ImagesFailed += len(d.Images)
		rep.fail("image", d.LegacyID, "refs_read")
		return
	}
	for i, img := range d.Images {
		if _, done := have[ids[i]]; done {
			rep.ImagesSkipped++
			continue
		}
		var color *string
		if img.Color != "" && slices.ContainsFunc(vars, func(v catalog.SKUVariant) bool { return v.Color == img.Color }) {
			color = &img.Color
		}
		if code := im.attachOne(ctx, img.URL, ids[i], prod, seller, color); code != "" {
			rep.ImagesFailed++
			rep.fail("image", d.LegacyID, code)
			continue
		}
		rep.ImagesAttached++
	}
}

// attachOne tải một ảnh, đưa vào kho tệp, chờ xử lý, gắn vào mẫu và ghi mã cũ trong cùng transaction; trả mã lỗi
// ("" là xong). Mã lỗi không chứa địa chỉ.
func (im *nhanhProductImporter) attachOne(ctx context.Context, url, refID string, prod, seller uuid.UUID, color *string) string {
	data, ct, err := im.Fetch(ctx, url)
	if err != nil {
		return "image_fetch"
	}
	fileID, err := uploadFile(ctx, im.DB, im.Files, im.Queue, im.Tenant, "product_image", "nhanh-"+refID[:12], ct, data)
	if err != nil {
		return "image_upload"
	}
	if err := im.WaitImage(ctx, fileID); err != nil {
		if errors.Is(err, errImageRejected) {
			return "image_rejected"
		}
		return "image_wait"
	}
	err = tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
		p, err := catalog.GetProduct(ctx, tx, seller, prod)
		if err != nil {
			return err //nolint:wrapcheck // catalog đã bọc
		}
		if _, err := im.Writer.AttachImage(ctx, tx, seller, prod, p.Version, fileID, color, im.actor()); err != nil {
			return err //nolint:wrapcheck // catalog đã bọc
		}
		_, err = catalog.RecordLegacyRefs(ctx, tx, im.Tenant, []catalog.LegacyRef{{Kind: catalog.LegacyKindImage, LegacyID: refID, EntityID: fileID}})
		return err //nolint:wrapcheck // catalog đã bọc
	})
	if err != nil {
		return "image_attach"
	}
	return ""
}

// loadNhanhDrafts đọc bản chụp, chọn mẫu và đổi sang bản nháp; trả cả giờ chụp.
func loadNhanhDrafts(fl nhanhImportFlags) ([]nhanh.Draft, time.Time, error) {
	path := fl.Snapshot
	if path == "" {
		var err error
		if path, err = nhanh.LatestSnapshot(fl.Dir); err != nil {
			return nil, time.Time{}, err //nolint:wrapcheck // adapter đã bọc
		}
	}
	snap, err := nhanh.ReadSnapshot(path)
	if err != nil {
		return nil, time.Time{}, err //nolint:wrapcheck // adapter đã bọc
	}
	picked, err := nhanh.Select(snap.Products, fl.Only, fl.Remove)
	if err != nil {
		return nil, time.Time{}, err //nolint:wrapcheck // adapter đã bọc
	}
	return nhanh.BuildDrafts(picked), snap.TakenAt, nil
}

// readCategoryMap đọc tệp ánh xạ {"mã danh mục Nhanh": "Áo > Áo sơ mi"}; path rỗng: không ánh xạ.
func readCategoryMap(path string) (map[int64][]string, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: đường dẫn do người chạy lệnh trên server đưa
	if err != nil {
		return nil, fmt.Errorf("đọc ánh xạ danh mục: %w", err)
	}
	var raw map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("giải mã ánh xạ danh mục: %w", err)
	}
	out := make(map[int64][]string, len(raw))
	for k, v := range raw {
		id, err := strconv.ParseInt(strings.TrimSpace(k), 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("mã danh mục %q: %w", k, errNhanhImportUsage)
		}
		for part := range strings.SplitSeq(v, ">") {
			out[id] = append(out[id], strings.TrimSpace(part))
		}
	}
	return out, nil
}

// runNhanhImportProducts chạy lệnh: kiểm cờ, đọc bản chụp và chọn mẫu trước khi mở database; --dry-run chỉ in số đếm.
func runNhanhImportProducts(ctx context.Context, cfg config.Config, args []string, getenv func(string) string,
	stdout io.Writer, logger *slog.Logger,
) error {
	fl, err := parseNhanhImportFlags(args, getenv)
	if err != nil {
		return err
	}
	drafts, takenAt, err := loadNhanhDrafts(fl)
	if err != nil {
		return err
	}
	catMap, err := readCategoryMap(fl.CatMap)
	if err != nil {
		return err
	}
	if fl.DryRun {
		newNhanhReport(fl.Step, drafts).print(stdout)
		return nil
	}
	if cfg.S3Endpoint == "" {
		return errNhanhImportNoFiles
	}
	d, err := openCommandDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer d.Pool.Close()
	tenants, err := tenancy.ListTenantIDs(ctx, d)
	if err != nil {
		return err //nolint:wrapcheck // tenancy đã bọc
	}
	if !slices.Contains(tenants, fl.Tenant) {
		return fmt.Errorf("%s: %w", fl.Tenant, errImportTenant)
	}
	store, err := files.NewS3Store(s3Config(cfg))
	if err != nil {
		return fmt.Errorf("dựng kho tệp: %w", err)
	}
	hub := realtime.NewPGHub(realtime.PGHubOptions{URL: cfg.DatabaseURL, Logger: logger})
	mods, err := newModules(cfg, d, hub, metrics.NewRegistry(), store)
	if err != nil {
		return err
	}
	fetcher := nhanh.ImageFetcher{}
	im := &nhanhProductImporter{
		DB: d, Tenant: fl.Tenant, Files: store, Queue: queue.NewQueue(), Warehouse: fl.Warehouse, Fetch: fetcher.Fetch,
		Writer: catalog.Writer{Events: mods.Events, Queue: queue.NewQueue(), Realtime: hub},
		WaitImage: func(ctx context.Context, id uuid.UUID) error {
			return waitFileReady(ctx, d, fl.Tenant, id, fl.ImageWait)
		},
	}
	var rep *nhanhImportReport
	if fl.Step == stepPrepare {
		rep, err = im.prepare(ctx, drafts, catMap)
	} else {
		rep, err = im.finish(ctx, drafts, takenAt)
	}
	if err != nil {
		return err
	}
	rep.print(stdout)
	logger.Info("nhanh-import-products xong", slog.String("tenant_id", fl.Tenant.String()), slog.String("step", fl.Step),
		slog.Int("models", rep.Models), slog.Int("errors", len(rep.Rows)))
	if len(rep.Rows) > 0 {
		return fmt.Errorf("%d dòng lỗi: %w", len(rep.Rows), errNhanhImportIncomplete)
	}
	return nil
}
