package reporting

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lần chạy nền (BC05, phần nền của BC04; kế hoạch M6 Task 235 = M6-12; vùng cao: quyền xem lần chạy). Tạo lần chạy
// ghi report_runs + lệnh command.report_run + audit trong transaction của yêu cầu; worker chạy lệnh ngoài transaction
// ghi (RunHandler.Execute), lệnh có tra lại (Lookup) nên worker chết giữa chừng không chạy hai lần một kết quả; mọi
// lần kết thúc phát báo hiệu report.run_finished; lịch reporting.cleanup dọn theo hạn. Dạng tệp (xlsx): exports.go (Task 237).

// Tên loại lệnh, việc, lịch, chủ đề báo hiệu của lần chạy nền (Global Constraints M6).
const (
	RunCommandName   = "command.report_run"
	CleanupKindName  = "reporting.cleanup"
	TopicRunFinished = "report.run_finished"
)

// Định dạng của lần chạy (cột format).
const (
	FormatJSON = "json"
	FormatXLSX = "xlsx"
)

// Trạng thái của lần chạy (cột status).
const (
	RunQueued  = "queued"
	RunRunning = "running"
	RunReady   = "ready"
	RunFailed  = "failed"
	RunExpired = "expired"
)

// Lý do hỏng của lần chạy (cột reason; ReasonBusy dùng chung với interactive.go).
const (
	ReasonTooHeavy  = "too_heavy"
	ReasonForbidden = "forbidden"
	ReasonError     = "error"
)

// Lệnh command.report_run: hết hạn sau 1 giờ, mỗi lần chạy tối đa 80 s (REPORT_BACKGROUND_TIMEOUT dưới 75 s cộng thời
// gian ghi tệp).
const (
	runCommandExpiresAfter = time.Hour
	runCommandTimeout      = 80 * time.Second
)

// Giới hạn của lần chạy nền.
const (
	// JSONTTL: kết quả dạng bảng giữ 24 giờ rồi bị xóa phần kết quả (Q24); dòng report_runs giữ 90 ngày.
	JSONTTL = 24 * time.Hour
	// MaxResultBytes: kết quả dạng bảng lớn hơn chừng này → failed/too_heavy (kế hoạch: 5 MiB).
	MaxResultBytes = 5 << 20
	// busyRetry: hết chỗ chạy → chờ chừng này rồi thử lại (không tính lượt; trần là hạn 1 giờ của lệnh).
	busyRetry = 15 * time.Second
	// backgroundLockTimeout: lock_timeout của transaction báo cáo chạy nền (Global Constraints M6).
	backgroundLockTimeout = 5 * time.Second
	// defaultWorkerSlots, defaultBackgroundTimeout: REPORT_WORKER_SLOTS, REPORT_BACKGROUND_TIMEOUT khi cấu hình rỗng.
	defaultWorkerSlots       = 2
	defaultBackgroundTimeout = 60 * time.Second
	// Dọn: chu kỳ lịch, số dòng tối đa mỗi phần mỗi lần, độ trễ của lưới an toàn (b).
	cleanupEvery = time.Hour
	cleanupBatch = 500
)

// Quyền xem lần chạy của người khác và quyền xuất tệp (Task 225).
const (
	permExportsManage identity.Permission = "report.exports.manage"
	permExport        identity.Permission = "report.export"
)

// RunCommandKind là loại lệnh chạy một lần chạy nền.
func RunCommandKind() queue.Kind {
	return commands.CommandKind(RunCommandName, runCommandExpiresAfter, runCommandTimeout)
}

// RunLimits là giới hạn khi tạo lần chạy: số lần đang chờ hay chạy của một người, hạn giữ của kết quả bảng, tệp báo
// cáo, tệp danh sách khách (tính từ lúc tạo; lúc xong tính lại).
type RunLimits struct {
	MaxPendingPerUser             int
	JSONTTL, FileTTL, CustomerTTL time.Duration
}

// ttl là hạn giữ của lần chạy họ f, định dạng format.
func (l RunLimits) ttl(f Family, format string) time.Duration {
	switch {
	case format == FormatJSON:
		return l.JSONTTL
	case f == FamilyCustomers:
		return l.CustomerTTL
	default:
		return l.FileTTL
	}
}

// CreateRunInput là yêu cầu tạo lần chạy. Params là JSON tham số (cùng tên tham số query của GET); RequestID ghi vào
// audit.
type CreateRunInput struct {
	Family    Family
	Report    Key
	Format    string
	Params    json.RawMessage
	RequestID string
}

