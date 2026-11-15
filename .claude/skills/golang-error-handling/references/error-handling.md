# Error Handling Patterns and Logging

## Table of Contents

- [The Single Handling Rule](#the-single-handling-rule)
- [Panic and Recover](#panic-and-recover)
  - [When to panic](#when-to-panic)
  - [Recovering from panics](#recovering-from-panics)
- [Structured Error Context Without External Libraries](#structured-error-context-without-external-libraries)
- [Logging Errors with `slog`](#logging-errors-with-slog)

## The Single Handling Rule

An error MUST be handled exactly once: either log it or return it, never both. Doing both causes duplicate log entries and makes debugging harder.

```go
// ✗ Bad — logs AND returns (duplicate noise)
func processOrder(ctx context.Context, id string) error {
    err := chargeCard(ctx, id)
    if err != nil {
        log.Printf("failed to charge card: %v", err)
        return fmt.Errorf("charging card: %w", err)
    }
    return nil
}

// ✓ Good — return with context, let the caller decide
func processOrder(ctx context.Context, id string) error {
    err := chargeCard(ctx, id)
    if err != nil {
        return fmt.Errorf("charging card for order %s: %w", id, err)
    }
    return nil
}

// ✓ Good — handle at the top level (HTTP handler, main, etc.)
func handleOrder(w http.ResponseWriter, r *http.Request) {
    err := processOrder(r.Context(), r.FormValue("id"))
    if err != nil {
        slog.ErrorContext(r.Context(), "order failed", "error", err)
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusOK)
}
```

## Panic and Recover

### When to panic

Panic MUST only be used for truly unrecoverable states — programmer errors, impossible conditions, or corrupt invariants. NEVER use panic for expected failures like network timeouts or missing files.

```go
// ✓ Acceptable — programmer error in initialization
func MustCompileRegex(pattern string) *regexp.Regexp {
    re, err := regexp.Compile(pattern)
    if err != nil {
        panic(fmt.Sprintf("invalid regex %q: %v", pattern, err))
    }
    return re
}

// ✗ Bad — panic for a normal failure
func GetUser(id string) *User {
    user, err := db.Find(id)
    if err != nil {
        panic(err) // callers cannot recover gracefully
    }
    return user
}
```

### Recovering from panics

Use `recover` in deferred functions at goroutine boundaries (HTTP handlers, worker goroutines) to prevent one panic from crashing the entire process.

```go
func safeHandler(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if r := recover(); r != nil {
                slog.Error("panic recovered",
                    "panic", r,
                    "stack", string(debug.Stack()),
                )
                http.Error(w, "internal error", http.StatusInternalServerError)
            }
        }()
        next.ServeHTTP(w, r)
    })
}
```

## Structured Error Context Without External Libraries

Dự án chỉ dùng thư viện chuẩn (không samber/oops). Khi lỗi cần mang dữ liệu có cấu trúc (mã lỗi, ID đơn), dùng custom error type đặt trong module sinh ra nó; vị trí lỗi trong hệ thống lấy từ dấu vết OpenTelemetry (`trace_id` trên mỗi dòng log), không cần stack trace:

```go
type OrderInsertError struct {
    OrderID int64
    Err     error
}

func (e *OrderInsertError) Error() string { return "inserting order: " + e.Err.Error() }
func (e *OrderInsertError) Unwrap() error { return e.Err }

// LogValue keeps the log line structured; never put phone numbers, addresses or tokens here.
func (e *OrderInsertError) LogValue() slog.Value {
    return slog.GroupValue(
        slog.Int64("order_id", e.OrderID),
        slog.String("cause", e.Err.Error()),
    )
}
```

Mapping errors to HTTP status codes happens in one place at the HTTP edge; the response body never contains the error chain.

## Logging Errors with `slog`

Log once, at the edge (HTTP handler, worker loop), with `slog` and the request context (`slog.ErrorContext(ctx, ...)`) so the project's handler can attach `trace_id` and mask personal data.
