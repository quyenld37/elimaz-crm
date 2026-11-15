---
name: use-modern-go
description: "Modern Go idioms up to Go 1.27 (the go.mod version) for writing, modifying, fixing, or refactoring Go code: range over int, min/max, slices/maps/cmp helpers, strings.Cut/CutPrefix/SplitSeq, errors.Is/AsType/Join, wg.Go, t.Context, b.Loop, omitzero, new(value), ServeMux patterns, typed atomics, context causes. Guidance text only — the upstream CLI is not installed."
user-invocable: true
license: Apache-2.0
metadata:
  author: JetBrains
  upstream-cli-version: v0.1.1
  curated-for: ELIMAZ CRM (see SOURCE.md)
paths:
  - "**/*.go"
---

# Modern Go Guidelines (ELIMAZ CRM copy)

Write modern, idiomatic Go for the Go version in `go.mod` (dự án: Go 1.27, ghim bằng dòng `toolchain`). Models tend to produce older Go because of training-data lag and frequency bias; this list corrects that.

**Công cụ không được cài.** Bản gốc gọi CLI `go-modern-guidelines` qua script `scripts/run-tool.sh`, script này chạy `go install github.com/JetBrains/go-modern-guidelines@v0.1.1` vào `~/.cache` ở lần dùng đầu. Bản của dự án bỏ script và CLI: không chạy, không cài. Danh sách hướng dẫn nằm ở [references/FEATURES.md](references/FEATURES.md) (cùng nội dung mà CLI trả về, sinh từ cùng tệp `guidelines.json` của upstream).

## Before editing Go code

1. Check the target Go version in `go.mod` (`go` and `toolchain` lines). Only use features available up to and including that version.
2. Look up the relevant entries in [references/FEATURES.md](references/FEATURES.md). The table at the top is ordered newest first with an impact rating; read the detailed entry for any guideline that may apply to the code you are writing.
3. Apply a guideline when it fits. Skip it when it would not compile, would change behavior, or clearly does not match the edited code.
4. **Project rules win.** If a guideline conflicts with `CLAUDE.md`, an ADR, or a project skill (for example: every goroutine owned and stoppable, no mutable package globals, generated code is never edited by hand), follow the project rule. Do not rewrite existing code only to modernize it unless the task asks for it; `go fix`/`modernize` changes go in their own commit, separate from behavior changes.

## Most common guidelines (see FEATURES.md for all and for examples)

- Loops: `for i := range n`; no `v := v` copies (Go 1.22 per-iteration variables).
- Collections: `min`/`max`, `slices.Contains`/`Index`/`IndexFunc`/`Sort`/`SortFunc` (with `cmp.Compare`)/`Max`/`Min`/`Reverse`/`Clone`, `maps.Clone`/`Copy`/`DeleteFunc`, `slices.Collect`/`Sorted(maps.Keys(m))`, `clear`.
- Strings: `strings.Cut`, `CutPrefix`/`CutSuffix`, `SplitSeq`/`FieldsSeq`, `bytes.Clone`, `fmt.Appendf`.
- Errors: `errors.Is` (never `==` against a sentinel), `errors.AsType[T]` (Go 1.26+), `errors.Join`.
- Sync and context: `wg.Go` (Go 1.25+), typed atomics, `sync.OnceFunc`/`OnceValue`, `context.AfterFunc`, `WithCancelCause`/`WithTimeoutCause` + `context.Cause`.
- Tests: `t.Context()` (canceled before `t.Cleanup` runs), `b.Loop()`.
- Types and JSON: `any`, `new(value)` (Go 1.26+), `omitzero` for zero-means-absent fields, `reflect.TypeFor[T]()`.
- HTTP: method and wildcard `ServeMux` patterns with `r.PathValue` (dự án dùng `net/http` + `ServeMux`; route API do ogen sinh).
- Time: `time.Since`, `time.Until`.

Go 1.27 entries (`generic_methods`, `json_v2`, `promoted_field_literals`, `strings_bytes_cut_last`, `stdlib_uuid`, `url_clone`) come from upstream and have not been checked against the Go 1.27 release notes in this project; confirm the API exists in the pinned toolchain before using it.

Attribution: JetBrains, "Modern Go Guidelines", Apache-2.0. See SOURCE.md.
