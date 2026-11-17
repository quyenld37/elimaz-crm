# Nguồn: golang-database

- Repo: https://github.com/samber/cc-skills-golang
- Commit: `d68d80aec298e1b74b37c2a472c002b5217248b3`
- Thư mục gốc: `skills/golang-database/` (bỏ `evals/`)
- Giấy phép: MIT (tác giả samber).
- Lấy ngày 15/11/2026 bằng `gh api`; không chạy script nào.

## Tệp đã chép

| Tệp | Gốc (byte) | Bản dự án (byte) |
|---|---|---|
| SKILL.md | 11.564 | 12.277 (viết lại phần lớn) |
| references/performance.md | 9.254 | 8.272 |
| references/scanning.md | 2.248 | 2.261 (viết lại) |
| references/testing.md | 6.678 | 5.181 (viết lại) |
| references/transactions.md | 1.504 | 2.559 (viết lại) |

Description: 471 → 478 ký tự. Bản gốc nhắm tới PostgreSQL, MariaDB, MySQL, SQLite với `database/sql`/sqlx/pgx; bản dự án chỉ PostgreSQL + pgx v5 + sqlc + goose.

## Sửa và bỏ (lý do)

SKILL.md
1. Frontmatter: bỏ `allowed-tools`, `compatibility`, `metadata.openclaw`; thêm `**/*.sql` vào `paths`.
2. Write/Review mode: bỏ "launch a background agent / sub-agent"; giữ việc tìm mẫu sẵn có trong `queries/**/*.sql`.
3. "Use sqlx or pgx … never an ORM" → pgx v5 + sqlc + goose; sqlx, gorm, ent, bun, squirrel ghi "Not used" (ADR-44).
4. Best practices viết lại theo dự án: thêm luật SQL tĩnh qua sqlc / SQL dựng lúc chạy chỉ trong `*_dynsql.go` có test PostgreSQL thật; cấm simple protocol; `sql.ErrNoRows` → `pgx.ErrNoRows`; `BeginTxx` → `pool.BeginTx`/`pgx.BeginTxFunc`; thêm `SKIP LOCKED`; pool `SetMaxOpenConns…` → `pgxpool.Config`.
5. Mục 12 gốc "Use golang-migrate or Flyway, never hand-rolled or AI-generated migration SQL" → goose, tệp SQL, mở rộng rồi thu hẹp, không sửa migration đã commit, dòng `-- contract:`. Lý do: ADR-44 chọn goose; dự án viết migration bằng tay/agent có người duyệt.
6. Mục 14 gốc "Never create or modify database schemas" → "Schema changes are written as migrations and reviewed by a human; never run DDL directly". Lý do: dự án có migration do agent viết theo spec.
7. Mục 15 gốc "do not rely on … row-level security" và mục "Avoid Hidden SQL Features" → RLS + `tenancy.WithTenant` + `set_config(..., true)` là một phần thiết kế, có test trên PostgreSQL thật. Lý do: **mâu thuẫn trực tiếp** với spec (RLS theo tenant).
8. Bảng Library Choice viết lại theo ADR-44.
9. Ví dụ `db.GetContext`, `sqlx.In` + `Rebind`, MySQL `?` → sqlc + `= ANY($1::bigint[])`.
10. Ví dụ cột động: thêm luật `*_dynsql.go`; map allowlist toàn cục → hàm `switch` (không biến gói).
11. Ví dụ `GetUser(id string)` không có ctx (dùng `ctx` không khai báo) → method có `ctx` đầu, `pgx.ErrNoRows`.
12. Bảng lỗi: thêm cách nhận `*pgconn.PgError` mã `23505`, `40001`, `40P01`.
13. Connection Pool → `pgxpool.ParseConfig`; ghi chú PgBouncer ≥ 1.21 (ADR-44).
14. Migrations: bỏ golang-migrate, Flyway, Atlas → goose (`-- +goose NO TRANSACTION` cho `CREATE INDEX CONCURRENTLY`).
15. "Schema Creation: This skill does NOT cover schema creation" → "Schema Design follows the spec", tiền `int64` đồng.
16. Cross-References → skill đã cài; References: bỏ database/sql tutorial, sqlx, golang-migrate; thêm sqlc, goose.

references/performance.md
17. `sqlx.Connect` + `SetMaxOpenConns…` → `pgxpool`; `db.Stats()` → `pool.Stat()`.
18. Bỏ collector Prometheus (đăng ký trong `init()`, dùng biến `mainDB`/`replicaDB` toàn cục) → gauge OpenTelemetry đăng ký trong `main`. Lý do: không `init()`, không biến toàn cục, số đo qua OpenTelemetry (ADR-38).
19. "Batch INSERT with sqlx" (`NamedExecContext`) → `pgx.Batch` theo khối 500 (`slices.Chunk`); giữ `CopyFrom`.
20. "Use SQL MCP to check existing indexes" → đọc migration trước, chỉ truy vấn catalog trên database dev, đọc-only. Đổi chỉ mục = migration goose mới có người duyệt.
21. `WHERE id IN (...)` → `= ANY($1)`. Bỏ trỏ skill golang-observability và `samber/cc-skills@promql-cli`.

references/scanning.md — viết lại: bỏ sqlx (`db:` tag + `GetContext`/`SelectContext`); sqlc sinh struct; `pgx.CollectRows` chỉ cho `*_dynsql.go`; `sql.NullString` → `pgtype.Text`; struct database không mang tag `json` (kiểu API do ogen sinh).

references/testing.md — viết lại: `testify/mock` → fake viết tay; **bỏ go-sqlmock** (không kiểm SQL trên schema thật, trái luật dự án); testify `suite` + sqlx → test `testing` chuẩn trên PostgreSQL 18 (testcontainers-go), transaction rollback mỗi test kèm cảnh báo giới hạn (khóa, `SKIP LOCKED`, RLS cần commit/kết nối thứ hai); bảng "What to Test" thêm RLS và khóa; luật "Unit tests MUST use mocks (… or sqlmock)" → "mọi câu SQL test trên PostgreSQL thật".

references/transactions.md — viết lại bằng pgx v5 (`pool.Begin`, `defer tx.Rollback(ctx)`, `pgx.BeginTxFunc`, `pgx.TxOptions{IsoLevel: pgx.Serializable}`); thêm `tenancy.WithTenant`; thêm `FOR UPDATE SKIP LOCKED` và lời nhắc test khóa trên database thật.

## Ghi chú

- Không thấy script, hook, lệnh mạng hay telemetry. Lời khuyên "suggest, never execute DDL/indexes" của bản gốc được giữ (hợp với luật duyệt migration).

- 17/11/2026: bỏ trường `paths:` khỏi frontmatter để skill được nạp sẵn vào agent `crm-implementer` (khai trong `skills:` của agent).
