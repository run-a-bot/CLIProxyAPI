---
name: otel-dev-loop
description: >
  Reusable local OpenTelemetry development loop for CLIProxyAPI. Uses a mock
  OpenAI-compatible backend and a real OTLP collector container to verify traces,
  metrics, W3C trace context, and optional payload capture over gRPC and HTTP OTLP.
---

# OTel Dev Loop

Use when working on CLIProxyAPI telemetry, OTLP export, trace context propagation,
metrics, or payload capture.

## Loop

1. Edit code.
2. Run focused tests for touched packages.
3. Run required build:
   ```bash
   go build -o test-output ./cmd/server && rm test-output
   ```
4. Run E2E:
   ```bash
   ./test/scripts/integration/otel-e2e.sh
   ```
5. If E2E fails, inspect:
   - collector logs from printed container name `cliproxyapi-otel-e2e`
   - server log under script temp dir if script printed an error
   - mock backend behavior in `test/scripts/integration/mock_openai_backend.py`
6. Fix and repeat until both OTLP transports pass.

## E2E Shape

`./test/scripts/integration/otel-e2e.sh` runs:

- local Python OpenAI-compatible mock backend
- CLIProxyAPI container image built from current worktree
- CLIProxyAPI app container on host network namespace
- real `otel/opentelemetry-collector-contrib` container
- collector container on host network namespace
- gRPC OTLP on `127.0.0.1:4317`
- HTTP OTLP on `127.0.0.1:4318` with path prefix `api/default/otel`

Everything is mocked locally except the collector container. The script removes the
CLIProxyAPI and collector containers during cleanup.

## What E2E Verifies

- missing client `Authorization` returns `401`
- authenticated `/v1/chat/completions` nonstream request succeeds
- authenticated streaming request succeeds
- inbound W3C `traceparent` trace ID appears in collector output
- collector receives `service.name=cliproxyapi-e2e`
- spans include `gen_ai.request.model`, prompt, and completion attributes
- metrics include request and token instruments
- both OTLP gRPC and HTTP transports work with collector bearer-token auth

## Files

- `test/scripts/integration/otel-e2e.sh`: full automated loop
- `test/scripts/integration/mock_openai_backend.py`: local mock LLM backend
- `test/scripts/integration/otelcol_config.yaml`: authenticated collector config

## Notes

- Requires `podman` or `docker` with host network support.
- Override collector image with `COLLECTOR_IMAGE=...`.
- Override app image/name with `APP_IMAGE=...` or `APP_NAME=...`.
- Do not add real API keys or real OAuth material; E2E must stay fully local.
