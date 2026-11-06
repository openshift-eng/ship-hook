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

### Refresh

Directory data is loaded from a remote source and refreshed automatically.
Consumers query the loaded directory without managing the source themselves.

## Verification

Verify known and unknown employee lookups using synthetic directory data. A live
source check is optional and requires credentials.
