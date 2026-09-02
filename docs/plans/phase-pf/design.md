# Phase PF design — v1 entry experience

Status: **Approved for planning** (2026-09-02) by the human owner in
conversation. This file is the phase spec. Its durable rules land in the design
pages and ADRs during Task 00; the file is deleted when PF exits.

## Goal

A visitor who opens the site sees what aboutme is and can sign in or create an
account. Version 1 offers password sign-in only. A developer who starts the
native stack gets one ready account with a sample resume. The public application
has no operator surface, and that rule is recorded so it cannot erode quietly.

## What exists today

- `/` renders the P0 placeholder hero and shows the app navigation to logged-out
  visitors.
- `/login` renders the password form and three provider links whose start routes
  are registered regardless of configuration.
- `/app/settings/sessions` shows provider linking, connected agents, and
  password settings unconditionally. With agent access disabled it requests the
  connected-agents list, receives a 404, and never leaves its loading state.
- No seed exists for daily development. `/admin` is a reserved public root that
  Caddy denies. No privileged role or operator page exists.

## Decisions

### D1 — Provider login is a server flag, off for v1

`PROVIDER_LOGIN_ENABLED` is a boolean environment variable read by Go, default
`false`. When false, Go does not register the provider start and callback routes
or the authenticated provider link and provider reauthentication starts, so
every such path returns the uniform `404` that unregistered routes already
produce. The provider code, tests, mock provider, and OpenAPI operations remain;
the operations gain a description stating that they are registered only when the
flag is true, following the agent-access precedent for the consent operations.

Consequences: a provider-only account cannot sign in while the flag is off.
Production has no accounts today, so no user is affected. The native HTTPS
harness sets the flag `true` because its authentication, password, and MCP
proofs sign in through the local Google mock. The native HTTP stack and Compose
leave it unset.

### D2 — One public capabilities read

`GET /api/v1/capabilities` is unauthenticated (`security: []`), rate-limited by
the global client-IP policy, and returns:

```json
{ "data": { "providerLogin": false, "agentAccess": false } }
```

Both fields are required booleans. The response uses the authenticated
transport's exact `no-store, no-transform` cache policy so a configuration
change is visible on the next request. Nuxt fetches it client-side only, after
hydration, keeping the rule that Nuxt never fetches Go during server-side
rendering. Pages render the password form and the landing content during SSR and
add provider links or the connected-agents block only after the capabilities
read returns `true`. A failed read is treated as all-false.

### D3 — No operator surface in the public application

ADR 0028 records: the public application serves end users only. There is no
platform-admin page, no privileged role, no operator session class, and no route
that changes another account's data. Operator actions run out of band with
database credentials through the Go commands under `apps/server/cmd/`;
infrastructure changes go through the infrastructure-as-code phase. `/admin`
stays a reserved, denied public root. A future operator need supersedes this ADR
explicitly; it is never added as a hidden route.

### D4 — App shell by authentication state

The shell header shows:

| State                 | Left  | Middle            | Right                          |
| --------------------- | ----- | ----------------- | ------------------------------ |
| Logged out            | Brand | nothing           | Sign in, Create account, theme |
| Signed in, app routes | Brand | Resumes, Settings | Account control, theme         |

The editor route keeps its own top bar. Authentication state on `/` and `/login`
is the client-side `/me` result; until it resolves, the shell renders the
logged-out variant.

### D5 — Landing page content

`/` is a static Nuxt SSR page with this copy, English only, no data fetch:

- Heading: **Build your resume. Publish it at its own link.**
- Lead: aboutme is an open-source resume builder. Write once, preview the exact
  page layout, and publish each resume at a clean URL you control.
- Three points:
  - **Yours to keep.** Up to three resumes per account, private until you
    publish.
  - **One link per resume.** Publish, unpublish, and control search indexing for
    each resume on its own.
  - **Bring your own agent.** Connect an MCP-capable assistant with scopes you
    grant and can revoke.
- Buttons: **Sign in** to `/login`, **Create account** to `/register`.
- Footer line: Open source under AGPL-3.0, linking to the repository.

The `PlaceholderHero` component is deleted. Copy that names a feature that is
not shipped (PDF download, realtime refresh) is not allowed on this page.

### D6 — Login and settings gating

- `/login` renders the divider and provider links only when
  `capabilities.providerLogin` is true. The `?error=` vocabulary and the bounded
  `next` handling are unchanged.
