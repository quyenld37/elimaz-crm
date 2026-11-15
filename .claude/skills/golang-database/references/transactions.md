# Transactions, Isolation Levels, and Locking

> Bản dự án ELIMAZ CRM: ví dụ đổi từ sqlx (`BeginTxx`, `GetContext`) sang pgx v5. Chi tiết: `../SOURCE.md`.

## Basic transaction pattern

```go
tx, err := pool.Begin(ctx) // default isolation (READ COMMITTED)
if err != nil {
    return fmt.Errorf("beginning transaction: %w", err)
}
defer tx.Rollback(ctx) // no-op (returns pgx.ErrTxClosed) if already committed

q := store.New(tx) // sqlc queries bound to the transaction
// ... execute queries using q / tx ...

if err := tx.Commit(ctx); err != nil {
    return fmt.Errorf("committing transaction: %w", err)
}
```

Or let pgx commit/rollback for you:

```go
err := pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
    return store.New(tx).MarkPaid(ctx, orderID)
})
```

Dự án: giao dịch chạm bảng có `tenant_id` đi qua `tenancy.WithTenant`, hàm này mở transaction và đặt tenant bằng `set_config(..., true)` (chỉ sống trong transaction đó); không tự gọi `set_config` ở chỗ khác.

## Custom isolation level

```go
tx, err := pool.BeginTx(ctx, pgx.TxOptions{
    IsoLevel: pgx.Serializable, // strongest guarantee
})
```

| Level | Use when |
| --- | --- |
| `pgx.ReadCommitted` | Default — good for most operations |
| `pgx.RepeatableRead` | Need consistent reads within a transaction |
| `pgx.Serializable` | Financial operations, inventory, anything with strict consistency — retry on `40001` |

## SELECT FOR UPDATE — prevent race conditions

```sql
-- name: LockAccountBalance :one
SELECT balance FROM accounts WHERE id = $1 FOR UPDATE;
```

```go
balance, err := store.New(tx).LockAccountBalance(ctx, accountID)
// Row is locked until tx.Commit(ctx) or tx.Rollback(ctx)
```

Use `FOR UPDATE` when you read a value, compute something from it, and then write it back. Without the lock, concurrent transactions can read stale data. A conditional `UPDATE ... WHERE stock >= $2 RETURNING ...` often does the same in one statement.

## Locking variants

| Clause | Effect |
| --- | --- |
| `FOR UPDATE` | Locks rows for write — other transactions block on same rows |
| `FOR UPDATE NOWAIT` | Same, but fails immediately instead of waiting |
| `FOR UPDATE SKIP LOCKED` | Skips rows locked by others — for queue-style claiming (e.g. jobs, flash-sale units) |
| `FOR SHARE` | Locks rows for read — prevents writes but allows other reads |

Lock behaviour (blocking, `SKIP LOCKED`, deadlocks) is only visible on a real database: test it with two connections against real PostgreSQL.
