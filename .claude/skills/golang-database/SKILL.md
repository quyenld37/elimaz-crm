---
name: golang-database
description: "Go database access for PostgreSQL with pgx v5 and sqlc — parameterized queries, scanning, NULLable columns, transactions, isolation levels, SELECT FOR UPDATE / SKIP LOCKED, pgxpool configuration, batch processing, context propagation, goose SQL migrations, and tests on real PostgreSQL. Use when writing, reviewing, or debugging Golang code that talks to PostgreSQL, or for database testing. ELIMAZ CRM copy: no ORM, no sqlx, no database/sql drivers, pgx simple protocol banned."
user-invocable: true
license: MIT
metadata:
  author: samber
  version: "1.3.2"
  curated-for: ELIMAZ CRM (see SOURCE.md)
---

**Persona:** You are a Go backend engineer who writes safe, explicit, and observable database code. You treat SQL as a first-class language — no ORMs, no magic — and you catch data integrity issues at the boundary, not deep in the application.

**Modes:**

- **Write mode** — generating new queries, repository functions, or transaction wrappers: first look for existing query patterns and naming conventions in `queries/**/*.sql` and the module, then follow the skill's sequential instructions.
- **Review/debug mode** — auditing or debugging existing database code: scan for unclosed `pgx.Rows`, un-parameterized queries, missing context propagation, and absent error checks while reading the business logic.

> **Thứ tự ưu tiên.** Luật dự án (CLAUDE.md, ADR-44, spec `04` mục 9) đứng trên skill này khi hai bên khác nhau.

# Go Database Best Practices (ELIMAZ CRM)

Dự án dùng **pgx v5** (`pgxpool`, ≥ 5.13.0) nối thẳng PostgreSQL 18, **sqlc** sinh mã Go có kiểu cho SQL tĩnh, **goose** (chỉ tệp SQL) cho migration. Không ORM (gorm, ent, bun), không sqlx, không `database/sql` + driver `lib/pq`.

When using pgx, sqlc or goose, refer to the library's official documentation for current API signatures.

## Best Practices Summary

1. **pgx + sqlc, not ORMs** — ORMs hide SQL, generate unpredictable queries, and make debugging harder
2. Queries MUST use parameterized placeholders (`$1`, `$2`) — NEVER concatenate user input into SQL strings
3. **Static SQL lives in `queries/**/*.sql`** and goes through sqlc (`sqlc vet` on real PostgreSQL in CI). **SQL built at runtime** (list filters, dynamic ORDER BY) lives only in `*_dynsql.go` files, each with a `*_dynsql_test.go` that runs on real PostgreSQL
4. **pgx simple protocol is banned** — never set `QueryExecModeSimpleProtocol` / `default_query_exec_mode=simple_protocol` / `prefer_simple_protocol`
5. Context MUST be passed to all database operations — every pgx and sqlc method takes `ctx` first
6. `pgx.ErrNoRows` MUST be handled explicitly — distinguish "not found" from real errors using `errors.Is`
7. Rows MUST be closed after iteration — `defer rows.Close()` immediately after `Query`, then check `rows.Err()`; or use `pgx.CollectRows`, which closes for you
8. Use `Exec` for statements that don't return rows
9. **Use transactions for multi-statement operations** — `pool.BeginTx` / `pgx.BeginTxFunc`, with `defer tx.Rollback(ctx)`
10. **Use `SELECT ... FOR UPDATE`** when reading data you intend to modify; `FOR UPDATE SKIP LOCKED` for queue-style claiming
11. **Set custom isolation levels** when default READ COMMITTED is insufficient (e.g., serializable for financial operations), and retry on serialization failure (`40001`)
12. **Handle NULLable columns** with pointer fields or `pgtype` types (sqlc chooses per its config)
13. Connection pool MUST be configured — `pgxpool.Config`: `MaxConns`, `MinConns`, `MaxConnLifetime`, `MaxConnIdleTime`, `HealthCheckPeriod`
14. **Migrations are goose SQL files** (`-- +goose Up/Down`), embedded in the binary, run by a separate migrate command with the table-owner role; expand-then-contract (spec `04` mục 9); committed migrations are never edited — add a new one; contracting statements need the `-- contract:` line
15. **Batch operations in reasonable sizes** — not row-by-row (too many round trips), not millions at once (locks and memory); `pgx.Batch` or `CopyFrom`
16. **Schema changes are written as migrations and reviewed by a human** — a schema that looks correct on toy data can create hotspots, lock contention, or missing indexes under real production load; never run DDL directly against a shared database
17. **Row-level security and tenant context are part of the design** — application queries run as role `crm_app` inside `tenancy.WithTenant`, which sets the tenant with `set_config(..., true)` in the transaction; RLS behaviour is covered by tests on real PostgreSQL. Avoid other hidden side effects (triggers, rules) unless the spec calls for them

