package telemetry

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func testProvider(sr *tracetest.SpanRecorder) *Provider {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return &Provider{
		cfg: config.TelemetryConfig{
			Enabled: true,
			OTLP: config.TelemetryOTLPConfig{
				SkipHealthTraces: true,
			},
			Traces:         config.TelemetrySignalConfig{Enabled: true},
			Metrics:        config.TelemetrySignalConfig{Enabled: false},
			PayloadCapture: config.TelemetryPayloadConfig{Enabled: false, MaxPromptBytes: 8},
		},
		tracer: tp.Tracer(instrumentationName),
	}
}

func TestMiddlewareExtractsInboundTraceparent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sr := tracetest.NewSpanRecorder()
	p := testProvider(sr)
	router := gin.New()
	router.Use(p.Middleware())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"m"}`)))
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d", len(ended))
	}
	got := ended[0].SpanContext().TraceID().String()
	if got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace ID = %s", got)
	}
	parent := ended[0].Parent().SpanID().String()
	if parent != "00f067aa0ba902b7" {
		t.Fatalf("parent span ID = %s", parent)
	}
}

func TestMiddlewareDirectRequestCreatesRootTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sr := tracetest.NewSpanRecorder()
	p := testProvider(sr)
	router := gin.New()
	router.Use(p.Middleware())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"m"}`)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d", len(ended))
	}
	if ended[0].Parent().IsValid() {
		t.Fatalf("parent = valid, want root span")
	}
	if !ended[0].SpanContext().TraceID().IsValid() {
		t.Fatalf("trace ID is invalid")
	}
}

func TestUsageRecordMapsToSpanAttributes(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer(instrumentationName).Start(context.Background(), "test")
	applyUsageAttributes(span, usage.Record{
		Provider:     "codex",
		ExecutorType: "CodexExecutor",
		Model:        "gpt-5.4",
		Alias:        "alias",
		AuthType:     "oauth",
		TTFT:         12_000_000,
		Detail: usage.Detail{
			InputTokens:     1,
			OutputTokens:    2,
			TotalTokens:     3,
			ReasoningTokens: 4,
			CachedTokens:    5,
		},
	})
	span.End()
	attrs := map[string]string{}
	intAttrs := map[string]int64{}
	for _, attr := range sr.Ended()[0].Attributes() {
		switch attr.Value.Type().String() {
		case "STRING":
			attrs[string(attr.Key)] = attr.Value.AsString()
		case "INT64":
			intAttrs[string(attr.Key)] = attr.Value.AsInt64()
		}
	}
	if attrs["gen_ai.system"] != "codex" {
		t.Fatalf("gen_ai.system = %q", attrs["gen_ai.system"])
	}
	if intAttrs["gen_ai.usage.total_tokens"] != 3 {
		t.Fatalf("total tokens = %d", intAttrs["gen_ai.usage.total_tokens"])
	}
}

func TestPayloadCaptureDisabledAndTruncated(t *testing.T) {
	p := &Provider{cfg: config.TelemetryConfig{PayloadCapture: config.TelemetryPayloadConfig{Enabled: false, MaxPromptBytes: 4}}}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{"prompt":"abcdef"}`)))
	if got, _ := p.capturePrompt(req); got != "" {
		t.Fatalf("disabled capture = %q", got)
	}
	p.cfg.PayloadCapture.Enabled = true
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`abcdef`)))
	if got, _ := p.capturePrompt(req); got != "abcd" {
		t.Fatalf("truncated capture = %q", got)
	}
}

var _ trace.Tracer
