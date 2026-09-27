# 0003: Resume slugs as the public namespace, and no operator surface

Status: Accepted (2026-08-01, 2026-09-02).

## Context

Public URLs need a namespace. The about.me model, a username plus per-user
resume slugs (`about.me/{username}/{resume-slug}`), gives users a stable profile
identity. The product publishes resumes, not people. A user may hold up to three
resumes, each for a different audience, with no shared profile page. Nesting
slugs under a username would invent an identity layer the product does not need
and would leak one resume's existence to visitors of another.

Every route the public application exposes is reachable from the internet
through the same origin as resumes. A platform-admin page would add a privileged
session class, authorization code that must never fail open, and a target for
credential attacks, for no need.

## Decision

**Resume slugs.** A resume's `slug` is globally unique, and `aboutme.vn/{slug}`
addresses exactly one resume. No `users.username` column exists; users have no
public page or public identifier.

**No operator surface.** The public application has no platform-admin page, no
privileged role, no operator session class, and no route that reads or changes
another account's data. Operator actions run out of band with database
credentials through the Go commands under `apps/server/cmd/`. Infrastructure
changes go through infrastructure as code. `/admin` stays a reserved public root
that Caddy denies. A future operator need supersedes this record explicitly; it
is never added as a hidden or undocumented route.

## Rejected alternatives

- **Username plus resume slug.** Adds an identity layer and links resumes.
- **A feature-flagged admin page.** A flag is one misconfiguration away from
  exposure and still ships the code to every deployment.
- **Admin routes on an internal listener.** Keeps privileged code in the same
  binary and process as the public surface; the boundary is a config line.

## Consequences

- A global slug namespace needs squatting controls: rate limits on claim
  attempts, a reserved list of root path segments (`api`, `app`, `u`, `people`,
  `admin`, and others) that can never be slugs, and a 180-day tombstone on a
  released slug before another user may claim it.
- Unpublishing sets `live=false` but keeps the slug. A slug is released only on
  explicit rename or delete, so a stale share link or search result can never
  point at a different resume.
- A future profile hub would need its own reserved root segment (`/u`,
  `/people`), not a prefix inside the slug namespace.
- Seeding, fixtures, migrations, and cleanup remain command-line tools that
  require a database URL and are guarded by database-name checks.
- The route table test keeps `/admin` denied.

## History

Consolidates former ADRs 0004 (resume slugs, 2026-08-01) and 0028 (no operator
surface, 2026-09-02), both unchanged in substance.