// Run là một lần chạy. Result chỉ có ở GetRun khi format json đã ready (đúng JSON của GET báo cáo).
type Run struct {
	ID              uuid.UUID
	Family          Family
	Report          Key
	Format, Status  string
	Reason          *string
	Params          json.RawMessage
	RowCount        *int
	Truncated       bool
	Result          json.RawMessage
	CreatedAt       time.Time
	FinishedAt      *time.Time
	ExpiresAt       time.Time
	RequestedBy     uuid.UUID
	RequestedByName string
	// FileID là dòng platform.files của tệp xlsx đã xong (nil khi chưa có, dạng json); không có trong API.
	FileID *uuid.UUID
}

// CreateRun tạo lần chạy nền cho u trong tx của bên gọi (ngữ cảnh tenant của u): kiểm báo cáo thuộc họ, định dạng,
// quyền xuất tệp, tham số, phạm vi, số lần đang chờ; chèn report_runs (người bán own, hạn now + TTL), ghi lệnh
// command.report_run (DedupeKey = mã lần chạy) và một dòng audit. Lỗi: ErrUnknownReport, ErrBadParams (định dạng không
// hợp với báo cáo, tham số sai), lỗi ngày của tham số, ErrExportForbidden, ErrForbidden (không có quyền đọc hay phạm vi
// rỗng), ErrTooManyRuns, ErrNoOwnSeller. Chạy lại được trong RetryTx (mã lần chạy sinh mới mỗi lần).
func CreateRun(ctx context.Context, tx pgx.Tx, q queue.JobQueue, u identity.AuthUser, in CreateRunInput, now time.Time, l RunLimits) (Run, error) {
	def, p, err := checkRunInput(u, in)
	if err != nil {
		return Run{}, err
	}
	seller, err := OwnSeller(ctx, tx)
	if err != nil {
		return Run{}, err
	}
	if !scopeAllows(def.Family, ScopeFor(u, seller)) {
		return Run{}, fmt.Errorf("tạo lần chạy %s: phạm vi rỗng: %w", def.Key, ErrForbidden)
	}
	st := store.New(tx)
	n, err := st.CountPendingRuns(ctx, u.UserID)
	if err != nil {
		return Run{}, fmt.Errorf("đếm lần chạy đang chờ: %w", err)
	}
	if int(n) >= l.MaxPendingPerUser {
		return Run{}, fmt.Errorf("tạo lần chạy: %d lần đang chờ: %w", n, ErrTooManyRuns)
	}
	params, err := json.Marshal(p)
	if err != nil {
		return Run{}, fmt.Errorf("mã hóa tham số %s: %w", def.Key, err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Run{}, fmt.Errorf("sinh mã lần chạy: %w", err)
	}
	// Thứ tự khóa ADR-46: report_runs (12a) trước jobs (13).
	if err := st.InsertRun(ctx, store.InsertRunParams{
		ID: id, TenantID: u.TenantID, SellerID: seller, Family: string(def.Family), Report: string(def.Key),
		Format: in.Format, Params: params, RequestedBy: u.UserID, ExpiresAt: now.Add(l.ttl(def.Family, in.Format)),
	}); err != nil {
		return Run{}, fmt.Errorf("ghi lần chạy: %w", err)
	}
	jobID, _, err := q.Enqueue(ctx, tx, RunCommandKind(), queue.NewJob{
		TenantID: &u.TenantID, DedupeKey: id.String(), Payload: runPayload{RunID: id},
	})
	if err != nil {
		return Run{}, fmt.Errorf("ghi lệnh chạy nền: %w", err)
	}
	if err := st.SetRunJob(ctx, store.SetRunJobParams{ID: id, JobID: pgtype.UUID{Bytes: jobID, Valid: true}}); err != nil {
		return Run{}, fmt.Errorf("gắn lệnh vào lần chạy: %w", err)
	}
	action := "report.run_requested"
	if def.Family == FamilyCustomers {
		action = "customer.export_requested"
	}
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: action,
		EntityType: "report_run", EntityID: id.String(), RequestID: in.RequestID,
		After: map[string]any{
			"family": string(def.Family), "report": string(def.Key), "format": in.Format, "params": json.RawMessage(params),
		},
	}); err != nil {
		return Run{}, fmt.Errorf("ghi audit lần chạy: %w", err)
	}
	return GetRun(ctx, tx, u, def.Family, id)
}

// checkRunInput kiểm phần không cần database của yêu cầu tạo lần chạy và đọc tham số.
//
//nolint:ireturn // Params là giao diện của mọi kiểu tham số báo cáo
func checkRunInput(u identity.AuthUser, in CreateRunInput) (Def, Params, error) {
	def, err := Lookup(in.Family, in.Report)
	if err != nil {
		return Def{}, nil, err
	}
	switch {
	case in.Format != FormatJSON && in.Format != FormatXLSX:
		return Def{}, nil, fmt.Errorf("định dạng %q: %w", in.Format, ErrBadParams)
	case in.Format == FormatJSON && (def.Family == FamilyCustomers || def.JSON == nil):
		return Def{}, nil, fmt.Errorf("báo cáo %s chỉ xuất tệp: %w", def.Key, ErrBadParams)
	}
	if !identity.Can(u, def.Read, nil) {
		return Def{}, nil, fmt.Errorf("tạo lần chạy %s: %w", def.Key, ErrForbidden)
	}
	if in.Format == FormatXLSX && def.Family != FamilyCustomers && !identity.Can(u, permExport, nil) {
		return Def{}, nil, fmt.Errorf("xuất tệp %s: %w", def.Key, ErrExportForbidden)
	}
	p, err := DecodeParams(def.Key, in.Params)
	if err != nil {
		return Def{}, nil, err
	}
	return def, p, nil
}

