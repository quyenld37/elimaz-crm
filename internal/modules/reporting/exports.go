package reporting

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Tệp XLSX của lần chạy nền (BC04, kế hoạch M6 Task 237 = M6-14): dựng tệp trong transaction chỉ đọc (ghi luồng ra tệp
// tạm, băm cùng lúc), tải lên bucket exports ngoài mọi transaction với khóa theo mã băm nội dung, rồi một transaction
// ghi ready + đăng ký dòng platform.files + audit. FileLink trả đường dẫn tải ký sẵn có hạn và ghi audit mỗi lần gọi.
// Tệp hết hạn do reporting.cleanup đánh dấu (expireFiles); đối tượng trên kho do luật hết hạn theo tiền tố của bucket
// xóa (Task 515).

// DefaultMaxRows là REPORT_MAX_ROWS khi cấu hình rỗng: số dòng dữ liệu tối đa của một tệp.
const DefaultMaxRows = 200_000

// Hạn giữ tệp khi cấu hình rỗng (như mặc định của config: REPORT_FILE_TTL 168h, CUSTOMER_EXPORT_TTL 24h).
const (
	defaultFileTTL     = 168 * time.Hour
	defaultCustomerTTL = 24 * time.Hour
)

// Lỗi nội bộ của việc dựng tệp.
var (
	// errRowLimit: emit đã nhận đủ REPORT_MAX_ROWS dòng; Def.Rows trả lại lỗi này để dừng đọc.
	errRowLimit = errors.New("reporting: đủ số dòng tối đa của tệp")
	// errFileTooLarge: tệp vượt MaxExportBytes của mục đích.
	errFileTooLarge = errors.New("reporting: tệp xuất quá lớn")
)

// exportFile là kết quả của một lần dựng tệp.
type exportFile struct {
	rows      int
	truncated bool
	size      int64
	sum       []byte
}

// exportPurpose là mục đích tệp của họ f: danh sách khách dùng customer_export (tiền tố customers/, hạn ngắn), mọi họ
// khác (gồm danh sách sản phẩm) dùng report_export (tiền tố reports/).
func exportPurpose(f Family) files.Purpose {
	key := PurposeReportExport
	if f == FamilyCustomers {
		key = PurposeCustomerExport
	}
	for _, p := range FilePurposes() {
		if p.Key == key {
			return p
		}
	}
	return files.Purpose{}
}

// fileTTL là hạn giữ tệp của họ f (REPORT_FILE_TTL hay CUSTOMER_EXPORT_TTL).
func (h *RunHandler) fileTTL(f Family) time.Duration {
	if f == FamilyCustomers {
		return cmp.Or(h.d.Cfg.Report.CustomerExportTTL, defaultCustomerTTL)
	}
	return cmp.Or(h.d.Cfg.Report.FileTTL, defaultFileTTL)
}

func (h *RunHandler) clock() time.Time {
	now := time.Now
	if h.d.Now != nil {
		now = h.d.Now
	}
	return now()
}

func (h *RunHandler) logger() *slog.Logger {
	if h.d.Log != nil {
		return h.d.Log
	}
	return slog.Default()
}

// executeXLSX dựng tệp của lần chạy rj, tải lên kho, đăng ký. (1) Một tệp tạm cho cả lượt (REPORT_TMP_DIR), xóa ở mọi
// đường; (2) dựng tệp trong readTx (hàm truyền vào không giữ trạng thái giữa các lần RetryTx chạy lại: buildSheet đặt
// lại tệp, bộ băm, số dòng); quá hạn → failed/too_heavy, lỗi nghiệp vụ → failed/error; (3) ngoài transaction: khóa
// "<reports|customers>/<tenant>/<run>/<sha256>.xlsx", Put (ErrObjectExists = cùng nội dung đã có → thành công; lỗi khác
// → not_sent, đối tượng mồ côi tự xóa theo tiền tố); hạn của tệp tính từ mốc lấy trước Put (hạn kho tính từ lúc ghi
// đối tượng, Task 515); (4) finishFile trong một transaction. Lỗi tệp tạm, database → not_sent (chạy lại an toàn).
func (h *RunHandler) executeXLSX(ctx context.Context, rj runJob, def Def) (commands.Outcome, error) {
	id := rj.row.ID
	p, err := DecodeParams(def.Key, rj.row.Params)
	if err != nil || def.Rows == nil || def.Columns == nil || def.FileName == nil {
		return h.fail(ctx, rj.tenant, id, ReasonError) // tham số đã lưu hỏng hay báo cáo không xuất tệp
	}
	start := time.Now()
	f, err := os.CreateTemp(h.d.Cfg.Report.TmpDir, "report-*.xlsx")
	if err != nil {
		return notSent(err), nil
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	built, err := h.buildFile(ctx, rj, def, p, f)
	if err != nil {
		if ctx.Err() != nil {
			return notSent(err), nil
		}
		if errors.Is(err, errFileTooLarge) {
			return h.fail(ctx, rj.tenant, id, ReasonTooHeavy)
		}
		if reason, ok := failReason(err); ok {
			return h.fail(ctx, rj.tenant, id, reason)
		}
		return notSent(err), nil
	}
	purpose := exportPurpose(Family(rj.row.Family))
	key := files.ExportKey(purpose.KeyPrefix, rj.tenant, id.String()+"/"+hex.EncodeToString(built.sum)+".xlsx")
	expires := h.clock().Add(h.fileTTL(Family(rj.row.Family))) // trước Put: không muộn hơn hạn kho tự xóa
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return notSent(err), nil
	}
	if err := h.d.Files.Put(ctx, purpose.Bucket, key, f, built.size, XLSXContentType); err != nil && !errors.Is(err, files.ErrObjectExists) {
		return notSent(err), nil
	}
	err = tenancy.WithTenant(ctx, h.d.DB, rj.tenant, func(tx pgx.Tx) error {
		return finishFile(ctx, tx, rj, def.FileName(p), purpose.Key, key, built, expires)
	})
	if err != nil {
		return notSent(err), nil
	}
	h.logger().InfoContext(ctx, "report export ready", slog.String("run_id", id.String()), slog.String("report", string(def.Key)),
		slog.Int("rows", built.rows), slog.Int64("bytes", built.size), slog.Int64("duration_ms", time.Since(start).Milliseconds()))
	return doneOutcome(built.rows), nil
}

// buildFile dựng tệp vào f trong transaction chỉ đọc của chạy nền; kết quả là của lần chạy hàm cuối cùng (RetryTx có
// thể chạy lại hàm: buildSheet làm lại tệp từ đầu mỗi lần).
func (h *RunHandler) buildFile(ctx context.Context, rj runJob, def Def, p Params, f *os.File) (exportFile, error) {
	maxRows := cmp.Or(h.d.Cfg.Report.MaxRows, DefaultMaxRows)
	scope := ScopeFor(rj.user, rj.row.SellerID)
	var out exportFile
	err := h.readTx(ctx, rj, def, func(rctx context.Context, tx pgx.Tx) error {
		out = exportFile{}
		meta, err := exportMeta(rctx, tx, def, p, scope, rj.row.CreatedAt)
		if err != nil {
			return err
		}
		r, err := buildSheet(rctx, tx, def, p, scope, f, maxRows, meta)
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	if err != nil {
		return exportFile{}, err
	}
	return out, nil
}

// countingWriter đếm số byte đã ghi.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err //nolint:wrapcheck // lỗi ghi tệp tạm, bên gọi bọc
}

// buildSheet ghi lại tệp f từ đầu (Truncate, Seek, bộ băm mới): dòng ghi kỳ/phạm vi (meta rỗng: không có), các dòng
// của Def.Rows tới maxRows (thêm thì dừng, ghi dòng ghi chú cắt), rồi đóng tệp. Kết quả trả bằng giá trị.
func buildSheet(ctx context.Context, tx pgx.Tx, def Def, p Params, s DataScope, f *os.File, maxRows int, meta string) (exportFile, error) {
	if err := f.Truncate(0); err != nil {
		return exportFile{}, fmt.Errorf("đặt lại tệp tạm: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return exportFile{}, fmt.Errorf("đặt lại tệp tạm: %w", err)
	}
	sum := sha256.New()
	cw := &countingWriter{w: io.MultiWriter(f, sum)}
	w, err := xlsx.NewWriter(cw, sheetName(def.Key), def.Columns(p))
	if err != nil {
		return exportFile{}, fmt.Errorf("mở tệp %s: %w", def.Key, err)
	}
	rows, truncated, err := writeRows(ctx, tx, def, p, s, w, maxRows, meta)
	if err != nil {
		_ = w.Close()
		return exportFile{}, err
	}
	if err := w.Close(); err != nil {
		return exportFile{}, fmt.Errorf("đóng tệp %s: %w", def.Key, err)
	}
	if cw.n > MaxExportBytes {
		return exportFile{}, fmt.Errorf("tệp %s %d byte: %w", def.Key, cw.n, errFileTooLarge)
	}
	return exportFile{rows: rows, truncated: truncated, size: cw.n, sum: sum.Sum(nil)}, nil
}

// writeRows ghi dòng meta và các dòng dữ liệu của def vào w; trả số dòng dữ liệu và đã cắt hay chưa.
func writeRows(ctx context.Context, tx pgx.Tx, def Def, p Params, s DataScope, w *xlsx.Writer, maxRows int, meta string) (int, bool, error) {
	if meta != "" {
		if err := w.Note(meta); err != nil {
			return 0, false, fmt.Errorf("ghi dòng kỳ: %w", err)
		}
	}
	rows, truncated := 0, false
	err := def.Rows(ctx, tx, s, p, func(cells []xlsx.Cell) error {
		if rows >= maxRows {
			truncated = true
			return errRowLimit
		}
		if err := w.Row(cells...); err != nil {
			return fmt.Errorf("ghi dòng %d: %w", rows+1, err)
		}
		rows++
		return nil
	})
	if err != nil && !errors.Is(err, errRowLimit) {
		return 0, false, fmt.Errorf("dựng tệp %s: %w", def.Key, err)
	}
	if truncated {
		if err := w.Note("Đã cắt ở " + groupDigits(maxRows) + " dòng, hãy thu hẹp khoảng thời gian"); err != nil {
			return 0, false, fmt.Errorf("ghi dòng ghi chú: %w", err)
		}
	}
	return rows, truncated, nil
}

// finishFile ghi ready (chỉ khi lần chạy còn running) cùng số dòng, cắt, số byte, hạn; thắng → đăng ký dòng tệp
// (khóa đã có dòng → id cũ), gắn file_id, audit export_ready; thua (bên khác đã kết thúc) → không đăng ký gì. Thứ tự
// khóa: report_runs (12a); dòng files mới chèn không tính (ADR-46 điểm 4).
func finishFile(ctx context.Context, tx pgx.Tx, rj runJob, fileName, purpose, key string, f exportFile, expires time.Time) error {
	st := store.New(tx)
	_, err := st.FinishRunFile(ctx, store.FinishRunFileParams{
		ID: rj.row.ID, RowCount: pgtype.Int4{Int32: int32(min(f.rows, 1<<31-1)), Valid: true}, //nolint:gosec // đã chặn
		Truncated: f.truncated, FileBytes: pgtype.Int8{Int64: f.size, Valid: true}, ExpiresAt: expires,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ghi tệp xong cho lần chạy: %w", err)
	}
	newID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("sinh mã tệp: %w", err)
	}
	requester := rj.row.RequestedBy
	fileID, err := files.Register(ctx, tx, files.NewFile{
		ID: newID, Purpose: purpose, Key: key, SHA256: f.sum, Size: f.size, ContentType: XLSXContentType,
		FileName: fileName, ExpiresAt: &expires, CreatedBy: &requester,
	})
	if err != nil {
		return fmt.Errorf("đăng ký tệp xuất: %w", err)
	}
	if err := st.SetRunFile(ctx, store.SetRunFileParams{ID: rj.row.ID, FileID: pgtype.UUID{Bytes: fileID, Valid: true}}); err != nil {
		return fmt.Errorf("gắn tệp vào lần chạy: %w", err)
	}
	action := "report.export_ready"
	if Family(rj.row.Family) == FamilyCustomers {
		action = "customer.export_ready"
	}
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: rj.tenant, ActorType: "system", ActorID: "reporting", Action: action, EntityType: "report_run",
		EntityID: rj.row.ID.String(),
		After:    map[string]any{"rows": f.rows, "truncated": f.truncated, "bytes": f.size},
	}); err != nil {
		return fmt.Errorf("ghi audit tệp xuất: %w", err)
	}
	return nil
}

// FileLink trả đường dẫn tải ký sẵn (hạn ttl, không quá hạn của lần chạy) tới tệp của lần chạy id họ f cho u, trong tx
// ghi của bên gọi (ngữ cảnh tenant của u): luật xem của GetRun (không thấy → ErrRunNotFound); dạng json →
// ErrBadParams; queued, running, failed → ErrRunNotReady; expired hay now ≥ expires_at, tệp đã xóa, hết hạn →
// ErrRunExpired; họ sales, inventory cần report.export lúc tải → ErrExportForbidden. Mỗi lần gọi ghi một dòng audit
// (report.file_downloaded hay customer.export_downloaded, người tải, entity_id = mã lần chạy) trước khi ký. Ký bằng
// files.DownloadURL (kiểm tệp ready, chưa xóa, chưa hết hạn, đúng tenant), tên tải về là tên đã đăng ký lúc tạo tệp
// (Def.FileName lúc chạy: tên theo "hôm nay" không đổi khi tải ngày sau).
func FileLink(ctx context.Context, tx pgx.Tx, u identity.AuthUser, f Family, id uuid.UUID, fs files.FileStore, ttl time.Duration,
	now time.Time,
) (string, time.Time, error) {
	run, err := GetRun(ctx, tx, u, f, id)
	if err != nil {
		return "", time.Time{}, err
	}
	switch {
	case run.Format != FormatXLSX:
		return "", time.Time{}, fmt.Errorf("lần chạy %s dạng %s: %w", id, run.Format, ErrBadParams)
	case run.Status == RunExpired, run.Status == RunReady && !now.Before(run.ExpiresAt):
		return "", time.Time{}, fmt.Errorf("lần chạy %s: %w", id, ErrRunExpired)
	case run.Status != RunReady:
		return "", time.Time{}, fmt.Errorf("lần chạy %s %s: %w", id, run.Status, ErrRunNotReady)
	case run.FileID == nil:
		return "", time.Time{}, fmt.Errorf("lần chạy %s không có tệp: %w", id, ErrRunExpired)
	case f != FamilyCustomers && !identity.Can(u, permExport, nil):
		return "", time.Time{}, fmt.Errorf("tải tệp %s: %w", id, ErrExportForbidden)
	}
	action := "report.file_downloaded"
	if f == FamilyCustomers {
		action = "customer.export_downloaded"
	}
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: action, EntityType: "report_run",
		EntityID: id.String(), After: map[string]any{"family": string(f), "report": string(run.Report)},
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("ghi audit tải tệp: %w", err)
	}
	ttl = min(ttl, run.ExpiresAt.Sub(now))
	info, err := files.Describe(ctx, tx, *run.FileID, "")
	if err != nil && !errors.Is(err, files.ErrNotFound) {
		return "", time.Time{}, fmt.Errorf("đọc tệp của lần chạy %s: %w", id, err)
	}
	url, err := files.DownloadURL(ctx, tx, fs, *run.FileID, ttl, info.OriginalName)
	if errors.Is(err, files.ErrNotFound) || errors.Is(err, files.ErrExpired) {
		return "", time.Time{}, fmt.Errorf("tải tệp %s: %w (%w)", id, ErrRunExpired, err)
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("ký đường dẫn tải %s: %w", id, err)
	}
	return url, now.Add(ttl), nil
}

