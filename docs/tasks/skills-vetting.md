# Soát skill bên thứ ba trước khi chép vào CRM (15/11/2026)

**Skill** là bộ hướng dẫn Claude Code tự mở khi làm đúng loại việc. Tên và mô tả (description) của mọi skill được nạp vào **mỗi phiên**; phần thân chỉ được đọc khi skill được dùng.

Nguồn đã soát (lấy bằng `gh api` ở đúng commit, không chạy script nào, không `go install`):

| Nguồn | Commit | Giấy phép | Skill lấy |
|---|---|---|---|
| JetBrains/go-modern-guidelines | `155dc7ca10da5e1f6c841503086957b1b37f5815` | Apache-2.0 | `use-modern-go` |
| samber/cc-skills-golang | `d68d80aec298e1b74b37c2a472c002b5217248b3` | MIT | `golang-error-handling`, `golang-testing`, `golang-concurrency`, `golang-context`, `golang-database`, `golang-security` (bỏ thư mục `evals/`) |

Bản đã sửa nằm ở `apps/crm/.claude/skills/<tên>/`, giữ cấu trúc `SKILL.md` + `references/`. Mỗi thư mục có `SOURCE.md` ghi nguồn, danh sách tệp, **từng chỗ sửa kèm lý do**, kích thước trước/sau. Chuẩn đối chiếu: bảng "Bộ dựng cho từng việc" của ADR-44 và luật trong `apps/crm/CLAUDE.md`.

## Việc chung cho mọi skill samber

- Bỏ `allowed-tools` (cấp sẵn quyền Read/Edit/Write/Bash(go, git, golangci-lint)/Agent, riêng `golang-security` còn cấp `WebFetch`, `WebSearch`, `EnterWorktree`, `ExitWorktree`). Bỏ khối `metadata.openclaw` (có lệnh cài `…@latest`) và `compatibility`.
- Bỏ các dòng "Orchestration mode / Thinking mode … dùng `ultracode` / `ultrathink`" và lệnh tự mở 3–5 sub-agent song song: thành "chỉ chạy song song khi người dùng yêu cầu".
- "Community default" → "Luật dự án đứng trên skill này".
- Lời trỏ tới ~15 skill samber khác (chưa cài) → trỏ tới skill đã cài trong dự án.
- Ví dụ `database/sql`/sqlx → pgx v5/sqlc; ví dụ thiếu `ctx` → `ctx` tham số đầu; biến gói thay đổi được → trường struct; `init()` → dựng trong `main`; `go f()` không chủ → goroutine có chủ.

## Từng skill

