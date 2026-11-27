package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// importWebsiteCommand là lệnh vận hành `crm import-website-data --dir <thư mục xuất> --tenant <mã tenant>
// --dry-run|--apply` (kế hoạch M5 Task 467, M5-55; RUNBOOK mục "Nhập dữ liệu website đêm mở"): nhập dữ liệu admin cũ
// của website (thư mục `admin-export-<giờ>/` do `scripts/export-admin-data-for-crm.ts` của website xuất, Task 466,
// data.json version 1) vào CRM qua public API của module sở hữu. Không đọc Nhanh, không gọi website. Sinh nhật không
// nhập ở đây (website `scripts/import-birthdays-to-crm.ts`, điều phối M5 27/11); unmatched.json chỉ được đếm.
const importWebsiteCommand = "import-website-data"

// importWebsiteVersion là version của data.json mà lệnh đọc được.
const importWebsiteVersion = 1

// defaultImageWait là thời gian chờ tối đa một ảnh banner được worker của CRM xử lý xong (ready).
const defaultImageWait = 3 * time.Minute

var (
	errImportWebsiteUsage = errors.New("cách gọi: `crm import-website-data --dir <thư mục admin-export-…> " +
		"--tenant <mã tenant (UUID)> --dry-run|--apply [--image-wait 3m]`")
	// errImportVersion: data.json có version khác importWebsiteVersion (không ghi gì).
	errImportVersion = errors.New("data.json khác phiên bản lệnh đọc được")
	// errBadExport: thư mục xuất hay data.json không đúng định dạng (không ghi gì).
	errBadExport = errors.New("thư mục xuất sai")
	// errImportTenant: --tenant không phải tenant nào của CRM.
	errImportTenant = errors.New("không có tenant với mã này")
	// errImportNoFiles: --apply mà tiến trình không có cấu hình kho tệp (ảnh banner đi qua kho tệp và worker).
	errImportNoFiles = errors.New("--apply cần kho tệp (S3_*) và worker xử lý ảnh banner: chạy lệnh trong container " +
		"worker (CRM_ROLE=worker)")
	// errImportIncomplete: có bản ghi hay loại dữ liệu lỗi (phần còn lại đã nhập hay đã kiểm); xem bảng in ra.
	errImportIncomplete = errors.New("có bản ghi lỗi, xem bảng")
)

// wxExport là data.json version 1 (đặc tả: docs/tasks/task-466.md của apps/storefront-crm, buildExport của
// scripts/export-admin-data-for-crm.ts). Đọc chặt: trường lạ là lỗi.
type wxExport struct {
	Version          int            `json:"version"`
	ExportedAt       time.Time      `json:"exportedAt"`
	IDMapGeneratedAt *string        `json:"idMapGeneratedAt"`
	Campaigns        []wxCampaign   `json:"campaigns"`
	Banners          *wxBannerList  `json:"banners"`
	Looks            *wxLookList    `json:"looks"`
	Feedback         []wxFeedback   `json:"feedback"`
	Birthdays        wxBirthdays    `json:"birthdays"`
	GiftSettings     wxGiftSettings `json:"giftSettings"`
	CheckoutPause    wxPause        `json:"checkoutPause"`
	Carriers         wxCarriers     `json:"carriers"`
}

// wxCategory là danh mục của website: mã Nhanh và đường tên từ cấp đầu ("Áo", "Áo sơ mi").
type wxCategory struct {
	NhanhID int      `json:"nhanhId"`
	Path    []string `json:"path"`
}

type wxCampaign struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Scope       string       `json:"scope"`
	ProductIDs  []uuid.UUID  `json:"productIds"`
	Categories  []wxCategory `json:"categories"`
	Percent     int          `json:"percent"`
	StartsAt    time.Time    `json:"startsAt"`
	EndsAt      time.Time    `json:"endsAt"`
	AllowCoupon bool         `json:"allowCoupon"`
	Clearance   bool         `json:"clearance"`
	State       string       `json:"state"`
	Revision    int          `json:"revision"`
	CreatedAt   *time.Time   `json:"createdAt"`
	UpdatedAt   *time.Time   `json:"updatedAt"`
}

