package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-faster/jx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Vùng báo cáo: lần chạy nền và tệp xuất (BC04, BC05, Task M6-12 = 235, M6-14 = 237). Tạo lần chạy trong WriteTx
// (chống trùng, một transaction: lần chạy, lệnh, audit); xem, liệt kê theo luật của module (người tạo hoặc
// report.exports.manage). …/file (Task 237): đường dẫn tải ký sẵn có hạn, ghi audit mỗi lần gọi.

// Giá trị mặc định khi cấu hình rỗng (test): như mặc định của config (REPORT_MAX_PENDING_PER_USER 5, REPORT_FILE_TTL
// 168h, CUSTOMER_EXPORT_TTL 24h).
const (
	defaultRunsPending = 5
	defaultRunsLimit   = 50
	defaultLinkTTL     = 5 * time.Minute
)

// CreateSalesReportRun (POST /api/v1/reports/sales/runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateSalesReportRun(ctx context.Context, req *gen.CreateReportRunRequest, _ gen.CreateSalesReportRunParams) (gen.CreateSalesReportRunRes, error) {
	return h.createReportRun(ctx, reporting.FamilySales, req)
}

// ListSalesReportRuns (GET /api/v1/reports/sales/runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListSalesReportRuns(ctx context.Context, params gen.ListSalesReportRunsParams) (gen.ListSalesReportRunsRes, error) {
	return h.listReportRuns(ctx, reporting.FamilySales, params.Cursor, params.Limit)
}

// GetSalesReportRun (GET /api/v1/reports/sales/runs/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetSalesReportRun(ctx context.Context, params gen.GetSalesReportRunParams) (gen.GetSalesReportRunRes, error) {
	return h.getReportRun(ctx, reporting.FamilySales, params.ID)
}

// GetSalesReportRunFile (GET /api/v1/reports/sales/runs/{id}/file).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetSalesReportRunFile(ctx context.Context, params gen.GetSalesReportRunFileParams) (gen.GetSalesReportRunFileRes, error) {
	return h.reportRunFile(ctx, reporting.FamilySales, params.ID)
}

// CreateInventoryReportRun (POST /api/v1/reports/inventory/runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateInventoryReportRun(ctx context.Context, req *gen.CreateReportRunRequest, _ gen.CreateInventoryReportRunParams) (gen.CreateInventoryReportRunRes, error) {
	return h.createReportRun(ctx, reporting.FamilyInventory, req)
}

// ListInventoryReportRuns (GET /api/v1/reports/inventory/runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListInventoryReportRuns(ctx context.Context, params gen.ListInventoryReportRunsParams) (gen.ListInventoryReportRunsRes, error) {
	return h.listReportRuns(ctx, reporting.FamilyInventory, params.Cursor, params.Limit)
}

// GetInventoryReportRun (GET /api/v1/reports/inventory/runs/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetInventoryReportRun(ctx context.Context, params gen.GetInventoryReportRunParams) (gen.GetInventoryReportRunRes, error) {
	return h.getReportRun(ctx, reporting.FamilyInventory, params.ID)
}

// GetInventoryReportRunFile (GET /api/v1/reports/inventory/runs/{id}/file).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetInventoryReportRunFile(ctx context.Context, params gen.GetInventoryReportRunFileParams) (gen.GetInventoryReportRunFileRes, error) {
	return h.reportRunFile(ctx, reporting.FamilyInventory, params.ID)
}

// reportRunFile trả đường dẫn tải ký sẵn (REPORT_LINK_TTL, mặc định 5 phút) tới tệp của lần chạy họ f: một transaction
// tenant ghi audit tải tệp rồi ký (reporting.FileLink; mỗi lần gọi một dòng audit, không cần Idempotency-Key). Không
// gọi mạng (ký tại chỗ).
func (h *handler) reportRunFile(ctx context.Context, f reporting.Family, id uuid.UUID) (*gen.ReportFileLink, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	if h.d.Files == nil {
		return nil, errNoFileStore
	}
	var (
		link    string
		expires time.Time
	)
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		link, expires, err = reporting.FileLink(ctx, tx, u, f, id, h.d.Files, cmp.Or(h.d.Config.Report.LinkTTL, defaultLinkTTL), h.now())
		return err //nolint:wrapcheck // lỗi đã bọc trong module
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đường dẫn tải tệp của lần chạy: %w", err)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("đường dẫn tải tệp của lần chạy: %w", err)
	}
	return &gen.ReportFileLink{URL: *parsed, ExpiresAt: expires}, nil
}