- `/app/settings/sessions` renders the provider block only when `providerLogin`
  is true and the Connected agents block only when `agentAccess` is true. The
  password block and the session list are unconditional. This removes the
  connected-agents 404.
- `/register`, `/verify-email`, `/forgot-password`, and `/reset-password` are
  unchanged.

### D7 — Development seed

A `dev-seed` command in the fixture pattern (`seed` and `cleanup` subcommands,
`--database-url`, five-minute context) that refuses any database whose name is
not `aboutme_dev`. `seed` is idempotent:

| Row    | Fixed identity                         | Content                                                                             |
| ------ | -------------------------------------- | ----------------------------------------------------------------------------------- |
| User   | `5d000000-0000-4000-8000-000000000001` | `dev@aboutme.invalid`, name `Dev User`, verified, password `aboutme-dev-password-1` |
| Resume | `5d000000-0000-4000-8000-000000000002` | Title `Sample resume`, the current-version `full` fixture document, private         |

Rules: create each row only when its fixed ID is absent; never overwrite an
existing document or password; fail loudly if the email exists under a different
ID. The password is hashed with the production Argon2id policy. The resume
document is an embedded copy of `packages/schema/fixtures/full.json` with a
static test that fails on drift. `cleanup` deletes both rows by ID.

Wiring: `scripts/dev-native.sh up` runs `seed` after migrations and prints the
credentials; `make dev-seed` runs it alone. Compose and cloud never run it. The
HTTPS entry check runs `seed` before its browser and does not run `cleanup`.

### D8 — Testing and evidence

- Go: config parsing for the flag; route absence for every provider path when
  off and presence when on; capabilities contract and cache headers; seed
  idempotency, database-name guard, and drift test.
- Web (vitest): shell variants by auth state; landing copy and links; login with
  capabilities false, true, and failed; settings gating for both flags.
- Browser: `deploy/dev-https-browser/entry.spec.ts` run by
  `make dev-https-entry-check` with the existing evidence rules. Steps: landing
  renders the heading and both buttons without app navigation; sign in as the
  seed user lands on the resume list; the login page shows no provider link when
  the harness runs the check with the flag off; settings hides the provider and
  connected-agents blocks when their flags are off; console and page errors are
  zero. The check starts the HTTPS server with both flags off for this proof
  only, so the other proofs keep their flag-on environment.

## Authorities and amendments

| Artifact                    | Change                                                                             |
| --------------------------- | ---------------------------------------------------------------------------------- |
| ADR 0027                    | Provider login behind a server flag, off for v1                                    |
| ADR 0028                    | No operator surface in the public application                                      |
| `docs/design/product.md`    | Landing page purpose and copy rules; v1 password-only sign-in; no operator surface |
| `docs/design/web.md`        | Landing surface; shell by state; capabilities-gated login and settings             |
| `docs/design/api.md`        | Capabilities operation; provider operations conditional on the flag                |
| `docs/design/security.md`   | Flag semantics; no privileged role or operator route                               |
| `docs/design/deployment.md` | `PROVIDER_LOGIN_ENABLED` in the configuration table; dev seed is native-only       |
| `docs/api/openapi.yaml`     | `GET /capabilities`; descriptions on provider operations                           |
| Traceability                | New rows below                                                                     |
| `.env.example`              | `PROVIDER_LOGIN_ENABLED=` with an empty value                                      |

## Traceability rows (proposed; Task 00 finalizes)

| ID          | Statement                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------ |
| AC-AUTH-017 | With the flag off, every provider route is absent and no page offers provider sign-in or linking |
| AC-AUTH-018 | The capabilities read is unauthenticated, exact, and `no-store`                                  |
| AC-SEC-006  | No operator surface: no privileged role, no admin route, `/admin` denied, ADR 0028 accepted      |
| AC-OPS-021  | The dev seed is idempotent, refuses non-development databases, and never runs outside native dev |
| AC-OPS-022  | The entry flow is proven headless: landing, sign-in, list, and gated settings                    |

## Out of scope

- A rendered sample resume on the landing page.
- Any marketing section beyond D5.
- Turning provider login on for any environment other than the HTTPS harness.
- The UI quality suite beyond the single entry spec; its design is a separate
  brainstorm after PF.

## Open questions

None. The human owner confirmed public self-registration, the text-only landing,
and the capabilities endpoint on 2026-09-02.
