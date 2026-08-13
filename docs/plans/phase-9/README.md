# Phase 9 — HTTPS overlay and local UAT

Status: **Revision 5** (2026-08-12). Two parts, run at different times.

1. **The HTTPS overlay** is a development tool. Build it as soon as a feature
   needs an authenticated browser check — that is, when P4 starts.
   Authentication uses Secure `__Host-` cookies, so authenticated flows cannot
   be exercised against the plain-HTTP native stack.
2. **Local UAT** is the release gate. It runs the complete product through
   user-like browser actions before any AWS resource exists, and it is the last
   thing that happens before the human owner authorizes cloud work.

The current `make dev` stack stays HTTP-only and remains an image and network
smoke check.

## Available native HTTPS authentication harness

The local development harness now serves `https://localhost:20443` without
changing host trust. It runs native Go, Nuxt, Caddy, and a deterministic local
Google OpenID Connect mock against the shared `aboutme_dev` database. A pinned
disposable Playwright image imports only that invocation's exported Caddy root
into an isolated browser profile.

Use `make dev-https`, `make dev-https-status`, `make dev-https-browser-image`,
`make dev-https-auth-check`, and `make dev-https-down`. The check retains only
bounded, secret-free local evidence under ignored `.dev/native-https/evidence/`.

This harness unlocks authenticated development checks. It is not the isolated
port-443 Compose overlay below, does not satisfy U1–U5, and does not advance P9
acceptance state.

## Part 1 — HTTPS overlay

An isolated Podman Compose overlay serving `https://localhost` on port 443, with
a locally trusted certificate and a mock OAuth provider, driven through
`make uat-up` and `make uat-down`.

| Task | Deliverable                                                               |
| ---- | ------------------------------------------------------------------------- |
| U1   | Overlay Compose file, local CA, and certificate issuance                  |
| U2   | Mock OAuth provider with deterministic accounts                           |
| U3   | `make uat-up` / `make uat-down` with fail-closed preflight                |
| U4   | Trusted browser profile for the project Playwright server                 |
| U5   | Isolation tests: no shared-container reuse, no port collision, clean down |

`make uat-up` fails closed if `net.ipv4.ip_unprivileged_port_start` is above
443, port 443 is occupied, or another aboutme PostgreSQL, S3-compatible, or
stack container is running. It never stops or changes another project. Only a
host administrator may change that sysctl; agents and scripts never use `sudo`.

Before bringing the overlay up, the integration owner announces the handoff,
waits for every worker using the live database, native stack, or S3 service to
go idle, then runs `make dev-native-down`, `make test-db-down`, and
`make test-s3-down`. One PostgreSQL container and one S3-compatible service
exist on this machine at a time.

The [harness](harness.md) has the detailed task content. Where it describes
frozen catalogs, blind authors, or sealed image identities, ADR 0024 applies
instead.

## Part 2 — Local UAT

The main-session executor runs the UAT; the human owner does not execute it.

Entry conditions:

- One clean commit containing completed P0–P8 web v1 and PI local-only work,
  with every phase gate and `make ci` passed at that commit.
- The commit contains `apps/server/migrations/.uat-baseline`; the run proves the
  migration history that becomes immutable when it lands.
- The HTTPS overlay tasks above are done and the resource handoff is complete.
- No AWS or Cloudflare credentials are present. No external mutation is
  authorized or required.

The run walks the complete user workflows in [execution](execution.md) and
records browser, network, console, server, and database evidence as described in
[evidence](evidence.md). A fresh reviewer verifies the run.

Renderer browser UAT uses the same named six-preset visual subset as Phase 3:
`classic-serif`, `engineer-compact`, `modern-sidebar`, `executive-band`,
`consulting-formal`, and `academic-dense`. Changing that set requires the Phase
3 screenshot table and this UAT set to change together.

**Evidence stays local.** It is never committed and never published: this
repository and its CI logs are public, and UAT evidence contains session
cookies, tokens, and account data. Keep it under an ignored path listed in
`.git/info/exclude`, and redact before sharing any excerpt. This rule replaces
the sealed-export tooling an earlier revision specified; the tooling existed to
make evidence safe to publish, and not publishing it is the cheaper guarantee.

## Exit

P9 passes when every exit item is met at one unchanged commit, the reviewer
finds no blocking defect, and teardown leaves no stray container, volume, or
port binding. A failing item is fixed and rerun.

Only then may the integration owner ask the human owner to authorize AWS
resource creation. Production still requires a separate approval after P9A.