// runLimits là giới hạn tạo lần chạy từ cấu hình của api.
func (h *handler) runLimits() reporting.RunLimits {
	c := h.d.Config.Report
	return reporting.RunLimits{
		MaxPendingPerUser: cmp.Or(c.MaxPendingPerUser, defaultRunsPending), JSONTTL: reporting.JSONTTL,
		FileTTL: cmp.Or(c.FileTTL, 168*time.Hour), CustomerTTL: cmp.Or(c.CustomerExportTTL, 24*time.Hour),
	}
}

// createReportRun tạo lần chạy họ f trong WriteTx; 202 kèm lần chạy. Lỗi nghiệp vụ (báo cáo lạ, tham số, quyền, quá
// số lần chờ) không lưu vào khóa chống trùng (final = false): gửi lại cùng khóa thì xét lại.
func (h *handler) createReportRun(ctx context.Context, f reporting.Family, req *gen.CreateReportRunRequest) (*gen.ReportRun, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	params, err := reportParamsJSON(req.Params)
	if err != nil {
		return nil, err
	}
	in := reporting.CreateRunInput{
		Family: f, Report: reporting.Key(req.Report), Format: string(req.Format), Params: params, RequestID: RequestID(ctx),
	}
	var (
		out     *gen.ReportRun
		refused error
	)
	err = WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46): gán lại mỗi lần
		run, err := reporting.CreateRun(ctx, tx, queue.NewQueue(), u, in, h.now(), h.runLimits())
		if err != nil {
			if isReportsDomainError(err) {
				refused = err
				return 0, nil, false, nil
			}
			return 0, nil, false, err //nolint:wrapcheck // lỗi đã bọc trong module
		}
		if out, err = reportRunOut(run); err != nil {
			return 0, nil, false, err
		}
		return http.StatusAccepted, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("tạo lần chạy: %w", refused)
	}
	return out, nil
}

// isReportsDomainError: lỗi nghiệp vụ của module báo cáo (bảng errors_reports.go), không phải lỗi database.
func isReportsDomainError(err error) bool {
	_, _, ok := reportsError(err)
	return ok && !errors.Is(err, db.ErrRetryLater)
}

// getReportRun đọc một lần chạy họ f (kèm kết quả khi đã ready).
func (h *handler) getReportRun(ctx context.Context, f reporting.Family, id uuid.UUID) (*gen.ReportRun, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var run reporting.Run
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		run, err = reporting.GetRun(ctx, tx, u, f, id)
		return err //nolint:wrapcheck // lỗi đã bọc trong module
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đọc lần chạy: %w", err)
	}
	return reportRunOut(run)
}

// listReportRuns liệt kê lần chạy họ f, mới nhất trước.
func (h *handler) listReportRuns(ctx context.Context, f reporting.Family, cursor gen.OptString, limit gen.OptInt32) (*gen.ReportRunPage, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var c *reporting.RunCursor
	if s, ok := cursor.Get(); ok {
		rc, err := reporting.ParseRunCursor(s)
		if err != nil {
			return nil, fmt.Errorf("liệt kê lần chạy: %w", err)
		}
		c = &rc
	}
	var (
		runs []reporting.Run
		next *reporting.RunCursor
	)
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		runs, next, err = reporting.ListRuns(ctx, tx, u, f, c, int(limit.Or(defaultRunsLimit)))
		return err //nolint:wrapcheck // lỗi đã bọc trong module
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("liệt kê lần chạy: %w", err)
	}
	out := &gen.ReportRunPage{Items: make([]gen.ReportRun, 0, len(runs))}
	for _, r := range runs {
		item, err := reportRunOut(r)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *item)
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}

