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

### Initial Policy

The initial checker supports CodeRabbit access through an externally supplied
eligibility check. It denies requests when no check is supplied and denies
unsupported operations. Policy inputs are supplied by the application.

## Verification

Verify eligible and ineligible accounts, absent eligibility checks, and
unsupported operations with `make verify integration-test`.
