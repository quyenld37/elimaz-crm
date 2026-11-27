package reporting_test

import (
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/migrations"
)

func pgCode(err error) string {
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pe.Code
	}
	return ""
}

// schemaEnv: database riêng có hai tenant A, B.
type schemaEnv struct {
	s    *testkit.Staff
	a, b uuid.UUID
}

func newSchemaEnv(t *testing.T) *schemaEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	return &schemaEnv{s: s, a: s.Tenant(t, "A"), b: s.Tenant(t, "B")}
}

// appExec chạy q bằng crm_app trong ngữ cảnh tenant, trả lỗi nguyên vẹn.
func (e *schemaEnv) appExec(t *testing.T, tenant uuid.UUID, q string, args ...any) error {
	t.Helper()
	ctx := t.Context()
	return tenancy.WithTenant(ctx, e.s.App, tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		_, err := tx.Exec(ctx, q, args...)
		return err //nolint:wrapcheck // test
	})
}

// appCount đếm bằng crm_app trong ngữ cảnh tenant.
func (e *schemaEnv) appCount(t *testing.T, tenant uuid.UUID, q string, args ...any) int {
	t.Helper()
	return appCount(t, e.s, tenant, q, args...)
}

// appCount đếm bằng crm_app trong ngữ cảnh tenant (câu q trả một số).
func appCount(t *testing.T, s *testkit.Staff, tenant uuid.UUID, q string, args ...any) int {
	t.Helper()
	ctx := t.Context()
	var n int
	if err := tenancy.WithTenant(ctx, s.App, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, q, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("đếm %q: %v", q, err)
	}
	return n
}

// insertRunSQL chèn một lần chạy; $1 family, $2 report, $3 format, $4 status, $5 reason, $6 file_id, $7 requested_by.
const insertRunSQL = `INSERT INTO reporting.report_runs (tenant_id, seller_id, family, report, format, params,
	requested_by, status, reason, file_id, expires_at)
	VALUES (current_setting('app.tenant_id')::uuid, uuidv7(), $1, $2, $3, '{}', $7, $4, $5, $6, now() + interval '7 days')`

func (e *schemaEnv) insertRun(t *testing.T, tenant uuid.UUID, family, report, format, status string, reason *string,
	fileID *uuid.UUID, by uuid.UUID,
) error {
	t.Helper()
	return e.appExec(t, tenant, insertRunSQL, family, report, format, status, reason, fileID, by)
}

func (e *schemaEnv) insertDay(t *testing.T, tenant uuid.UUID, day string) error {
	t.Helper()
	return e.appExec(t, tenant, `INSERT INTO reporting.stock_snapshot_days (tenant_id, day, cutoff_at, row_count)
		VALUES (current_setting('app.tenant_id')::uuid, $1::date, $1::date + interval '17 hours', 1)`, day)
}

func (e *schemaEnv) insertSnapshot(t *testing.T, tenant uuid.UUID, day string) error {
	t.Helper()
	return e.appExec(t, tenant, `INSERT INTO reporting.stock_daily_snapshots (tenant_id, day, warehouse_id, variant_id,
		seller_id, on_hand, held, in_transit, damaged)
		VALUES (current_setting('app.tenant_id')::uuid, $1::date, uuidv7(), uuidv7(), uuidv7(), 5, 1, 0, 0)`, day)
}

// Sau 0601, 0603, 0604 database theo đủ luật schema (RLS, unique ghép tenant, khóa ngoại ghép, view security_invoker).
func TestReportingSchema_Rules(t *testing.T) {
	t.Parallel()
	testkit.CheckSchemaRulesClean(t, testkit.OpenStaff(t).Owner)
}

