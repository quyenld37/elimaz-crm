# Nguồn: golang-security

- Repo: https://github.com/samber/cc-skills-golang
- Commit: `d68d80aec298e1b74b37c2a472c002b5217248b3`
- Thư mục gốc: `skills/golang-security/` (bỏ `evals/`, 50 KB)
- Giấy phép: MIT (tác giả samber).
- Lấy ngày 15/11/2026 bằng `gh api`; không chạy script nào.

## Tệp đã chép

| Tệp | Gốc (byte) | Bản dự án (byte) |
|---|---|---|
| SKILL.md | 14.332 | 13.032 |
| references/architecture.md | 10.617 | 11.686 |
| references/checklist.md | 4.105 | 4.199 |
| references/cookies.md | 4.873 | 4.423 |
| references/cryptography.md | 11.231 | 10.787 |
| references/filesystem.md | 6.919 | 6.919 (không đổi) |
| references/injection.md | 7.479 | 7.770 |
| references/logging.md | 4.017 | 4.632 |
| references/memory-safety.md | 5.191 | 5.358 |
| references/network.md | 6.420 | 7.029 |
| references/secrets.md | 4.894 | 4.810 |
| references/third-party.md | 5.057 | 5.655 |
| references/threat-modeling.md | 8.641 | 8.851 |

Description: 818 → 633 ký tự.

## Sửa và bỏ (lý do)

SKILL.md
1. Frontmatter: bỏ `allowed-tools` — bản gốc cấp nhiều quyền nhất trong bộ: `WebFetch`, `WebSearch`, `EnterWorktree`, `ExitWorktree`, `Agent`, `Bash(govulncheck:*)`, `Bash(git:*)`… Bỏ `metadata.openclaw.install` (cài `golang.org/x/vuln/cmd/govulncheck@latest`) và `compatibility`.
2. Description: bỏ "`gosec` SAST" và ba lời trỏ skill samber chưa cài.
3. Bỏ "`ultrathink`" và "Orchestration mode … `ultracode`".
4. Audit mode: bỏ "apply each fix in its own isolated worktree … one PR" (agent tự tạo worktree và PR) → báo phát hiện, sửa trong task được giao, sub-agent song song chỉ khi người dùng yêu cầu. Lý do: đổi hành vi agent, trái luật "không push, chỉ làm task được giao".
5. Coding mode: bỏ "launch a background agent".
6. Bỏ mục "Dependencies: go install govulncheck@latest". Mục Tooling: bỏ `go get -tool github.com/securego/gosec/v2/cmd/gosec@latest` và `go get -tool govulncheck@latest` (sửa `go.mod`, phiên bản không ghim) → gosec qua golangci-lint, govulncheck đã ghim trong `make ci`. Bỏ trỏ skill golang-lint, golang-pkg-go-dev.
7. Bảng Quick Reference: SQL `database/sql` với `?` → sqlc + pgx `$1` + `*_dynsql.go`; rate limiting `golang.org/x/time/rate` → bộ đếm Valkey (ADR-39/40) + giới hạn in-flight.
8. Common Mistakes: "Argon2id or bcrypt" → chỉ argon2id; thêm ghi chú bind 0.0.0.0 trong container.
9. Cross-References → skill đã cài.

references
10. architecture.md: rate limiter `x/time/rate` toàn cục và map theo IP không giới hạn → middleware in-flight bằng channel + ghi chú Valkey, cảnh báo map không giới hạn và `X-Forwarded-For`; audit log bỏ IP; `golang.org/x/oauth2` ghi "ngoài ADR-44"; thêm cách xác thực của dự án (Cloudflare Access + golang-jwt, argon2id + TOTP, passkey sau); `validateJWT` dùng `publicKey` toàn cục → method của verifier tra khóa theo `kid`, `interface{}` → `any`; `var DefaultConfig` argon2 ("OWASP recommended" — chưa xác minh) → hàm trả thông số ví dụ, ghi chú lấy lại thông số OWASP và giới hạn số lần băm song song (ADR-44).
11. checklist.md: "ORM/library protects against SQL injection" → sqlc/`*_dynsql.go`; bỏ bcrypt; "No data races on global variables" → không có biến toàn cục thay đổi được.
12. cookies.md: bỏ mục gorilla/sessions (ngoài ADR-44).
13. cryptography.md: bỏ bcrypt, scrypt, PBKDF2 khỏi khuyến nghị băm mật khẩu → chỉ argon2id (ADR-35/44).
14. injection.md: ví dụ `db.Query` không ctx → `pool.Query(ctx, …)`; bỏ `sqlx.In`/`Rebind` và mục "Prefer sqlx or pgx" → `= ANY($1)`, luật sqlc + `*_dynsql.go`, lý do cấm simple protocol.
15. logging.md: ví dụ "Good" `handleDatabaseError` **vừa log vừa trả lỗi** (trái chính luật single-handling của bộ skill) → tầng trong chỉ trả lỗi có ngữ cảnh, mép HTTP log một lần; bỏ `username`, `ip_address` khỏi log mẫu; thêm luật che dữ liệu cá nhân của dự án.
16. memory-safety.md: ví dụ "Good" dùng `unsafe` (trái luật ngay trong tệp) gắn nhãn "chỉ minh họa, dự án cấm unsafe"; `atomic.AddInt64` → `atomic.Int64`.
17. network.md: `go http.ListenAndServe(...)` bỏ lỗi, không tắt được → `*http.Server` có `ReadHeaderTimeout`, chạy trong WaitGroup, `Shutdown` khi SIGTERM; bỏ bcrypt trong ghi chú so sánh; thêm ghi chú bind trong container.
18. secrets.md: bỏ ví dụ MySQL và `sql.Open("postgres")` → `pgxpool.New`; ghi chú AWS Secrets Manager không có trong ADR-44; interface `SecretManager` thêm `ctx`, bỏ `context.TODO()`.
19. third-party.md: ghi chú dự án không dùng Sentry/analytics ngoài, OpenTelemetry → Jaeger tự vận hành; `hashEmail` (SHA-256 không muối, cắt 8 ký tự — dễ dò ngược) → HMAC có khóa; `interface{}` → `any`.
20. threat-modeling.md: `r.Use(authMiddleware)` (kiểu chi/gin) → bọc `ServeMux` bằng middleware; audit log bỏ IP; `database/sql` → sqlc + pgx; bỏ bcrypt; bước Verify `gosec` → `make ci`.

## Ghi chú

- filesystem.md giữ nguyên.
- Không thấy script hay hook trong thư mục. Phần đáng ngờ nhất là `allowed-tools` rộng (mạng + worktree) và lệnh cài/`go get -tool …@latest`; đã bỏ hết.
