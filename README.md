# SHIP Hook

SHIP Hook is an external [Prow](https://docs.prow.k8s.io/) plugin that receives GitHub webhook events from Prow Hook and dispatches them to internal sub-plugins. It runs as a single binary hosting multiple independent behaviors, each implemented as a sub-plugin.

## Sub-Plugins

| Plugin | Description |
|--------|-------------|
| `ready-for-humans` | Adds a label to PRs when [CodeRabbit](https://coderabbit.ai/) submits an approving review, signaling the PR is ready for human review |
| [two-human-approvals](specs/012-two-human-approvals.md) | Reports a check while PRs opened by `redhat-chai-bot` await participation from two eligible humans |
| `example` | Reference implementation that logs received events |

## Configuration

SHIP Hook uses a YAML config file with layered overrides at the top-level, organization, and repository levels. Lower levels override upper levels.

```yaml
plugins:
  - name: ready-for-humans
    config:
      label: ready-for-humans
      bot_login: "coderabbitai[bot]"

orgs:
  my-org:
    plugins:
      - name: ready-for-humans
        config:
          label: custom-label
    repos:
      my-repo:
        plugins:
          - name: ready-for-humans
            disabled: true
```

Supplemental config files can be placed in a separate directory and merged into the main config, allowing org/repo owners to manage their own configuration shards.

See `specs/004-configuration.md` for full details.

## Building

```
make build     # compile binary to _output/ship-hook
make test      # run unit tests
make verify    # vet + test
make image     # build container image
```

## Running

```
_output/ship-hook \
  --config-path=config.yaml \
  --supplemental-config-dir=config.d/ \
  --hmac-secret-file=/path/to/hmac \
  --github-token-path=/path/to/token \
  --dry-run=false
```

Plugins that publish GitHub checks require GitHub App authentication with
Checks write permission. To keep `two-human-approvals` advisory, leave its check
out of required checks in repository rules.

## Development

An interactive dev server runs ship-hook with an in-memory fake GitHub client:

```
make dev-server                    # start on port 8888
make dev-webhook EVENT=pull_request_review PAYLOAD=test/integration/testdata/review_submitted_approved.json
make dev-state                     # inspect mutations
make dev-reset                     # clear state
```

The dev server permits `alice` and `bob` to approve PRs, without Cyborg
credentials. CodeRabbit access remains denied. To exercise the approval check:

```shell
make dev-webhook EVENT=pull_request PAYLOAD=test/integration/testdata/two_human_approvals_opened.json
make dev-state
make dev-webhook EVENT=issue_comment PAYLOAD=test/integration/testdata/two_human_approvals_comment.json
make dev-state
```

The first payload opens a bot-authored PR requested by `alice`; its check is
`in_progress`. The second supplies an approval from `bob`; the same check becomes
`completed` with conclusion `success`. Inspect `check_runs` in the state response.
The dev server retains PR snapshots and delivered comments/reviews for later
head lookups and history evaluation. Send a PR or review webhook before comment
webhooks so the fake client knows the PR's head commit.

## Log Explorer

A CLI tool for investigating plugin behavior across multi-replica log files.

```shell
make build-explorer    # compile to _output/ship-hook-explorer
```

### Usage

```shell
ship-hook-explorer --plugin=<name> [flags] <logfile> [logfile...]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--plugin` | (required) | Plugin name to focus on |
| `--show` | `domain` | Display level: `acted`, `domain`, `all` |
| `--verbose`, `-v` | false | Show intermediate plugin log lines under each event |
| `--org` | | Filter by GitHub org |
| `--repo` | | Filter by repo |
| `--pr` | | Filter by PR number |
| `--since` | | Events after this time (RFC 3339 or YYYY-MM-DD) |
| `--until` | | Events before this time |
| `--json` | false | JSON output per matched event |

Accepts `-` for stdin (pipe from `oc logs`) and `.gz` files.

### Display levels

| Level | What's shown |
|-------|-------------|
| `acted` | ✅ acted, ❌ errored |
| `domain` | ✅ acted, ❌ errored, 💤 no_change |
| `all` | ✅ acted, ❌ errored, 💤 no_change, 🔽 filtered, ⬜ skipped |

### Examples

```bash
# Show domain-relevant events for ready-for-humans across two replica logs
ship-hook-explorer --plugin=ready-for-humans pod1.log pod2.log

# Show only events where the plugin took action
ship-hook-explorer --plugin=ready-for-humans --show=acted pod1.log pod2.log

# Show everything including filtered/skipped, with intermediate logs
ship-hook-explorer --plugin=ready-for-humans --show=all -v pod1.log pod2.log

# Pipe from oc logs
oc logs -n ci deploy/ship-hook -c ship-hook | ship-hook-explorer --plugin=ready-for-humans -

# Filter to a specific PR
ship-hook-explorer --plugin=ready-for-humans --pr=3557 pod1.log pod2.log
```

See `specs/009-log-explorer.md` for the full design.

See `AGENTS.md` for detailed architecture and contribution guidelines.
