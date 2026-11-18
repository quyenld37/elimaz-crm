package queue

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// tracerName là tên tracer của hàng đợi.
const tracerName = "elimaz.com.vn/crm/internal/platform/queue"

// traceContext dùng đúng W3C Trace Context (traceparent, tracestate); không mang baggage (có thể chứa dữ liệu tùy ý).
//
//nolint:gochecknoglobals // bộ truyền không trạng thái, chỉ đọc
var traceContext = propagation.TraceContext{}

// injectTrace trả {"traceparent": …, "tracestate": …} của span trong ctx, hoặc nil khi ctx không có span hợp lệ.
func injectTrace(ctx context.Context) []byte {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return nil
	}
	carrier := propagation.MapCarrier{}
	traceContext.Inject(ctx, carrier)
	b, err := json.Marshal(carrier)
	if err != nil { // map[string]string luôn mã hóa được
		return nil
	}
	return b
}

// startJobSpan mở span "job <kind>" (consumer) là con của span đã ghi khi Enqueue; không có trace_context thì span
// gốc mới. Thuộc tính chỉ có mã việc, loại, số lần (không payload).
func startJobSpan(ctx context.Context, tracer trace.Tracer, stored []byte, j Job) (context.Context, trace.Span) { //nolint:ireturn // API của OpenTelemetry trả interface trace.Span
	if len(stored) > 0 {
		var carrier propagation.MapCarrier
		if json.Unmarshal(stored, &carrier) == nil {
			ctx = traceContext.Extract(ctx, carrier)
		}
	}
	return tracer.Start(ctx, "job "+j.Kind, //nolint:spancheck // span được đóng bởi bên gọi (runJob)
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("job.id", j.ID.String()),
			attribute.String("job.kind", j.Kind),
			attribute.Int("job.attempts", j.Attempts),
		))
}

func newTracer() trace.Tracer { //nolint:ireturn // API của OpenTelemetry trả interface trace.Tracer
	return otel.Tracer(tracerName)
}
