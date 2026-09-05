# Local HTTPS checks

Status: **available for feature verification**. Native authenticated checks run
at `https://localhost:20443`. Complete product UAT runs in AWS during
[Phase 10](../plans/phase-10/README.md), after Phase 9 cost research.
[ADR 0031](../adr/0031-aws-cost-research-and-hosted-uat.md) replaces the old
local port-443 acceptance prerequisite; no host sysctl change is needed for this
plan.

Keep Secure cookies, normal TLS verification, and the local network allowlist.
The HTTP Compose smoke does not prove authenticated browser behavior.

## Native HTTPS feature checks

The available native harness proves real Secure-cookie authentication and
user-authorized MCP agent access at `https://localhost:20443`. It uses the one
shared `aboutme-test-db` container and only its `aboutme_dev` database. It
starts or reuses that container and leaves it running after teardown.

Run the exact sequence from the repository root while no other native stack or
heavy browser/build worker is active:

```sh
make dev-native-down
make dev-https
make dev-https-status
make dev-https-browser-image
make dev-https-auth-check
make dev-https-transport-check
make dev-https-editor-check
make dev-https-mcp-check
make dev-https-entry-check
make dev-https-public-check
make dev-https-publish-check
make dev-https-password-check
make dev-https-down
make dev-native
```

The entry check seeds the development account, proves the stamped landing page,
the `Create account` then `Sign in` hero order, the password toggle labels,
sign-in, both-theme axe scans for `/`, `/login`, `/app/resumes`, and
`/app/settings/sessions`, the signed-in shell, and sign-out; it deletes nothing.

The UI proof vocabulary is part of the contract. Use these names when inspecting
a run or updating a selector:

- Landing: heading `The resume is public. You are not.`, links `Create account`
  and `Sign in`, signed-in link `Open your resumes`, and
  `[data-testid="landing-sample"]` with the seal label
  `Public at aboutme.vn/ada-lovelace`.
- Authentication: the password control is an icon button named `Show password`
  or `Hide password`; there is no visible `Show` button text.
- Resume list: rows are `[data-testid="resume-row-<id>"]`; the overflow trigger
  is `More actions for <title>`, with `Rename` and `Delete` menu items; state is
  `Draft` or the canonical `aboutme.vn/<slug>` link.
- Editor: the page-count mark is `1 page` or `{n} pages` under the sheet and is
  selected with `[data-testid="page-count"]`; at 390 px use the fixed `Edit` /
  `Preview` switch and the `show-editor` / `show-preview` actions.
- Publish: the success state shows the canonical stamp and `Copy link` on
  `[data-action="copy-link"]`; the only seal-colored control is `Publish`.
- Inspector: customization labels are words such as `Font`, `Section gap`, and
  `Page size`, not schema paths or raw enum IDs.

The entry, editor, and publish proofs include these current selectors and
strings. A local feature-check record must identify the exact proof file and
command; a selector list alone is not a passing result.

The publish check seeds the same development account, creates a uniquely named
complete resume through the editor, and proves save-before-publish ordering. It
publishes with discovery off, verifies a second editor tab updates the owner and
public views without a page reload or scroll reset, enables discovery, then
unpublishes and observes automatic public `404` navigation within five seconds.
Its `finally` cleanup deletes only the resume created for that run with a fresh
CSRF token, current revision, and new idempotency key. The check signs out and
leaves the shared account and sample resume intact.

The browser image imports only the root exported by this harness. It uses no TLS
bypass and permits network traffic only to the fixed HTTPS origin. Each check
writes a new bounded verdict under ignored `.dev/native-https/evidence/` and
keeps only the newest ten runs per check. Use `make dev-https-logs` for redacted
diagnostics.

The image bakes only the pinned browser, dependencies, and `run.sh`; spec
sources are staged and mounted read-only per run by
`scripts/dev-https-check.sh`. Editing a spec therefore needs no image rebuild —
rerun the check directly. Rebuild the image (`make dev-https-browser-image`)
only when `Dockerfile`, `run.sh`, or the package manifests change; the check
fails closed with "browser image sources changed" when a rebuild is required.

The MCP check enables MCP only in the isolated HTTPS server. It seeds one
reserved provider identity, registers a public loopback client, completes S256
consent, creates and edits a resume through MCP, renders that content in the
editor, revokes the grant in Connected agents, and proves the next bearer call
returns the closed 401. Browser teardown deletes the resume through the user
session. Fixture teardown verifies that its user, identity, resume, client,
authorization code, grant, and token rows are absent. Its retained verdict is
`mcp-proof.json`, mode 0600 and at most 4,096 bytes; it contains only the ten M9
step booleans and four error counters.

The publish check retains `publish-proof.json`, mode 0600 and at most 8,192
bytes. It contains only fixed step booleans, response statuses, the bounded
revocation time, mutation-header presence flags, and error counters. It never
stores header values, cookies, passwords, request or response bodies, resume
content, or personal data.

For renderer specs, `make web-e2e-fast` iterates in the pinned browser against
the working tree without the hermetic tar (`ARGS="print.spec.ts"` selects specs,
`PLAYWRIGHT_WORKERS=4` parallelizes files, `PLAYWRIGHT_SKIP_BUILD=1` reuses the
last Nuxt build for spec-only edits). It is a development loop, not a gate; the
hermetic `make web-e2e` remains the gate.

This check supports authenticated feature development. Complete product and
operational acceptance belong to the hosted AWS Phase 10 environment; see the
[handoff below](#handoff-to-hosted-uat).

## Current Compose smoke check

The current deployment can still prove image construction, migration ordering,
container health, network routing, and teardown. This is a smoke check, not UAT.
It cannot run beside the shared `aboutme-test-db` container. Only the
integration owner may schedule this handoff after every live-database worker is
idle. `make dev` must fail its preflight while that container is running.

From the repository root:

```sh
cp .env.example .env
# Set POSTGRES_PASSWORD to a new local value.
# Set CADDY_HTTP_PORT=8080 when rootless Podman cannot bind port 80.
make test-db-down
make dev
podman compose --env-file .env -f deploy/compose.yml ps
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
make dev-down
make test-db-up
```

Use port 80 when `CADDY_HTTP_PORT` is unset. Confirm that the `migrate` service
exited successfully and that server, web, Caddy, and PostgreSQL are healthy.
Always run `make dev-down` when the smoke session ends; the Compose stack is not
the daily development environment. Restore the shared database only after
Compose teardown, and only when later work needs it. A worker never performs
this handoff or stops the shared database.

## Handoff to hosted UAT

These checks remain local and use only the shared development database. Do not
point their seed, reset, or cleanup commands at AWS. Phase 10 supplies a
separate hosted harness for `https://uat.aboutme.vn`, with synthetic fixtures
and bounded cleanup, after its local infrastructure checks pass.

The migration baseline marker must be committed before the first hosted UAT
migration. Phase 10 records candidate/image identities and proves complete user
workflows, SES delivery, and the live operational drills. The owner has already
authorized that Singapore UAT environment and its Cloudflare DNS; production
promotion is Phase 11 and requires separate approval.

Use the [email runbook](email.md) for the existing SES setup. Its sandbox and
runtime IAM/adoption requirements are part of the hosted UAT handoff, not a
reason to contact SES from native feature checks.