type wxBannerList struct {
	Revision int        `json:"revision"`
	Items    []wxBanner `json:"items"`
}

type wxPicture struct {
	File string `json:"file"`
}

type wxBannerImage struct {
	Desktop *wxPicture `json:"desktop"`
	Mobile  *wxPicture `json:"mobile"`
}

type wxLink struct {
	Kind       string      `json:"kind"`
	CampaignID *uuid.UUID  `json:"campaignId"`
	Category   *wxCategory `json:"category"`
	ProductID  *uuid.UUID  `json:"productId"`
}

type wxBanner struct {
	ID         uuid.UUID      `json:"id"`
	Position   int            `json:"position"`
	Enabled    bool           `json:"enabled"`
	Kind       string         `json:"kind"`
	CampaignID *uuid.UUID     `json:"campaignId"`
	Category   *wxCategory    `json:"category"`
	Kicker     string         `json:"kicker"`
	Title      string         `json:"title"`
	CTA        string         `json:"cta"`
	StartsAt   *time.Time     `json:"startsAt"`
	EndsAt     *time.Time     `json:"endsAt"`
	Theme      string         `json:"theme"`
	ProductIDs []uuid.UUID    `json:"productIds"`
	Image      *wxBannerImage `json:"image"`
	Alt        string         `json:"alt"`
	Link       *wxLink        `json:"link"`
}

type wxLookList struct {
	Revision int      `json:"revision"`
	Items    []wxLook `json:"items"`
}