// Ba bảng tách theo tenant: A chèn một dòng mỗi bảng; B đọc 0 dòng; ngoài ngữ cảnh tenant 0 dòng.
func TestReportingSchema_TenantIsolation(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	user := e.s.User(t, e.a, "bao-cao@elimaz.test", "active")
	if err := e.insertRun(t, e.a, "sales", "dashboard", "json", "queued", nil, nil, user); err != nil {
		t.Fatalf("chèn report_runs: %v", err)
	}
	if err := e.insertDay(t, e.a, "2026-11-26"); err != nil {
		t.Fatalf("chèn stock_snapshot_days: %v", err)
	}
	if err := e.insertSnapshot(t, e.a, "2026-11-26"); err != nil {
		t.Fatalf("chèn stock_daily_snapshots: %v", err)
	}
	for _, table := range []string{"report_runs", "stock_snapshot_days", "stock_daily_snapshots"} {
		q := "SELECT count(*) FROM reporting." + table
		if n := e.appCount(t, e.a, q); n != 1 {
			t.Errorf("%s: tenant A đọc %d dòng, muốn 1", table, n)
		}
		if n := e.appCount(t, e.b, q); n != 0 {
			t.Errorf("%s: tenant B đọc %d dòng, muốn 0", table, n)
		}
		var n int
		if err := e.s.App.Pool.QueryRow(t.Context(), q).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: ngoài ngữ cảnh tenant đọc %d dòng (%v), muốn 0", table, n, err)
		}
	}
}

// Hai bảng chụp tồn chỉ ghi thêm: crm_app sửa, xóa → lỗi quyền 42501; chủ bảng → trigger báo append-only. report_runs
// thì crm_app sửa, xóa được (dọn sau 90 ngày).
func TestReportingSchema_SnapshotsAppendOnly(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	if err := e.insertDay(t, e.a, "2026-11-26"); err != nil {
		t.Fatal(err)
	}
	if err := e.insertSnapshot(t, e.a, "2026-11-26"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"UPDATE reporting.stock_daily_snapshots SET on_hand = 1",
		"DELETE FROM reporting.stock_daily_snapshots",
		"UPDATE reporting.stock_snapshot_days SET row_count = 2",
		"DELETE FROM reporting.stock_snapshot_days",
		"TRUNCATE reporting.stock_daily_snapshots",
	} {
		if err := e.appExec(t, e.a, q); pgCode(err) != "42501" {
			t.Errorf("crm_app %q: muốn 42501, nhận %v", q, err)
		}
	}
	e.s.CheckOwnerAppendOnly(t, e.a,
		"UPDATE reporting.stock_daily_snapshots SET on_hand = 1",
		"DELETE FROM reporting.stock_daily_snapshots",
		"UPDATE reporting.stock_snapshot_days SET row_count = 2",
		"DELETE FROM reporting.stock_snapshot_days",
	)
	if n := e.s.CountIn(t, e.a, "SELECT count(*) FROM reporting.stock_daily_snapshots WHERE on_hand = 5"); n != 1 {
		t.Fatalf("dòng chụp tồn bị đổi: %d", n)
	}

	user := e.s.User(t, e.a, "don@elimaz.test", "active")
	if err := e.insertRun(t, e.a, "sales", "orders", "json", "queued", nil, nil, user); err != nil {
		t.Fatal(err)
	}
	if err := e.appExec(t, e.a, "UPDATE reporting.report_runs SET status = 'running', version = version + 1"); err != nil {
		t.Fatalf("crm_app sửa report_runs: %v", err)
	}
	if err := e.appExec(t, e.a, "DELETE FROM reporting.report_runs"); err != nil {
		t.Fatalf("crm_app xóa report_runs: %v", err)
	}
}

