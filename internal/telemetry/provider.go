package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/router-for-me/CLIProxyAPI/internal/telemetry"

// Provider owns OpenTelemetry providers and instruments.
type Provider struct {
	cfg      config.TelemetryConfig
	tracer   trace.Tracer
	meter    metric.Meter
	traces   *sdktrace.TracerProvider
	metrics  *sdkmetric.MeterProvider
	recorder *MetricRecorder
	unhook   func()
}

// NewProvider initializes OpenTelemetry according to cfg.
func NewProvider(ctx context.Context, cfg config.TelemetryConfig) (*Provider, error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if !cfg.Enabled {
		return disabledProvider(cfg), nil
	}
	res, errResource := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
	))
	if errResource != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", errResource)
	}
	p := &Provider{cfg: cfg}
	if cfg.Traces.Enabled {
		tp, errTrace := newTraceProvider(ctx, cfg, res)
		if errTrace != nil {
			return nil, errTrace
		}
		p.traces = tp
		otel.SetTracerProvider(tp)
	}
	if cfg.Metrics.Enabled {
		mp, errMetric := newMetricProvider(ctx, cfg, res)
		if errMetric != nil {
			return nil, errMetric
		}
		p.metrics = mp
		otel.SetMeterProvider(mp)
	}
	p.tracer = otel.Tracer(instrumentationName)
	p.meter = otel.Meter(instrumentationName)
	p.recorder = NewMetricRecorder(p.meter)
	p.unhook = usage.RegisterHook(p.HandleUsage)
	return p, nil
}

func disabledProvider(cfg config.TelemetryConfig) *Provider {
	return &Provider{
		cfg:    cfg,
		tracer: trace.NewNoopTracerProvider().Tracer(instrumentationName),
		meter:  noop.NewMeterProvider().Meter(instrumentationName),
	}
}

func newTraceProvider(ctx context.Context, cfg config.TelemetryConfig, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	switch cfg.OTLP.Protocol {
	case "grpc":
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTLP.Endpoint), otlptracegrpc.WithHeaders(headerMap(cfg.OTLP.Headers))}
		if cfg.OTLP.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exp, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("create OTLP gRPC trace exporter: %w", err)
		}
		return sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(exp)), nil
	case "http":
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.OTLP.Endpoint), otlptracehttp.WithHeaders(headerMap(cfg.OTLP.Headers))}
		if cfg.OTLP.PathPrefix != "" {
			opts = append(opts, otlptracehttp.WithURLPath("/"+strings.Trim(cfg.OTLP.PathPrefix, "/")+"/v1/traces"))
		}
		if cfg.OTLP.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exp, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("create OTLP HTTP trace exporter: %w", err)
		}
		return sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(exp)), nil
	default:
		return nil, fmt.Errorf("unsupported OTLP trace protocol %q", cfg.OTLP.Protocol)
	}
}

func newMetricProvider(ctx context.Context, cfg config.TelemetryConfig, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	switch cfg.OTLP.Protocol {
	case "grpc":
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTLP.Endpoint), otlpmetricgrpc.WithHeaders(headerMap(cfg.OTLP.Headers))}
		if cfg.OTLP.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		exp, err := otlpmetricgrpc.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("create OTLP gRPC metric exporter: %w", err)
		}
		return sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(time.Second)))), nil
	case "http":
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(cfg.OTLP.Endpoint), otlpmetrichttp.WithHeaders(headerMap(cfg.OTLP.Headers))}
		if cfg.OTLP.PathPrefix != "" {
			opts = append(opts, otlpmetrichttp.WithURLPath("/"+strings.Trim(cfg.OTLP.PathPrefix, "/")+"/v1/metrics"))
		}
		if cfg.OTLP.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		exp, err := otlpmetrichttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("create OTLP HTTP metric exporter: %w", err)
		}
		return sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(time.Second)))), nil
	default:
		return nil, fmt.Errorf("unsupported OTLP metric protocol %q", cfg.OTLP.Protocol)
	}
}

func headerMap(headers []string) map[string]string {
	out := map[string]string{}
	for _, header := range headers {
		key, value, ok := strings.Cut(header, ":")
		if !ok {
			continue
		}
		key = http.CanonicalHeaderKey(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if out[key] == "" {
			out[key] = value
		} else {
			out[key] += "," + value
		}
	}
	return out
}

// Shutdown flushes telemetry providers.
func (p *Provider) Shutdown(ctx context.Context) {
	if p == nil {
		return
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	if p.unhook != nil {
		p.unhook()
		p.unhook = nil
	}
	if p.traces != nil {
		if err := p.traces.Shutdown(ctx); err != nil {
			log.WithError(err).Warn("telemetry trace shutdown failed")
		}
	}
	if p.metrics != nil {
		if err := p.metrics.Shutdown(ctx); err != nil {
			log.WithError(err).Warn("telemetry metric shutdown failed")
		}
	}
}