type wxLookItem struct {
	ProductID uuid.UUID `json:"productId"`
	Color     string    `json:"color"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
}

// wxLookPhoto là ảnh người mẫu: ảnh thứ ImagePosition (0 = ảnh đại diện) của mẫu ProductID; VariantID khi là ảnh riêng
// của một SKU.
type wxLookPhoto struct {
	ProductID     uuid.UUID  `json:"productId"`
	VariantID     *uuid.UUID `json:"variantId"`
	ImagePosition int        `json:"imagePosition"`
}

type wxLook struct {
	ID          uuid.UUID    `json:"id"`
	Position    int          `json:"position"`
	Enabled     bool         `json:"enabled"`
	Name        string       `json:"name"`
	Title       string       `json:"title"`
	Headline    string       `json:"headline"`
	Story       string       `json:"story"`
	Items       []wxLookItem `json:"items"`
	Photo       wxLookPhoto  `json:"photo"`
	Categories  []wxCategory `json:"categories"`
	ModelHeight *int         `json:"modelHeight"`
	ModelSize   string       `json:"modelSize"`
	StartsAt    *time.Time   `json:"startsAt"`
	EndsAt      *time.Time   `json:"endsAt"`
}

type wxFeedback struct {
	ID        uuid.UUID `json:"id"`
	Topic     string    `json:"topic"`
	Mood      *int      `json:"mood"`
	Body      string    `json:"body"`
	Name      string    `json:"name"`
	Contact   string    `json:"contact"`
	OrderRef  string    `json:"orderRef"`
	Page      string    `json:"page"`
	Device    string    `json:"device"`
	State     string    `json:"state"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// wxBirthdays: hồ sơ không đọc (nhập bằng đường khác), chỉ đếm.
type wxBirthdays struct {
	DeclinedLeftOut int               `json:"declinedLeftOut"`
	Profiles        []json.RawMessage `json:"profiles"`
}

// wxGiftSettings: LeftOut khi cài đặt sai nằm ở unmatched.json; Saved false là website chạy bằng mặc định của nó.
type wxGiftSettings struct {
	LeftOut     bool  `json:"leftOut"`
	Saved       bool  `json:"saved"`
	Enabled     bool  `json:"enabled"`
	Amount      int64 `json:"amount"`
	DaysBefore  int   `json:"daysBefore"`
	ValidDays   int   `json:"validDays"`
	MinLeadDays int   `json:"minLeadDays"`
	MinOrder    int64 `json:"minOrder"`
}

type wxPause struct {
	LeftOut bool   `json:"leftOut"`
	Paused  bool   `json:"paused"`
	Reason  string `json:"reason"`
	At      string `json:"at"`
	By      string `json:"by"`
}

type wxCarrier struct {
	NhanhID int    `json:"nhanhId"`
	Code    string `json:"code"`
}

type wxCarriers struct {
	LeftOut    bool        `json:"leftOut"`
	Configured bool        `json:"configured"`
	Revision   *int        `json:"revision"`
	Enabled    []wxCarrier `json:"enabled"`
	Preferred  *wxCarrier  `json:"preferred"`
	At         string      `json:"at"`
	By         string      `json:"by"`
}

// wxUnmatched là unmatched.json: chỉ đếm theo loại (bản ghi gốc có dữ liệu cá nhân, không in).
type wxUnmatched struct {
	Version int `json:"version"`
	Items   []struct {
		Kind string `json:"kind"`
	} `json:"items"`
}

// readWebsiteExport đọc dir/data.json: version khác 1 → errImportVersion (kiểm trước khi đọc chặt); JSON sai, trường
// lạ → errBadExport. unmatched.json (có thì đọc) → số bản ghi theo loại.
func readWebsiteExport(dir string) (wxExport, map[string]int, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "data.json")) //nolint:gosec // G304: thư mục do người vận hành chỉ định
	if err != nil {
		return wxExport{}, nil, fmt.Errorf("%w: đọc data.json: %w", errBadExport, err)
	}
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return wxExport{}, nil, fmt.Errorf("%w: data.json không phải JSON: %w", errBadExport, err)
	}
	if head.Version != importWebsiteVersion {
		return wxExport{}, nil, fmt.Errorf("%w: version %d, chỉ đọc %d", errImportVersion, head.Version, importWebsiteVersion)
	}
	var x wxExport
	if err := decodeStrict(raw, &x); err != nil {
		return wxExport{}, nil, fmt.Errorf("%w: data.json: %w", errBadExport, err)
	}
	unmatched := map[string]int{}
	rawU, err := os.ReadFile(filepath.Join(dir, "unmatched.json")) //nolint:gosec // G304: như data.json
	switch {
	case errors.Is(err, os.ErrNotExist):
		return x, unmatched, nil
	case err != nil:
		return wxExport{}, nil, fmt.Errorf("%w: đọc unmatched.json: %w", errBadExport, err)
	}
	var u wxUnmatched
	if err := json.Unmarshal(rawU, &u); err != nil || u.Version != importWebsiteVersion {
		return wxExport{}, nil, fmt.Errorf("%w: unmatched.json sai (version %d): %w", errBadExport, u.Version, err)
	}
	for _, it := range u.Items {
		unmatched[it.Kind]++
	}
	return x, unmatched, nil
}

func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("đọc JSON: %w", err)
	}
	if dec.More() {
		return errors.New("thừa dữ liệu sau đối tượng gốc")
	}
	return nil
}

// Tên các loại theo thứ tự chạy (nhóm khóa ADR-46 tăng dần, quyết định 8 của tệp task).
const (
	kindFeedback = "góp ý"
	kindPause    = "tạm dừng nhận đơn"
	kindCarriers = "hãng trên web"
	kindCampaign = "chiến dịch"
	kindGift     = "cài đặt quà"
	kindBanner   = "banner"
	kindLook     = "bộ phối"
)

func importKinds() []string {
	return []string{kindFeedback, kindPause, kindCarriers, kindCampaign, kindGift, kindBanner, kindLook}
}

// importCount là kết quả của một loại: thêm (hay đổi), bỏ qua (đã có, giữ của CRM), lỗi; Notes là dòng giải thích
// (mã bản ghi và lỗi, không dữ liệu cá nhân).
type importCount struct {
	Added, Skipped, Failed int
	Notes                  []string
}

func (c *importCount) fail(id string, err error) {
	c.Failed++
	c.Notes = append(c.Notes, fmt.Sprintf("lỗi %s: %v", id, err))
}

