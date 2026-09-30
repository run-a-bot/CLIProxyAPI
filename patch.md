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

### 2. Add trusted header authentication for management API/UI

#### background

When deploying CLIProxyAPI behind an authenticating reverse proxy (such as Authelia,
OAuth2-Proxy, or Cloudflare Access), the proxy has already verified user identity and
forwards identity headers (e.g. `X-User-UUID`). Requiring administrators to manually
enter a secret key inside protected internal networks creates unnecessary friction.
This change introduces trusted header authentication for the `/v0/management` and
`/v8/management` APIs, validating client IPs against configured trusted proxies or CIDRs.
In addition, when serving the management web dashboard (`/management.html`), an inline
bootstrap script is automatically injected to pre-seed the single-page application's
`localStorage` credentials (`isLoggedIn` and `managementKey`), allowing administrators
to access the UI directly without an extraneous login prompt.

#### files

##### internal/config/config_types.go (modify)

Add `TrustedHeaderAuth` to `RemoteManagement` with settings for `enabled`, `user-id-header`,
and `trusted-proxies`.

##### internal/config/config_normalization.go (modify)

Add `applyRemoteManagementEnv` to support environment variable configuration:
`CLIPROXYAPI_TRUSTED_HEADER_AUTH_ENABLED`, `CLIPROXYAPI_TRUSTED_USER_ID_HEADER`, and
`CLIPROXYAPI_TRUSTED_HEADER_AUTH_PROXIES`.

##### internal/config/config_load.go (modify)

Apply remote management environment overrides during configuration loading.

##### internal/config/parse.go (modify)

Apply remote management environment overrides when parsing configuration bytes.

##### internal/api/handlers/management/handler.go (modify)

Implement trusted header authentication in `Handler.Middleware()`, verifying that the
caller's IP belongs to `TrustedProxies` (or localhost) and extracting the user ID header.
Add the `Whoami` endpoint returning authentication status, method, and user ID.

##### internal/api/handlers/management/handler_test.go (modify)

Add test cases covering disabled header auth, untrusted client IPs, trusted CIDRs, trusted
single IPs, empty user ID headers, and localhost access.

##### internal/api/server_management.go (modify)

Register `/v0/management/whoami` on the management router. In `serveManagementControlPanel`,
inject the inline trusted-header bootstrap script into `/management.html` when
`cfg.RemoteManagement.TrustedHeaderAuth.Enabled` is `true`.

##### internal/api/server_management_v8.go (modify)

Register `/v8/management/whoami` on the v8 management router.

##### internal/api/server_test.go (modify)

Add `TestInjectTrustedHeaderBootstrap` verifying script injection before `<script type="module">`
or `</head>`, ensuring idempotency and verification of `localStorage` initialization.

#### verify

- Requests from untrusted IPs with header authentication are rejected with HTTP 403.
- Requests from trusted CIDRs/IPs with valid user headers succeed and authenticate.
- `/v0/management/whoami` and `/v8/management/whoami` return `{"auth_method":"header","authenticated":true,"user_id":"..."}`.
- Navigating to `/management.html` with trusted header authentication enabled serves HTML containing the bootstrap script, pre-populating `localStorage.isLoggedIn` and `localStorage.managementKey`.
- `go test -v ./internal/api/handlers/management` passes.
- `go test -v -run TestInjectTrustedHeaderBootstrap ./internal/api` passes.

### 3. Bundle latest compatible management.html release

#### background

CLIProxyAPI serves the management control panel at `/management.html`. While upstream
attempts to download `management.html` dynamically from GitHub releases at runtime into
the runtime `static/` directory, air-gapped, containerized, or offline environments
cannot reach GitHub, leading to startup delays, missing dashboards, or download failures.
By tracking the latest compatible release of `management.html` directly in the repository
under `./web/management.html`, container image builds and deployments can copy this
bundled asset into the runtime location (e.g. `./static/management.html` or the path
specified by `MANAGEMENT_STATIC_PATH`) without runtime external network calls.

#### how to find newest compatible release

When rebasing CLIProxyAPI to a newer upstream release or updating the management UI:

1. Identify the upstream management center repository:
   By default, CLIProxyAPI uses `https://github.com/router-for-me/Cli-Proxy-API-Management-Center`
   (configured as `DefaultPanelGitHubRepository` in `internal/config/config_defaults.go` and
   `defaultManagementReleaseURL` in `internal/managementasset/updater.go`).

2. Query GitHub releases for the latest version tag:
   ```bash
   MANAGEMENT_PANEL_VERSION=$(curl -s https://api.github.com/repos/router-for-me/Cli-Proxy-API-Management-Center/releases/latest | jq -r '.tag_name')
   echo "Latest panel release: ${MANAGEMENT_PANEL_VERSION}"
   ```

3. Download the matching `management.html` asset directly into `./web/`:
   ```bash
   mkdir -p web
   curl -sL -o ./web/management.html "https://github.com/router-for-me/Cli-Proxy-API-Management-Center/releases/download/${MANAGEMENT_PANEL_VERSION}/management.html"
   ```

4. Verify that `./web/management.html` was downloaded correctly and is a valid non-empty HTML file:
   ```bash
   head -n 5 ./web/management.html
   ```

5. When building container images or deploying, copy `./web/management.html` to the target runtime static location (or point `MANAGEMENT_STATIC_PATH` to it):
   ```dockerfile
   COPY ./web/management.html /CLIProxyAPI/static/management.html
   ```

#### files

##### web/management.html (create)

Bundled production release asset of the management center web dashboard (version `v1.25.0`).

#### verify

- `./web/management.html` exists and contains valid HTML for the management panel.
- Container builds or deployments copying `./web/management.html` to `static/management.html` (or setting `MANAGEMENT_STATIC_PATH`) serve the bundled dashboard without fetching from GitHub.
- `patch.md` documents all fork patches, their rationale, file changes, and verification procedures.
