# Handler Results

## Status
Implemented

## Motivation

Sub-plugin handlers need to report their outcome back to the dispatcher so the dispatcher can log, metric-label, and expose the result to downstream tooling (log explorers, dashboards).

The original `HandlerResult{Relevant bool, Reason string}` collapsed three distinct "didn't act" situations into a single boolean, making it impossible for downstream consumers to distinguish noise (wrong event type) from meaningful inaction (domain logic ran, no mutation needed) without pattern-matching on reason strings.

A second gap was error handling: handlers that failed logged the error themselves and still returned a success result, hiding failures from the dispatcher and metrics.

## Design

### Outcome Type

A five-value `Outcome` enum classifies every handler invocation:

| Outcome | Constructor | Meaning | Log level |
|---------|-------------|---------|-----------|
| `OutcomeSkipped` | `Skipped(reason)` | Wrong event type entirely | Trace |
| `OutcomeFiltered` | `Filtered(reason)` | Right event type, properties don't match | Debug |
| `OutcomeNoChange` | `NoChange(reason)` | Domain logic ran, no mutation needed | Info |
| `OutcomeActed` | `Acted(reason)` | Plugin mutated state | Info |
| `OutcomeErrored` | `Errored(reason, err)` | Handler failed | Error |

The zero value is `OutcomeSkipped`, so an uninitialized `HandlerResult{}` means "didn't do anything."

### HandlerResult Struct

```go
type HandlerResult struct {
    Outcome Outcome
    Reason  string
    Err     error
}
```

Errors are captured in the result itself rather than as a second return value. The dispatcher logs the error (at Error level) and records it in metrics. Plugins should not log errors themselves — return `Errored(reason, err)` and let the dispatcher handle it.

### Prometheus Metric

The histogram `shiphook_plugin_handle_duration_seconds` uses an `outcome` label with values `skipped`, `filtered`, `no_change`, `acted`, `errored`.

### SubPlugin Interface

Handler method signatures are unchanged — they return `HandlerResult`:

```go
type SubPlugin interface {
    Name() string
    HandlePullRequestEvent(context.Context, *logrus.Entry, github.PullRequestEvent) HandlerResult
    HandleIssueCommentEvent(context.Context, *logrus.Entry, github.IssueCommentEvent) HandlerResult
    HandleReviewEvent(context.Context, *logrus.Entry, github.ReviewEvent) HandlerResult
}
```

### Choosing the Right Outcome

Use `Skipped` when the handler does not process this event type at all. A review-only plugin receiving a pull_request event returns `Skipped("only handles review events")`.

Use `Filtered` when the event type is correct but the event's properties (action, user, state) do not match the plugin's criteria. The handler checked surface properties and stopped before entering its core domain logic.

Use `NoChange` when the plugin's domain logic ran and correctly determined no mutation was needed. The handler made at least one significant business-logic decision specific to its purpose.

Use `Acted` when the plugin mutated state (added a label, posted a comment, etc.).

Use `Errored` when the handler encountered an error. Return the error via `Errored(reason, err)` — do not log it manually.

**Boundary guidance (Filtered vs NoChange):** if the handler has not yet made any decision specific to its core purpose, use `Filtered`. Once the handler enters logic that evaluates whether its mutation is needed, use `NoChange`. For example, in a label-adding plugin: checking whether the reviewer matches or the action is correct is filtering (prerequisites). Checking whether the label already exists is a domain decision (NoChange).

## Verification

1. `make verify` passes
2. Dev server test: send events, confirm dispatcher logs show all outcomes (`"skipped"`, `"filtered"`, `"no_change"`, `"acted"`, `"errored"`) at the correct log levels
3. Check `/metrics` endpoint for `shiphook_plugin_handle_duration_seconds` with `outcome` label
