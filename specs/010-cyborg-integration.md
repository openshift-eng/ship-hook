# Cyborg Integration

## Status
Implemented

## Motivation

SHIP Hook needs a shared source of Red Hat employee membership information.
Consumers use that information to define their own policies without each owning
directory credentials, loading, and refresh.

## Design

### Directory Membership

The directory associates GitHub accounts with employee records. A lookup either
finds a known employee or reports no match. Directory membership supplies facts;
consumers decide what those facts permit and how to distinguish GitHub accounts.

### Credentials and Availability

Directory access is optional. Without credentials, it is disabled rather than
implicitly using another identity. Initialization is independent of individual
plugin feature gates. When access is enabled, failures to load credentials or
initial directory data prevent startup.

The shared secret agent loads and validates credentials before use and
registers them for log censoring. Credential replacement requires a restart:
reloading the secret does not replace credentials in an existing remote client.

### Ownership and Lifecycle

The integration owns directory construction, source access, and refresh.
The application supplies configuration, starts refresh, and controls shutdown.
Query consumers do not need to manage refresh or remote clients.

Initial loading and ongoing refresh have separate cancellation lifetimes.
Cancellation is supplied by the caller for each operation rather than retained
as service state. Shutdown cancels refresh before closing the directory source.
Taking ownership of a source includes releasing it if initial loading fails.

### GitHub Account Matching

GitHub accounts are matched case-insensitively, consistently across initial
loading and later refreshes. Case variants identifying the same employee are
compatible; variants identifying different employees make the directory data
ambiguous and must be rejected. Conflict errors must not disclose raw logins.

Account matching resolves lookup spelling. It does not decide whether separate
GitHub accounts should count as one person in a consuming feature.

## Verification

Verify disabled access, invalid credentials, initial-load failure, cancellation,
resource cleanup, and employee lookup using synthetic directory data. A live
source check is optional and requires credentials. Run the automated checks with
`make verify integration-test build`.
