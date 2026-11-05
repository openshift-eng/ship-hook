# Permissions

## Status
Implemented

## Motivation

Plugins need permission decisions without owning directory access or mixing
eligibility policy into their approval and labeling behavior.

## Design

### Permission Decisions

A permission request identifies a GitHub account and an operation, expressed as
an action on a resource. The result grants or denies that request. The consuming
plugin owns what happens after the decision and how it distinguishes accounts.

### Independent Policies

Each operation has its own externally supplied eligibility check. Policies may
reuse a membership check or combine several conditions, without affecting the
policy for another operation. An unconfigured operation or absent check denies
access; it must not fall back to another operation's policy.

The application composes policy from facts such as directory membership.
The checker delegates eligibility to that policy and passes account identifiers
unchanged. Account classification and normalization belong to the providers of
those facts. Policies for specific features are described in their feature specs.

## Verification

Verify that operations can independently allow or deny the same account, that
missing policies deny access, and that identifiers reach the selected policy
unchanged. Run these checks with `make verify integration-test`.
