# SHIP Hook

SHIP Hook is an external [Prow](https://docs.prow.k8s.io/) plugin that receives GitHub webhook events from Prow Hook and dispatches them to internal sub-plugins. It runs as a single binary hosting multiple independent behaviors, each implemented as a sub-plugin.

## Sub-Plugins

| Plugin | Description |
|--------|-------------|
| `ready-for-humans` | Adds a label to PRs when [CodeRabbit](https://coderabbit.ai/) submits an approving review, signaling the PR is ready for human review |
| `example` | Reference implementation that logs received events |

> _This README was last verified on 2026-05-29._

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

## Development

An interactive dev server runs ship-hook with an in-memory fake GitHub client:

```
make dev-server                    # start on port 8888
make dev-webhook EVENT=pull_request_review PAYLOAD=test/integration/testdata/review_submitted_approved.json
make dev-state                     # inspect mutations
make dev-reset                     # clear state
```

See `AGENTS.md` for detailed architecture and contribution guidelines.