// expireFiles là phần (d) của dọn: khóa lần chạy xlsx ready quá hạn (12a), chuyển expired, rồi đánh dấu xóa dòng tệp
// theo file_id tăng dần (12b; người xóa uuid.Nil là hệ thống). Trả số lần chạy đã chuyển.
func expireFiles(ctx context.Context, tx pgx.Tx) (int, error) {
	st := store.New(tx)
	ids, err := st.LockExpiredFileRuns(ctx, cleanupBatch)
	if err != nil {
		return 0, fmt.Errorf("khóa tệp xuất quá hạn: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	fileIDs, err := st.ExpireFileRuns(ctx, ids)
	if err != nil {
		return 0, fmt.Errorf("chuyển tệp xuất quá hạn sang expired: %w", err)
	}
	owned := make([]uuid.UUID, 0, len(fileIDs))
	for _, fid := range fileIDs {
		if fid.Valid {
			owned = append(owned, fid.Bytes)
		}
	}
	slices.SortFunc(owned, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, fid := range owned {
		if err := files.MarkDeleted(ctx, tx, fid, uuid.Nil); err != nil && !errors.Is(err, files.ErrNotFound) {
			return 0, fmt.Errorf("đánh dấu xóa tệp xuất %s: %w", fid, err)
		}
	}
	return len(fileIDs), nil
}
