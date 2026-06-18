# patch.md

## meta

- name: run-a-bot-cliproxyapi
- target: CLIProxyAPI Go fork
- version: 1.0.0

## commits

### 1. Add OTLP telemetry support

#### background

Operators running CLIProxyAPI in distributed environments require OpenTelemetry
observability to monitor model routing, token usage, latency, and system health.
This change integrates OpenTelemetry (traces and metrics) over gRPC and HTTP OTLP
exporters, supports optional request/response payload capture with configurable size
limits, and introduces an end-to-end integration test harness with a mock OpenAI backend.

#### files

##### internal/config/telemetry.go (create)

Define `TelemetryConfig` and related structs (`TelemetryOTLPConfig`, `TelemetrySignalConfig`,
`TelemetryLogsConfig`, `TelemetryPayloadConfig`), default configuration values, and
environment variable overrides (`CLIPROXY_TELEMETRY_*`).

##### internal/config/config.go (modify)

Add the `Telemetry` field to the root `Config` struct.

##### internal/config/config_load.go (modify)

Initialize `cfg.Telemetry` defaults and invoke `NormalizeTelemetryConfig` during configuration loading.

##### internal/config/config_v8.go (modify)

Map legacy `telemetry` configuration paths to `observability.telemetry` under the v8 layout.

##### internal/config/telemetry_test.go (create)

Unit test telemetry defaults, environment variable overrides, and protocol validation.

##### internal/telemetry/provider.go (create)

Implement `telemetry.Provider` to manage the TracerProvider and MeterProvider lifecycles,
OTLP gRPC/HTTP exporter creation, and shutdown.

##### internal/telemetry/provider_test.go (create)

Unit test provider initialization and resource merge schema compatibility.

##### internal/telemetry/middleware.go (create)

Implement Gin middleware that extracts W3C trace context, starts HTTP server spans,
records request and response attributes, and correlates trace IDs.

##### internal/telemetry/middleware_test.go (create)

Unit test the Gin telemetry middleware and span attribute recording.

##### internal/telemetry/usage.go (create)

Record token usage metrics and span events from usage records dispatched by the proxy.

##### sdk/cliproxy/service.go (modify)

Add the `telemetry` provider field to `cliproxy.Service`.

##### sdk/cliproxy/service_lifecycle.go (modify)

Initialize the telemetry provider on service startup (`Run`), attach its middleware to server options,
and gracefully shut down telemetry exporters during `Shutdown`.

##### sdk/cliproxy/usage/manager.go (modify)

Support synchronous observer hooks via `RegisterHook` and dispatch them on usage publishing.

##### config.example.yaml (modify)

Add the `telemetry` configuration example under `observability`.

##### docs/telemetry.md (create)

Document OpenTelemetry configuration, supported protocols, signal toggles, payload capture,
and environment variable overrides.

##### .agents/skills/otel-dev-loop/SKILL.md (create)

Add development loop skill instructions for local OTLP collector testing.

##### test/scripts/integration/otel-e2e.sh (create)

Add an end-to-end test script verifying OTLP trace and metric export using a local collector.

##### test/scripts/integration/mock_openai_backend.py (create)

Implement a mock OpenAI-compatible HTTP backend for OTLP integration testing.

##### test/scripts/integration/otelcol_config.yaml (create)

Configuration for the OpenTelemetry Collector container used in integration testing.

##### go.mod (modify)

Add OpenTelemetry dependencies (`go.opentelemetry.io/otel`, exporters, and SDKs).

##### .gitignore (modify)

Allow tracking `docs/telemetry.md` and the otel dev loop skill files.

##### AGENTS.md (modify)

Document the existence and maintenance expectations of `patch.md`.

#### verify

- `go test -v ./internal/telemetry ./internal/config` passes.
- `CLIPROXY_TELEMETRY_ENABLED=true` enables tracing and metric export to the configured OTLP collector.
- `otel-e2e.sh` verifies span creation, W3C trace propagation, and token metric export against the mock backend.