func (c *importCount) note(format string, a ...any) {
	c.Notes = append(c.Notes, fmt.Sprintf(format, a...))
}

// importReport là bảng của một lần chạy.
type importReport struct {
	DryRun    bool
	Tenant    uuid.UUID
	Kinds     map[string]*importCount
	Unmatched map[string]int
	Birthdays int
	Declined  int
}

func newImportReport(dryRun bool, tenant uuid.UUID) *importReport {
	r := &importReport{DryRun: dryRun, Tenant: tenant, Kinds: map[string]*importCount{}}
	for _, k := range importKinds() {
		r.Kinds[k] = &importCount{}
	}
	return r
}

// Failed là tổng số lỗi.
func (r *importReport) Failed() int {
	_, failed := r.totals()
	return failed
}

// totals là tổng số thêm (hay đổi) và tổng số lỗi của mọi loại.
func (r *importReport) totals() (added, failed int) {
	for _, c := range r.Kinds {
		added, failed = added+c.Added, failed+c.Failed
	}
	return added, failed
}

// print in bảng: mỗi loại một dòng, rồi sinh nhật, unmatched.json, các dòng giải thích. Chỉ số, mã, lý do.
func (r *importReport) print(w io.Writer) {
	mode := "ĐÃ GHI"
	if r.DryRun {
		mode = "CHẠY KHÔ (đã lùi, không ghi gì)"
	}
	_, _ = fmt.Fprintf(w, "%s: %s, tenant %s\n", importWebsiteCommand, mode, r.Tenant)
	_, _ = fmt.Fprintf(w, "%-20s %6s %7s %5s\n", "loại", "thêm", "bỏ qua", "lỗi")
	for _, k := range importKinds() {
		c := r.Kinds[k]
		_, _ = fmt.Fprintf(w, "%-20s %6d %7d %5d\n", k, c.Added, c.Skipped, c.Failed)
	}
	_, _ = fmt.Fprintf(w, "sinh nhật: %d hồ sơ không nhập ở lệnh này (nhập bằng scripts/import-birthdays-to-crm.ts của "+
		"website), %d từ chối không xuất\n", r.Birthdays, r.Declined)
	_, _ = fmt.Fprintf(w, "unmatched.json (không nhập): %s\n", seedReport{}.line(r.Unmatched)) // "kind n, …" như seed
	for _, k := range importKinds() {
		for _, n := range r.Kinds[k].Notes {
			_, _ = fmt.Fprintf(w, "- %s: %s\n", k, n)
		}
	}
}

// isImportWebsiteCommand: args là lệnh `crm import-website-data …`.
func isImportWebsiteCommand(args []string) bool {
	return len(args) >= 1 && args[0] == importWebsiteCommand
}

// runOneOffCommand chạy một lệnh chạy một lần rồi thoát: import-website-data (vận hành), còn lại là lệnh dev
// (runDevCommand: service-key, seed-web-fixture); trả mã thoát.
func runOneOffCommand(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer, logger *slog.Logger) int {
	if isImportWebsiteCommand(args) {
		return importWebsiteExit(runImportWebsite(ctx, cfg, args[1:], stdout, logger), stderr)
	}
	return runDevCommand(ctx, cfg, args, stdout, stderr, logger)
}

// importWebsiteExit in lỗi ra stderr và trả mã thoát: sai cách gọi, thư mục sai, version lạ, tenant lạ → exitConfig;
// có bản ghi lỗi, lỗi database → exitFail.
func importWebsiteExit(err error, stderr io.Writer) int {
	if err == nil {
		return exitOK
	}
	_, _ = fmt.Fprintln(stderr, importWebsiteCommand+":", err)
	for _, cfgErr := range []error{errImportWebsiteUsage, errBadExport, errImportVersion, errImportTenant, errImportNoFiles} {
		if errors.Is(err, cfgErr) {
			return exitConfig
		}
	}
	return exitFail
}

// importWebsiteFlags là cờ đã kiểm của lệnh.
type importWebsiteFlags struct {
	Dir       string
	Tenant    uuid.UUID
	DryRun    bool
	ImageWait time.Duration
}