## Library Choice

| Library | Status in this project |
| --- | --- |
| `pgx` v5 + `pgxpool` | **Driver** (ADR-44): COPY, LISTEN, arrays, batch, `BIGINT` as `int64` |
| `sqlc` | **Static SQL** → typed Go code (ADR-44) |
| `goose` | **Migrations**, SQL files only (ADR-44) |
| `database/sql` | Only where a tool requires `*sql.DB` (e.g. goose), via `pgx/v5/stdlib` |
| sqlx, GORM, ent, bun, squirrel | **Not used** |

**Why NOT ORMs:**

- Unpredictable query generation — N+1 problems you cannot see in code
- Magic hooks and callbacks (BeforeCreate, AfterUpdate) make debugging harder
- Schema migrations coupled to application code
- Learning the ORM API is harder than learning SQL, and the abstraction leaks

## Parameterized Queries

```sql
-- queries/users.sql (sqlc)
-- name: GetUserByEmail :one
SELECT id, name, email FROM users WHERE email = $1;
```

```go
// ✗ VERY BAD — SQL injection vulnerability
query := fmt.Sprintf("SELECT * FROM users WHERE email = '%s'", email)

// ✓ Good — sqlc-generated, parameterized
user, err := q.GetUserByEmail(ctx, email)
```

### Lists of values (instead of dynamic IN clauses)

PostgreSQL takes an array parameter, so no placeholder building is needed:

```sql
-- name: ListUsersByIDs :many
SELECT id, name, email FROM users WHERE id = ANY($1::bigint[]);
```

```go
users, err := q.ListUsersByIDs(ctx, ids) // ids []int64
```

### Dynamic column names

Never interpolate column names from user input. Use an allowlist — and, in this project, only inside a `*_dynsql.go` file with a real-PostgreSQL test:

```go
// users_dynsql.go
func sortColumn(sortKey string) (string, error) {
    switch sortKey { // allowlist; no package-level mutable map
    case "name":
        return "name", nil
    case "email":
        return "email", nil
    case "created":
        return "created_at", nil
    default:
        return "", fmt.Errorf("invalid sort column: %q", sortKey)
    }
}

func listUsersSQL(sortKey string) (string, error) {
    col, err := sortColumn(sortKey)
    if err != nil {
        return "", err
    }
    return "SELECT id, name, email FROM users ORDER BY " + col, nil
}
```

For more injection prevention patterns, see the `golang-security` skill (`references/injection.md`).

## Struct Scanning and NULLable Columns

sqlc generates the row structs and scanning. For `*_dynsql.go` queries use `pgx.CollectRows` with `pgx.RowToStructByName`. Handle NULLable columns with pointer fields (`*string`, `*time.Time`) or `pgtype` types. See [Scanning Reference](./references/scanning.md).

## Error Handling

```go
func (r *UserRepo) GetUser(ctx context.Context, id int64) (store.User, error) {
    user, err := r.q.GetUser(ctx, id)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return store.User{}, ErrUserNotFound // translate to domain error
        }
        return store.User{}, fmt.Errorf("querying user %d: %w", id, err)
    }
    return user, nil
}
```

or:

```go
func (r *UserRepo) FindUser(ctx context.Context, id int64) (u store.User, exists bool, err error) {
    user, err := r.q.GetUser(ctx, id)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return store.User{}, false, nil // "no user" is not a technical error, but a domain error
        }
        return store.User{}, false, fmt.Errorf("querying user %d: %w", id, err)
    }
    return user, true, nil
}
```

