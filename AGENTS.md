# SHIP Hook

External Prow plugin that receives GitHub webhook events from Prow Hook and dispatches them to internal sub-plugins.

## Build

```shell
make build     # compile binary to _output/ship-hook
make test      # run all unit tests (via gotestsum)
make vet       # run go vet
make verify    # vet + test
make image     # build container image (requires build first)
```

## Architecture

- `cmd/ship-hook/` - Server entrypoint. Uses `sigs.k8s.io/prow/pkg/githubeventserver` for HTTP event handling.
- `pkg/config/` - Configuration loading, merging, and resolution. Supports layered config (top/org/repo) with supplemental config files.
- `pkg/dispatch/` - Event dispatcher that multiplexes events to registered sub-plugins. Uses `config.Resolver` for enable/disable gating.
- `pkg/subplugins/` - Sub-plugin implementations. Each sub-package implements `dispatch.SubPlugin`.
- `images/ship-hook/` - Container image definition.
- `specs/` - Lightweight feature specifications.

## Adding a Sub-Plugin

1. Create a new package under `pkg/subplugins/<name>/`
2. Define a narrow `githubClient` interface for the GitHub API methods the plugin needs
3. Inject dependencies via constructor. Simple plugins typically start with `func New(ghc githubClient, resolver *config.Resolver) *Plugin`; add further parameters as the plugin requires (e.g. feature gates, permissions checker).
4. Implement the `dispatch.SubPlugin` interface
5. Define a `PluginConfig` struct and `defaultConfig()` if the plugin needs configuration
6. Use `config.ResolvePluginConfig[T](resolver, name, defaultCfg, org, repo)` in handlers to get typed config
7. Register the plugin in `cmd/ship-hook/main.go`
8. Add unit tests in `pkg/subplugins/<name>/<name>_test.go` using `fakegithub.NewFakeClient()`

See `pkg/subplugins/example/` for a reference implementation.

### Sub-Plugin Observability Rules

- Use the provided `*logrus.Entry` — never create your own logger. The dispatcher injects structured fields (`event_type`, `org`, `repo`, `pr`, `plugin`).
- Return meaningful `dispatch.HandlerResult` from every handler using the appropriate constructor: `Skipped` (wrong event type), `Filtered` (right type, properties don't match), `NoChange` (domain logic ran, no mutation needed), `Acted` (mutated state), `Errored` (failure). This is the primary observability channel. See `specs/006-handler-results.md` for boundary guidance.
- Do not log errors from handler failures — return `dispatch.Errored(reason, err)` and let the dispatcher log it. Do not log handler outcomes or decisions that the `HandlerResult` reason already captures — the dispatcher logs those. Reserve Debug logging for supplementary context not in the result (e.g., "config resolved to label=X"). Do not log event lifecycle ("received event", "handler complete") — the dispatcher handles that.
- Do not register Prometheus metrics directly. The dispatcher tracks handler duration per plugin automatically.

See `specs/007-observability.md` for the full observability design.

## Testing

Three layers of testing are available:

### Unit Tests

```shell
make test      # runs go test ./...
```

All sub-plugin handlers must have unit tests. Use `fakegithub.NewFakeClient()` from `sigs.k8s.io/prow/pkg/github/fakegithub` and inject it via the plugin constructor. Assert mutations via FakeClient tracking fields (`IssueCommentsAdded`, `IssueLabelsAdded`, etc.). Use `pkg/testhelpers/` for event construction: build a PR with `testhelpers.NewPR(org, repo, number)`, optionally chain `.WithAuthor(login)` and `.WithLabel(name)`, then produce an event with `.PullRequestEvent(action)`, `.ReviewEvent(action, reviewer, state)`, or `.CommentEvent(body)`.

### Integration Tests

```shell
make integration-test   # runs tests with //go:build integration tag
```

Located in `test/integration/plugins/<name>/` and `test/integration/cyborg/`. Use realistic JSON webhook payloads from `test/integration/testdata/`. Load events with `integration.LoadTestEvent[T](t, filename)`. Integration tests are excluded from `make test`. Cyborg integration tests use `orgdatacore.FakeDataSource` with synthetic test data — no GCP credentials needed.

For live GCS verification against the real cyborg data (requires `gcloud auth application-default login`):

```shell
go test -v -tags=gcs,integration,cyborg_live ./test/integration/cyborg/...
```

### Interactive Dev Server

```shell
make dev-server    # start ship-hook with fakegithub (port 8888 webhook, 8889 state)
make dev-webhook EVENT=pull_request PAYLOAD=test/integration/testdata/pull_request_opened.json
make dev-state     # inspect fakegithub mutations
make dev-reset     # clear state between tests
make dev-watch     # auto-restart on code changes (requires watchexec)
```

The dev server uses an in-memory fakegithub client. No real GitHub API calls are made. Use the `/dev-test` Claude Code skill for agent-driven testing.

## Dependencies

- `sigs.k8s.io/prow` - Prow libraries for event server, GitHub types, and utilities
- `github.com/openshift/ci-tools` - May be added for shared CI tooling code
