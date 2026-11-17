package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

// openApp mở pool của crm_app trên một database thử mới (đã migrate) và trả kèm chuỗi kết nối của chủ bảng.
func openApp(t *testing.T, o db.PoolOptions) (d *db.DB, ownerURL string) {
	t.Helper()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	d, err := db.Open(t.Context(), appURL, o)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(d.Pool.Close)
	return d, ownerURL
}

// ownerConn nối bằng chuỗi url (thường là chủ bảng), đóng khi test xong.
func ownerConn(t *testing.T, url string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// showIn đọc một tham số của phiên trên kết nối q (pool hay transaction).
func showIn(t *testing.T, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, name string,
) string {
	t.Helper()
	var v string
	if err := q.QueryRow(t.Context(), "SHOW "+name).Scan(&v); err != nil {
		t.Fatalf("SHOW %s: %v", name, err)
	}
	return v
}

func showPool(t *testing.T, d *db.DB, name string) string {
	t.Helper()
	return showIn(t, d.Pool, name)
}

func TestOpenReadsInt8AsInt64(t *testing.T) {
	d, _ := openApp(t, db.PoolOptions{})
	var v any
	if err := d.Pool.QueryRow(t.Context(), "SELECT 150000::int8").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if got, ok := v.(int64); !ok || got != 150000 {
		t.Fatalf("SELECT 150000::int8 = %#v, muốn int64(150000)", v)
	}
	var sum int64
	if err := d.Pool.QueryRow(t.Context(), "SELECT $1::int8 + 30000", int64(150000)).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum != 180000 {
		t.Fatalf("150000 + 30000 = %d", sum)
	}
}

func TestOpenSetsConnectionLimits(t *testing.T) {
	d, _ := openApp(t, db.PoolOptions{})
	for name, want := range map[string]string{
		"statement_timeout":                   "30s",
		"idle_in_transaction_session_timeout": "1min",
		"lock_timeout":                        "5s",
	} {
		if got := showPool(t, d, name); got != want {
			t.Errorf("SHOW %s = %q, muốn %q", name, got, want)
		}
	}

	short, _ := openApp(t, db.PoolOptions{StatementTimeout: 100 * time.Millisecond})
	_, err := short.Pool.Exec(t.Context(), "SELECT pg_sleep(1)")
	if err == nil || !strings.Contains(err.Error(), "canceling statement due to statement timeout") {
		t.Fatalf("pg_sleep(1) với statement_timeout 100ms: %v", err)
	}
}

func TestAppRoleCannotCreateTable(t *testing.T) {
	d, _ := openApp(t, db.PoolOptions{})
	_, err := d.Pool.Exec(t.Context(), "CREATE TABLE x()")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("crm_app CREATE TABLE phải lỗi quyền 42501, nhận %v", err)
	}
}

func TestAppRoleReadsButCannotWriteTenants(t *testing.T) {
	d, _ := openApp(t, db.PoolOptions{})
	var n int64
	if err := d.Pool.QueryRow(t.Context(), "SELECT count(*) FROM platform.tenants").Scan(&n); err != nil {
		t.Fatalf("crm_app đọc platform.tenants: %v", err)
	}
	_, err := d.Pool.Exec(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('x')")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("crm_app ghi platform.tenants phải lỗi quyền, nhận %v", err)
	}
}
