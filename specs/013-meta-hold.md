# Meta Hold

## Status
Implemented

## Motivation

A single aggregate label lets repository merge rules recognize any reason a PR
is held without enumerating every specific hold label.

## Design

### Aggregate Hold

The plugin maintains `do-not-merge/meta` exactly while a PR has at least one
other label whose name starts with `do-not-merge/`. Label matching is
case-insensitive. The meta label does not count as a hold itself. Labels outside
that prefix do not affect the aggregate hold.

Adding the first hold adds the aggregate label. Removing the last hold removes
it. Removing the aggregate label while another hold remains restores it; adding
the aggregate label without another hold removes it. Other labels are preserved.
The label names and prefix are fixed in this version.

### Reconciliation

Opening, reopening, labeling, or unlabeling a PR triggers reconciliation against
its current GitHub labels. Delayed webhook snapshots do not determine the
desired aggregate state. Events caused by the plugin's own mutations and repeated
deliveries settle without further changes once the labels agree.

Concurrent reconciliations for the same PR are coordinated within a server
instance across the entire read, evaluation, and mutation. This coordination
does not lock other server instances or GitHub actors; subsequent label events
reconcile their changes. Failed label reads cause no mutation, and API failures
are reported through the shared handler-result contract.

The plugin owns only the aggregate label. The reasons for individual holds and
their creation and removal belong to other actors. Repository rules determine
whether the aggregate label prevents merging. The dispatcher controls plugin
enablement through the shared configuration model; no directory membership or
actor permission decision is needed to mirror existing holds.

## Verification

Verify the aggregate invariant with zero, one, and multiple holds; manual meta
label changes; case variants; unrelated labels; delayed and repeated events;
concurrent reconciliation; and remote failures. Exercise webhook payloads with
the fake GitHub client and dev server. Run `make verify integration-test build`.
