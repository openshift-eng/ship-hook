# SHIP Hook

SHIP Hook is an external [Prow](https://docs.prow.k8s.io/) plugin that receives GitHub webhook events from Prow Hook and dispatches them to internal sub-plugins. It runs as a single binary hosting multiple independent behaviors, each implemented as a sub-plugin.

## Sub-Plugins

| Plugin | Description |
|--------|-------------|
| `ready-for-humans` | Adds a label to PRs when [CodeRabbit](https://coderabbit.ai/) submits an approving review, signaling the PR is ready for human review |
| `two-human-approvals` | Provides a `needs-human-review` reminder to help reviewers follow the two-human review process for PRs opened by `redhat-chai-bot` |
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

`two-human-approvals` provides guardrails that help people follow the review
process for bot-authored PRs. Its `needs-human-review` label is a reminder that
more human input is needed. The plugin removes that reminder when it finds
qualifying participation from two distinct Red Hat humans.

This is a workflow aid, not a security control or a guarantee of review quality.
It trusts webhook snapshots and requester attribution in PR descriptions, and
only manages a label; it does not block merges or prevent bypassing the process.
Reviewers and repository maintainers remain responsible for following the
intended review process.

Enable `two-human-approvals` by adding `- name: two-human-approvals` to the
appropriate `plugins` list. Its bot login (`redhat-chai-bot`), label
(`needs-human-review`), and description attribution pattern (a standalone
`@LOGIN requested from Slack` line) are currently hardcoded. The first matching
attribution counts as one human if Cyborg recognizes them as a Red Hat employee.

Approval eligibility is supplied through the permissions checker using action
`approve` and resource `pr`. The current permissions stub implements that policy
with its `isCyborgKnownRedHatter` built-in predicate, shared with CodeRabbit access.
The checker also compares identities, counting multiple accounts belonging to
one employee as a single participant. The plugin finds approvals
and manages the label independently of the eligibility policy.

The plugin uses webhook labels and descriptions directly, avoiding a PR lookup.
Approval events on unlabeled PRs require no GitHub calls. A qualifying description
attribution requires only label removal; otherwise it searches comments, then
reviews, then dismissal history only as needed, stopping when it finds a second
approver.

The plugin handles newly created PR comments and submitted reviews. An approving
review, or a command line containing `/ok-to-test`, `/lgtm`, `/approve`, or
`/verify`, counts as approval. Commands with a `cancel` argument are excluded,
except `/verify`, which counts with any arguments. The plugin counts events from
Cyborg-confirmed employees and looks for attribution or earlier comments/reviews
from a different employee. Multiple GitHub accounts resolving to the same Cyborg
employee count once. Bot accounts and GitHub apps are excluded from the human
count. Review bodies are checked even for non-approving reviews; pending reviews
do not count.

Earlier approvals are not revoked by later cancellations, changes requested,
review dismissals, or new commits. Dismissal history recovers the original state
of dismissed reviews. Deleted comments and removed command text cannot be
recovered from the current GitHub history. Once removed, the label is not restored.
Use `--cyborg.credentials-file` to provide the same Cyborg data used by
`ready-for-humans`; without it, the plugin adds the label but cannot remove it.
The dev server likewise has no Cyborg lookup and cannot count approvals.

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

## Development

An interactive dev server runs ship-hook with an in-memory fake GitHub client:

```
make dev-server                    # start on port 8888
make dev-webhook EVENT=pull_request_review PAYLOAD=test/integration/testdata/review_submitted_approved.json
make dev-state                     # inspect mutations
make dev-reset                     # clear state
```

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