### Always close rows

```go
rows, err := pool.Query(ctx, "SELECT id, name FROM users") // only in *_dynsql.go; static SQL goes through sqlc
if err != nil {
    return fmt.Errorf("querying users: %w", err)
}
defer rows.Close() // returns the connection to the pool

for rows.Next() {
    // ...
}
if err := rows.Err(); err != nil { // always check after iteration
    return fmt.Errorf("iterating users: %w", err)
}
```

### Common database error patterns

| Error | How to detect | Action |
| --- | --- | --- |
| Row not found | `errors.Is(err, pgx.ErrNoRows)` | Return domain error |
| Unique constraint | `errors.As(err, &pgErr)` with `*pgconn.PgError`, `pgErr.Code == "23505"` | Return conflict error |
| Connection refused | `err != nil` on `pool.Ping(ctx)` | Fail fast (readiness fails), retry with backoff |
| Serialization failure | `pgErr.Code == "40001"` (or deadlock `40P01`) | Retry the entire transaction |
| Context canceled | `errors.Is(err, context.Canceled)` | Stop processing, propagate |

## Context Propagation

Every pgx and sqlc call takes `ctx` first, so deadlines and cancellation propagate as long as you pass the request's `ctx` (never `context.Background()` mid-request). For context patterns in depth, see the `golang-context` skill.

## Transactions, Isolation Levels, and Locking

For transaction patterns, isolation levels, `SELECT FOR UPDATE`, and locking variants, see [Transactions](./references/transactions.md).

## Connection Pool

```go
cfg, err := pgxpool.ParseConfig(dsn)
if err != nil {
    return fmt.Errorf("parsing database config: %w", err)
}
cfg.MaxConns = 25                       // limit total connections
cfg.MinConns = 5                        // keep warm connections ready
cfg.MaxConnLifetime = 30 * time.Minute  // recycle stale connections
cfg.MaxConnIdleTime = 5 * time.Minute   // close idle connections
// cfg.ConnConfig.DefaultQueryExecMode stays at the default — never simple protocol
pool, err := pgxpool.NewWithConfig(ctx, cfg)
```

Sau PgBouncer chế độ transaction (PgBouncer ≥ 1.21), làm theo tài liệu pgx về chạy sau pooler; vẫn không bật simple protocol. For sizing guidance, see [Database Performance](./references/performance.md).

## Migrations

Use goose with SQL files only. Schema changes require human review with understanding of data volumes, existing indexes, foreign keys, and production constraints.

```sql
-- +goose Up
ALTER TABLE orders ADD COLUMN note text;

-- +goose Down
ALTER TABLE orders DROP COLUMN note;
```

- Statements that cannot run in a transaction (`CREATE INDEX CONCURRENTLY`) go in a file marked `-- +goose NO TRANSACTION`.
- Migration SQL is versioned in source control, embedded in the binary (`embed`), and applied by the separate migrate command — not at application start.

## Schema Design

Schema design follows the spec (`docs/crm/specs/...`), not ad-hoc choices: data volumes, access patterns, query profiles, and business constraints are decided there. A wrong schema is often subtly wrong — missing indexes, incorrect column types (money is `int64` đồng), bad normalization, or missing constraints — so every migration is reviewed.

## Deep Dives

- **[Transactions](./references/transactions.md)** — Transaction boundaries, isolation levels, deadlock prevention, `SELECT FOR UPDATE`, `SKIP LOCKED`
- **[Testing Database Code](./references/testing.md)** — Fakes for business logic, real-PostgreSQL tests with testcontainers-go for every query
- **[Database Performance](./references/performance.md)** — Pool sizing, batch processing, indexing strategy, query optimization
- **[Struct Scanning](./references/scanning.md)** — sqlc types, NULLable column handling, JSON marshaling patterns

## Related skills in this project

- `golang-security` — SQL injection prevention patterns
- `golang-context` — context propagation to database operations
- `golang-error-handling` — database error wrapping patterns
- `golang-testing` — integration test patterns

## References

- [pgx](https://github.com/jackc/pgx)
- [sqlc](https://docs.sqlc.dev/)
- [goose](https://github.com/pressly/goose)