| Skill | Nói về gì | Bỏ / sửa chính và vì sao |
|---|---|---|
| `use-modern-go` (JetBrains) | Cách viết Go đời mới tới Go 1.27: `range n`, `min/max`, `slices`/`maps`, `strings.Cut`, `errors.AsType`, `wg.Go`, `t.Context`, `b.Loop`, `omitzero`, mẫu `ServeMux`… | **Không chép script.** Script gốc lần đầu chạy `go install github.com/JetBrains/go-modern-guidelines@v0.1.1` (tải mã qua mạng, biên dịch, ghi vào `~/.cache`). SKILL.md viết lại: không gọi CLI, tra `references/FEATURES.md` (cùng nội dung CLI trả về), ghi rõ công cụ không được cài. Bỏ câu "hướng dẫn trả về là chuẩn, theo cả khi trái quy ước của repo" → luật dự án thắng. `time.Tick` vòng lặp không lối thoát → mẫu có `ctx.Done()`. `json_v2`: ghi chú JSON API do ogen sinh. |
| `golang-error-handling` | Tạo lỗi, bọc `%w`, `errors.Is/As/AsType/Join`, sentinel, custom error type, panic/recover, luật "log hoặc trả, không cả hai", log bằng slog | Bỏ **samber/oops** ở mọi chỗ (thư viện ngoài ADR-44, của chính tác giả skill) → custom error type + `slog.LogValuer`, vị trí lỗi qua `trace_id`. Bỏ link các gói `samber/slog-*`, `tint`. Thêm luật "bọc một lần ở mỗi tầng thêm ngữ cảnh". `sql.ErrNoRows` → `pgx.ErrNoRows`. |
| `golang-testing` | Test bảng, subtest, `t.Parallel`, fuzz, synctest, ví dụ, coverage, benchmark, integration test | Bỏ **testify** ở mọi ví dụ (assert, require, mock, suite), **goleak**, **clockwork**, **gotests** (kèm lệnh cài `@latest`). Thêm: testing chuẩn + **rapid** (property test) + testcontainers-go. `integration-testing.md` viết lại: docker-compose + `database/sql` + `time.Sleep` → PostgreSQL 18 bằng testcontainers-go, migration goose thật, pgx. `mocking.md` viết lại: fake viết tay, interface phía dùng, fixture là hàm (bản gốc là biến toàn cục trong gói `fixtures`). Tiền trong ví dụ `float64` → `int64`. |
| `golang-concurrency` | Vòng đời goroutine, channel, `select`, mutex, atomic, `sync.Map/Pool/Once`, WaitGroup, worker pool, pipeline | `errgroup`, `singleflight` (`golang.org/x/sync`, **không có trong ADR-44**) giữ lại nhưng gắn "cần duyệt", cách mặc định là WaitGroup + ctx + ô kết quả riêng. Bỏ **samber/ro**, **goleak**. Ví dụ biến gói (`requestCount`, `cache`, `bufPool`, `loadConfig`, `group`) → trường struct; `sync.Once` + `sql.Open` + `panic` → dựng trong `main`. |
| `golang-context` | Truyền `ctx`, hủy, timeout, deadline, giá trị trong ctx, `WithoutCancel`, ctx trong HTTP | Middleware tự đọc/ghi `X-Trace-ID` (tin header client) → **otelhttp** + `traceparent` (ADR-44). Ví dụ `go h.audit…(WithoutCancel(ctx))` trong handler (goroutine không chủ) → audit/job trong transaction, worker có chủ. `processItems` bỏ goroutine chạy tiếp → chờ hết. "main, init" → "main, tests". |
| `golang-database` | Truy vấn có tham số, scan, NULL, transaction, isolation, `FOR UPDATE`, pool, batch, migration, test DB | Viết lại phần lớn. Bỏ **sqlx**, `database/sql`, MySQL/SQLite, **golang-migrate/Flyway/Atlas**, **go-sqlmock**, **testify**, collector **Prometheus** đăng ký trong `init()`. Bản gốc có câu **"không dựa vào row-level security"** — trái trực tiếp với thiết kế RLS theo tenant → sửa thành RLS + `tenancy.WithTenant` + test PostgreSQL thật. "Không bao giờ tạo/sửa schema, không viết migration bằng AI" → migration goose do người duyệt, không chạy DDL tay. Thêm luật sqlc / `*_dynsql.go`, cấm simple protocol, `SKIP LOCKED`, `pgxpool.Config`. |
| `golang-security` | Injection, mật mã, path traversal, SSRF, header, cookie, bí mật, an toàn bộ nhớ, PII trong log, STRIDE/DREAD | Bỏ chế độ Audit "mỗi chỗ sửa một worktree, một PR" (agent tự tạo worktree/PR). Bỏ `go install govulncheck@latest`, `go get -tool gosec@latest` (sửa `go.mod`) → govulncheck/golangci-lint đã ghim trong `make ci`. **bcrypt/scrypt/PBKDF2** → chỉ argon2id (ADR-35/44). `golang.org/x/time/rate` → bộ đếm Valkey + giới hạn in-flight. Bỏ **gorilla/sessions**. `oauth2` gắn "ngoài ADR-44". Sửa ba ví dụ "Good" tự mâu thuẫn: log rồi vẫn trả lỗi (`logging.md`), dùng `unsafe` (`memory-safety.md`), `go http.ListenAndServe` bỏ lỗi (`network.md`). `hashEmail` SHA-256 không muối → HMAC có khóa. `r.Use(...)` kiểu chi → bọc `ServeMux`. |

