# Two Human Approvals

## Status
Implemented

## Motivation

PRs opened by automation benefit from a reminder to involve two human reviewers.
The plugin reports an advisory check as a process guardrail. Repository rules
determine whether the check blocks merging; it should remain optional for this
workflow. Maintainers remain responsible for following the intended process.

## Design

### Scope

Opening or reopening a PR from the designated automation account evaluates its
participation. New revisions trigger reevaluation against the full PR history. The initial version supports one fixed
account and one known requester-attribution format; these are not configurable.

### Qualifying Participation

A newly created PR comment or submitted review can express approval. An approving
review qualifies directly. A comment or review body also qualifies when it
contains an `/ok-to-test`, `/lgtm`, `/approve`, or `/verify` command line
without a `cancel` argument.

Commands are case-insensitive. A non-approving review may carry a qualifying
command; a pending review does not qualify.

### Two Distinct Approvers

When an eligible account expresses approval on an in-scope PR, look for
qualifying participation from a different eligible GitHub account.
Earlier comments and reviews count. Attribution of the human requester in the
PR description also counts as participation, subject to the same eligibility
check. Only the first matching attribution is considered.

The plugin compares GitHub accounts case-insensitively. Separate accounts count
separately even if the directory associates them with the same employee. Once
two eligible accounts have participated, report a passing check.

Participation accumulates across PR history. Later cancellations, changes
requested, review dismissals, or new commits do not revoke earlier approval.
Recoverable dismissal history counts the original reviewer, not the person who
dismissed the review. Deleted comments or removed command text cannot be
recovered. A passing check remains passing on that revision. A new revision is
evaluated from the history still available, without requiring approvals to
refer to that revision.

### Eligibility

The injected [permissions checker](011-permissions.md) determines who may approve
PRs. The application composes this policy from two independent conditions:
being known to [Cyborg](010-cyborg-integration.md) and being a human account.
Automation accounts are excluded. The plugin owns finding distinct approvers;
it does not own directory access or account classification.

Without directory membership information, the plugin reports a waiting check
but cannot recognize eligible approvals.

### Check Lifecycle

Each PR revision receives its own check. While fewer than two eligible accounts
have participated, it remains in progress with no conclusion. Once the condition
is satisfied, it completes successfully. Repeated events update or reuse the
existing check rather than creating additional checks for the same PR revision.
Concurrent evaluations for the same PR are coordinated within a server instance
so they reuse the same check and cannot leave a competing waiting result after
approval. There are no custom action buttons or override operations in this
version.

### Trust and API Usage

Requester attribution and webhook PR snapshots are trusted. This is a workflow
aid rather than proof of review quality. PR and review events identify the
revision directly; comment events require a current-head lookup.

Ineligible approval events require no history requests. A revision whose check
already passes requires no further history evaluation. Otherwise, search only
as needed, stopping once two eligible accounts are known. Requester attribution
can supply a participant without a history request.

## Verification

Verify waiting and passing checks, approval commands and reviews, eligibility,
distinct accounts, requester attribution, and historical approvals across new
revisions. Also verify check reuse under concurrent events, remote failures, and
avoiding unnecessary history requests. Run `make verify integration-test build`,
with synthetic directory data for integration checks.
