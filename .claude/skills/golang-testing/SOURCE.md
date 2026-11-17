# Nguồn: golang-testing

- Repo: https://github.com/samber/cc-skills-golang
- Commit: `d68d80aec298e1b74b37c2a472c002b5217248b3`
- Thư mục gốc: `skills/golang-testing/` (bỏ `evals/`)
- Giấy phép: MIT (tác giả samber).
- Lấy ngày 15/11/2026 bằng `gh api`; không chạy script nào.

## Tệp đã chép

| Tệp | Gốc (byte) | Bản dự án (byte) |
|---|---|---|
| SKILL.md | 17.907 | 17.275 |
| references/benchmarks.md | 1.970 | 1.891 |
| references/coverage.md | 1.850 | 1.740 |
| references/examples.md | 1.931 | 1.931 (không đổi) |
| references/helpers.md | 1.054 | 1.054 (không đổi) |
| references/http-testing.md | 2.268 | 2.503 |
| references/integration-testing.md | 4.457 | 5.578 (viết lại) |
| references/mocking.md | 4.757 | 5.637 (viết lại) |

Description: 505 → 472 ký tự.

## Sửa và bỏ (lý do)

SKILL.md
1. Frontmatter: bỏ `allowed-tools` (gồm `Bash(gotests:*)`), bỏ `metadata.openclaw.install` (cài `github.com/cweill/gotests/gotests@latest`), bỏ `compatibility`. Bỏ mục "Dependencies: go install …gotests@latest". Lý do: không cấp quyền, không cài công cụ không ghim phiên bản.
2. Description: bỏ "testify suites and mocks", "goleak", "snapshot testing" và lời trỏ skill testify/benchmark; thêm rapid, synctest, testcontainers-go.
3. Bỏ "Thinking mode … `ultrathink`" và "Orchestration mode … `ultracode`"; Audit mode chỉ chạy song song sub-agent khi người dùng yêu cầu. Lý do: từ khóa đổi hành vi agent.
4. Write mode: bỏ "use `gotests` to scaffold".
5. Thêm khối "Dự án dùng testing chuẩn + rapid + testcontainers-go, không testify/gomock/mockery/goleak/clockwork/gotests".
6. Best practices: mục 6 (goleak) → synctest + test tắt êm + `-race`; mục 7 (testify) → testing chuẩn; mục 8 Mock → Fake viết tay, interface phía dùng; thêm mục 14 rapid cho quy luật vùng rủi ro cao (ADR-25). Mục 2 thêm ghi chú theo `make ci`.
7. Ví dụ bảng giá đổi `float64` → `int64` đồng (luật tiền của dự án).
8. Mục "Assert Scope Leaking into Subtests" (riêng testify) → mục chung "Using the Parent `t` Inside Subtests".
9. Thêm mục "Property Tests with rapid" (ví dụ `rapid.Check`).
10. Mục goleak → "Goroutine Leak Detection" không dùng goleak.
11. Các khẳng định về Go 1.27 (`synctest.Sleep`, `httptest.NewTestServer`, `stdversion` tự chạy) giữ, gắn "chưa xác minh với release notes Go 1.27".
12. Ví dụ Parallel Tests: `assert.New(t)`/`is.NotNil` → `t.Fatal`.
13. Integration Tests: `sql.Open("postgres", os.Getenv(...))` → helper testcontainers-go + goose + pgx.
14. Mocking → "Fakes Instead of Mocks". Linters: bỏ `testifylint`, bỏ trỏ skill golang-lint. Cross-References → skill đã cài. Coverage: thêm ghi chú gremlins.
15. Unit Tests: thêm "mọi câu SQL test trên PostgreSQL thật".

references
16. benchmarks.md, coverage.md: bỏ lời trỏ skill samber golang-benchmark / golang-continuous-integration.
17. http-testing.md: `assert.New(t)`/`is.Equal` → `if w.Code != … { t.Errorf }`; thêm ghi chú handler ogen.
18. integration-testing.md: viết lại. Bỏ docker-compose (kèm redis), `schema.sql` riêng, `database/sql` + `lib/pq`, testify `suite`, `time.Sleep(5s)`, `exec.Command("docker-compose")`; thay bằng testcontainers-go PostgreSQL 18, migration goose thật qua `stdlib.OpenDBFromPool`, pgxpool, `testing` chuẩn, tiền `int64`. Giữ ý fixture dữ liệu SQL.
19. mocking.md: viết lại. `testify/mock` → fake viết tay; gói `fixtures` với biến toàn cục `DefaultUser`/`AdminUser` → hàm trả giá trị mới (luật không biến toàn cục thay đổi được, không gói dùng chung kiểu util); `google/uuid` trong fixture bỏ; `clockwork` (+ `go get`) → synctest hoặc đồng hồ tiêm qua interface.

## Ghi chú

- helpers.md giữ nguyên (có một chú thích link issue testify, chỉ là nguồn tham khảo).
- Không thấy script, hook, lệnh mạng hay telemetry.

- 17/11/2026: bỏ trường `paths:` khỏi frontmatter để skill được nạp sẵn vào agent `crm-implementer` (khai trong `skills:` của agent).

- 18/11/2026: thêm lại `paths:` sau khi bỏ agent crm-implementer (không còn nạp sẵn).
