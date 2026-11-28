package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// nhanhImportCustomersCommand là lệnh `crm nhanh-import-customers --snapshot <tệp> --tenant <mã> --dry-run|--apply`
// (ADR-48, kế hoạch M7 Task 542): nhập khách từ bản chụp do `crm nhanh-snapshot` ghi, qua đường tạo khách của CRM
// (customer.LegacyImporter): chạy lại không trùng (mã Nhanh và SĐT là khóa), SĐT đã là của khách CRM thì không ghi đè,
// trường hợp không chắc vào customer.legacy_review (không tự gộp). Lệnh KHÔNG gọi Nhanh, chỉ đọc tệp bản chụp trên
// server; chạy bằng DATABASE_URL (crm_app, RLS áp). In ra chỉ số đếm, không tên hay SĐT.
const nhanhImportCustomersCommand = "nhanh-import-customers"

// nhanhSnapshotVersion là khuôn bản chụp lệnh đọc được (khớp nhanh.SnapshotVersion; lệnh này không nhập gói adapter).
const nhanhSnapshotVersion = 1

// maxNhanhSnapshotBytes chặn tệp quá lớn (khách + sản phẩm của một bản chụp thật xa dưới mức này).
const maxNhanhSnapshotBytes = 512 << 20

var (
	errNhanhImportUsage = errors.New("cách gọi: `crm " + nhanhImportCustomersCommand +
		" --snapshot <tệp nhanh-snapshot-….json> --tenant <mã tenant (UUID)> --dry-run|--apply`")
	errNhanhSnapshotBad     = errors.New("tệp bản chụp sai")
	errNhanhSnapshotVersion = errors.New("bản chụp khác phiên bản lệnh đọc được")
	errNhanhImportTenant    = errors.New("không có tenant với mã này")
	errNhanhImportErrors    = errors.New("có khách lỗi, xem số đếm")
)

func isNhanhImportCustomersCommand(args []string) bool {
	return len(args) >= 1 && args[0] == nhanhImportCustomersCommand
}

// nhanhImportSystemID là người làm của lệnh: tài khoản hệ thống cố định (không có dòng identity.users).
func nhanhImportSystemID() uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("elimaz-crm:system:"+nhanhImportCustomersCommand))
}

type nhanhImportFlags struct {
	Snapshot string
	Tenant   uuid.UUID
	DryRun   bool
}