// CHECK của report_runs (định dạng, trạng thái và lý do, họ báo cáo) và khóa ngoại ngày chụp.
func TestReportingSchema_RunChecks(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	user := e.s.User(t, e.a, "check@elimaz.test", "active")
	fileID := uuid.Must(uuid.NewV7())
	reason := "error"
	cases := []struct {
		name                           string
		family, report, format, status string
		reason                         *string
		file                           *uuid.UUID
		want                           string
	}{
		{"json có file_id", "sales", "orders", "json", "ready", nil, &fileID, "23514"},
		{"failed không reason", "sales", "orders", "xlsx", "failed", nil, nil, "23514"},
		{"reason khi không failed", "sales", "orders", "xlsx", "ready", &reason, nil, "23514"},
		{"customers json", "customers", "customers", "json", "queued", nil, nil, "23514"},
		{"customers sai report", "customers", "orders", "xlsx", "queued", nil, nil, "23514"},
		{"products json", "inventory", "products", "json", "queued", nil, nil, "23514"},
		{"products sai family", "sales", "products", "xlsx", "queued", nil, nil, "23514"},
		{"family lạ", "money", "orders", "json", "queued", nil, nil, "23514"},
		{"report lạ", "sales", "profit", "json", "queued", nil, nil, "23514"},
		{"status lạ", "sales", "orders", "json", "done", nil, nil, "23514"},
		{"xlsx có tệp", "sales", "orders", "xlsx", "ready", nil, &fileID, ""},
		{"failed có reason", "sales", "carriers", "json", "failed", &reason, nil, ""},
		{"customers xlsx", "customers", "customers", "xlsx", "queued", nil, nil, ""},
		{"products xlsx", "inventory", "products", "xlsx", "queued", nil, nil, ""},
	}
	for _, c := range cases {
		err := e.insertRun(t, e.a, c.family, c.report, c.format, c.status, c.reason, c.file, user)
		if got := pgCode(err); got != c.want {
			t.Errorf("%s: mã %q (%v), muốn %q", c.name, got, err, c.want)
		}
	}
	if err := e.appExec(t, e.a, `INSERT INTO reporting.report_runs (tenant_id, seller_id, family, report, format,
		params, requested_by, row_count, expires_at) VALUES (current_setting('app.tenant_id')::uuid, uuidv7(), 'sales',
		'orders', 'json', '{}', $1, -1, now())`, user); pgCode(err) != "23514" {
		t.Errorf("row_count âm: %v", err)
	}
	// Chụp tồn cho ngày chưa có dòng stock_snapshot_days → khóa ngoại; ngày của tenant khác cũng không nối được.
	if err := e.insertSnapshot(t, e.a, "2026-11-20"); pgCode(err) != "23503" {
		t.Errorf("ngày chưa chụp: muốn 23503, nhận %v", err)
	}
	if err := e.insertDay(t, e.b, "2026-11-21"); err != nil {
		t.Fatal(err)
	}
	if err := e.insertSnapshot(t, e.a, "2026-11-21"); pgCode(err) != "23503" {
		t.Errorf("ngày của tenant B: muốn 23503, nhận %v", err)
	}
	if err := e.insertDay(t, e.b, "2026-11-21"); pgCode(err) != "23505" {
		t.Errorf("chụp hai lần một ngày: muốn 23505, nhận %v", err)
	}
	// Số âm ở từng cột số lượng → CHECK; cả bốn bằng 0 thì nhận.
	if err := e.insertDay(t, e.a, "2026-11-22"); err != nil {
		t.Fatal(err)
	}
	const snap = `INSERT INTO reporting.stock_daily_snapshots (tenant_id, day, warehouse_id, variant_id, seller_id,
		on_hand, held, in_transit, damaged) VALUES (current_setting('app.tenant_id')::uuid, '2026-11-22', uuidv7(),
		uuidv7(), uuidv7(), $1, $2, $3, $4)`
	if err := e.appExec(t, e.a, snap, 0, 0, 0, 0); err != nil {
		t.Fatalf("bốn số 0: %v", err)
	}
	for i, col := range []string{"on_hand", "held", "in_transit", "damaged"} {
		vals := []any{0, 0, 0, 0}
		vals[i] = -1
		if err := e.appExec(t, e.a, snap, vals...); pgCode(err) != "23514" {
			t.Errorf("%s âm: muốn 23514, nhận %v", col, err)
		}
	}
}