// scopeAllows: người có phạm vi s thấy được dữ liệu của họ f (họ khách: quyền toàn shop, không phạm vi).
func scopeAllows(f Family, s DataScope) bool {
	switch f {
	case FamilySales:
		return !s.NoSales()
	case FamilyInventory:
		return !s.NoInventory()
	default:
		return true
	}
}

// canSeeRun: người tạo, hoặc người có report.exports.manage.
func canSeeRun(u identity.AuthUser, requestedBy uuid.UUID) bool {
	return requestedBy == u.UserID || identity.Can(u, permExportsManage, nil)
}

// GetRun đọc lần chạy id của họ f cho u trong tx của bên gọi (ngữ cảnh tenant): kèm kết quả bảng khi đã ready. Không
// có, thuộc họ khác, của tenant khác, hay u không phải người tạo và không có report.exports.manage → ErrRunNotFound
// (không lộ sự tồn tại).
func GetRun(ctx context.Context, tx pgx.Tx, u identity.AuthUser, f Family, id uuid.UUID) (Run, error) {
	r, err := store.New(tx).GetRun(ctx, store.GetRunParams{ID: id, Family: string(f)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, fmt.Errorf("lần chạy %s: %w", id, ErrRunNotFound)
	}
	if err != nil {
		return Run{}, fmt.Errorf("đọc lần chạy: %w", err)
	}
	if !canSeeRun(u, r.RequestedBy) {
		return Run{}, fmt.Errorf("lần chạy %s: %w", id, ErrRunNotFound)
	}
	out := runOut(store.ListRunsAllRow{
		ID: r.ID, Family: r.Family, Report: r.Report, Format: r.Format, Status: r.Status, Reason: r.Reason,
		Params: r.Params, RowCount: r.RowCount, Truncated: r.Truncated, CreatedAt: r.CreatedAt,
		FinishedAt: r.FinishedAt, ExpiresAt: r.ExpiresAt, RequestedBy: r.RequestedBy, RequestedByName: r.RequestedByName,
	})
	if r.Status == RunReady && len(r.Result) > 0 {
		out.Result = r.Result
	}
	if r.FileID.Valid {
		fid := uuid.UUID(r.FileID.Bytes)
		out.FileID = &fid
	}
	return out, nil
}

// RunCursor là vị trí sau dòng cuối của một trang lần chạy (created_at giảm, rồi id giảm).
type RunCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// runCursorLen: 8 byte micro giây Unix + 16 byte mã.
const runCursorLen = 8 + 16

// String mã hóa con trỏ thành chuỗi mờ (base64url).
func (c RunCursor) String() string {
	buf := make([]byte, 8, runCursorLen)
	binary.BigEndian.PutUint64(buf, uint64(c.CreatedAt.UnixMicro()))
	return base64.RawURLEncoding.EncodeToString(append(buf, c.ID[:]...))
}

// ParseRunCursor đọc chuỗi do RunCursor.String tạo; hỏng → ErrBadParams.
func ParseRunCursor(s string) (RunCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != runCursorLen {
		return RunCursor{}, fmt.Errorf("con trỏ lần chạy: %w", ErrBadParams)
	}
	at := time.UnixMicro(int64(binary.BigEndian.Uint64(raw[:8]))).UTC() //nolint:gosec // G115: chỉ đọc lại giá trị đã ghi
	return RunCursor{CreatedAt: at, ID: uuid.UUID(raw[8:])}, nil
}

// MaxRunsLimit là số dòng tối đa một trang lần chạy (hợp đồng Limit: 1–100).
const MaxRunsLimit = 100

// ListRuns liệt kê lần chạy của họ f, mới nhất trước: của u, hoặc của mọi người khi u có report.exports.manage; tối đa
// limit dòng (ngoài 1–MaxRunsLimit → ErrBadParams), next khác nil khi còn trang sau. Không kèm kết quả.
func ListRuns(ctx context.Context, tx pgx.Tx, u identity.AuthUser, f Family, cursor *RunCursor, limit int) ([]Run, *RunCursor, error) {
	if limit < 1 || limit > MaxRunsLimit {
		return nil, nil, fmt.Errorf("limit %d: %w", limit, ErrBadParams)
	}
	var c RunCursor
	if cursor != nil {
		c = *cursor
	}
	st := store.New(tx)
	lim := int32(limit + 1)
	var rows []store.ListRunsAllRow
	if identity.Can(u, permExportsManage, nil) {
		r, err := st.ListRunsAll(ctx, store.ListRunsAllParams{
			Family: string(f), HasCursor: cursor != nil, CursorAt: c.CreatedAt, CursorID: c.ID, Lim: lim,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("liệt kê lần chạy: %w", err)
		}
		rows = r
	} else {
		r, err := st.ListRunsMine(ctx, store.ListRunsMineParams{
			RequestedBy: u.UserID, Family: string(f), HasCursor: cursor != nil, CursorAt: c.CreatedAt, CursorID: c.ID, Lim: lim,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("liệt kê lần chạy của mình: %w", err)
		}
		rows = make([]store.ListRunsAllRow, 0, len(r))
		for _, x := range r {
			rows = append(rows, store.ListRunsAllRow(x))
		}
	}
	var next *RunCursor
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = &RunCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	out := make([]Run, 0, len(rows))
	for _, r := range rows {
		out = append(out, runOut(r))
	}
	return out, next, nil
}

func runOut(r store.ListRunsAllRow) Run {
	out := Run{
		ID: r.ID, Family: Family(r.Family), Report: Key(r.Report), Format: r.Format, Status: r.Status, Params: r.Params,
		Truncated: r.Truncated, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, RequestedBy: r.RequestedBy,
		RequestedByName: r.RequestedByName,
	}
	if r.Reason.Valid {
		out.Reason = &r.Reason.String
	}
	if r.RowCount.Valid {
		n := int(r.RowCount.Int32)
		out.RowCount = &n
	}
	if r.FinishedAt.Valid {
		t := r.FinishedAt.Time
		out.FinishedAt = &t
	}
	return out
}

// runPayload là payload của lệnh command.report_run: chỉ mã lần chạy.
type runPayload struct {
	RunID uuid.UUID `json:"runId"`
}

// RunHandler chạy lần chạy nền (lệnh command.report_run): hiện thực commands.Handler, commands.Looker,
// commands.DoneHook, queue.TerminalHook. Số lần chạy cùng lúc của tiến trình là REPORT_WORKER_SLOTS.
type RunHandler struct {
	d       Deps
	defs    map[Key]Def
	kind    queue.Kind
	slots   chan struct{}
	timeout time.Duration
}

// NewRunHandler dựng handler của lệnh chạy nền từ d: chỗ REPORT_WORKER_SLOTS, hạn REPORT_BACKGROUND_TIMEOUT (rỗng:
// 2 chỗ, 60 s); d.Defs nil → Defs().
func NewRunHandler(d Deps) *RunHandler {
	slots := d.Cfg.Report.WorkerSlots
	if slots < 1 {
		slots = defaultWorkerSlots
	}
	timeout := d.Cfg.Report.BackgroundTimeout
	if timeout <= 0 {
		timeout = defaultBackgroundTimeout
	}
	defs := d.Defs
	if defs == nil {
		defs = Defs()
	}
	return &RunHandler{d: d, defs: defs, kind: RunCommandKind(), slots: make(chan struct{}, slots), timeout: timeout}
}

// Kind là RunCommandKind.
func (h *RunHandler) Kind() queue.Kind { return h.kind }

// AutoRetryNotSent trả true: lần chạy nền không gửi gì ra ngoài CRM, chạy lại an toàn.
func (*RunHandler) AutoRetryNotSent() bool { return true }

// runJob là lần chạy đã chuyển sang running cùng người tạo đã nạp lại.
type runJob struct {
	tenant uuid.UUID
	row    store.MarkRunningRow
	user   identity.AuthUser
}

// Execute chạy lần chạy của lệnh j ngoài mọi transaction ghi: hết chỗ → not_sent (chờ 15 s, không tính lượt); chuyển
// running (đã kết thúc → done); nạp lại người tạo (khóa, mất quyền đọc, phạm vi, report.export với xlsx → failed/
// forbidden); dạng bảng: chạy Def.JSON trong transaction chỉ đọc (quá hạn hay kết quả quá MaxResultBytes → failed/
// too_heavy; lỗi nghiệp vụ của báo cáo → failed/error), đổi kết quả sang JSON của API (Deps.Result) rồi ghi ready trong
// một transaction. Lỗi database ở bước nào cũng là not_sent (chạy lại an toàn). Mọi đường failed ghi có điều kiện rồi
// trả done (không thành dead).
func (h *RunHandler) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	tenant, id, ok := runOfJob(j)
	if !ok {
		return commands.Outcome{Status: commands.StatusRejected, Reason: "bad_payload"}, nil
	}
	select {
	case h.slots <- struct{}{}:
	default:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: ReasonBusy, RetryAfter: busyRetry, KeepAttempt: true}, nil
	}
	defer func() { <-h.slots }()
	rj, fail, err := h.start(ctx, tenant, id)
	switch {
	case errors.Is(err, errRunFinished):
		return doneOutcome(0), nil
	case err != nil:
		return notSent(err), nil
	case fail != "":
		return h.fail(ctx, tenant, id, fail)
	}
	def, ok := h.defs[Key(rj.row.Report)]
	if !ok {
		return h.fail(ctx, tenant, id, ReasonError)
	}
	if rj.row.Format == FormatXLSX {
		return h.executeXLSX(ctx, rj, def)
	}
	return h.executeJSON(ctx, rj, def)
}

// errRunFinished: lần chạy đã kết thúc (MarkRunning 0 dòng hay đã bị xóa).
var errRunFinished = errors.New("reporting: lần chạy đã kết thúc")

// start chuyển lần chạy sang running và nạp lại người tạo trong một transaction; fail khác rỗng là lý do hỏng do quyền.
func (h *RunHandler) start(ctx context.Context, tenant, id uuid.UUID) (runJob, string, error) {
	var (
		rj   runJob
		fail string
	)
	err := tenancy.WithTenant(ctx, h.d.DB, tenant, func(tx pgx.Tx) error {
		rj, fail = runJob{tenant: tenant}, "" // RetryTx có thể chạy lại: gán lại mỗi lần
		row, err := store.New(tx).MarkRunning(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return errRunFinished
		}
		if err != nil {
			return fmt.Errorf("chuyển lần chạy sang running: %w", err)
		}
		rj.row = row
		u, err := identity.LoadUserByID(ctx, tx, row.RequestedBy)
		switch {
		case errors.Is(err, identity.ErrUnknownUser), errors.Is(err, identity.ErrLocked):
			fail = ReasonForbidden
			return nil
		case err != nil:
			return fmt.Errorf("nạp người tạo lần chạy: %w", err)
		}
		rj.user = u
		if !h.allowed(u, row) {
			fail = ReasonForbidden
		}
		return nil
	})
	if err != nil {
		return runJob{}, "", err //nolint:wrapcheck // errRunFinished hay lỗi đã bọc ở trên
	}
	return rj, fail, nil
}

// allowed: người tạo còn quyền đọc của họ, phạm vi không rỗng, và report.export khi xuất tệp báo cáo (lúc chạy).
func (h *RunHandler) allowed(u identity.AuthUser, row store.MarkRunningRow) bool {
	def, ok := h.defs[Key(row.Report)]
	if !ok || !identity.Can(u, def.Read, nil) || !scopeAllows(Family(row.Family), ScopeFor(u, row.SellerID)) {
		return false
	}
	return row.Format != FormatXLSX || Family(row.Family) == FamilyCustomers || identity.Can(u, permExport, nil)
}

// executeJSON chạy bản bảng của báo cáo rồi ghi kết quả.
func (h *RunHandler) executeJSON(ctx context.Context, rj runJob, def Def) (commands.Outcome, error) {
	id := rj.row.ID
	if def.JSON == nil {
		return h.fail(ctx, rj.tenant, id, ReasonError)
	}
	p, err := DecodeParams(def.Key, rj.row.Params)
	if err != nil {
		return h.fail(ctx, rj.tenant, id, ReasonError)
	}
	res, err := h.runReport(ctx, rj, def, p)
	if err != nil {
		// Worker đang dừng (ctx của lệnh bị hủy): câu bị hủy cũng trả 57014, không phải báo cáo quá nặng → chạy lại.
		if ctx.Err() != nil {
			return notSent(err), nil
		}
		if reason, ok := failReason(err); ok {
			return h.fail(ctx, rj.tenant, id, reason)
		}
		return notSent(err), nil
	}
	encode := h.d.Result
	if encode == nil {
		encode = defaultResult
	}
	raw, err := encode(def.Key, p, res)
	if err != nil {
		return h.fail(ctx, rj.tenant, id, ReasonError)
	}
	if len(raw) > MaxResultBytes {
		return h.fail(ctx, rj.tenant, id, ReasonTooHeavy)
	}
	rows := 0
	if rr, ok := res.(interface{ ReportRows() int }); ok {
		rows = rr.ReportRows()
	}
	err = tenancy.WithTenant(ctx, h.d.DB, rj.tenant, func(tx pgx.Tx) error {
		_, err := store.New(tx).FinishRunReady(ctx, store.FinishRunReadyParams{
			ID: id, Result: raw, RowCount: pgtype.Int4{Int32: int32(min(rows, 1<<31-1)), Valid: true}, //nolint:gosec // đã chặn
			TtlSeconds: int64(JSONTTL / time.Second),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // bên khác đã kết thúc lần chạy: không ghi lần hai
		}
		if err != nil {
			return fmt.Errorf("ghi kết quả lần chạy: %w", err)
		}
		return nil
	})
	if err != nil {
		return notSent(err), nil
	}
	return doneOutcome(rows), nil
}

// runReport chạy Def.JSON trong transaction chỉ đọc của chạy nền (readTx). Kết quả chỉ lấy khi transaction xong không
// lỗi.
func (h *RunHandler) runReport(ctx context.Context, rj runJob, def Def, p Params) (any, error) {
	var res any
	err := h.readTx(ctx, rj, def, func(rctx context.Context, tx pgx.Tx) error {
		res = nil // RetryTx có thể chạy lại hàm: gán lại kết quả mỗi lần
		v, err := def.JSON(rctx, tx, ScopeFor(rj.user, rj.row.SellerID), p)
		if err != nil {
			return fmt.Errorf("báo cáo %s: %w", def.Key, err)
		}
		res = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// readTx chạy fn trong transaction chỉ đọc của chạy nền (TxReport(timeout, 5 s)) của tenant lần chạy, hạn ctx riêng
// timeout + ctxMargin (fn nhận ctx đó); ghi thời gian vào report_ms. Quá hạn ctx riêng (ctx của lệnh còn) → errTooHeavy.
func (h *RunHandler) readTx(ctx context.Context, rj runJob, def Def, fn func(ctx context.Context, tx pgx.Tx) error) error {
	rctx, cancel := context.WithTimeout(ctx, h.timeout+ctxMargin)
	defer cancel()
	start := time.Now()
	err := tenancy.WithTenant(rctx, h.d.DB, rj.tenant, func(tx pgx.Tx) error {
		return fn(rctx, tx)
	}, TxReport(h.timeout, backgroundLockTimeout))
	if h.d.Metrics != nil {
		h.d.Metrics.Observe("report_ms", float64(time.Since(start).Milliseconds()), string(def.Key))
	}
	if err != nil {
		if ctx.Err() == nil && errors.Is(rctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("báo cáo %s quá hạn: %w", def.Key, errTooHeavy)
		}
		return err //nolint:wrapcheck // lỗi của fn đã bọc
	}
	return nil
}

// errTooHeavy: báo cáo chạy nền quá hạn giờ.
var errTooHeavy = errors.New("reporting: báo cáo quá nặng")

// failReason phân loại lỗi của lần chạy báo cáo: quá hạn (57014, hạn ctx riêng) → too_heavy; ngoài phạm vi → forbidden;
// lỗi nghiệp vụ của báo cáo (tham số, ngày, chưa có bản chụp, chưa làm, không có người bán) → error. Lỗi khác (database,
// ctx của lệnh hết hạn) không phải lỗi của lần chạy: chạy lại.
func failReason(err error) (string, bool) {
	if code, _ := db.TransientCode(err); code == sqlstateQueryCanceled || errors.Is(err, errTooHeavy) {
		return ReasonTooHeavy, true
	}
	for _, e := range []error{ErrPageForbidden, ErrWarehouseForbidden} {
		if errors.Is(err, e) {
			return ReasonForbidden, true
		}
	}
	for _, e := range []error{
		ErrBadDate, ErrFromAfterTo, ErrRangeTooLong, ErrBadParams, ErrUnknownReport, ErrNotImplemented, ErrNoSnapshot,
		ErrNoOwnSeller, ErrUnknownDocType,
	} {
		if errors.Is(err, e) {
			return ReasonError, true
		}
	}
	return "", false
}

// defaultResult mã hóa kết quả bằng encoding/json (Deps.Result rỗng: test của module); cmd/crm nối bộ đổi sang JSON
// của API.
func defaultResult(_ Key, _ Params, res any) (json.RawMessage, error) {
	b, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả: %w", err)
	}
	return b, nil
}

// fail ghi lần chạy hỏng với lý do reason (chỉ khi còn queued/running) rồi trả done: lỗi nghiệp vụ không thành việc
// dead. Ghi lỗi → not_sent (chạy lại).
func (h *RunHandler) fail(ctx context.Context, tenant, id uuid.UUID, reason string) (commands.Outcome, error) {
	err := tenancy.WithTenant(ctx, h.d.DB, tenant, func(tx pgx.Tx) error {
		return failRun(ctx, tx, id, reason)
	})
	if err != nil {
		return notSent(err), nil
	}
	return commands.Outcome{Status: commands.StatusDone, Reason: reason, Result: json.RawMessage(`{"rows":0}`)}, nil
}

// failRun ghi lần chạy hỏng có điều kiện; đã kết thúc → không đổi, nil.
func failRun(ctx context.Context, tx pgx.Tx, id uuid.UUID, reason string) error {
	_, err := store.New(tx).FailRun(ctx, store.FailRunParams{ID: id, Reason: pgtype.Text{String: reason, Valid: true}})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("ghi lần chạy hỏng: %w", err)
	}
	return nil
}

func doneOutcome(rows int) commands.Outcome {
	return commands.Outcome{Status: commands.StatusDone, Result: json.RawMessage(fmt.Sprintf(`{"rows":%d}`, rows))}
}

// notSent: lỗi hạ tầng (database, hết hạn ctx của lệnh) → chưa làm xong, chạy lại an toàn. Lý do là mã ngắn.
func notSent(_ error) commands.Outcome {
	return commands.Outcome{Status: commands.StatusNotSent, Reason: "retry"}
}

// runOfJob đọc tenant và mã lần chạy của lệnh; thiếu tenant hay payload hỏng → ok = false.
func runOfJob(j queue.Job) (tenant, id uuid.UUID, ok bool) {
	if j.TenantID == nil {
		return uuid.Nil, uuid.Nil, false
	}
	var p runPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return *j.TenantID, p.RunID, p.RunID != uuid.Nil
}

// Lookup tra lần chạy của lệnh j (worker chết, lệnh in_doubt): ready, failed, expired hay đã bị xóa → done (không chạy
// lại); queued, running → not_sent (chạy lại).
func (h *RunHandler) Lookup(ctx context.Context, j queue.Job) (string, error) {
	tenant, id, ok := runOfJob(j)
	if !ok {
		return commands.StatusDone, nil
	}
	var status string
	err := tenancy.WithTenant(ctx, h.d.DB, tenant, func(tx pgx.Tx) error {
		head, err := store.New(tx).RunHead(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			status = RunExpired
			return nil
		}
		if err != nil {
			return fmt.Errorf("tra lần chạy: %w", err)
		}
		status = head.Status
		return nil
	})
	if err != nil {
		return commands.StatusUnknown, err //nolint:wrapcheck // đã bọc trong hàm
	}
	if status == RunQueued || status == RunRunning {
		return commands.StatusNotSent, nil
	}
	return commands.StatusDone, nil
}

// OnDone phát báo hiệu report.run_finished trong transaction đổi lệnh sang done (chỉ tới client khi commit).
func (h *RunHandler) OnDone(ctx context.Context, j queue.Job, _ json.RawMessage, tx pgx.Tx) error {
	return h.signal(ctx, j, tx)
}

// OnTerminal xử lý lệnh kết thúc không thành: dead (hết lượt) hay expired (quá 1 giờ chờ chỗ) → lần chạy còn
// queued/running thành failed (error, busy) rồi báo hiệu. in_doubt: giữ nguyên (tra lại quyết định chạy tiếp hay đã
// xong).
func (h *RunHandler) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if status == queue.StatusInDoubt {
		return nil
	}
	_, id, ok := runOfJob(j)
	if !ok {
		return nil
	}
	reason := ReasonError
	if status == queue.StatusExpired {
		reason = ReasonBusy
	}
	if err := failRun(ctx, tx, id, reason); err != nil {
		return err
	}
	return h.signal(ctx, j, tx)
}

// signal phát report.run_finished cho lần chạy của j: quyền đọc của họ, phạm vi lấy từ grant của người tạo để người
// tạo nhận được (báo hiệu chỉ là "tải lại danh sách", không mang dữ liệu). Lần chạy đã bị xóa: không báo hiệu.
func (h *RunHandler) signal(ctx context.Context, j queue.Job, tx pgx.Tx) error {
	if h.d.Bus == nil {
		return nil
	}
	tenant, id, ok := runOfJob(j)
	if !ok {
		return nil
	}
	head, err := store.New(tx).RunHead(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("đọc lần chạy để báo hiệu: %w", err)
	}
	perm := familyRead(Family(head.Family))
	var grants []identity.Grant
	u, err := identity.LoadUserByID(ctx, tx, head.RequestedBy)
	switch {
	case err == nil:
		grants = u.Grants
	case errors.Is(err, identity.ErrUnknownUser), errors.Is(err, identity.ErrLocked):
	default:
		return fmt.Errorf("nạp người tạo để báo hiệu: %w", err)
	}
	if err := h.d.Bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenant, Topic: TopicRunFinished, Requires: perm, Scope: signalScope(perm, grants),
		Entity: realtime.Entity{Type: "report_run", ID: id.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu lần chạy xong: %w", err)
	}
	return nil
}

// familyRead là quyền đọc của họ f.
func familyRead(f Family) identity.Permission {
	switch f {
	case FamilySales:
		return permSalesRead
	case FamilyInventory:
		return permInventoryRead
	default:
		return permCustomerExport
	}
}

// signalScope là phạm vi của báo hiệu quyền p: quyền toàn shop (customer.export) không có phạm vi; quyền theo đích lấy
// phạm vi của grant đầu tiên của người tạo cho p (phạm vi all nếu có), không có grant → all (chỉ người xem toàn shop).
func signalScope(p identity.Permission, grants []identity.Grant) *identity.Scope {
	if identity.KindOf(p) != identity.ScopeByTarget {
		return nil
	}
	var first *identity.Scope
	for _, g := range grants {
		if g.Permission != p {
			continue
		}
		if g.Scope.Type == identity.ScopeAll {
			return &identity.Scope{Type: identity.ScopeAll}
		}
		if first == nil {
			s := g.Scope
			first = &s
		}
	}
	if first == nil {
		return &identity.Scope{Type: identity.ScopeAll}
	}
	return first
}

// CleanupHandler dọn lần chạy theo hạn (việc reporting.cleanup, theo tenant): (a) kết quả bảng quá hạn → expired, bỏ
// kết quả; (d) tệp xlsx ready quá hạn → expired và dòng platform.files đánh dấu xóa (đối tượng trên kho do luật hết
// hạn theo tiền tố của bucket xóa, ứng dụng không xóa vĩnh viễn); (b) lần chạy còn queued/running quá hạn + 1 giờ mà
// lệnh đã kết thúc → failed/error (lưới an toàn); (c) dòng tạo quá 90 ngày bị xóa. Mỗi phần tối đa 500 dòng mỗi lần,
// khóa theo id tăng dần, bỏ qua dòng đang bị khóa.
type CleanupHandler struct{}

// NewCleanupHandler dựng việc dọn lần chạy.
func NewCleanupHandler(_ Deps) *CleanupHandler { return &CleanupHandler{} }

// Kind là việc giao dịch reporting.cleanup.
func (*CleanupHandler) Kind() queue.Kind {
	return queue.Kind{Name: CleanupKindName, Policy: queue.DefaultPolicy}
}

// cleanupOutput là kết quả của một lượt dọn (chỉ số đếm).
type cleanupOutput struct {
	Expired      int   `json:"expired"`
	ExpiredFiles int   `json:"expiredFiles"`
	Failed       int   `json:"failed"`
	Deleted      int64 `json:"deleted"`
}

// Handle chạy một lượt dọn trong tx của việc (ngữ cảnh tenant). Khóa report_runs (ADR-46 12a) trước (mọi phần),
// rồi dòng tệp (12b, phần d, cuối lượt) theo file_id tăng dần; trạng thái lệnh đọc không khóa (commands.Status).
func (*CleanupHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("việc %s: thiếu tenant: %w", j.ID, queue.ErrPermanent)
	}
	st := store.New(tx)
	expired, err := st.ExpireJSONRuns(ctx, cleanupBatch)
	if err != nil {
		return nil, fmt.Errorf("dọn kết quả bảng quá hạn: %w", err)
	}
	stale, err := st.LockStaleRuns(ctx, cleanupBatch)
	if err != nil {
		return nil, fmt.Errorf("khóa lần chạy treo: %w", err)
	}
	out := cleanupOutput{Expired: len(expired)}
	for _, r := range stale {
		finished, err := jobFinished(ctx, tx, r.JobID)
		if err != nil {
			return nil, err
		}
		if !finished {
			continue
		}
		if err := failRun(ctx, tx, r.ID, ReasonError); err != nil {
			return nil, err
		}
		out.Failed++
	}
	if out.Deleted, err = st.DeleteOldRuns(ctx, cleanupBatch); err != nil {
		return nil, fmt.Errorf("xóa lần chạy quá 90 ngày: %w", err)
	}
	// Phần (d) cuối cùng: mọi khóa report_runs (12a) của lượt dọn đứng trước khóa platform.files (12b).
	if out.ExpiredFiles, err = expireFiles(ctx, tx); err != nil {
		return nil, err
	}
	res, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả dọn: %w", err)
	}
	return res, nil
}

// jobFinished: lệnh của lần chạy đã kết thúc (done, dead, expired) hay không còn; in_doubt, pending, running: chưa.
func jobFinished(ctx context.Context, tx pgx.Tx, jobID pgtype.UUID) (bool, error) {
	if !jobID.Valid {
		return true, nil
	}
	s, err := commands.Status(ctx, tx, jobID.Bytes)
	if errors.Is(err, commands.ErrCommandNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("đọc trạng thái lệnh chạy nền: %w", err)
	}
	return s == "done" || s == queue.StatusDead || s == queue.StatusExpired, nil
}

// cleanupSchedules là lịch reporting.cleanup (mỗi 3.600 s, theo tenant).
func cleanupSchedules() []schedules.Schedule {
	return []schedules.Schedule{{Name: CleanupKindName, Every: cleanupEvery, PerTenant: true, Kind: (&CleanupHandler{}).Kind()}}
}
