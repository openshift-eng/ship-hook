# CodeRabbit Auto-Label on PR Creation

## Status
Draft

## Motivation

The ready-for-humans plugin currently labels PRs only after CodeRabbit submits an approving review. This works for RH employees who have CodeRabbit licenses, but external contributors never receive a CodeRabbit review, so their PRs are never labeled as ready for human attention.

When a PR is opened by someone without CodeRabbit access, the label should be added immediately — there is no CodeRabbit review to wait for.

## Design

### Feature Gate

The entire behavior is behind the `coderabbit-auto-label` feature gate (default ON). When the gate is off, `HandlePullRequestEvent` remains a no-op. The existing review-based labeling is unaffected by the gate.

### Permissions Interface

A new `pkg/permissions` package provides a generic authorization interface:

```go
type Checker interface {
    IsAllowed(user, action, resource string) bool
}
```

The initial implementation is `StubChecker` — a minimal stub that only handles `action="access"`, `resource="coderabbit"` by delegating to an injected function. All other action/resource combinations are denied. The stub is explicitly documented as a placeholder for a future policy engine (e.g., casbin).

### Cyborg Integration

For the coderabbit access check, the stub delegates to a function that queries the cyborg-data library (`GetEmployeeByGitHubID`). If the GitHub user is found as an RH employee, they are assumed to have CodeRabbit access. If not found, they don't.

The cyborg-data service is initialized in `main.go` when `--cyborg.credentials-file` is provided:
- GCS data source: bucket `resolved-org`, object `orgdata/comprehensive_index_dump.json`, project `openshift-crt`
- Data source watcher enabled for automatic refresh
- If the flag is not provided, the access check always returns false (no access → label immediately)

The flag is always accepted regardless of gate state. When the gate is off, cyborg data loads but is never queried.

### Plugin Behavior

`HandlePullRequestEvent` in the ready-for-humans plugin:

1. Check feature gate — if off, return Irrelevant
2. Check action is "opened" — other PR actions are ignored
3. Check PR doesn't already have the label (idempotency)
4. Check `permissions.IsAllowed(author, "access", "coderabbit")`
   - If allowed (RH employee): return Irrelevant — CodeRabbit will review and the existing review handler will label
   - If not allowed: add the ready-for-human-review label immediately

The existing `HandleReviewEvent` is unchanged.

### Build

The cyborg-data library requires `//go:build gcs` for GCS functionality. All Makefile targets that compile code (`build`, `test`, `vet`, `lint`, `dev-server`, `dev-watch`) pass `-tags gcs`.

## Verification

1. `make verify` passes
2. Dev server test: send a `pull_request` opened event with `SHIP_HOOK_FEATURE_CODERABBIT_AUTO_LABEL=true`, confirm label is added
3. Dev server test: send the same event with the gate off, confirm no label
4. Integration test: with GCP auth configured, cyborg lookup finds a known RH employee by GitHub ID
