package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTelemetryDefaultsAndEnvOverrides(t *testing.T) {
	t.Setenv("CLIPROXY_TELEMETRY_ENABLED", "true")
	t.Setenv("CLIPROXY_TELEMETRY_SERVICE_NAME", "custom-service")
	t.Setenv("CLIPROXY_TELEMETRY_OTLP_ENDPOINT", "otel:4318")
	t.Setenv("CLIPROXY_TELEMETRY_OTLP_PROTOCOL", "http")
	t.Setenv("CLIPROXY_TELEMETRY_OTLP_PATH", "otel")
	t.Setenv("CLIPROXY_TELEMETRY_OTLP_INSECURE", "false")
	t.Setenv("CLIPROXY_TELEMETRY_PAYLOAD_CAPTURE_ENABLED", "true")

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8317\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if !cfg.Telemetry.Enabled {
		t.Fatalf("Telemetry.Enabled = false, want true")
	}
	if got := cfg.Telemetry.ServiceName; got != "custom-service" {
		t.Fatalf("ServiceName = %q", got)
	}
	if got := cfg.Telemetry.OTLP.Endpoint; got != "otel:4318" {
		t.Fatalf("Endpoint = %q", got)
	}
	if got := cfg.Telemetry.OTLP.Protocol; got != "http" {
		t.Fatalf("Protocol = %q", got)
	}
	if got := cfg.Telemetry.OTLP.PathPrefix; got != "otel" {
		t.Fatalf("PathPrefix = %q", got)
	}
	if cfg.Telemetry.OTLP.Insecure {
		t.Fatalf("Insecure = true, want false")
	}
	if !cfg.Telemetry.PayloadCapture.Enabled {
		t.Fatalf("PayloadCapture.Enabled = false, want true")
	}
}

func TestTelemetryProtocolValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("telemetry:\n  otlp:\n    protocol: zipkin\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig error = nil, want protocol validation error")
	}
}
