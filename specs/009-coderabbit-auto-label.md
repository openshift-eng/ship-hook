# CodeRabbit Auto-Label on PR Creation

## Status
Draft

## Motivation

Contributors without CodeRabbit access receive no automated review to signal
that their PR is ready for human attention. Their PRs should receive that signal
immediately rather than wait for a review that will not arrive.

## Design

### Access Policy

The plugin relies on an injected [permission decision](011-permissions.md) for
CodeRabbit access. The current policy treats accounts known to
[Cyborg](010-cyborg-integration.md) as having access. When directory access is
disabled, authors are treated as lacking CodeRabbit access.

### Labeling

On PR creation, add the ready-for-human-review label if the author lacks
CodeRabbit access. Authors with access continue through the existing automated
review flow. Repeated delivery must not add a label that is already present.

A feature gate controls immediate labeling. Disabling it leaves the existing
review-based labeling behavior unchanged.

## Verification

Check both access decisions, disabled directory access, feature gating, repeated
events, and the existing review-based labeling flow. These checks run through
`make verify` and `make integration-test`.