func parseImportWebsiteFlags(args []string) (importWebsiteFlags, error) {
	fs := flag.NewFlagSet(importWebsiteCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "thư mục admin-export-… do website xuất")
	tenant := fs.String("tenant", "", "mã tenant (UUID)")
	dry := fs.Bool("dry-run", false, "chạy cùng đường trong transaction rồi lùi")
	apply := fs.Bool("apply", false, "ghi thật")
	wait := fs.Duration("image-wait", defaultImageWait, "thời gian chờ tối đa mỗi ảnh banner được xử lý")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *dir == "" || *dry == *apply || *wait <= 0 {
		return importWebsiteFlags{}, errImportWebsiteUsage
	}
	id, err := uuid.Parse(*tenant)
	if err != nil {
		return importWebsiteFlags{}, errImportWebsiteUsage
	}
	return importWebsiteFlags{Dir: *dir, Tenant: id, DryRun: *dry, ImageWait: *wait}, nil
}

// runImportWebsite chạy lệnh: kiểm cờ, đọc và kiểm thư mục xuất trước khi mở database; mở pool bằng DATABASE_URL
// (crm_app, RLS áp); tenant phải có; dựng sổ sự kiện đủ consumer (newModules, như tiến trình thật: bộ đệm web làm mới)
// và kho tệp (chỉ --apply); in bảng; có lỗi → errImportIncomplete. Log chỉ số đếm.
func runImportWebsite(ctx context.Context, cfg config.Config, args []string, stdout io.Writer, logger *slog.Logger) error {
	fl, err := parseImportWebsiteFlags(args)
	if err != nil {
		return err
	}
	if !fl.DryRun && cfg.S3Endpoint == "" {
		return errImportNoFiles
	}
	x, unmatched, err := readWebsiteExport(fl.Dir)
	if err != nil {
		return err
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
	var store files.FileStore
	if !fl.DryRun {
		content.RegisterFilePurposes() // web_banner: lệnh không đi qua bước khởi động của tiến trình

		if store, err = files.NewS3Store(s3Config(cfg)); err != nil {
			return fmt.Errorf("dựng kho tệp: %w", err)
		}
	}
	hub := realtime.NewPGHub(realtime.PGHubOptions{URL: cfg.DatabaseURL, Logger: logger})
	mods, err := newModules(cfg, d, hub, metrics.NewRegistry(), store)
	if err != nil {
		return err
	}
	im := &websiteImporter{
		DB: d, Tenant: fl.Tenant, DryRun: fl.DryRun, Events: mods.Events, Queue: queue.NewQueue(), Bus: hub, Files: store,
		ImageView: catalog.ImageView{BaseURL: cfg.FilesPublicBaseURL, Preset: cfg.ImagePresetVersion},
		Now:       time.Now,
	}
	im.WaitImage = func(ctx context.Context, fileID uuid.UUID) error {
		return waitFileReady(ctx, d, fl.Tenant, fileID, fl.ImageWait)
	}
	if mods.M3.Fulfillment != nil {
		im.Fulfillment = mods.M3.Fulfillment.Service
	}
	rep, err := im.run(ctx, fl.Dir, x, unmatched)
	if rep != nil {
		reportImport(rep, stdout, logger, fl.DryRun)
	}
	if err != nil {
		return err
	}
	if rep.Failed() > 0 {
		return fmt.Errorf("%d lỗi: %w", rep.Failed(), errImportIncomplete)
	}
	return nil
}

// reportImport in bảng ra stdout và ghi một dòng log chỉ có số đếm (không SĐT, nội dung, lý do).
func reportImport(rep *importReport, stdout io.Writer, logger *slog.Logger, dryRun bool) {
	rep.print(stdout)
	added, failed := rep.totals()
	logger.Info("import-website-data xong", slog.String("tenant_id", rep.Tenant.String()), slog.Bool("dry_run", dryRun),
		slog.Int("added", added), slog.Int("failed", failed))
}
