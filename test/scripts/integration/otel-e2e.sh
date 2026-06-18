#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT_DIR="$ROOT_DIR/test/scripts/integration"
COLLECTOR_IMAGE="${COLLECTOR_IMAGE:-otel/opentelemetry-collector-contrib:0.140.1}"
COLLECTOR_NAME="${COLLECTOR_NAME:-cliproxyapi-otel-e2e}"
APP_IMAGE="${APP_IMAGE:-cliproxyapi-otel-e2e:local}"
APP_NAME="${APP_NAME:-cliproxyapi-otel-e2e-app}"
CLIENT_KEY="cliproxy-e2e-client-key"
OTLP_TOKEN="cliproxy-otel-e2e-token"
TRACE_ID="4bf92f3577b34da6a3ce929d0e0e4736"
TRACEPARENT="00-${TRACE_ID}-00f067aa0ba902b7-01"

TMP_DIR="$(mktemp -d)"
MOCK_PID=""
RUNTIME=""

cleanup() {
  if [[ -n "${MOCK_PID}" ]] && kill -0 "${MOCK_PID}" 2>/dev/null; then
    kill "${MOCK_PID}" 2>/dev/null || true
    wait "${MOCK_PID}" 2>/dev/null || true
  fi
  if [[ -n "${RUNTIME}" ]]; then
    "${RUNTIME}" rm -f "${APP_NAME}" >/dev/null 2>&1 || true
    "${RUNTIME}" rm -f "${COLLECTOR_NAME}" >/dev/null 2>&1 || true
  fi
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

pick_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

wait_http() {
  local url="$1"
  local label="$2"
  for _ in $(seq 1 80); do
    if curl -fsS --max-time 1 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  echo "timeout waiting for ${label}: ${url}" >&2
  if [[ "${label}" == "CLIProxyAPI" ]]; then
    "${RUNTIME}" logs "${APP_NAME}" >&2 2>/dev/null || true
  fi
  if [[ "${label}" == "mock backend" ]]; then
    cat "${TMP_DIR}/mock.log" >&2 2>/dev/null || true
  fi
  return 1
}

wait_log() {
  local pattern="$1"
  local label="$2"
  local logs
  for _ in $(seq 1 100); do
    logs="$("${RUNTIME}" logs "${COLLECTOR_NAME}" 2>&1 || true)"
    if grep -Fq "$pattern" <<<"${logs}"; then
      return 0
    fi
    sleep 0.3
  done
  echo "missing collector log for ${label}: ${pattern}" >&2
  "${RUNTIME}" logs "${COLLECTOR_NAME}" >&2 || true
  return 1
}

require_runtime() {
  if command -v podman >/dev/null 2>&1; then
    RUNTIME=podman
    return
  fi
  if command -v docker >/dev/null 2>&1; then
    RUNTIME=docker
    return
  fi
  echo "podman or docker required for otel collector container" >&2
  exit 1
}

start_collector() {
  "${RUNTIME}" rm -f "${COLLECTOR_NAME}" >/dev/null 2>&1 || true
  "${RUNTIME}" run -d --name "${COLLECTOR_NAME}" \
    --network host \
    -v "${SCRIPT_DIR}/otelcol_config.yaml:/etc/otelcol-contrib/config.yaml:ro" \
    "${COLLECTOR_IMAGE}" \
    --config /etc/otelcol-contrib/config.yaml >/dev/null
  for _ in $(seq 1 80); do
    if "${RUNTIME}" logs "${COLLECTOR_NAME}" 2>&1 | grep -Eq "Everything is ready|serving"; then
      return 0
    fi
    sleep 0.25
  done
  echo "collector did not become ready" >&2
  "${RUNTIME}" logs "${COLLECTOR_NAME}" >&2 || true
  return 1
}

start_mock() {
  local port="$1"
  python3 "${SCRIPT_DIR}/mock_openai_backend.py" --port "${port}" >"${TMP_DIR}/mock.log" 2>&1 &
  MOCK_PID=$!
  wait_http "http://127.0.0.1:${port}/healthz" "mock backend"
}

write_config() {
  local path="$1"
  local api_port="$2"
  local mock_port="$3"
  local protocol="$4"
  local endpoint="$5"
  local path_prefix="$6"
  cat >"${path}" <<YAML
host: "127.0.0.1"
port: ${api_port}
auth-dir: "${TMP_DIR}/auths"
api-keys:
  - "${CLIENT_KEY}"
debug: false
request-retry: 0
telemetry:
  enabled: true
  service-name: "cliproxyapi-e2e"
  service-version: "e2e"
  otlp:
    endpoint: "${endpoint}"
    protocol: "${protocol}"
    path-prefix: "${path_prefix}"
    headers:
      - "Authorization: Bearer ${OTLP_TOKEN}"
    insecure: true
    skip-health-traces: true
  traces:
    enabled: true
  metrics:
    enabled: true
  payload-capture:
    enabled: true
    max-prompt-bytes: 4096
    max-response-bytes: 8192
openai-compatibility:
  - name: "mock-openai"
    base-url: "http://127.0.0.1:${mock_port}/v1"
    api-key-entries:
      - api-key: "mock-upstream-key"
    models:
      - name: "mock-upstream-model"
        alias: "mock-model"
YAML
}

start_server() {
  local config_path="$1"
  local api_port="$2"
  "${RUNTIME}" rm -f "${APP_NAME}" >/dev/null 2>&1 || true
  "${RUNTIME}" run -d --name "${APP_NAME}" \
    --network host \
    -v "${TMP_DIR}:${TMP_DIR}" \
    "${APP_IMAGE}" \
    ./CLIProxyAPI --config "${config_path}" --local-model >/dev/null
  wait_http "http://127.0.0.1:${api_port}/healthz" "CLIProxyAPI"
}

stop_server() {
  "${RUNTIME}" rm -f "${APP_NAME}" >/dev/null 2>&1 || true
}

request_chat() {
  local api_port="$1"
  local stream="$2"
  local prompt="$3"
  local out="$TMP_DIR/response-${api_port}-${stream}.txt"
  curl -fsS ${stream:+-N} "http://127.0.0.1:${api_port}/v1/chat/completions" \
    -H "Authorization: Bearer ${CLIENT_KEY}" \
    -H "traceparent: ${TRACEPARENT}" \
    -H "Content-Type: application/json" \
    -d "{\"model\":\"mock-model\",\"messages\":[{\"role\":\"user\",\"content\":\"${prompt}\"}],\"stream\":${stream}}" >"${out}"
  grep -Fq "mock completion" "${out}"
}

run_transport() {
  local protocol="$1"
  local endpoint="$2"
  local path_prefix="$3"
  local api_port
  local mock_port
  api_port="$(pick_port)"
  mock_port="$(pick_port)"

  echo "== ${protocol} =="
  start_collector
  start_mock "${mock_port}"
  local config_path="${TMP_DIR}/config-${protocol}.yaml"
  write_config "${config_path}" "${api_port}" "${mock_port}" "${protocol}" "${endpoint}" "${path_prefix}"
  start_server "${config_path}" "${api_port}"

  local unauth_code
  unauth_code="$(curl -sS -o /dev/null -w "%{http_code}" "http://127.0.0.1:${api_port}/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -d '{"model":"mock-model","messages":[{"role":"user","content":"unauth"}],"stream":false}')"
  [[ "${unauth_code}" == "401" ]] || { echo "unauth status ${unauth_code}, want 401" >&2; exit 1; }

  request_chat "${api_port}" "false" "nonstream telemetry prompt"
  request_chat "${api_port}" "true" "stream telemetry prompt"

  wait_log "cliproxyapi-e2e" "service.name"
  wait_log "gen_ai.request.model" "request model attribute"
  wait_log "mock-model" "requested model value"
  wait_log "gen_ai.prompt" "payload prompt attribute"
  wait_log "nonstream telemetry prompt" "captured prompt"
  wait_log "gen_ai.completion" "payload completion attribute"
  wait_log "mock completion" "captured completion"
  wait_log "${TRACE_ID}" "propagated trace id"
  sleep 2
  wait_log "cliproxy.requests" "request metric"
  wait_log "cliproxy.tokens.total" "token metric"

  stop_server
  if [[ -n "${MOCK_PID}" ]] && kill -0 "${MOCK_PID}" 2>/dev/null; then
    kill "${MOCK_PID}" 2>/dev/null || true
    wait "${MOCK_PID}" 2>/dev/null || true
  fi
  MOCK_PID=""
  "${RUNTIME}" rm -f "${COLLECTOR_NAME}" >/dev/null 2>&1 || true
}

main() {
  require_runtime
  mkdir -p "${TMP_DIR}/auths"
  "${RUNTIME}" build -t "${APP_IMAGE}" "${ROOT_DIR}"
  run_transport grpc "127.0.0.1:4317" ""
  run_transport http "127.0.0.1:4318" "api/default/otel"
  echo "otel e2e ok"
}

main "$@"
