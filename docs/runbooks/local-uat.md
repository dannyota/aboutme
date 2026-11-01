# Local HTTPS checks

Status: **Linux container browser checks available for feature verification**.
The native service stack uses `https://localhost:20443`; a native macOS browser
runner is accepted but pending integration and runtime acceptance. Complete
product checks run in production, per
[ADR 0026](../adr/0026-vietnam-hosted-production.md). No host sysctl change is
needed.

Keep Secure cookies, normal TLS verification, and the local network allowlist.
The HTTP Compose smoke does not prove authenticated browser behavior.

## HTTPS feature checks

The available native harness proves real Secure-cookie authentication and
user-authorized MCP agent access at `https://localhost:20443`. It uses the one
shared `aboutme-test-db` container and only its `aboutme_dev` database. It
starts or reuses that container and leaves it running after teardown.

The commands below describe the existing Linux container path. Agent execution
needs a bounded primary brief under
[resources](../../instructions/resources.md). Run from the repository root while
no other native stack or conflicting browser/build worker is active:

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
make dev-https-exports-check
make dev-https-sample-start-check
make dev-https-privacy-check
make dev-https-linkedin-check
make dev-https-linkedin-import-check
make dev-https-down
make dev-native
```

The entry check seeds the development account, proves the stamped landing page,
the `Create your resume` then `Sign in` hero order, the password toggle labels,
sign-in, both-theme axe scans for `/`, `/login`, `/app/resumes`, and
`/app/settings/sessions`, the signed-in shell, and sign-out; it deletes nothing.

The workspace uses Vietnamese by default and English when the
`aboutme-locale=en` cookie is set. Settings and account pages remain English.
Pin the interface locale before using a translated accessible name. Test hooks
stay the same in both languages. Use these names when inspecting a run or
updating a selector:

- Landing: Vietnamese by default, with heading
  `CV của bạn. Chia sẻ theo cách của bạn.` and `html lang="vi"`. With the
  `aboutme-locale=en` cookie: heading `Your resume. Your link. Your control.`,
  links `Create your resume` and `Sign in`, signed-in link `Open your resumes`,
  and `[data-testid="landing-sample"]` with the seal label
  `Public at aboutme.vn/danny`.
- Authentication: the password control is an icon button named `Show password`
  or `Hide password`; there is no visible `Show` button text.
- Resume list: rows are `[data-testid="resume-row-<id>"]`; the overflow trigger
  is `More actions for <title>`, with `Rename` and `Delete` menu items; state is
  `Draft` or the canonical `aboutme.vn/<slug>` link in English. Vietnamese uses
  `Thao tác khác cho <title>`, `Đổi tên`, and `Xóa` for those actions.
- Editor: the page-count mark is `1 page` or `{n} pages` under the sheet and is
  selected with `[data-testid="page-count"]`; at 390 px and tablet widths use
  the fixed `Edit` / `Preview` switch and the `show-editor` / `show-preview`
  actions. Vietnamese uses `{n} trang`, `Chỉnh sửa`, and `Xem trước`.
- Publish: the success state shows the canonical stamp and `Copy link` on
  `[data-action="copy-link"]`; the only seal-colored control is `Publish`.
  Vietnamese uses `Sao chép liên kết` and `Xuất bản`.
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
`scripts/dev-https-check.sh`. Editing a spec therefore needs no image rebuild;
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

The privacy check exports a portable account attachment, cancels deletion, then
proves provider reauthentication and a fresh explicit confirmation before real
account deletion. One exact DELETE response is forced to `reauth_required` to
exercise the settings branch; live database API tests prove stale-session
refusal. The check verifies old session, grant, public representation and
discovery access is revoked, and that the old slug remains tombstoned. It
removes its synthetic accounts through the API and its reserved OAuth client
through the fixture cleanup. `privacy-proof.json` is mode 0600, at most 8,192
bytes, and contains only fixed step booleans and error counters.

Until display integration, `make web-e2e-fast` iterates in the pinned Linux
browser against the working tree without the hermetic tar.
`ARGS="print.spec.ts"` selects specs, `PLAYWRIGHT_WORKERS=4` parallelizes files,
and `PLAYWRIGHT_SKIP_BUILD=1` reuses the last Nuxt build for spec-only edits. It
is a development loop, not a gate; hermetic `make web-e2e` remains the gate.

This check supports authenticated feature development. Complete product and
operational acceptance happen in production; see the
[handoff below](#handoff-to-production).

## Accepted native macOS path, pending

After session ownership, native runner, and integration changes land and pass
acceptance, `make dev-https` and `dev-https-*-check` select native execution on
Darwin. The pending `make dev-https-browser-install` prepares the locked
Chromium/WebKit builds in a private checkout-local path before startup. Do not
substitute a system browser or fall back after failure.

Follow the
[native admission and account contract](../design/local-tooling-simplification.md#native-admission-and-account):
clean owner-written/reviewed commits, disposable builds/profiles, explicit
test-only environments, and no production credentials. Contributor heads use
hosted CI. Attended Chromium profile-only CA import must pass strict TLS probes;
never change host trust or bypass certificate checks.

The
[server-rendering exclusions](../design/local-tooling-simplification.md#server-rendering)
skip the whole exports and privacy journeys and two coupled public journeys.
Native HTTPS WebKit also skips. Native results report `PARTIAL` and never
satisfy Linux aggregate completeness; portable steps inside excluded journeys
are not locally proved. Production proof commands remain on their existing
container path.

The accepted [display contract](../design/display-assertions.md) keeps `web-e2e`
and `web-e2e-fast`, with native Chromium harness PDFs on Mac. Retain existing
comparisons until Linux/macOS inventory and mutation acceptance passes. Stop old
stacks with old helpers before session migration; preserve shared DB, keys, and
media. Ambiguous ownership needs manual recovery, never name-based kills. This
runbook update authorizes no local run.

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

## Handoff to production

These checks remain local and use only the shared development database. Never
point their seed, reset, or cleanup commands at any production service.

The [Vietnam production design](../design/vietnam-production.md) and
[production runbook](production.md) own deploy and launch checks under ADR 0026.
The [email runbook](email.md) owns Bizfly production mail; native feature checks
use only loopback mail capture.
