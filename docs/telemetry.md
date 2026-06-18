# OpenTelemetry

CLIProxyAPI can export OpenTelemetry traces and metrics over OTLP. Telemetry is disabled by default.

Minimal OTLP gRPC:

```yaml
telemetry:
  enabled: true
  service-name: "cliproxyapi"
  otlp:
    endpoint: "localhost:4317"
    protocol: "grpc"
    insecure: true
```

Minimal OTLP HTTP, for example OpenObserve:

```yaml
telemetry:
  enabled: true
  otlp:
    endpoint: "localhost:5080"
    protocol: "http"
    path-prefix: "api/default"
    insecure: true
    headers:
      - "Authorization: Basic <redacted>"
```

Supported environment overrides:

```bash
CLIPROXY_TELEMETRY_ENABLED=true
CLIPROXY_TELEMETRY_SERVICE_NAME=cliproxyapi
CLIPROXY_TELEMETRY_OTLP_ENDPOINT=localhost:4317
CLIPROXY_TELEMETRY_OTLP_PROTOCOL=grpc
CLIPROXY_TELEMETRY_OTLP_PATH=
CLIPROXY_TELEMETRY_OTLP_INSECURE=true
CLIPROXY_TELEMETRY_PAYLOAD_CAPTURE_ENABLED=false
```

CLIProxyAPI uses W3C Trace Context. If Olla forwards `traceparent` and `tracestate`, CLIProxyAPI spans become children in the same trace. Direct agent requests create root traces.

Payload capture is off by default. When enabled, CLIProxyAPI attaches bounded `gen_ai.prompt` and `gen_ai.completion` attributes. Keep it disabled when an upstream proxy such as Olla already captures payloads, to avoid duplicate sensitive data.
