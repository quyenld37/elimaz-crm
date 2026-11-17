package tenancy_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// env là một database thử đã migrate: pool của crm_app và của chủ bảng.
type env struct {
	app      *db.DB
	owner    *pgxpool.Pool
	adminURL string
	appURL   string
}

func newEnv(t *testing.T, o db.PoolOptions) env {
	t.Helper()
	adminURL, ownerURL, appURL := testkit.StartPostgres(t)
	app, err := db.Open(t.Context(), appURL, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Pool.Close)
	owner, err := pgxpool.New(t.Context(), ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	return env{app: app, owner: owner, adminURL: adminURL, appURL: appURL}
}

// ownerExec chạy từng câu bằng chủ bảng.
func ownerExec(t *testing.T, owner *pgxpool.Pool, stmts ...string) {
	t.Helper()
	for _, q := range stmts {
		if _, err := owner.Exec(t.Context(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// probeRLS dựng probe.rls_parent và probe.rls_child (khóa ngoại ghép), bật RLS bằng platform.enable_tenant_rls.
func probeRLS(t *testing.T, owner *pgxpool.Pool) {
	t.Helper()
	ownerExec(t, owner,
		"CREATE SCHEMA probe",
		`CREATE TABLE probe.rls_parent (
			id        uuid PRIMARY KEY DEFAULT uuidv7(),
			tenant_id uuid NOT NULL,
			name      text NOT NULL,
			UNIQUE (tenant_id, id)
		)`,
		`CREATE TABLE probe.rls_child (
			id        uuid PRIMARY KEY DEFAULT uuidv7(),
			tenant_id uuid NOT NULL,
			parent_id uuid NOT NULL,
			UNIQUE (tenant_id, id),
			FOREIGN KEY (tenant_id, parent_id) REFERENCES probe.rls_parent (tenant_id, id)
		)`,
		"SELECT platform.enable_tenant_rls('probe.rls_parent')",
		"SELECT platform.enable_tenant_rls('probe.rls_child')",
		"GRANT USAGE ON SCHEMA probe TO crm_app",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA probe TO crm_app",
	)
}

func insertParent(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "INSERT INTO probe.rls_parent (tenant_id, name) VALUES ($1, $2) RETURNING id",
		tenantID, name).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("chèn probe.rls_parent: %w", err)
	}
	return id, nil
}

// seedParent chèn một dòng cha của tenant trong ngữ cảnh của chính tenant đó.
func seedParent(t *testing.T, d *db.DB, tenantID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tenancy.WithTenant(t.Context(), d, tenantID, func(tx pgx.Tx) error {
		var err error
		id, err = insertParent(t.Context(), tx, tenantID, name)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func parentNames(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
},
) ([]string, error) {
	rows, err := q.Query(ctx, "SELECT name FROM probe.rls_parent ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("đọc probe.rls_parent: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("đọc probe.rls_parent: %w", err)
	}
	return names, nil
}

func namesIn(t *testing.T, d *db.DB, tenantID uuid.UUID) []string {
	t.Helper()
	var names []string
	err := tenancy.WithTenant(t.Context(), d, tenantID, func(tx pgx.Tx) error {
		var err error
		names, err = parentNames(t.Context(), tx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestTenantsSeeOnlyOwnRows(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	probeRLS(t, e.owner)
	a, b := uuid.New(), uuid.New()
	seedParent(t, e.app, a, "a1")
	seedParent(t, e.app, a, "a2")
	seedParent(t, e.app, b, "b1")
	if got := namesIn(t, e.app, a); !slices.Equal(got, []string{"a1", "a2"}) {
		t.Fatalf("tenant A thấy %v, muốn [a1 a2]", got)
	}
	if got := namesIn(t, e.app, b); !slices.Equal(got, []string{"b1"}) {
		t.Fatalf("tenant B thấy %v, muốn [b1]", got)
	}
}

// Review Focus 3: kết nối duy nhất của pool được dùng lại sau WithTenant(A); câu ngoài ngữ cảnh trả 0 dòng, không lỗi
// "invalid input syntax for type uuid" (sau set_config(…, true) tham số còn lại là ” chứ không phải NULL).
func TestReusedConnectionOutsideContextSeesNothing(t *testing.T) {
	e := newEnv(t, db.PoolOptions{MaxConns: 1})
	probeRLS(t, e.owner)
	a := uuid.New()
	seedParent(t, e.app, a, "a1")
	if got := namesIn(t, e.app, a); len(got) != 1 {
		t.Fatalf("trong ngữ cảnh A thấy %v, muốn 1 dòng", got)
	}
	var setting *string
	if err := e.app.Pool.QueryRow(t.Context(), "SELECT current_setting('app.tenant_id', true)").Scan(&setting); err != nil {
		t.Fatal(err)
	}
	if setting != nil && *setting != "" {
		t.Fatalf("app.tenant_id rò ra ngoài transaction: %q", *setting)
	}
	rows, err := e.app.Pool.Query(t.Context(), "SELECT * FROM probe.rls_parent")
	if err != nil {
		t.Fatalf("câu ngoài ngữ cảnh phải không lỗi: %v", err)
	}
	n := 0
	for rows.Next() {
		n++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("câu ngoài ngữ cảnh phải không lỗi: %v", err)
	}
	if n != 0 {
		t.Fatalf("ngoài ngữ cảnh thấy %d dòng, muốn 0", n)
	}
}

func TestInsertForOtherTenantViolatesPolicy(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	probeRLS(t, e.owner)
	a, b := uuid.New(), uuid.New()
	err := tenancy.WithTenant(t.Context(), e.app, a, func(tx pgx.Tx) error {
		_, err := insertParent(t.Context(), tx, b, "lạc")
		return err
	})
	if code := sqlState(err); code != "42501" {
		t.Fatalf("err=%v (SQLSTATE %q), muốn 42501 vi phạm policy", err, code)
	}
	if got := namesIn(t, e.app, b); len(got) != 0 {
		t.Fatalf("tenant B có dòng lạc: %v", got)
	}
}

func TestChildPointingToOtherTenantParentViolatesForeignKey(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	probeRLS(t, e.owner)
	a, b := uuid.New(), uuid.New()
	parentA := seedParent(t, e.app, a, "a1")
	err := tenancy.WithTenant(t.Context(), e.app, b, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "INSERT INTO probe.rls_child (tenant_id, parent_id) VALUES ($1, $2)", b, parentA)
		if err != nil {
			return fmt.Errorf("chèn probe.rls_child: %w", err)
		}
		return nil
	})
	if code := sqlState(err); code != "23503" {
		t.Fatalf("err=%v (SQLSTATE %q), muốn 23503 vi phạm khóa ngoại", err, code)
	}
}

func TestWithTenantRejectsNilTenant(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	called := false
	err := tenancy.WithTenant(t.Context(), e.app, uuid.Nil, func(pgx.Tx) error { called = true; return nil })
	if !errors.Is(err, tenancy.ErrNoTenant) || called {
		t.Fatalf("err=%v called=%v, muốn ErrNoTenant và hàm không chạy", err, called)
	}
}

// Lần chạy lại (sau lỗi 40001) cũng có app.tenant_id, giới hạn và Settings của o[0].
func TestWithTenantSetsContextOnEveryAttempt(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	a := uuid.New()
	type seen struct{ tenant, lock, stmt, extra string }
	var got []seen
	o := db.TxOptions{
		LockTimeout: 1500 * time.Millisecond, StatementTimeout: 7 * time.Second,
		// Còn sức chứa: nếu WithTenant append thẳng vào mảng của bên gọi, phần tử thêm sẽ hiện ở callerSettings[:2].
		Settings: append(make([]db.Setting, 0, 4), db.Setting{Name: "app.probe", Value: "x"}),
	}
	callerSettings := o.Settings
	err := tenancy.WithTenant(t.Context(), e.app, a, func(tx pgx.Tx) error {
		var s seen
		err := tx.QueryRow(t.Context(), `SELECT current_setting('app.tenant_id'), current_setting('lock_timeout'),
			current_setting('statement_timeout'), current_setting('app.probe')`).Scan(&s.tenant, &s.lock, &s.stmt, &s.extra)
		if err != nil {
			return fmt.Errorf("đọc tham số: %w", err)
		}
		got = append(got, s)
		if len(got) == 1 {
			_, err := tx.Exec(t.Context(), "DO $$ BEGIN RAISE EXCEPTION 'thử' USING ERRCODE = '40001'; END $$")
			return fmt.Errorf("lỗi tạm thời giả: %w", err)
		}
		return nil
	}, o)
	if err != nil {
		t.Fatal(err)
	}
	want := seen{tenant: a.String(), lock: "1500ms", stmt: "7s", extra: "x"}
	if len(got) != 2 || got[0] != want || got[1] != want {
		t.Fatalf("tham số qua các lần chạy %+v, muốn 2 lần %+v", got, want)
	}
	if spill := callerSettings[:2]; spill[1] != (db.Setting{}) {
		t.Fatalf("WithTenant ghi vào mảng Settings của bên gọi: %+v", spill)
	}
}

func TestWithTenantExhaustedReturnsRetryLater(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	calls := 0
	err := tenancy.WithTenant(t.Context(), e.app, uuid.New(), func(tx pgx.Tx) error {
		calls++
		_, err := tx.Exec(t.Context(), "DO $$ BEGIN RAISE EXCEPTION 'thử' USING ERRCODE = '55P03'; END $$")
		return fmt.Errorf("lỗi tạm thời giả: %w", err)
	})
	if !errors.Is(err, db.ErrRetryLater) || calls != 4 {
		t.Fatalf("err=%v calls=%d, muốn ErrRetryLater sau 4 lần chạy", err, calls)
	}
}

func TestResolveAndListTenantIDs(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	var t1, t2 uuid.UUID
	if err := e.owner.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('shop 1') RETURNING id").Scan(&t1); err != nil {
		t.Fatal(err)
	}
	if err := e.owner.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('shop 2') RETURNING id").Scan(&t2); err != nil {
		t.Fatal(err)
	}
	if _, err := e.owner.Exec(t.Context(),
		"INSERT INTO platform.tenant_routes (kind, external_key, tenant_id) VALUES ('fb_page', 'p1', $1), ('fb_page', 'p2', $2)",
		t1, t2); err != nil {
		t.Fatal(err)
	}
	got, ok, err := tenancy.Resolve(t.Context(), e.app, "fb_page", "p1")
	if err != nil || !ok || got != t1 {
		t.Fatalf("Resolve(fb_page, p1) = %v, %v, %v; muốn %v, true, nil", got, ok, err, t1)
	}
	got, ok, err = tenancy.Resolve(t.Context(), e.app, "fb_page", "khong-co")
	if err != nil || ok || got != uuid.Nil {
		t.Fatalf("Resolve khóa không có = %v, %v, %v; muốn Nil, false, nil", got, ok, err)
	}
	if _, ok, _ := tenancy.Resolve(t.Context(), e.app, "staff_email", "p1"); ok {
		t.Fatal("Resolve phải so cả kind")
	}
	ids, err := tenancy.ListTenantIDs(t.Context(), e.app)
	want := []uuid.UUID{t1, t2}
	slices.SortFunc(want, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	if err != nil || !slices.Equal(ids, want) {
		t.Fatalf("ListTenantIDs = %v, %v; muốn %v", ids, err, want)
	}
}

// Thuộc tính (vùng rủi ro cao, 100 lượt): với dòng ngẫu nhiên của 1–5 tenant, mọi truy vấn trong WithTenant(x) chỉ
// trả dòng của x; ngoài ngữ cảnh 0 dòng.
func TestPropertyQueriesOnlySeeOwnTenant(t *testing.T) {
	e := newEnv(t, db.PoolOptions{MaxConns: 2})
	probeRLS(t, e.owner)
	rapid.Check(t, func(rt *rapid.T) {
		ownerExec(t, e.owner, "TRUNCATE probe.rls_child, probe.rls_parent")
		nTenants := rapid.IntRange(1, 5).Draw(rt, "tenants")
		tenants := make([]uuid.UUID, nTenants)
		own := map[uuid.UUID][]string{}
		for i := range tenants {
			tenants[i] = uuid.New()
		}
		nRows := rapid.IntRange(0, 12).Draw(rt, "rows")
		for i := range nRows {
			x := tenants[rapid.IntRange(0, nTenants-1).Draw(rt, "owner")]
			name := fmt.Sprintf("r%02d", i)
			seedParent(t, e.app, x, name)
			own[x] = append(own[x], name)
		}
		for _, x := range tenants {
			checkOnlyOwn(t, rt, e.app, x, own[x], nRows)
		}
		if got, err := parentNames(t.Context(), e.app.Pool); err != nil || len(got) != 0 {
			rt.Fatalf("ngoài ngữ cảnh thấy %v (err %v), muốn 0 dòng", got, err)
		}
	})
}

// checkOnlyOwn chạy bốn kiểu truy vấn trong WithTenant(x): đọc hết, đếm, lọc bằng điều kiện khớp dòng của tenant khác, nối cha con
// sau khi chèn con cho mỗi cha thấy được. Kết quả phải đúng tập dòng của x.
func checkOnlyOwn(t *testing.T, rt *rapid.T, d *db.DB, x uuid.UUID, want []string, total int) {
	t.Helper()
	var names []string
	var count, byID, joined int
	if err := d.Pool.QueryRow(t.Context(), "SELECT count(*) FROM probe.rls_parent").Scan(&count); err != nil || count != 0 {
		rt.Fatalf("ngoài ngữ cảnh đếm %d (err %v)", count, err)
	}
	err := tenancy.WithTenant(t.Context(), d, x, func(tx pgx.Tx) error {
		var err error
		if names, err = parentNames(t.Context(), tx); err != nil {
			return err
		}
		if err := tx.QueryRow(t.Context(), "SELECT count(*) FROM probe.rls_parent").Scan(&count); err != nil {
			return fmt.Errorf("đếm: %w", err)
		}
		// Điều kiện khớp mọi dòng của tenant khác nếu RLS không lọc.
		if err := tx.QueryRow(t.Context(),
			"SELECT count(*) FROM probe.rls_parent WHERE id = ANY(SELECT id FROM probe.rls_parent) OR tenant_id <> $1",
			x).Scan(&byID); err != nil {
			return fmt.Errorf("lọc theo id: %w", err)
		}
		if _, err := tx.Exec(t.Context(),
			"INSERT INTO probe.rls_child (tenant_id, parent_id) SELECT tenant_id, id FROM probe.rls_parent"); err != nil {
			return fmt.Errorf("chèn con: %w", err)
		}
		if err := tx.QueryRow(t.Context(),
			"SELECT count(*) FROM probe.rls_child c JOIN probe.rls_parent p ON p.id = c.parent_id").Scan(&joined); err != nil {
			return fmt.Errorf("nối cha con: %w", err)
		}
		return nil
	})
	if err != nil {
		rt.Fatal(err)
	}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		rt.Fatalf("tenant %v thấy %v, muốn %v (tổng %d dòng)", x, names, want, total)
	}
	if count != len(want) || byID != len(want) || joined != len(want) {
		rt.Fatalf("tenant %v: đếm %d, lọc %d, nối %d; muốn đều %d", x, count, byID, joined, len(want))
	}
}
