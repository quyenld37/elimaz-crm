package queue_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// Dấu vết đi qua hàng đợi (spec 04 mục 17): Enqueue trong một span → handler chạy trong span con cùng TraceID.
func TestTraceFlowsThroughQueue(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTracerProvider(prevTP); otel.SetTextMapPropagator(prevProp) })

	e := newEnv(t)
	var inHandler trace.SpanContext
	h := newFH(kind("t.trace"), func(ctx context.Context, _ queue.Job, _ pgx.Tx) (json.RawMessage, error) {
		inHandler = trace.SpanContextFromContext(ctx)
		return nil, nil
	})
	ctx, span := tp.Tracer("test").Start(t.Context(), "request")
	parent := span.SpanContext()
	var stored []byte
	err := e.app.RetryTx(ctx, dbTx(), func(tx pgx.Tx) error {
		_, _, err := e.q.Enqueue(ctx, tx, h.kind, queue.NewJob{DedupeKey: "k"})
		return err //nolint:wrapcheck // test
	})
	span.End()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.owner.Pool.QueryRow(t.Context(), "SELECT trace_context::text FROM platform.jobs").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	runOnce(t, worker(e, "w1", h), 10)

	if inHandler.TraceID() != parent.TraceID() || inHandler.SpanID() == parent.SpanID() {
		t.Fatalf("handler phải ở span con cùng TraceID %s: %s/%s (trace_context %s)",
			parent.TraceID(), inHandler.TraceID(), inHandler.SpanID(), stored)
	}
	var job sdktrace.ReadOnlySpan
	for _, s := range exp.GetSpans().Snapshots() {
		if s.Name() == "job t.trace" {
			job = s
		}
	}
	if job == nil || job.Parent().SpanID() != parent.SpanID() || job.SpanKind() != trace.SpanKindConsumer {
		t.Fatalf("thiếu span con 'job t.trace' của request: %+v", exp.GetSpans())
	}
}

// Không có span trong ctx: trace_context NULL; việc vẫn chạy bình thường.
func TestNoTraceStoresNull(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.notrace"), nil)
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	if n := e.count(t, "SELECT count(*) FROM platform.jobs WHERE trace_context IS NULL AND id = $1", id); n != 1 {
		t.Fatal("trace_context phải NULL khi không có span")
	}
	runOnce(t, worker(e, "w1", h), 10)
	if e.job(t, id).Status != "done" {
		t.Fatal("việc không dấu vết phải chạy được")
	}
}
