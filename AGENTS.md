# AGENTS.md

Go 1.26+ proxy server providing OpenAI/Gemini/Claude/Codex compatible APIs with provider API key scheduling.

## Repository
- GitHub: https://github.com/icaruszezen/AIProviderScheduling

## Commands
```bash
gofmt -w . # Format (required after Go changes)
go build -o AIProviderScheduling ./cmd/server # Build
go run ./cmd/server # Run dev server
go test ./... # Run all tests
go test -v -run TestName ./path/to/pkg # Run single test
go build -o test-output ./cmd/server && rm test-output # Verify compile (REQUIRED after changes)
```
- Common flags: `--config <path>`, `--tui`, `--standalone`, `--local-model`

## Config
- Default config: `config.yaml` (template: `config.example.yaml`)
- `.env` is auto-loaded from the working directory
- Provider credentials come from config (`*-api-key`, `openai-compatibility`, `vertex-api-key`, `antigravity-api-key`). Official Vertex service accounts use `vertex-api-key[].service-account`.
- Account OAuth / `auths/` JSON login is not supported. Remote stores (`PGSTORE_*`, `GITSTORE_*`, `OBJECTSTORE_*`) host `config.yaml`; leftover `auths/` directories are not loaded into the scheduler. `Manager.Load` and remote token-store `List` skip `AuthKindOAuth` records if a store is reattached later.
- Plugin ABI: `AuthProvider.StartLogin` / `PollLogin` are retired (`ErrOAuthLoginUnsupported`). `HostConfigSummary` no longer has `AuthDir`, `OAuthModelAlias`, or `ExcludedModels` (breaking change; only `ProxyURL` and `ForceModelPrefix` remain).
- `cmd/fetch_codex_models` and `cmd/fetch_antigravity_models` are retired stubs. They do not call upstream; configure `codex-api-key` / `antigravity-api-key` (with `project-id`) in `config.yaml` instead.

## Architecture
- `cmd/server/` — Server entrypoint
- `internal/api/` — Gin HTTP API (routes, middleware)
- `internal/thinking/` — Main thinking/reasoning pipeline. `ApplyThinking()` (apply.go) parses suffixes (`suffix.go`, suffix overrides body), normalizes config to canonical `ThinkingConfig` (`types.go`), normalizes and validates centrally (`validate.go`/`convert.go`), then applies provider-specific output via `ProviderApplier`. Do not break this "canonical representation → per-provider translation" architecture.
- `internal/runtime/executor/` — Per-provider runtime executors (incl. Codex WebSocket)
- `internal/translator/` — Provider protocol translators (and shared `common`)
- `internal/registry/` — Model registry + remote updater (`StartModelsUpdater`); `--local-model` disables remote updates
- `internal/store/` — Storage implementations and secret resolution
- `internal/managementasset/` — Config snapshots and management assets
- `internal/cache/` — Request signature caching
- `internal/watcher/` — Config hot-reload and watchers
- `internal/wsrelay/` — WebSocket relay sessions
- `internal/usage/` — Usage and token accounting
- `internal/tui/` — Bubbletea terminal UI (`--tui`, `--standalone`)
- `sdk/cliproxy/` — Embeddable SDK entry (service/builder/watchers/pipeline)
- `test/` — Cross-module integration tests

## Code Conventions
- Keep changes small and simple (KISS)
- Comments in English only
- If editing code that already contains non-English comments, translate them to English (don’t add new non-English comments)
- For user-visible strings, keep the existing language used in that file/area
- New Markdown docs should be in English unless the file is explicitly language-specific (e.g. `README_CN.md`)
- As a rule, do not make standalone changes to `internal/translator/`. You may modify it only as part of broader changes elsewhere.
- If a task requires changing only `internal/translator/`, run `gh repo view --json viewerPermission -q .viewerPermission` to confirm you have `WRITE`, `MAINTAIN`, or `ADMIN`. If you do, you may proceed; otherwise, file a GitHub issue including the goal, rationale, and the intended implementation code, then stop further work.
- `internal/runtime/executor/` should contain executors and their unit tests only. Place any helper/supporting files under `internal/runtime/executor/helps/`.
- Follow `gofmt`; keep imports goimports-style; wrap errors with context where helpful
- Do not use `log.Fatal`/`log.Fatalf` (terminates the process); prefer returning errors and logging via logrus
- Shadowed variables: use method suffix (`errStart := server.Start()`)
- Wrap defer errors: `defer func() { if err := f.Close(); err != nil { log.Errorf(...) } }()`
- Use logrus structured logging; avoid leaking secrets/tokens in logs
- Avoid panics in HTTP handlers; prefer logged errors and meaningful HTTP status codes
- Timeouts are allowed only during credential acquisition; after an upstream connection is established, do not set timeouts for any subsequent network behavior. Intentional exceptions that must remain allowed are the Codex websocket liveness deadlines in `internal/runtime/executor/codex_websockets_executor.go`, the wsrelay session deadlines in `internal/wsrelay/session.go`, the management APICall timeout in `internal/api/handlers/management/api_tools.go`, and a channel's `stream-first-token-timeout-seconds` wait. That wait applies only when the channel sets a positive value, only until the first streaming token, and only by canceling that attempt's context. Do not add a read timeout after the first token has arrived.
- Avoid wall-clock `time.Sleep` in TTL, expiration, ordering, or cache-eviction unit tests due to platform timer granularity (e.g. Windows default timer resolution of ~15.6ms) and CI jitter under load; prefer controllable clocks (`nowFunc` / mock clock), explicit timestamp manipulation, or deterministic synchronization primitives.