// identity.users_v là view của M1 (0113): đúng ba cột; không migration 06xx nào tạo lại hay thay view này; tenant B nối
// report_runs với view không thấy tên người của A.
func TestReportingSchema_UsersView(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	var names []string
	for _, c := range testkit.Columns(t, e.s.Owner, "identity", "users_v") {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ", "); got != "id, display_name, status" {
		t.Fatalf("identity.users_v: cột %q", got)
	}
	viewRe := regexp.MustCompile(`(?i)\b(?:CREATE|REPLACE)\s+(?:OR\s+REPLACE\s+)?VIEW\s+identity\.users_v\b`)
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if !strings.HasPrefix(en.Name(), "06") || !strings.HasSuffix(en.Name(), ".sql") {
			continue
		}
		raw, err := fs.ReadFile(migrations.FS, en.Name())
		if err != nil {
			t.Fatal(err)
		}
		if viewRe.Match(raw) {
			t.Errorf("%s tạo hay thay identity.users_v (view của M1, M6 chỉ đọc)", en.Name())
		}
	}

	user := e.s.User(t, e.a, "ten-a@elimaz.test", "active")
	if err := e.insertRun(t, e.a, "sales", "dashboard", "json", "queued", nil, nil, user); err != nil {
		t.Fatal(err)
	}
	const join = `SELECT count(*) FROM reporting.report_runs r JOIN identity.users_v u ON u.id = r.requested_by
		WHERE u.display_name <> ''`
	if n := e.appCount(t, e.a, join); n != 1 {
		t.Fatalf("tenant A nối lần chạy với người yêu cầu: %d dòng, muốn 1", n)
	}
	if n := e.appCount(t, e.b, join); n != 0 {
		t.Fatalf("tenant B thấy %d dòng", n)
	}
	if n := e.appCount(t, e.b, "SELECT count(*) FROM identity.users_v WHERE id = $1", user); n != 0 {
		t.Fatalf("tenant B thấy người của A qua users_v: %d", n)
	}
}

// Chỉ mục phục vụ báo cáo (G16 sau Task 223): ba chỉ mục 0603 thêm và hai chỉ mục có sẵn mà M6 dựa vào.
func TestReportingSchema_Indexes(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	want := map[string]string{
		"ord.order_status_history_report":   "(tenant_id, to_status, occurred_at)",
		"inventory.stock_ledger_created":    "(tenant_id, created_at)",
		"ord.returns_received":              "(tenant_id, received_at)",
		"fulfillment.shipments_created":     "(tenant_id, created_at)",
		"catalog.variants_sku_prefix":       "(tenant_id, seller_id, sku text_pattern_ops)",
		"reporting.report_runs_requester":   "(tenant_id, requested_by, created_at DESC, id DESC)",
		"reporting.report_runs_family":      "(tenant_id, family, created_at DESC, id DESC)",
		"reporting.report_runs_expiry":      "(tenant_id, expires_at) WHERE",
		"reporting.stock_snapshots_variant": "(tenant_id, day, seller_id, variant_id)",
	}
	for name, cols := range want {
		schema, index, _ := strings.Cut(name, ".")
		var def string
		err := s.Owner.Pool.QueryRow(t.Context(),
			"SELECT indexdef FROM pg_indexes WHERE schemaname = $1 AND indexname = $2", schema, index).Scan(&def)
		if err != nil {
			t.Errorf("thiếu chỉ mục %s: %v", name, err)
			continue
		}
		if !strings.Contains(def, cols) {
			t.Errorf("%s: %q không chứa %q", name, def, cols)
		}
	}
	// Chỉ mục hết hạn chỉ gồm các trạng thái còn phải dọn.
	var def string
	if err := s.Owner.Pool.QueryRow(t.Context(), `SELECT indexdef FROM pg_indexes WHERE schemaname = 'reporting'
		AND indexname = 'report_runs_expiry'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{"queued", "ready", "failed"} {
		if !strings.Contains(def, "'"+st+"'") {
			t.Errorf("report_runs_expiry thiếu trạng thái %s: %s", st, def)
		}
	}
}

// Transaction chỉ đọc của báo cáo: INSERT vào report_runs → 25006, không thử lại (hàm chạy đúng một lần).
func TestReportingSchema_ReadOnlyTxNoRetry(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	user := e.s.User(t, e.a, "ro@elimaz.test", "active")
	calls := 0
	ctx := t.Context()
	err := tenancy.WithTenant(ctx, e.s.App, e.a, func(tx pgx.Tx) error {
		calls++
		_, err := tx.Exec(ctx, strings.ReplaceAll(insertRunSQL, "$7", "$6"), "sales", "dashboard", "json", "queued",
			nil, user)
		return err //nolint:wrapcheck // test
	}, db.TxOptions{Settings: []db.Setting{{Name: "transaction_read_only", Value: "on"}}})
	if pgCode(err) != "25006" {
		t.Fatalf("ghi trong transaction chỉ đọc: muốn 25006, nhận %v", err)
	}
	if calls != 1 {
		t.Fatalf("hàm chạy %d lần, muốn 1 (25006 không phải lỗi tạm)", calls)
	}
}
