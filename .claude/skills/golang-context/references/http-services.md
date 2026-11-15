# Context in HTTP Servers & Service Calls

## Table of Contents

- [Context in HTTP Servers](#context-in-http-servers)
- [Middleware enriching context](#middleware-enriching-context)
- [Context in Calls to Other Services](#context-in-calls-to-other-services)

## Context in HTTP Servers

`http.Request` carries a context that is cancelled when the client disconnects or the request handler returns. MUST use `r.Context()` — NEVER create a new `context.Background()` inside a handler.

```go
func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context() // this context is cancelled if the client disconnects

    order, err := h.orderService.Get(ctx, r.PathValue("id"))
    if err != nil {
        if ctx.Err() != nil {
            // Client disconnected, no point writing a response
            return
        }
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }

    json.NewEncoder(w).Encode(order)
}
```

(Dự án: handler sinh từ ogen nhận `ctx` sẵn; nguyên tắc giữ nguyên.)

## Middleware enriching context

Middleware injects request-scoped values before handlers run. Use unexported key types to prevent collisions. Dự án: dấu vết do OpenTelemetry lo (`otelhttp` cho server và client, chuẩn W3C `traceparent`), không tự đọc/ghi header `X-Trace-ID` — header do client gửi là dữ liệu không tin được:

```go
import (
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Server: otelhttp starts a span from the incoming traceparent (or a new trace) and puts it in r.Context().
handler := otelhttp.NewHandler(mux, "api")

// Client: otelhttp.NewTransport injects traceparent from ctx into outgoing requests.
client := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}

func (c *HTTPClient) Do(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
    req, err := http.NewRequestWithContext(ctx, method, url, body)
    if err != nil {
        return nil, fmt.Errorf("creating request: %w", err)
    }
    return c.client.Do(req) // trace context travels via ctx; nothing to copy by hand
}
```

A value that really is request-scoped and not covered by OpenTelemetry (for example the authenticated tenant) goes in context through a typed key and accessor:

```go
type tenantKey struct{}

func WithTenant(ctx context.Context, id int64) context.Context {
    return context.WithValue(ctx, tenantKey{}, id)
}

func TenantFrom(ctx context.Context) (int64, bool) {
    id, ok := ctx.Value(tenantKey{}).(int64)
    return id, ok
}
```

## Context in Calls to Other Services

Context MUST be propagated to all HTTP clients and databases using context-aware APIs: `http.NewRequestWithContext`, and pgx/sqlc methods (which always take `ctx` first). This ensures that client disconnections cancel all downstream operations.

```go
// ✗ Bad — downstream calls ignore the request context
func (c *PaymentClient) Charge(ctx context.Context, amount int) error {
    req, _ := http.NewRequest("POST", c.url+"/charge", body)
    return c.client.Do(req) // not context-aware
}

// ✓ Good — all downstream operations respect the context
func (c *PaymentClient) Charge(ctx context.Context, amount int) error {
    req, err := http.NewRequestWithContext(ctx, "POST", c.url+"/charge", body)
    if err != nil {
        return fmt.Errorf("creating request: %w", err)
    }
    return c.client.Do(req)
}
```

```go
// ✗ Bad — downstream calls ignore the request context
func (r *UserRepo) FindByID(ctx context.Context, id int64) (store.User, error) {
    return r.q.GetUser(context.Background(), id)
}

// ✓ Good — all downstream operations respect the context
func (r *UserRepo) FindByID(ctx context.Context, id int64) (store.User, error) {
    return r.q.GetUser(ctx, id) // r.q: sqlc-generated queries over pgx
}
```
