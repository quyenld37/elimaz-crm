# Context Values & Cross-Service Tracing

## Using context values correctly

Context values carry request-scoped metadata that crosses API boundaries — not function parameters, configuration, or optional arguments. Good candidates: request IDs, authenticated user/tenant info, correlation IDs. (Dự án: trace/span do OpenTelemetry giữ trong ctx; không tự lưu `trace_id` thành giá trị riêng.)

Always use an unexported type as the key to prevent collisions between packages:

```go
// ✓ Good — unexported key type prevents collisions
type contextKey string

const requestIDKey contextKey = "request_id"

func WithRequestID(ctx context.Context, requestID string) context.Context {
    return context.WithValue(ctx, requestIDKey, requestID)
}

func RequestIDFromContext(ctx context.Context) (string, bool) {
    requestID, ok := ctx.Value(requestIDKey).(string)
    return requestID, ok
}
```

```go
// ✗ Bad — string keys collide across packages
ctx = context.WithValue(ctx, "request_id", requestID) // another package could use the same key
```

## What belongs in context values vs function parameters

| Data | Context value? | Why |
| --- | --- | --- |
| request_id | Yes | Request-scoped metadata for observability |
| trace/span | Yes, via OpenTelemetry | `trace.SpanFromContext(ctx)`; never a hand-made key |
| Authenticated user/tenant | Yes | Request-scoped, crosses API boundaries |
| Database connection | No | Infrastructure dependency, pass explicitly |
| Feature flags | No | Configuration, pass explicitly or inject |
| Function arguments (user ID, order data) | No | Business logic parameters, pass as arguments |
| Logger | Depends | OK if enriched with request-scoped fields (trace_id); otherwise pass explicitly |

## Trace propagation between services

`context.Context` is the vehicle for trace propagation. The project uses OpenTelemetry Go: `otelhttp` reads the W3C `traceparent` header on the way in and writes it on the way out, `otelpgx` and `valkeyotel` add spans for database and Valkey calls, all from the same `ctx`. The rule is the same as for cancellation: pass `ctx` through every layer.

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/trace"
)

// trace_id for a log line (dự án: handler slog tự làm việc này)
func traceID(ctx context.Context) string {
    return trace.SpanContextFromContext(ctx).TraceID().String()
}

// Carrying trace context through a queued job (dự án: cột trace_context của bảng jobs)
func injectTrace(ctx context.Context) map[string]string {
    carrier := propagation.MapCarrier{}
    otel.GetTextMapPropagator().Inject(ctx, carrier)
    return carrier
}

func extractTrace(ctx context.Context, stored map[string]string) context.Context {
    return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(stored))
}
```

The propagator and tracer provider are set once in `main` during startup (`otel.SetTextMapPropagator(propagation.TraceContext{})`), not in `init()`.
