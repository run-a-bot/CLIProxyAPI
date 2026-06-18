package config

import (
	"fmt"
	"os"
	"strings"
)

// TelemetryConfig controls OpenTelemetry export.
type TelemetryConfig struct {
	Enabled        bool                   `yaml:"enabled" json:"enabled"`
	ServiceName    string                 `yaml:"service-name" json:"service-name"`
	ServiceVersion string                 `yaml:"service-version" json:"service-version"`
	OTLP           TelemetryOTLPConfig    `yaml:"otlp" json:"otlp"`
	Traces         TelemetrySignalConfig  `yaml:"traces" json:"traces"`
	Metrics        TelemetrySignalConfig  `yaml:"metrics" json:"metrics"`
	Logs           TelemetryLogsConfig    `yaml:"logs" json:"logs"`
	PayloadCapture TelemetryPayloadConfig `yaml:"payload-capture" json:"payload-capture"`
}

// TelemetryOTLPConfig controls OTLP exporter settings.
type TelemetryOTLPConfig struct {
	Endpoint         string   `yaml:"endpoint" json:"endpoint"`
	Protocol         string   `yaml:"protocol" json:"protocol"`
	PathPrefix       string   `yaml:"path-prefix" json:"path-prefix"`
	Headers          []string `yaml:"headers" json:"headers"`
	Insecure         bool     `yaml:"insecure" json:"insecure"`
	SkipHealthTraces bool     `yaml:"skip-health-traces" json:"skip-health-traces"`
}

// TelemetrySignalConfig toggles one OpenTelemetry signal.
type TelemetrySignalConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// TelemetryLogsConfig controls log correlation settings.
type TelemetryLogsConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Level   string `yaml:"level" json:"level"`
}

// TelemetryPayloadConfig controls optional prompt and completion capture.
type TelemetryPayloadConfig struct {
	Enabled          bool `yaml:"enabled" json:"enabled"`
	MaxPromptBytes   int  `yaml:"max-prompt-bytes" json:"max-prompt-bytes"`
	MaxResponseBytes int  `yaml:"max-response-bytes" json:"max-response-bytes"`
}

// DefaultTelemetryConfig returns safe OpenTelemetry defaults.
func DefaultTelemetryConfig() TelemetryConfig {
	return TelemetryConfig{
		Enabled:        false,
		ServiceName:    "cliproxyapi",
		ServiceVersion: "",
		OTLP: TelemetryOTLPConfig{
			Endpoint:         "localhost:4317",
			Protocol:         "grpc",
			Insecure:         true,
			SkipHealthTraces: true,
		},
		Traces:  TelemetrySignalConfig{Enabled: true},
		Metrics: TelemetrySignalConfig{Enabled: true},
		Logs: TelemetryLogsConfig{
			Enabled: false,
			Level:   "info",
		},
		PayloadCapture: TelemetryPayloadConfig{
			Enabled:          false,
			MaxPromptBytes:   32768,
			MaxResponseBytes: 65536,
		},
	}
}

// NormalizeTelemetryConfig applies defaults, environment overrides, and validation.
func (cfg *Config) NormalizeTelemetryConfig() error {
	if cfg == nil {
		return nil
	}
	defaults := DefaultTelemetryConfig()
	t := &cfg.Telemetry
	if strings.TrimSpace(t.ServiceName) == "" {
		t.ServiceName = defaults.ServiceName
	}
	if strings.TrimSpace(t.OTLP.Endpoint) == "" {
		t.OTLP.Endpoint = defaults.OTLP.Endpoint
	}
	if strings.TrimSpace(t.OTLP.Protocol) == "" {
		t.OTLP.Protocol = defaults.OTLP.Protocol
	}
	if t.PayloadCapture.MaxPromptBytes <= 0 {
		t.PayloadCapture.MaxPromptBytes = defaults.PayloadCapture.MaxPromptBytes
	}
	if t.PayloadCapture.MaxResponseBytes <= 0 {
		t.PayloadCapture.MaxResponseBytes = defaults.PayloadCapture.MaxResponseBytes
	}
	if strings.TrimSpace(t.Logs.Level) == "" {
		t.Logs.Level = defaults.Logs.Level
	}
	applyTelemetryEnv(t)
	t.ServiceName = strings.TrimSpace(t.ServiceName)
	t.ServiceVersion = strings.TrimSpace(t.ServiceVersion)
	t.OTLP.Endpoint = strings.TrimSpace(t.OTLP.Endpoint)
	t.OTLP.Protocol = strings.ToLower(strings.TrimSpace(t.OTLP.Protocol))
	t.OTLP.PathPrefix = strings.Trim(strings.TrimSpace(t.OTLP.PathPrefix), "/")
	t.Logs.Level = strings.ToLower(strings.TrimSpace(t.Logs.Level))
	switch t.OTLP.Protocol {
	case "grpc", "http", "http/protobuf":
		if t.OTLP.Protocol == "http/protobuf" {
			t.OTLP.Protocol = "http"
		}
	default:
		return fmt.Errorf("telemetry otlp protocol %q is unsupported (use grpc or http)", t.OTLP.Protocol)
	}
	return nil
}

func applyTelemetryEnv(t *TelemetryConfig) {
	if t == nil {
		return
	}
	setBool := func(key string, dst *bool) {
		value, ok := os.LookupEnv(key)
		if !ok {
			return
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "y", "on":
			*dst = true
		case "0", "false", "no", "n", "off":
			*dst = false
		}
	}
	setString := func(key string, dst *string) {
		if value, ok := os.LookupEnv(key); ok {
			*dst = strings.TrimSpace(value)
		}
	}
	setBool("CLIPROXY_TELEMETRY_ENABLED", &t.Enabled)
	setString("CLIPROXY_TELEMETRY_SERVICE_NAME", &t.ServiceName)
	setString("CLIPROXY_TELEMETRY_OTLP_ENDPOINT", &t.OTLP.Endpoint)
	setString("CLIPROXY_TELEMETRY_OTLP_PROTOCOL", &t.OTLP.Protocol)
	setString("CLIPROXY_TELEMETRY_OTLP_PATH", &t.OTLP.PathPrefix)
	setBool("CLIPROXY_TELEMETRY_OTLP_INSECURE", &t.OTLP.Insecure)
	setBool("CLIPROXY_TELEMETRY_PAYLOAD_CAPTURE_ENABLED", &t.PayloadCapture.Enabled)
}