func parseNhanhImportFlags(args []string) (nhanhImportFlags, error) {
	fs := flag.NewFlagSet(nhanhImportCustomersCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	snap := fs.String("snapshot", "", "")
	tenant := fs.String("tenant", "", "")
	dry := fs.Bool("dry-run", false, "")
	apply := fs.Bool("apply", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *snap == "" || *dry == *apply {
		return nhanhImportFlags{}, errNhanhImportUsage
	}
	id, err := uuid.Parse(*tenant)
	if err != nil {
		return nhanhImportFlags{}, errNhanhImportUsage
	}
	return nhanhImportFlags{Snapshot: *snap, Tenant: id, DryRun: *dry}, nil
}

// nhanhSnapshotFile là phần của bản chụp mà lệnh cần.
type nhanhSnapshotFile struct {
	Version   int               `json:"version"`
	Customers []json.RawMessage `json:"customers"`
}

// readNhanhCustomers đọc tệp bản chụp (tệp thường, ≤ maxNhanhSnapshotBytes, đúng phiên bản) và đổi từng khách.
func readNhanhCustomers(path string) ([]customer.LegacyCustomer, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxNhanhSnapshotBytes {
		return nil, errNhanhSnapshotBad
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: đường dẫn do người vận hành đưa vào lệnh
	if err != nil {
		return nil, errNhanhSnapshotBad
	}
	var f nhanhSnapshotFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, errNhanhSnapshotBad // không kèm lỗi json: có thể trích dữ liệu khách
	}
	if f.Version != nhanhSnapshotVersion {
		return nil, fmt.Errorf("phiên bản %d: %w", f.Version, errNhanhSnapshotVersion)
	}
	out := make([]customer.LegacyCustomer, 0, len(f.Customers))
	for i, raw := range f.Customers {
		out = append(out, nhanhCustomer(i+1, raw))
	}
	return out, nil
}

// Tên trường của khách trong customer/list (API v3), mỗi ý nhận vài tên vì chưa xác minh với API thật (Task 540); chỉnh ở
// đây khi lệch. Khách thiếu hay sai trường vẫn được nhập phần còn lại.
var (
	nhanhIDKeys       = []string{"id", "customerId"}
	nhanhNameKeys     = []string{"name", "fullName"}
	nhanhPhoneKeys    = []string{"mobile", "mobile2", "phone", "phone2", "otherMobile"}
	nhanhBirthdayKeys = []string{"birthday", "birthDay", "dateOfBirth"}
	nhanhTagKeys      = []string{"tags", "tag", "group", "groupName", "customerGroup", "typeName"}
	nhanhNoteKeys     = []string{"note", "description", "remark"}
	nhanhAddressKeys  = []string{"address", "fullAddress"}
)

// nhanhCustomer đổi một khách của Nhanh sang customer.LegacyCustomer. Địa chỉ của Nhanh dùng mã địa phương riêng (không
// khớp danh mục hành chính của CRM) nên ghi vào ghi chú ("Địa chỉ cũ: …") thay vì địa chỉ giao.
func nhanhCustomer(row int, raw json.RawMessage) customer.LegacyCustomer {
	c := customer.LegacyCustomer{Row: row}
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if dec.Decode(&m) != nil {
		return c
	}
	if s := firstText(m, nhanhIDKeys); s != "" {
		c.LegacyID, _ = strconv.ParseInt(s, 10, 64)
	}
	c.Name = firstText(m, nhanhNameKeys)
	for _, k := range nhanhPhoneKeys {
		c.Phones = append(c.Phones, texts(m[k])...)
	}
	c.Birthday = nhanhBirthday(firstText(m, nhanhBirthdayKeys))
	for _, k := range nhanhTagKeys {
		c.Tags = append(c.Tags, texts(m[k])...)
	}
	note := firstText(m, nhanhNoteKeys)
	if addr := firstText(m, nhanhAddressKeys); addr != "" {
		note = strings.TrimSpace(note + "\nĐịa chỉ cũ: " + addr)
	}
	c.Note = note
	return c
}

// firstText trả giá trị chữ hay số đầu tiên khác rỗng của các khóa (không tách theo dấu phẩy: tên, ghi chú, địa chỉ).
func firstText(m map[string]any, keys []string) string {
	for _, k := range keys {
		switch x := m[k].(type) {
		case string:
			if v := strings.TrimSpace(x); v != "" {
				return v
			}
		case json.Number:
			return x.String()
		}
	}
	return ""
}

// texts đổi một giá trị JSON thành các chuỗi khác rỗng: chuỗi (tách theo , ; cho danh sách nhãn không ảnh hưởng SĐT vì
// SĐT không chứa dấu đó), số, đối tượng có name, mảng của các loại đó.
func texts(v any) []string {
	switch x := v.(type) {
	case string:
		var out []string
		for part := range strings.FieldsFuncSeq(x, func(r rune) bool { return r == ',' || r == ';' }) {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
		return out
	case json.Number:
		return []string{x.String()}
	case map[string]any:
		return texts(x["name"])
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, texts(e)...)
		}
		return out
	}
	return nil
}

// nhanhBirthday đọc ngày sinh dạng yyyy-mm-dd (có hay không giờ), dd/mm/yyyy hay giây Unix; không đọc được → nil.
func nhanhBirthday(s string) *time.Time {
	if s == "" {
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n <= 0 || n > math.MaxInt32*2 {
			return nil
		}
		t := time.Unix(n, 0).UTC()
		return &t
	}
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04:05", time.RFC3339, "02/01/2006", "2/1/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

// runNhanhImportCustomers chạy lệnh: kiểm cờ, đọc bản chụp trước khi mở database, tenant phải có, chạy (khô hay thật),
// in số đếm. Có khách lỗi → errNhanhImportErrors (phần còn lại đã nhập hay đã kiểm).
func runNhanhImportCustomers(ctx context.Context, cfg config.Config, args []string, stdout io.Writer, logger *slog.Logger) error {
	fl, err := parseNhanhImportFlags(args)
	if err != nil {
		return err
	}
	rows, err := readNhanhCustomers(fl.Snapshot)
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
		return fmt.Errorf("%s: %w", fl.Tenant, errNhanhImportTenant)
	}
	hub := realtime.NewPGHub(realtime.PGHubOptions{URL: cfg.DatabaseURL, Logger: logger})
	mods, err := newModules(cfg, d, hub, metrics.NewRegistry(), nil)
	if err != nil {
		return err
	}
	imp := customer.LegacyImporter{DB: d, Writer: customer.Writer{Events: mods.Events, Queue: queue.NewQueue(), Realtime: hub}}
	actor := customer.Actor{Type: customer.ActorSystem, ID: nhanhImportSystemID()}
	res, err := imp.Run(ctx, fl.Tenant, actor, rows, fl.DryRun)
	if err != nil {
		return fmt.Errorf("nhập khách: %w", err)
	}
	mode := "ĐÃ GHI"
	if fl.DryRun {
		mode = "CHẠY KHÔ (không ghi gì)"
	}
	if _, err := fmt.Fprintf(stdout, "%s: khách %d, nhập mới %d, bỏ qua %d, cần xem %d, lỗi %d\n",
		mode, res.Total, res.OK, res.Skipped, res.Review, res.Failed); err != nil {
		return fmt.Errorf("in kết quả: %w", err)
	}
	for _, e := range res.Errors {
		if _, err := fmt.Fprintf(stdout, "lỗi: dòng %d, cột %s, mã %s\n", e.Row, e.Column, e.Code); err != nil {
			return fmt.Errorf("in lỗi: %w", err)
		}
	}
	logger.Info(nhanhImportCustomersCommand+" xong", slog.String("tenant_id", fl.Tenant.String()),
		slog.Bool("dry_run", fl.DryRun), slog.Int("ok", res.OK), slog.Int("skipped", res.Skipped),
		slog.Int("review", res.Review), slog.Int("failed", res.Failed))
	if res.Failed > 0 {
		return fmt.Errorf("%d khách lỗi: %w", res.Failed, errNhanhImportErrors)
	}
	return nil
}

// nhanhImportExit in lỗi và trả mã thoát: sai cách gọi, tệp sai, phiên bản lạ, tenant lạ → exitConfig; còn lại exitFail.
func nhanhImportExit(err error, stderr io.Writer) int {
	if err == nil {
		return exitOK
	}
	_, _ = fmt.Fprintln(stderr, nhanhImportCustomersCommand+":", err)
	for _, cfgErr := range []error{errNhanhImportUsage, errNhanhSnapshotBad, errNhanhSnapshotVersion, errNhanhImportTenant} {
		if errors.Is(err, cfgErr) {
			return exitConfig
		}
	}
	return exitFail
}
