# Database Performance

## Table of Contents

- [Connection Pool Sizing](#connection-pool-sizing)
  - [Configuration](#configuration)
  - [Monitoring](#monitoring)
  - [Pool Metrics with OpenTelemetry](#pool-metrics-with-opentelemetry)
- [Batch Processing](#batch-processing)
  - [Sweet spot: 100–1,000 rows per batch](#sweet-spot-1001000-rows-per-batch)
  - [Batch INSERT with pgx.Batch](#batch-insert-with-pgxbatch)
  - [Bulk INSERT with pgx (PostgreSQL COPY protocol)](#bulk-insert-with-pgx-postgresql-copy-protocol)
  - [Cursor-based pagination (avoid OFFSET)](#cursor-based-pagination-avoid-offset)
- [Indexing Strategy](#indexing-strategy)
  - [Use SQL MCP to check existing indexes](#use-sql-mcp-to-check-existing-indexes)
  - [When to suggest adding indexes](#when-to-suggest-adding-indexes)
  - [When to suggest removing indexes](#when-to-suggest-removing-indexes)
- [Query Performance Tips](#query-performance-tips)

## Connection Pool Sizing

### Configuration

```go
cfg, err := pgxpool.ParseConfig(dsn)
if err != nil {
    return fmt.Errorf("parsing database config: %w", err)
}
cfg.MaxConns = 25                      // total connections (match your DB capacity)
cfg.MinConns = 5                       // keep connections warm, reduce handshake overhead
cfg.MaxConnLifetime = 30 * time.Minute // recycle connections (DNS changes, server restarts)
cfg.MaxConnIdleTime = 5 * time.Minute  // release idle connections
pool, err := pgxpool.NewWithConfig(ctx, cfg)
if err != nil {
    return fmt.Errorf("connecting to database: %w", err)
}
```

| Setting | Too low | Too high |
| --- | --- | --- |
| `MaxConns` | Requests queue waiting for conn | DB overwhelmed, context switches |
| `MinConns` | Cold connections, slow queries | Wasted memory holding idle conns |
| `MaxConnLifetime` | Frequent reconnection overhead | Stale connections after failover |
| `MaxConnIdleTime` | Same as MinConns too low | Idle conns consume server memory |

### Monitoring

Check pool stats in production to detect exhaustion:

```go
stats := pool.Stat()
slog.InfoContext(ctx, "db pool",
    "total", stats.TotalConns(),
    "acquired", stats.AcquiredConns(),
    "idle", stats.IdleConns(),
    "empty_acquire_count", stats.EmptyAcquireCount(), // acquires that had to wait for a connection
    "acquire_duration", stats.AcquireDuration(),      // total time spent acquiring
)
```

If `EmptyAcquireCount` keeps climbing, increase `MaxConns` or optimize slow queries.

### Pool Metrics with OpenTelemetry

Dự án không dùng Prometheus client và không `init()`; số đo đi qua OpenTelemetry (ADR-38). Register an observable gauge in `main`, reading `pool.Stat()` on each collection (no background goroutine):

```go
func registerPoolMetrics(meter metric.Meter, pool *pgxpool.Pool) error {
    acquired, err := meter.Int64ObservableGauge("db.pool.acquired_connections")
    if err != nil {
        return fmt.Errorf("creating gauge: %w", err)
    }
    idle, err := meter.Int64ObservableGauge("db.pool.idle_connections")
    if err != nil {
        return fmt.Errorf("creating gauge: %w", err)
    }
    _, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
        s := pool.Stat()
        o.ObserveInt64(acquired, int64(s.AcquiredConns()))
        o.ObserveInt64(idle, int64(s.IdleConns()))
        return nil
    }, acquired, idle)
    if err != nil {
        return fmt.Errorf("registering pool metrics: %w", err)
    }
    return nil
}
```

(otelpgx may also provide pool metrics; check its current API before writing your own.)

**Alert thresholds:**

- Acquired connections approaching `MaxConns` → risk of request queuing
- Empty-acquire count climbing steadily → pool is exhausted, increase `MaxConns`
- Idle connections too high → reduce `MinConns` or lower `MaxConnIdleTime`

## Batch Processing

Avoid two extremes:

- **Row-by-row** — N round trips for N rows, extremely slow
- **One giant batch** — locks tables, consumes memory, can timeout and block other queries

### Sweet spot: 100–1,000 rows per batch

Adjust based on row size and database load. Larger rows → smaller batches.

### Batch INSERT with pgx.Batch

sqlc can generate batch queries (`:batchexec`) and `:copyfrom`; prefer those for static SQL. With pgx directly:

```go
func insertUsersBatch(ctx context.Context, tx pgx.Tx, users []User) error {
    const batchSize = 500
    for chunk := range slices.Chunk(users, batchSize) {
        b := &pgx.Batch{}
        for _, u := range chunk {
            b.Queue(`INSERT INTO users (name, email) VALUES ($1, $2)`, u.Name, u.Email)
        }
        if err := tx.SendBatch(ctx, b).Close(); err != nil { // one round trip per chunk
            return fmt.Errorf("inserting users batch: %w", err)
        }
    }
    return nil
}
```

### Bulk INSERT with pgx (PostgreSQL COPY protocol)

For maximum throughput on PostgreSQL, use `pgx.CopyFrom` which uses the binary COPY protocol — significantly faster than multi-row INSERT:

```go
rows := make([][]any, len(users))
for i, u := range users {
    rows[i] = []any{u.Name, u.Email}
}
_, err := pool.CopyFrom(ctx,
    pgx.Identifier{"users"},
    []string{"name", "email"},
    pgx.CopyFromRows(rows),
)
```

### Cursor-based pagination (avoid OFFSET)

For reading large datasets, use cursor-based pagination instead of `OFFSET`. OFFSET re-scans skipped rows, getting slower as you paginate deeper:

```go
// ✗ Bad — OFFSET re-scans rows, O(offset + limit)
SELECT * FROM events ORDER BY created_at LIMIT 100 OFFSET 10000

// ✓ Good — cursor-based, O(limit) regardless of depth
SELECT * FROM events WHERE created_at > $1 ORDER BY created_at LIMIT 100
```

## Indexing Strategy

**Never create or drop indexes yourself.** Index changes affect production query performance and write throughput. Always suggest to the developer and let them decide.

### Check existing indexes

Read the migrations first. On a local dev database (never production) you can also query the catalog read-only:

```sql
-- PostgreSQL: list indexes on a table
SELECT indexname, indexdef
FROM pg_indexes
WHERE tablename = 'users';

-- Check for unused indexes (low scan count relative to writes)
SELECT schemaname, relname, indexrelname, idx_scan, idx_tup_read
FROM pg_stat_user_indexes
WHERE idx_scan < 10
ORDER BY idx_scan;
```

### When to suggest adding indexes

- Foreign key columns (PostgreSQL does NOT auto-index foreign keys)
- Columns frequently used in `WHERE`, `JOIN`, or `ORDER BY`
- Composite indexes for multi-column queries (leftmost column is most selective)
- Partial indexes for filtered queries (`WHERE active = true`)

### When to suggest removing indexes

- Indexes with near-zero `idx_scan` count (nobody reads them)
- Duplicate indexes (same columns in same order)
- Indexes on write-heavy tables that slow down INSERT/UPDATE/DELETE
- Wide composite indexes where a narrower one would suffice

Always present findings as suggestions with data (scan counts, table size); an index change is a new goose migration reviewed by a human (`CREATE INDEX CONCURRENTLY` in a `-- +goose NO TRANSACTION` file), never DDL run by hand.

## Query Performance Tips

- **`EXPLAIN ANALYZE`** before optimizing — measure, don't guess
- **List columns explicitly** — avoid `SELECT *`, it fetches unnecessary data and breaks struct scanning when schema changes
- **Use `LIMIT`** for pagination, always with an `ORDER BY`
- **Prefer `EXISTS` over `COUNT`** for existence checks — `EXISTS` stops at the first match
- **Avoid N+1 queries** — use `JOIN` or `WHERE id = ANY($1)` instead of querying in a loop
- **Suggest improvements, never execute them** — performance changes (indexes, query rewrites, configuration) need human review in context of production data and workload patterns

**Rules:**

- Batch operations SHOULD use 100–1,000 rows per batch — adjust based on row size and database load.
- Cursor-based pagination MUST replace `OFFSET` for large datasets — the cursor column MUST be chosen based on actual indexes (e.g., `created_at`, `user_id`).
- NEVER create indexes blindly — check existing indexes, measure with `EXPLAIN ANALYZE`, and present findings as suggestions.
- N+1 queries MUST be eliminated — use `JOIN` or `WHERE id = ANY($1)`.
