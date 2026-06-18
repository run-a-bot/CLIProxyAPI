package telemetry

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestNewProviderResourceMerge(t *testing.T) {
	cfg := config.TelemetryConfig{
		Enabled:        true,
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Traces: config.TelemetrySignalConfig{
			Enabled: false,
		},
		Metrics: config.TelemetrySignalConfig{
			Enabled: false,
		},
	}
	p, err := NewProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider to be non-nil")
	}
}
