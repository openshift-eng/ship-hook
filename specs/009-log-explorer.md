# Log Explorer

## Status
Implemented

## Motivation

Ship-hook runs in multiple replicas processing GitHub webhook events. When investigating plugin behavior (e.g., "did ready-for-humans label this PR?"), operators must manually grep through interleaved JSON log files from multiple pods. The log explorer provides a focused, per-plugin view of event handling results — one line per event, filtering out noise.

The five-value `outcome` field logged by the dispatcher (spec 006) maps directly to the explorer's display levels, with no heuristics needed.

## Design

### CLI

```shell
ship-hook-explorer --plugin=<name> [flags] <logfile> [logfile...]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--plugin` | (required) | Plugin name to focus on |
| `--show` | `domain` | Display level: `acted`, `domain`, `all` |
| `--org` | | Filter by GitHub org |
| `--repo` | | Filter by repo |
| `--pr` | | Filter by PR number |
| `--since` | | Events after this time (RFC 3339 or YYYY-MM-DD) |
| `--until` | | Events before this time |
| `--json` | false | JSON output per matched event |

Accepts `-` for stdin (pipe from `oc logs`). Accepts `.gz` files.

### Display Levels

Each level includes the outcomes above it:

| Level | Outcomes shown |
|-------|----------------|
| `acted` | `acted`, `errored` |
| `domain` | `acted`, `errored`, `no_change` |
| `all` | `acted`, `errored`, `no_change`, `filtered`, `skipped` |

Errors are always shown regardless of display level.

### Output Format

One line per event with an emoji indicator for the outcome:

| Indicator | Outcome |
|-----------|---------|
| ✅ | `acted` — plugin mutated state |
| 💤 | `no_change` — domain logic ran, no mutation needed |
| ⏭️ | `filtered` — right event type, preconditions not met |
| ⬜ | `skipped` — wrong event type entirely |
| ❌ | `errored` — handler failed |

A trailing summary line shows total counts per outcome.

### Data Source

The tool reads only `"Plugin completed"` log lines from the dispatcher. These lines are self-contained: they carry all event context fields (`org`, `repo`, `pr`, `action`, `url`, `reviewer`/`author`, `duration`, `reason`, `outcome`). No cross-line correlation by `event-GUID` is needed.

Note: `skipped` events are logged at Trace level, so they typically do not appear in production logs.

### Package Structure

- `pkg/explorer/event.go` — `Event` type and `Outcome` constants
- `pkg/explorer/parse.go` — JSON line parsing
- `pkg/explorer/filter.go` — show levels and event filters
- `pkg/explorer/format.go` — one-line formatter and summary
- `pkg/explorer/explorer.go` — orchestration: read files, parse, filter, sort, format
- `cmd/ship-hook-explorer/main.go` — CLI entrypoint

## Verification

1. `make build-explorer` compiles the binary
2. `make verify` passes (unit tests + vet + lint + fmt)
3. Run against real pod logs with `--show=domain` and `--show=all`
4. Test stdin: `cat *.log | ship-hook-explorer --plugin=<name> -`
