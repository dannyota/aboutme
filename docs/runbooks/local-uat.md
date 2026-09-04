# Local UAT

Status: **blocked for acceptance**. The local UAT contract requires the complete
image-based deployment at an HTTPS origin on port 443. The current Compose
Caddyfile exposes HTTP only, so it cannot produce valid authentication or
end-to-end UAT evidence. The native HTTPS checks below are the available PV
feature proofs at `https://localhost:20443`; they do not close this port-443
acceptance gate.

Do not mark a UAT criterion `PASS` against the current HTTP origin. Do not
weaken Secure cookies or the HTTPS requirement to bypass the blocker.

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

The current PV UI proof vocabulary is part of the contract. Use these names when
inspecting a run or updating a selector:

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
strings. A local UAT record must identify the exact proof file and command; a
selector list alone is not a passing result.

## PV visual captures

The T10 finish review captures are local review artifacts, not UAT evidence.
After the native stack is ready and motion has settled, capture the landing,
list, settings, and editor at the named viewports and open each image once:

```text
.impeccable/review/desktop.png       # landing, 1440 px
.impeccable/review/mobile.png        # landing, 390 px
.impeccable/review/list-desktop.png  # list, desktop
.impeccable/review/list-mobile.png   # list, 390 px
.impeccable/review/settings-desktop.png
.impeccable/review/editor-desktop.png
.impeccable/review/editor-mobile.png # editor, 390 px
.impeccable/review/desktop-dark.png  # landing, dark theme
.impeccable/review/editor-dark.png   # editor, dark theme
```

The finish reviewer records `ship`, `recapture`, `fix`, or `rebuild`; keep any
open finding and its owner decision in the exit report. Do not commit `.dev/`
evidence or substitute these captures for the port-443 UAT scenarios.

The publish check seeds the same development account, creates a uniquely named
complete resume through the editor, and proves save-before-publish ordering. It
publishes with discovery off, enables discovery, unpublishes, and observes the
uniform public `404` within five seconds. Its `finally` cleanup deletes only the
resume created for that run with a fresh CSRF token, current revision, and new
idempotency key. The check signs out and leaves the shared account and sample
resume intact.

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

This check supports authenticated feature development. It does not replace the
complete image-based port-443 deployment, isolated resources, frozen scenario
set, or exit evidence required below.

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

## Acceptance entry conditions

Local UAT can start only when all of these are true:

- the UAT deployment serves `https://localhost` on port 443 through the real
  route table;
- the future `make uat-up`, `make uat-reset`, and `make uat-down` targets exist
  and manage only the isolated `aboutme-uat` project and its disposable data;
- rootless Podman can bind 443, and the isolated browser profile trusts the
  recorded UAT Caddy root without certificate-error bypass flags;
- the complete product slice and frozen UAT scenarios are present;
- the exact candidate contains `apps/server/migrations/.uat-baseline`, which
  freezes every existing migration after that candidate lands;
- `make ci` passes at the exact candidate commit;
- the integration owner has every live-database worker's idle acknowledgment,
  has stopped the native stack and `aboutme-test-db`, and has verified that no
  other aboutme PostgreSQL container is running;
- image IDs, migration head, configuration names without values, and tool
  versions can be recorded;
- the scripted headless Playwright harness can reach the HTTPS origin;
- no AWS, Cloudflare, certificate, DNS, registry, or staging mutation is
  required.

The native HTTPS targets above exist, but these separate `uat-*` targets do not
exist yet. Update this runbook and the deployment together when the port-443
overlay lands.

## Required execution shape

At the accepted candidate commit, the main session will:

1. After `make ci`, wait for live-worker acknowledgments, stop the native stack
   and shared test database, verify the one-database handoff, then build and
   start the complete Compose deployment on HTTPS port 443. The future
   `make uat-up` target must fail closed if another aboutme PostgreSQL container
   is running.
2. Record the commit, image IDs, migration head, commands, timestamps, tool
   versions, configuration names, and every state change.
3. Run each frozen scenario through the scripted headless Playwright suites and
   attach accessibility, screenshot, console, network, request-ID, server-log,
   and database evidence as required by that criterion.
4. Record expected and observed results with `PASS`, `FAIL`, or `BLOCKED`.
   Missing evidence and `BLOCKED` both fail the gate.
5. Send the pinned evidence to a fresh independent reviewer for a live,
   read-only probe while the deployment still runs.
6. After that live review passes, tear down the isolated deployment with the
   future `make uat-down` target.
7. Have the reviewer verify cleanup and issue the final cleanup verdict.
8. Verify the final manifest and reviewer digest at the candidate commit.
9. Restore `aboutme-test-db` only after UAT teardown and the cleanup verdict,
   and only when later work needs it.

A later product-code change makes evidence stale for every changed path. Rerun
those scenarios at the commit being shipped.

Only after local UAT and its evidence review pass may the owner be asked to
authorize external resource creation. Production launch needs separate later
approval.
