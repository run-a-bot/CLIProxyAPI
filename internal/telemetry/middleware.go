package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type requestStateKey struct{}

type requestState struct {
	mu      sync.Mutex
	span    trace.Span
	records []usage.Record
	prompt  string
	output  string
}

// Middleware creates server spans and extracts W3C trace context.
func (p *Provider) Middleware() gin.HandlerFunc {
	if p == nil || !p.cfg.Enabled || !p.cfg.Traces.Enabled {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if p.cfg.OTLP.SkipHealthTraces && c.Request != nil && c.Request.URL != nil && c.Request.URL.Path == "/healthz" {
			c.Next()
			return
		}
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		prompt, model := p.capturePrompt(c.Request)
		operation := operationFromPath(c.Request.URL.Path)
		ctx, span := p.tracer.Start(ctx, c.Request.Method+" "+c.FullPath(), trace.WithSpanKind(trace.SpanKindServer))
		state := &requestState{span: span, prompt: prompt}
		ctx = context.WithValue(ctx, requestStateKey{}, state)
		c.Request = c.Request.WithContext(ctx)
		if p.cfg.PayloadCapture.Enabled {
			c.Writer = &captureWriter{ResponseWriter: c.Writer, state: state, limit: p.cfg.PayloadCapture.MaxResponseBytes}
		}
		span.SetAttributes(
			attribute.String("http.request.method", c.Request.Method),
			attribute.String("url.path", c.Request.URL.Path),
			attribute.String("server.address", c.Request.Host),
			attribute.String("client.address", clientAddress(c.Request)),
			attribute.String("user_agent.original", c.Request.UserAgent()),
			attribute.String("gen_ai.operation.name", operation),
			attribute.String("gen_ai.request.model", model),
		)
		c.Next()
		state.finish(c.Writer.Status(), p.cfg.PayloadCapture.Enabled)
	}
}

func (s *requestState) finish(status int, capturePayload bool) {
	if s == nil || s.span == nil {
		return
	}
	s.mu.Lock()
	records := append([]usage.Record(nil), s.records...)
	prompt := s.prompt
	output := s.output
	s.mu.Unlock()
	s.span.SetAttributes(attribute.Int("http.response.status_code", status))
	if status >= http.StatusInternalServerError {
		s.span.SetStatus(codes.Error, http.StatusText(status))
	}
	if capturePayload && prompt != "" {
		s.span.SetAttributes(attribute.String("gen_ai.prompt", prompt))
	}
	if capturePayload && output != "" {
		s.span.SetAttributes(attribute.String("gen_ai.completion", extractPrompt([]byte(output))))
	}
	for _, record := range records {
		applyUsageAttributes(s.span, record)
	}
	s.span.End()
}

func stateFromContext(ctx context.Context) *requestState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(requestStateKey{}).(*requestState)
	if state != nil {
		return state
	}
	ginCtx, _ := ctx.Value("gin").(*gin.Context)
	if ginCtx != nil && ginCtx.Request != nil {
		state, _ = ginCtx.Request.Context().Value(requestStateKey{}).(*requestState)
	}
	return state
}

func (p *Provider) capturePrompt(req *http.Request) (string, string) {
	if p == nil || req == nil || req.Body == nil {
		return "", ""
	}
	var buf bytes.Buffer
	limit := int64(32768)
	if p.cfg.PayloadCapture.Enabled && p.cfg.PayloadCapture.MaxPromptBytes > 0 {
		limit = int64(p.cfg.PayloadCapture.MaxPromptBytes)
	}
	if limit <= 0 {
		return "", ""
	}
	_, _ = io.CopyN(&buf, req.Body, limit+1)
	data := buf.Bytes()
	req.Body = io.NopCloser(io.MultiReader(bytes.NewReader(data), req.Body))
	if int64(len(data)) > limit {
		data = data[:limit]
	}
	model := requestModelFromBody(data)
	if !p.cfg.PayloadCapture.Enabled {
		return "", model
	}
	return extractPrompt(data), model
}

func operationFromPath(path string) string {
	switch {
	case strings.Contains(path, "/chat/completions"), strings.HasSuffix(path, "/messages"):
		return "chat"
	case strings.Contains(path, "/responses"):
		return "responses"
	case strings.Contains(path, "/embeddings"):
		return "embeddings"
	case strings.Contains(path, "/images"):
		return "images"
	default:
		return "unknown"
	}
}

func clientAddress(req *http.Request) string {
	if req == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		return host
	}
	return req.RemoteAddr
}

func extractPrompt(data []byte) string {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	var parts []string
	walkText(v, &parts)
	return strings.Join(parts, "\n")
}

func requestModelFromBody(body []byte) string {
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	if model, ok := v["model"].(string); ok {
		return strings.TrimSpace(model)
	}
	return ""
}

func walkText(v any, parts *[]string) {
	switch value := v.(type) {
	case map[string]any:
		for _, key := range []string{"messages", "choices", "message", "delta", "content", "text", "input", "prompt", "output"} {
			if child, ok := value[key]; ok {
				walkText(child, parts)
			}
		}
	case []any:
		for _, child := range value {
			walkText(child, parts)
		}
	case string:
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			*parts = append(*parts, trimmed)
		}
	}
}

type captureWriter struct {
	gin.ResponseWriter
	state *requestState
	limit int
	used  int
}

func (w *captureWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *captureWriter) WriteString(data string) (int, error) {
	w.capture([]byte(data))
	return w.ResponseWriter.WriteString(data)
}

func (w *captureWriter) capture(data []byte) {
	if w == nil || w.state == nil || w.limit <= 0 || len(data) == 0 || w.used >= w.limit {
		return
	}
	remaining := w.limit - w.used
	if len(data) > remaining {
		data = data[:remaining]
	}
	w.used += len(data)
	w.state.mu.Lock()
	w.state.output += string(data)
	w.state.mu.Unlock()
}
