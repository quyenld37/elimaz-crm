// Package telemetry dựng dấu vết OpenTelemetry của tiến trình: tracer provider toàn cục, sampler theo cấu hình,
// exporter OTLP HTTP tới Jaeger, và bộ truyền ngữ cảnh W3C traceparent.
package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"elimaz.com.vn/crm/internal/platform/config"
)

// ErrSampler báo tên sampler không được hỗ trợ.
var ErrSampler = errors.New("telemetry: sampler không được hỗ trợ")

// Endpoint OTLP HTTP khi OTEL_EXPORTER_OTLP_ENDPOINT rỗng.
const defaultEndpoint = "http://localhost:4318"

type options struct {
	syncExporter sdktrace.SpanExporter
}

// Option chỉnh Setup; chỉ dùng trong test.
type Option func(*options)

// WithSyncExporter thay exporter OTLP bằng exp, xuất từng span ngay khi kết thúc (dùng cho test).
func WithSyncExporter(exp sdktrace.SpanExporter) Option {
	return func(o *options) { o.syncExporter = exp }
}

// Setup đặt tracer provider toàn cục theo cfg. OTEL_ENABLED=false → tracer no-op, không xuất gì. Ngược lại:
// sampler theo OTEL_TRACES_SAMPLER/OTEL_TRACES_SAMPLER_ARG, thuộc tính service.name = "crm-" + vai trò.
// shutdown đẩy nốt span còn trong bộ đệm rồi đóng exporter.
func Setup(ctx context.Context, cfg config.Config, opts ...Option) (shutdown func(context.Context) error, err error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !cfg.OTelEnabled {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}
	sampler, err := newSampler(cfg.TracesSampler, cfg.TracesSamplerArg)
	if err != nil {
		return nil, err
	}
	res := resource.NewSchemaless(
		attribute.String("service.name", "crm-"+string(cfg.Role)),
		attribute.String("deployment.environment", cfg.Env),
	)
	tpOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res), sdktrace.WithSampler(sampler)}
	if o.syncExporter != nil {
		tpOpts = append(tpOpts, sdktrace.WithSyncer(o.syncExporter))
	} else {
		endpoint := cfg.OTelEndpoint
		if endpoint == "" {
			endpoint = defaultEndpoint
		}
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
		if err != nil {
			return nil, fmt.Errorf("tạo exporter OTLP: %w", err)
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	return func(ctx context.Context) error {
		if err := tp.Shutdown(ctx); err != nil {
			return fmt.Errorf("tắt tracer provider: %w", err)
		}
		return nil
	}, nil
}

// newSampler dựng sampler theo tên chuẩn của OTEL_TRACES_SAMPLER.
func newSampler(name string, arg float64) (sdktrace.Sampler, error) { //nolint:ireturn // SDK chỉ có các sampler dưới dạng interface sdktrace.Sampler
	switch name {
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(arg), nil
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(arg)), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrSampler, name)
	}
}
