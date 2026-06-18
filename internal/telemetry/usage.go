package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// HandleUsage maps usage records to active spans and metrics.
func (p *Provider) HandleUsage(ctx context.Context, record usage.Record) {
	if p == nil || !p.cfg.Enabled {
		return
	}
	if p.cfg.Traces.Enabled {
		if state := stateFromContext(ctx); state != nil {
			state.mu.Lock()
			state.records = append(state.records, record)
			state.mu.Unlock()
		}
	}
	if p.cfg.Metrics.Enabled && p.recorder != nil {
		p.recorder.Record(ctx, record)
	}
}

func applyUsageAttributes(span trace.Span, record usage.Record) {
	if span == nil {
		return
	}
	system := providerSystem(record.Provider)
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.system", system),
		attribute.String("gen_ai.response.model", record.Model),
		attribute.Int64("gen_ai.usage.input_tokens", record.Detail.InputTokens),
		attribute.Int64("gen_ai.usage.output_tokens", record.Detail.OutputTokens),
		attribute.Int64("gen_ai.usage.total_tokens", record.Detail.TotalTokens),
		attribute.Int64("gen_ai.usage.reasoning_tokens", record.Detail.ReasoningTokens),
		attribute.Int64("gen_ai.usage.cached_tokens", record.Detail.CachedTokens),
		attribute.String("cliproxy.provider", record.Provider),
		attribute.String("cliproxy.executor_type", record.ExecutorType),
		attribute.String("cliproxy.model_alias", record.Alias),
		attribute.String("cliproxy.auth_type", record.AuthType),
		attribute.String("cliproxy.auth_index", safeID(record.AuthIndex)),
		attribute.String("cliproxy.source", safeID(record.Source)),
		attribute.Int64("cliproxy.ttft_ms", record.TTFT.Milliseconds()),
	}
	if record.Failed {
		span.SetStatus(codes.Error, strings.TrimSpace(record.Fail.Body))
		if record.Fail.StatusCode > 0 {
			attrs = append(attrs, attribute.Int("cliproxy.failure_status_code", record.Fail.StatusCode))
		}
	}
	span.SetAttributes(attrs...)
}

func providerSystem(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch {
	case strings.Contains(p, "openai"):
		return "openai"
	case strings.Contains(p, "gemini"), strings.Contains(p, "vertex"), strings.Contains(p, "aistudio"):
		return "gemini"
	case strings.Contains(p, "claude"), strings.Contains(p, "anthropic"):
		return "anthropic"
	case strings.Contains(p, "codex"):
		return "codex"
	case strings.Contains(p, "xai"), strings.Contains(p, "grok"):
		return "grok"
	default:
		return "unknown"
	}
}

func safeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 16 {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

// MetricRecorder owns bounded metric instruments.
type MetricRecorder struct {
	requests     metric.Int64Counter
	failures     metric.Int64Counter
	latency      metric.Float64Histogram
	ttft         metric.Float64Histogram
	inputTokens  metric.Int64Histogram
	outputTokens metric.Int64Histogram
	totalTokens  metric.Int64Histogram
}

// NewMetricRecorder creates metric instruments.
func NewMetricRecorder(meter metric.Meter) *MetricRecorder {
	if meter == nil {
		return nil
	}
	r := &MetricRecorder{}
	r.requests, _ = meter.Int64Counter("cliproxy.requests")
	r.failures, _ = meter.Int64Counter("cliproxy.failures")
	r.latency, _ = meter.Float64Histogram("cliproxy.request.latency_ms")
	r.ttft, _ = meter.Float64Histogram("cliproxy.request.ttft_ms")
	r.inputTokens, _ = meter.Int64Histogram("cliproxy.tokens.input")
	r.outputTokens, _ = meter.Int64Histogram("cliproxy.tokens.output")
	r.totalTokens, _ = meter.Int64Histogram("cliproxy.tokens.total")
	return r
}

// Record emits metrics for one usage record.
func (r *MetricRecorder) Record(ctx context.Context, record usage.Record) {
	if r == nil {
		return
	}
	success := "true"
	if record.Failed {
		success = "false"
	}
	attrs := metric.WithAttributes(
		attribute.String("provider", providerSystem(record.Provider)),
		attribute.String("executor_type", record.ExecutorType),
		attribute.String("operation", operationFromModel(record.Model)),
		attribute.String("model", boundedModel(record.Alias, record.Model)),
		attribute.String("success", success),
	)
	r.requests.Add(ctx, 1, attrs)
	if record.Failed {
		r.failures.Add(ctx, 1, attrs)
	}
	r.latency.Record(ctx, float64(record.Latency.Milliseconds()), attrs)
	r.ttft.Record(ctx, float64(record.TTFT.Milliseconds()), attrs)
	r.inputTokens.Record(ctx, record.Detail.InputTokens, attrs)
	r.outputTokens.Record(ctx, record.Detail.OutputTokens, attrs)
	r.totalTokens.Record(ctx, record.Detail.TotalTokens, attrs)
}

func boundedModel(alias, model string) string {
	value := strings.TrimSpace(alias)
	if value == "" {
		value = strings.TrimSpace(model)
	}
	if len(value) > 96 {
		return value[:96]
	}
	return value
}

func operationFromModel(model string) string {
	if strings.Contains(strings.ToLower(model), "image") {
		return "images"
	}
	return "chat"
}
