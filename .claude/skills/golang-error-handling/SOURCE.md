# Nguồn: golang-error-handling

- Repo: https://github.com/samber/cc-skills-golang
- Commit: `d68d80aec298e1b74b37c2a472c002b5217248b3`
- Thư mục gốc: `skills/golang-error-handling/` (bỏ `evals/`)
- Giấy phép: MIT (tác giả samber). Giữ ghi công trong frontmatter (`metadata.author`, `license`).
- Lấy ngày 15/11/2026 bằng `gh api .../contents/...?ref=<SHA>`; không chạy script nào.

## Tệp đã chép

| Tệp | Gốc (byte) | Bản dự án (byte) |
|---|---|---|
| SKILL.md | 7.363 | 5.813 |
| references/error-creation.md | 5.781 | 5.807 |
| references/error-handling.md | 4.591 | 4.173 |
| references/error-wrapping.md | 3.860 | 4.028 |

Description: 583 → 374 ký tự.

## Sửa và bỏ (lý do)

SKILL.md
1. Frontmatter: bỏ `allowed-tools` (cấp sẵn Read/Edit/Write/Bash(go/git/golangci-lint)/Agent), bỏ `compatibility` và khối `metadata.openclaw`; thêm `metadata.curated-for`. Lý do: không cấp quyền công cụ qua skill.
2. Description: bỏ "samber/oops" và hai lời trỏ tới skill samber khác (không được cài). Lý do: thư viện ngoài ADR-44.
3. Bỏ dòng "Orchestration mode … use `ultracode`" và chế độ Audit "up to 5 parallel sub-agents": đổi thành "chỉ chạy song song sub-agent khi người dùng yêu cầu". Lý do: lệnh đổi hành vi agent (tự mở nhiều sub-agent, từ khóa kích hoạt) ngoài chủ đề.
4. Bỏ ý "launch a background sub-agent" ở Coding mode. Lý do như trên.
5. "Community default …" → "Luật dự án đứng trên skill này".
6. Mục 2: thêm luật dự án "bọc một lần tại mỗi tầng thật sự thêm ngữ cảnh".
7. Mục 11 "Use samber/oops …" → custom error type + `slog.LogValuer`, vị trí lỗi qua `trace_id` OpenTelemetry. Lý do: oops ngoài ADR-44.
8. Cross-References (5 skill samber chưa cài) → danh sách skill đã cài trong dự án. References: bỏ link tint, oops, slog-multi, slog-sampling, slog-formatter, slog-http, slog-sentry; giữ `log/slog`.

references/error-creation.md
9. Ví dụ `oops.With(...).Errorf` (2 chỗ) và dòng import oops → custom error type (`NotFoundError`, `CSVParseError`). Đoạn giải thích "samber/oops solves this" → custom error type có `LogValue()`.
10. Bảng quyết định: dòng `samber/oops` → custom error type cài `slog.LogValuer`.

references/error-handling.md
11. Ví dụ đúng của "single handling rule" dùng `oops.Wrapf` → `fmt.Errorf("…: %w")`; thêm `ctx` làm tham số đầu cho `processOrder`/`chargeCard`; `slog.Error` → `slog.ErrorContext(r.Context(), …)`. Lý do: luật ctx tham số đầu, oops ngoài stack.
12. Bỏ cả mục "Why Use samber/oops" → mục "Structured Error Context Without External Libraries" (custom error type + `LogValue`, đổi lỗi sang HTTP ở một chỗ).
13. Mục "Logging Errors with slog" trỏ skill golang-observability (chưa cài) → một đoạn: log một lần ở mép, dùng `ErrorContext` để handler gắn `trace_id` và che dữ liệu cá nhân.

references/error-wrapping.md
14. "Errors SHOULD be wrapped at each layer" → "at each layer that adds context" + luật bọc một lần.
15. `GetUser(id)` → `GetUser(ctx, id)`. `sql.ErrNoRows` → `pgx.ErrNoRows`. Lý do: ctx tham số đầu; dự án dùng pgx, không `database/sql`.

## Ghi chú

- Biến sentinel `var ErrX = errors.New(...)` giữ nguyên: CLAUDE.md của CRM cho phép rõ.
- Không thấy script, hook, lệnh mạng hay telemetry trong các tệp này.

- 17/11/2026: bỏ trường `paths:` khỏi frontmatter để skill được nạp sẵn vào agent `crm-implementer` (khai trong `skills:` của agent).