// reportParamsJSON đổi tham số của thân yêu cầu (map khóa → JSON) sang một đối tượng JSON cho module.
func reportParamsJSON(p gen.ReportParams) (json.RawMessage, error) {
	m := make(map[string]json.RawMessage, len(p))
	for k, v := range p {
		m[k] = json.RawMessage(v)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("đọc tham số lần chạy: %w (%w)", reporting.ErrBadParams, err)
	}
	return b, nil
}

// reportRunOut đổi lần chạy của module sang ReportRun của hợp đồng.
func reportRunOut(r reporting.Run) (*gen.ReportRun, error) {
	out := &gen.ReportRun{
		ID: r.ID, Family: gen.ReportRunFamily(r.Family), Report: gen.ReportKey(r.Report),
		Format: gen.ReportRunFormat(r.Format), Status: gen.ReportRunStatus(r.Status), Truncated: r.Truncated,
		CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, RequestedByName: r.RequestedByName,
	}
	params, err := jsonObject(r.Params)
	if err != nil {
		return nil, fmt.Errorf("tham số lần chạy %s: %w", r.ID, err)
	}
	out.Params = gen.ReportParams(params)
	if r.Reason != nil {
		out.Reason = gen.NewOptReportRunReason(gen.ReportRunReason(*r.Reason))
	}
	if r.RowCount != nil {
		out.RowCount = gen.NewOptInt32(int32(min(*r.RowCount, 1<<31-1))) //nolint:gosec // đã chặn
	}
	if r.FinishedAt != nil {
		out.FinishedAt = gen.NewOptDateTime(*r.FinishedAt)
	}
	if len(r.Result) > 0 {
		res, err := jsonObject(r.Result)
		if err != nil {
			return nil, fmt.Errorf("kết quả lần chạy %s: %w", r.ID, err)
		}
		out.Result = gen.NewOptReportRunResult(gen.ReportRunResult(res))
	}
	return out, nil
}

// jsonObject đọc một đối tượng JSON thành map khóa → JSON thô (kiểu đối tượng tự do của ogen).
func jsonObject(raw []byte) (map[string]jx.Raw, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("đọc đối tượng JSON: %w", err)
	}
	out := make(map[string]jx.Raw, len(m))
	for k, v := range m {
		out[k] = jx.Raw(v)
	}
	return out, nil
}

// ReportResultJSON đổi kết quả Def.JSON của báo cáo k (tham số p) sang đúng JSON mà GET của báo cáo đó trả (cùng hàm
// đổi với GET), để lần chạy nền lưu kết quả giống báo cáo tại chỗ; cmd/crm nối vào reporting.Deps.Result của worker.
func ReportResultJSON(k reporting.Key, p reporting.Params, res any) (json.RawMessage, error) {
	var (
		v   json.Marshaler
		err error
	)
	switch k {
	case reporting.KeyDashboard:
		v, err = marshalerOf(dashboardOut(res))
	case reporting.KeyOrders:
		v, err = marshalerOf(ordersOut(res))
	case reporting.KeyOrderDrilldown:
		v, err = marshalerOf(drilldownOut(res))
	case reporting.KeyCarriers:
		v, err = marshalerOf(carriersOut(res))
	case reporting.KeyBestSellers:
		bp, ok := p.(*reporting.BestSellersParams)
		if !ok {
			return nil, fmt.Errorf("tham số bán chạy kiểu %T", p)
		}
		v, err = marshalerOf(bestSellersOut(bp, res))
	case reporting.KeyStock:
		v, err = marshalerOf(stockOut(res))
	case reporting.KeyStockMovements:
		v, err = marshalerOf(movementsOut(res))
	default:
		return nil, fmt.Errorf("báo cáo %s không có bản bảng: %w", k, reporting.ErrNotImplemented)
	}
	if err != nil {
		return nil, err
	}
	b, err := v.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả %s: %w", k, err)
	}
	return b, nil
}

// marshalerOf trả con trỏ tới v (kiểu của ogen mã hóa bằng phương thức con trỏ MarshalJSON).
func marshalerOf[T any, PT interface {
	*T
	json.Marshaler
}](v T, err error) (json.Marshaler, error) {
	if err != nil {
		return nil, err
	}
	return PT(&v), nil
}
