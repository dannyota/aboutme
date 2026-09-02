# 0028 — No operator surface in the public application

Status: Accepted (2026-09-02)

## Context

The public application serves end users. Every route it exposes is reachable
from the internet through the same origin as resumes. A platform-admin page
would add a privileged session class, authorization code that must never fail
open, and a target that attracts credential attacks, for no v1 need.

## Decision

The public application has no platform-admin page, no privileged role, no
operator session class, and no route that reads or changes another account's
data. Operator actions run out of band with database credentials through the Go
commands under `apps/server/cmd/`. Infrastructure changes go through the
infrastructure-as-code phase. `/admin` stays a reserved public root that Caddy
denies. Any future operator need supersedes this ADR explicitly; it is never
added as a hidden or undocumented route.

## Rejected alternatives

- **A feature-flagged admin page.** A flag is one misconfiguration away from
  exposure and still ships the code to every deployment.
- **Admin routes on an internal listener.** Keeps privileged code in the same
  binary and process as the public surface; the boundary is a config line.

## Consequences

- Seeding, fixtures, migrations, and cleanup remain command-line tools that
  require a database URL and are guarded by database-name checks.
- The route table test keeps `/admin` denied; the design records the rule so a
  later phase cannot add an operator page without superseding this record.