## Điều đáng ngờ đã thấy

Không thấy mã độc, không thấy hook, không thấy telemetry, không thấy ký tự ẩn hay chữ giấu (đã quét ký tự zero-width/bidi và comment HTML), không có câu nào nhắm vào agent kiểu "bỏ qua chỉ dẫn", "đừng hỏi". Có những điểm cần biết:

1. **JetBrains — script tự cài.** Lần đầu dùng, `run-tool.sh` tải và biên dịch CLI qua mạng rồi ghi vào `~/.cache`. Phiên bản có ghim (`v0.1.1`) và có kiểm `--version`. Ngoài ra, nếu biến môi trường `GO_MODERN_GUIDELINES_DEV` có mặt, script chạy bất kỳ tệp nào nằm ở `~/.cache/go-modern-guidelines/dev/`. Mã của chính CLI **chưa được xem**. Đã không chép script.
2. **samber — cấp quyền sẵn.** `allowed-tools` cho phép dùng công cụ mà không hỏi; `golang-security` cấp cả truy cập mạng (`WebFetch`, `WebSearch`) và tạo worktree. Đã bỏ hết.
3. **samber — lệnh đổi hành vi agent.** Từ khóa `ultrathink`/`ultracode`, tự mở tới 5 sub-agent, tự tạo một worktree và một PR cho mỗi chỗ sửa. Đã bỏ.
4. **samber — lệnh cài không ghim.** `go install …@latest`, `go get -tool …@latest` (sửa `go.mod`), khối `openclaw.install`. Đã bỏ.
5. **samber — quảng bá thư viện của tác giả** (samber/oops, samber/ro, samber/slog-*). Không độc hại, nhưng đẩy dự án sang phụ thuộc ngoài ADR-44. Đã bỏ.
6. **Khẳng định chưa kiểm về Go 1.27** ở cả hai nguồn (`synctest.Sleep`, `httptest.NewTestServer`, `stdversion` tự chạy, profile `goroutineleak`, gói `uuid` chuẩn, `encoding/json/v2`, generic method…). Giữ lại, gắn "chưa xác minh với release notes Go 1.27".

## Mô tả nạp vào mỗi phiên

| Skill | Ký tự description | Tên + description |
|---|---|---|
| use-modern-go | 366 | 379 |
| golang-error-handling | 374 | 395 |
| golang-testing | 472 | 486 |
| golang-concurrency | 667 | 685 |
| golang-context | 470 | 484 |
| golang-database | 478 | 493 |
| golang-security | 633 | 648 |
| **Tổng** | **3.460** | **3.570** |

Mỗi description dưới giới hạn 1.536 ký tự (dài nhất 667). Bản gốc tổng 3.701 ký tự. Mọi skill có `paths: **/*.go` (database thêm `**/*.sql`); nếu Claude Code chỉ đưa skill có `paths` vào khi chạm tệp khớp thì phiên không đụng Go sẽ không tốn phần này (chưa kiểm hành vi đó).

## Việc phát hiện thêm (chưa làm, cần chủ shop quyết)

- `golang.org/x/sync` (errgroup, singleflight) không có trong ADR-44; skill đang ghi "cần duyệt". Nếu muốn dùng thì ghi vào `docs/phu-thuoc.md`.
- Chưa có công cụ thay goleak để bắt goroutine rò rỉ trong test; skill hướng dẫn dùng `testing/synctest` + test tắt êm + `-race`.
- Biến gói chỉ đọc (regex đã biên dịch `var emailRegex = regexp.MustCompile(...)` trong `golang-security/references/network.md`) được giữ; luật "không biến toàn cục thay đổi được" có tính loại này là ngoại lệ hay không thì chưa ghi rõ trong CLAUDE.md.
- Chưa commit (theo yêu cầu).
