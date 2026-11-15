# Nguồn: golang-context

- Repo: https://github.com/samber/cc-skills-golang
- Commit: `d68d80aec298e1b74b37c2a472c002b5217248b3`
- Thư mục gốc: `skills/golang-context/` (bỏ `evals/`)
- Giấy phép: MIT (tác giả samber).
- Lấy ngày 15/11/2026 bằng `gh api`; không chạy script nào.

## Tệp đã chép

| Tệp | Gốc (byte) | Bản dự án (byte) |
|---|---|---|
| SKILL.md | 6.340 | 6.161 |
| references/cancellation.md | 6.075 | 6.788 |
| references/http-services.md | 3.805 | 3.900 |
| references/values-tracing.md | 3.162 | 3.049 |

Description: 441 → 470 ký tự (thêm "OpenTelemetry trace context").

## Sửa và bỏ (lý do)

SKILL.md
1. Frontmatter: bỏ `allowed-tools`, `compatibility`, `metadata.openclaw`.
2. "Community default" → luật dự án đứng trên; nhắc ctx tham số đầu, không lưu trong struct, không `init()`, dấu vết qua OpenTelemetry.
3. Mục 6 và bảng "Creating Contexts": "main, init, tests" → "main, tests" (dự án cấm `init()`); test ưu tiên `t.Context()`.
4. Mục 10 (`WithoutCancel` cho việc nền): thêm luật việc nền giao cho thành phần có chủ (bảng `jobs`/worker có `Shutdown`), không `go f()` trần.
5. Ví dụ `s.db.ExecContext(...)` (database/sql) → lời gọi sqlc `s.q.InsertOrder(ctx, …)`.
6. Mô tả deep-dive: tracing dùng `otelhttp`/`traceparent`; DB dùng pgx/sqlc.
7. Cross-References → skill đã cài; linters: bỏ trỏ skill golang-lint, nêu linter cụ thể theo cấu hình golangci-lint v2 của repo.

references/cancellation.md
8. `processItems`: bản gốc trả về ngay lỗi đầu tiên, bỏ lại goroutine còn chạy (không ai chờ) → `wg.Go` + `wg.Wait()` + `errors.Join`. Lý do: mọi goroutine có chủ.
9. Ví dụ `WithoutCancel` gốc chạy `go h.auditService.LogOrderCreated(...)` trong handler (goroutine không chủ, bị cắt khi deploy) → audit và job đi trong cùng transaction, đoạn ngắn còn lại dùng `WithoutCancel` + deadline riêng + worker có chủ.

references/http-services.md
10. Middleware tự đọc/ghi `X-Trace-ID`/`X-Span-ID` (tin header do client gửi) → `otelhttp.NewHandler` / `otelhttp.NewTransport` (ADR-44); thêm ví dụ khóa ctx có kiểu cho tenant.
11. Ví dụ repo `r.db.QueryRowContext` (database/sql, `SELECT *`) → sqlc `r.q.GetUser(ctx, id)`; câu dẫn đổi `QueryContext/ExecContext` → pgx/sqlc.

references/values-tracing.md
12. Ví dụ khóa ctx dùng `trace_id` → `request_id` (trace do OpenTelemetry giữ); bảng thêm dòng trace/span qua OpenTelemetry.
13. Mục "Trace propagation" tự viết `X-Trace-ID` → OpenTelemetry: `trace.SpanContextFromContext`, `propagation.MapCarrier` cho cột `trace_context` của bảng `jobs`, đặt propagator trong `main`.

## Ghi chú

- Không thấy script, hook, lệnh mạng hay telemetry.
