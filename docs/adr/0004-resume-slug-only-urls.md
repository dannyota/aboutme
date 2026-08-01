# 0004 — Resume slugs, not usernames, as the public namespace

Status: Accepted (2026-08-01)

## Context

Public URLs need a namespace. The about.me model — a username plus one or more
per-user resume slugs (`about.me/{username}/{resume-slug}`) — was the obvious
alternative: it gives users a stable profile identity and a place to list
multiple resumes.

The product publishes resumes, not people. A user may hold up to three resumes
(§3), each targeting a different audience or role, with no shared "profile" page
to bind them together. Nesting slugs under a username would invent an identity
layer the product doesn't otherwise need, and would leak one resume's existence
to visitors of another. Users are invisible on the platform; only resumes are.

## Decision

A resume's `slug` is **globally unique** and `aboutme.vn/{slug}` addresses
exactly one resume. No `users.username` column exists; the public namespace is
resume slugs, not accounts.

## Consequences

- A global (not per-user) slug namespace needs squatting controls: rate limits
  on claim attempts, a reserved list of root path segments (`api`, `app`, `u`,
  `people`, `admin`, …) that can never be claimed as slugs, and a 180-day
  tombstone on any slug a user releases before another user may claim it.
- Unpublishing a resume sets `live=false` but **keeps the slug** — it is
  released only on explicit rename/delete — so a stale share link or search
  result can never be hijacked by a different resume claiming the same slug.
- A future profile hub (listing a user's public resumes) would need its own
  reserved root segment (`/u`, `/people`) outside the resume slug namespace, not
  a prefix inside it.
