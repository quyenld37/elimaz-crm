---
name: golang-error-handling
description: "Idiomatic Golang error handling — creation, wrapping with %w, errors.Is/As/AsType, errors.Join, custom error types, sentinel errors, panic/recover, the single handling rule (log OR return, never both), and structured error logging with log/slog. Apply when creating, wrapping, inspecting, or logging errors in Go code. ELIMAZ CRM copy: standard library only, no samber/oops."
user-invocable: true
license: MIT
metadata:
  author: samber
  version: "1.3.1"
  curated-for: ELIMAZ CRM (see SOURCE.md)
paths:
  - "**/*.go"
---

**Persona:** You are a Go reliability engineer. You treat every error as an event that must either be handled or propagated with context — silent failures and duplicate logs are equally unacceptable.

**Modes:**

- **Coding mode** — writing new error handling code. Follow the best practices sequentially.
- **Review mode** — reviewing a PR's error handling changes. Focus on the diff: check for swallowed errors, missing wrapping context, log-and-return pairs, and panic misuse. Sequential.
- **Audit mode** — auditing existing error handling across a codebase, by category (creation, wrapping, single-handling rule, panic/recover, structured logging). Dự án: chỉ chạy song song sub-agent khi người dùng yêu cầu.

> **Thứ tự ưu tiên.** Luật dự án (CLAUDE.md, ADR trong `docs/crm/specs`) đứng trên skill này khi hai bên khác nhau.

# Go Error Handling Best Practices

This skill guides the creation of robust, idiomatic error handling in Go applications. Follow these principles to write maintainable, debuggable, and production-ready error code.

## Best Practices Summary

1. **Returned errors MUST always be checked** — NEVER discard with `_`
2. **Errors MUST be wrapped with context** using `fmt.Errorf("{context}: %w", err)` — dự án: bọc một lần tại mỗi tầng thật sự thêm ngữ cảnh, một `%w` cho mỗi lần bọc; không bọc lặp lại cùng một lỗi mà không thêm thông tin
3. **Error strings MUST be lowercase**, without trailing punctuation
4. **Use `%w` internally, `%v` at system boundaries** to control error chain exposure
5. **MUST use `errors.Is` for sentinel matching and `errors.As`/`errors.AsType` for typed chain inspection** instead of direct comparison or bare type assertions. For Go 1.26+, prefer `errors.AsType[T](err)` when `T` implements `error`; use `errors.As(err, &target)` for Go <1.26 or for non-error interface targets.
6. **SHOULD use `errors.Join`** (Go 1.20+) to combine independent errors
7. **Errors MUST be either logged OR returned**, NEVER both (single handling rule)
8. **Use sentinel errors** for expected conditions, custom types for carrying data
9. **NEVER use `panic` for expected error conditions** — reserve for truly unrecoverable states
10. **SHOULD use `slog`** (Go 1.21+) for structured error logging — not `fmt.Println` or `log.Printf`
11. Dự án không dùng samber/oops: lỗi cần mang thuộc tính có cấu trúc thì dùng custom error type (có thể cài `slog.LogValuer`); vị trí lỗi lấy từ dấu vết OpenTelemetry (`trace_id`) thay cho stack trace
12. **Log HTTP requests** with structured middleware capturing method, path, status, and duration
13. **Use log levels** to indicate error severity
14. **Never expose technical errors to users** — translate internal errors to user-friendly messages, log technical details separately
15. **Keep log grouping low-cardinality** — at logging/APM boundaries, keep message templates stable and attach IDs, paths, line numbers, and counts as structured attributes. Error values may include useful operational context, but avoid putting high-cardinality data into the stable log message used for grouping.

## Detailed Reference

- **[Error Creation](./references/error-creation.md)** — How to create errors that tell the story: error messages should be lowercase, no punctuation, and describe what happened without prescribing action. Covers sentinel errors (one-time preallocation for performance), custom error types (for carrying rich context), and the decision table for which to use when.

- **[Error Wrapping and Inspection](./references/error-wrapping.md)** — Why `fmt.Errorf("{context}: %w", err)` beats `fmt.Errorf("{context}: %v", err)` (chains vs concatenation). How to inspect chains with `errors.Is`, `errors.As`, and Go 1.26+ `errors.AsType` for type-safe error handling, and `errors.Join` for combining independent errors.

- **[Error Handling Patterns and Logging](./references/error-handling.md)** — The single handling rule: errors are either logged OR returned, NEVER both (prevents duplicate logs cluttering aggregators). Panic/recover design, structured error context with the standard library, and `slog` structured logging.

## Error Handling Audits

When auditing error handling across a large codebase, split the work by independent category:

- Error creation — validate `errors.New`/`fmt.Errorf` usage, low-cardinality messages, custom types
- Error wrapping — audit `%w` vs `%v`, verify `errors.Is`/`errors.As` patterns
- Single handling rule — find log-and-return violations, swallowed errors, discarded errors (`_`)
- Panic/recover — audit `panic` usage, verify recovery at goroutine boundaries
- Structured logging — verify `slog` usage at error sites, check for PII in error messages

## Related skills in this project

- `golang-context` — cancellation errors (`context.Canceled`, `context.DeadlineExceeded`, `context.Cause`)
- `golang-security` — `references/logging.md` for PII in logs and generic errors to clients
- `golang-database` — translating `pgx.ErrNoRows` and PostgreSQL error codes into domain errors

## References

- [log/slog package](https://pkg.go.dev/log/slog)
