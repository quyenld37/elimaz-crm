# Integration Testing

> Bản dự án ELIMAZ CRM. Bản gốc dùng docker-compose, `database/sql`, testify `suite` và tệp `schema.sql` riêng; dự án thay bằng testcontainers-go, pgx v5, migration goose thật và `testing` chuẩn (ADR-44). Chi tiết thay đổi: `../SOURCE.md`.

## Table of Contents

- [PostgreSQL Container Fixture](#postgresql-container-fixture)
- [Schema Comes From the Real Migrations](#schema-comes-from-the-real-migrations)
- [Test Data Fixture](#test-data-fixture)
- [Using Fixtures in Tests](#using-fixtures-in-tests)

## PostgreSQL Container Fixture

Start a real PostgreSQL 18 per test package with testcontainers-go. The container lifetime is owned by the test (`testcontainers.CleanupContainer`), so nothing is left running:

```go
//go:build integration

package testdb

import (
    "context"
    "testing"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// New starts PostgreSQL 18, applies the goose migrations and returns a pool.
func New(t *testing.T) *pgxpool.Pool {
    t.Helper()
    ctx := t.Context()

    ctr, err := postgres.Run(ctx, "postgres:18-alpine",
        postgres.WithDatabase("crm_test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        postgres.BasicWaitStrategies(),
    )
    testcontainers.CleanupContainer(t, ctr)
    if err != nil {
        t.Fatalf("starting postgres: %v", err)
    }

    dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
    if err != nil {
        t.Fatalf("connection string: %v", err)
    }

    migrate(t, ctx, dsn) // see next section

    pool, err := pgxpool.New(ctx, dsn) // default exec mode; never the simple protocol
    if err != nil {
        t.Fatalf("connecting: %v", err)
    }
    t.Cleanup(pool.Close)
    return pool
}
```

No `time.Sleep` waiting for the database: the wait strategy blocks until PostgreSQL accepts connections.

## Schema Comes From the Real Migrations

Do not keep a separate `schema.sql` for tests: apply the same goose migrations that production uses, so tests exercise the real schema, indexes, RLS policies and roles.

```go
import (
    "context"
    "testing"

    "github.com/jackc/pgx/v5/stdlib"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/pressly/goose/v3"
)

func migrate(t *testing.T, ctx context.Context, dsn string) {
    t.Helper()
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil {
        t.Fatalf("connecting for migrations: %v", err)
    }
    defer pool.Close()

    db := stdlib.OpenDBFromPool(pool) // goose needs *sql.DB
    defer db.Close()

    provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationsFS) // migrationsFS: embed.FS của dự án
    if err != nil {
        t.Fatalf("goose provider: %v", err)
    }
    if _, err := provider.Up(ctx); err != nil {
        t.Fatalf("applying migrations: %v", err)
    }
}
```

Dự án: migration chạy bằng role chủ bảng; test kiểm RLS phải nối lại bằng role ứng dụng (`crm_app`) và đặt tenant như code thật (`tenancy.WithTenant`).

## Test Data Fixture

Keep test data next to the test, e.g. `pkg/myfeature/testdata/testdata.sql`, or insert it through the module's own public API (preferred: it keeps fixtures valid when the schema changes):

```sql
INSERT INTO users (name, email) VALUES
    ('Alice Johnson', 'alice@example.com'),
    ('Bob Smith', 'bob@example.com'),
    ('Charlie Brown', 'charlie@example.com');

INSERT INTO orders (user_id, amount, status) VALUES
    (1, 100000, 'completed'),
    (1, 50000, 'pending'),
    (2, 200000, 'completed');
```

(Tiền là `int64` đồng, không dùng `DECIMAL`/`float64`.)

## Using Fixtures in Tests

```go
//go:build integration

package database_test

import (
    "os"
    "testing"

    "github.com/jackc/pgx/v5/pgxpool"
)

func loadTestData(t *testing.T, pool *pgxpool.Pool) {
    t.Helper()
    data, err := os.ReadFile("testdata/testdata.sql")
    if err != nil {
        t.Fatalf("reading test data: %v", err)
    }
    // pgx sends an argument-free Exec as one simple-query message, so several statements are allowed here.
    // That is not the banned pool setting QueryExecModeSimpleProtocol (which interpolates arguments client-side).
    if _, err := pool.Exec(t.Context(), string(data)); err != nil {
        t.Fatalf("loading test data: %v", err)
    }
}

func TestUserCount(t *testing.T) {
    pool := testdb.New(t)
    loadTestData(t, pool)

    var count int
    if err := pool.QueryRow(t.Context(), "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
        t.Fatalf("counting users: %v", err)
    }
    if count != 3 {
        t.Errorf("count = %d, want 3", count)
    }
}

func TestOrderSum(t *testing.T) {
    pool := testdb.New(t)
    loadTestData(t, pool)

    var sum int64
    if err := pool.QueryRow(t.Context(), "SELECT SUM(amount) FROM orders").Scan(&sum); err != nil {
        t.Fatalf("summing orders: %v", err)
    }
    if sum != 350000 {
        t.Errorf("sum = %d, want 350000", sum)
    }
}
```

Ghi chú: câu SQL trong ví dụ là SQL viết thẳng trong test để đọc kết quả; SQL của sản phẩm vẫn chỉ nằm trong `queries/**/*.sql` (sqlc) hoặc tệp `*_dynsql.go`. One container per test is simple but slow; a package-level `TestMain` that starts one container and gives each test its own database (`CREATE DATABASE ... TEMPLATE ...`) is the usual speed-up — keep it inside the test package, not in a mutable global outside tests.
