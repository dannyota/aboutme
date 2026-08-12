# 0014 — Privileged OAuth starts use authenticated POST

Status: Accepted (2026-08-12)

## Context

Login, provider linking, and recent reauthentication all create OAuth
transactions, but they do not have the same authority. Login is public. Linking
and reauthentication act on the current account and require an existing session.
A top-level link cannot carry the synchronizer token used by the service's CSRF
boundary.

The implemented API already separates the methods. The older design text and
settings UI still described privileged starts as GET navigation.

## Decision

`GET /api/v1/auth/{provider}/start` starts login only. A query that requests
`link` or `reauth` returns `405` and creates no transaction.

Provider linking and recent reauthentication use authenticated
`POST /api/v1/auth/{provider}/start?purpose=link|reauth`. The bodiless request
passes the normal origin and CSRF checks. The response returns an authorize URL
in the normal data envelope. The browser then opens that URL as a top-level
navigation.

OAuth callbacks remain GET because the provider initiates that navigation and
the stored one-use transaction is the callback authority.

## Consequences

- UI controls for linking and reauthentication must call POST. An anchor to a
  privileged GET route is a defect.
- GET stays convenient for public login without weakening the authenticated
  mutation boundary.
- OpenAPI examples and shared error descriptions must not imply that GET can
  start a privileged purpose.
