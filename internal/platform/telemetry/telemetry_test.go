package telemetry_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/telemetry"
)

// Các test này đổi tracer provider toàn cục của OpenTelemetry nên không chạy song song.

func setup(t *testing.T, cfg config.Config, exp *tracetest.InMemoryExporter) {
	t.Helper()
	shutdown, err := telemetry.Setup(t.Context(), cfg, telemetry.WithSyncExporter(exp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := shutdown(context.Background()); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
}

func startEnd(name string) trace.SpanContext {
	_, span := otel.Tracer("test").Start(context.Background(), name)
	span.End()
	return span.SpanContext()
}

func TestSetupDisabledExportsNothing(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	setup(t, config.Config{Role: config.RoleAPI, OTelEnabled: false, TracesSampler: "parentbased_traceidratio", TracesSamplerArg: 1}, exp)
	sc := startEnd("op")
	if sc.IsValid() {
		t.Fatal("OTEL_ENABLED=false phải cho span no-op")
	}
	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("xuất %d span khi tắt", n)
	}
}

func TestSetupEnabledHasServiceName(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	setup(t, config.Config{Role: config.RoleAPI, Env: "dev", OTelEnabled: true, TracesSampler: "parentbased_traceidratio", TracesSamplerArg: 1}, exp)
	startEnd("op")
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("số span = %d, muốn 1", len(spans))
	}
	var got string
	for _, kv := range spans[0].Resource.Attributes() {
		if kv.Key == "service.name" {
			got = kv.Value.AsString()
		}
	}
	if got != "crm-api" {
		t.Fatalf("service.name = %q", got)
	}
}

func TestSetupSamplerFromConfig(t *testing.T) {
	tests := []struct {
		sampler string
		arg     float64
		want    int
	}{
		{"parentbased_traceidratio", 1, 20},
		{"parentbased_traceidratio", 0, 0},
		{"traceidratio", 0, 0},
		{"always_on", 0, 20},
		{"always_off", 1, 0},
		{"parentbased_always_on", 1, 20},
		{"parentbased_always_off", 1, 0},
	}
	for _, tc := range tests {
		t.Run(tc.sampler, func(t *testing.T) {
			exp := tracetest.NewInMemoryExporter()
			setup(t, config.Config{Role: config.RoleWorker, OTelEnabled: true, TracesSampler: tc.sampler, TracesSamplerArg: tc.arg}, exp)
			for range 20 {
				startEnd("op")
			}
			if n := len(exp.GetSpans()); n != tc.want {
				t.Fatalf("%s(%v): %d span, muốn %d", tc.sampler, tc.arg, n, tc.want)
			}
		})
	}
}

func TestSetupRejectsUnknownSampler(t *testing.T) {
	_, err := telemetry.Setup(t.Context(), config.Config{Role: config.RoleAPI, OTelEnabled: true, TracesSampler: "nope"},
		telemetry.WithSyncExporter(tracetest.NewInMemoryExporter()))
	if err == nil {
		t.Fatal("sampler lạ phải lỗi")
	}
}
