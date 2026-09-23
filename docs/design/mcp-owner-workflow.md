# MCP owner workflow

## Status

Approved for implementation by the manager under the owner's delegated release
scope. The owner has not reviewed this artifact. The owner authorized one new
private Vietnamese resume in the owner's account. No product choice remains that
requires owner approval.

## Outcome and invariants

The workflow uses the production Model Context Protocol (MCP) interface as a
real client. It reads the only English resume and creates exactly one new
private Vietnamese resume. The English source receives reads only. The workflow
does not publish or delete either resume. Publication requires a separate owner
request through the web application.

The official Go MCP SDK first proves the same path locally with synthetic data.
The existing raw JSON-RPC browser proof remains a server regression test. The
new proof tests interoperability with a conforming client.

The workflow uses [ADR 0026](../adr/0026-mcp-agent-access.md). It does not
change MCP tools, the resume schema, or the public resume contract. It adds the
OAuth resource-indicator handling required by the pinned SDK.

The exact 15 tools are `list_resumes`, `get_resume`, `create_resume`,
`delete_resume`, `update_resume_metadata`, `upsert_entry`, `delete_entry`,
`update_section`, `update_structure`, `update_personal_details`,
`update_customization`, `get_photo`, `upload_photo`, `update_photo_crop`, and
`delete_photo`.

## OAuth server contract

The official SDK needs three narrow compatibility changes: protected-resource
scopes, OAuth `resource` handling, and native dynamic client registration (DCR)
metadata.

`/.well-known/oauth-protected-resource` adds the exact ordered field
`"scopes_supported":["resumes:read","resumes:write"]`. The SDK derives its
authorization request scopes from this metadata, so both values are required.
The unauthenticated `/mcp` challenge remains unchanged and names only the
protected-resource metadata URL. Metadata tests pin the complete JSON, exact
scope spellings and order, absence of extra scopes, and unchanged challenge.

The authorization endpoint accepts zero or one `resource`. A present value must
equal the canonical public origin byte for byte. Missing preserves legacy
clients. Empty, duplicate, malformed, non-canonical, and different values
receive the existing closed `invalid_request` result. The token endpoint
applies the same rule to an `authorization_code` exchange: invalid values
receive `invalid_grant` and no token, missing preserves the raw client, and
refresh-token requests retain their exact form and reject `resource`.

The server has one protected resource, so this needs no database field or token
shape change. Tests cover SDK and legacy forms, duplicates, empty values, and
mismatches at both endpoints.

SDK construction adds `application_type: "native"` for the loopback redirect.
Registration accepts zero or one `application_type` for legacy compatibility. A
present value must be exactly `native`, and every redirect must be an accepted
HTTP loopback URI. Other, empty, duplicate, mixed, or non-loopback shapes fail
closed. The response echoes `native` only when supplied. No database field is
needed because registration validates the value.
The runner supplies only `client_name: "aboutme MCP owner workflow"`, the exact
loopback `redirect_uris` value, and `token_endpoint_auth_method: "none"`. The
SDK adds `application_type`; all other optional metadata stays unset. Tests use
the real SDK shape and cover legacy omission, invalid values, duplicates, and
non-loopback redirects.

## SDK and HTTP boundary

The runner uses the pinned `github.com/modelcontextprotocol/go-sdk v1.7.0`,
`mcp.StreamableClientTransport`, and the SDK authorization-code handler. It does
not reimplement JSON-RPC, discovery, DCR, authorization code, or token exchange.
It follows the official SDK
[protocol guide](https://github.com/modelcontextprotocol/go-sdk/blob/v1.7.0/docs/protocol.md)
and MCP
[authorization specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization).

The SDK discovers protected-resource and authorization-server metadata,
dynamically registers a public client, uses authorization code with Proof Key
for Code Exchange (PKCE) S256, exchanges the code, connects Streamable HTTP, and
lists tools. Consent requests exactly `resumes:read resumes:write`.

The callback binds to `127.0.0.1` on an ephemeral port. Registration and the
request use that exact URI. The listener accepts one matching callback and
closes.

One restricted HTTP client handles metadata, registration, token exchange,
refresh, revocation, and MCP transport. It permits only the configured local
origin or canonical production HTTPS origin. It rejects cross-origin endpoints
and redirects, uses no ambient cookies or proxy credentials, and permits
loopback only for the exact callback.
The runner wraps the SDK OAuth handler with a one-shot gate. The first
authorization may reach discovery, registration, consent, and exchange. After
the grant succeeds, another `Authorize` call always returns
`reauthorization_disabled`. The gate never resets after a token error. The SDK
cannot replace a revoked grant after `401`.

The SDK has no revocation helper, so the runner makes one narrow RFC 7009 call
through the restricted client. It posts the current refresh token to the
discovered `/oauth/revoke` endpoint as the exact form `token=<value>` and
`token_type_hint=refresh_token`, with the exact form content type, no cookie,
and no bearer header. The token never enters logs or errors. A transport error
or 5xx permits one byte-equivalent retry. The runner then probes `/mcp` with the
access token: `401` confirms revocation; a successful or unavailable probe
retains private state for revocation-only recovery. Evidence records only
`revoked` or `revocation_unconfirmed`.

A probe confirms revocation only before a conservative access cutoff, 55 minutes
after the token response's same-origin server `Date`. After that cutoff, but
before a conservative refresh cutoff of 29 days, recovery submits the exact
current refresh token to the token endpoint. `invalid_grant` confirms dead
authority. A successful rotation proves the grant is live; recovery atomically
journals the successor tokens and new server-bound cutoffs before revoking the
successor refresh token. An unknown response, missing server time, or reaching
the refresh cutoff retains the journal and cannot report revocation.

## Browser helper interface

The Go runner executes on the host. QA owns
`deploy/dev-https-browser/mcp-sdk.spec.ts`, which runs in the pinned browser
image through `deploy/dev-https-browser/run.sh`. The container retains host
networking, so Chromium can reach the host runner's loopback callback. The
launcher creates `<run>/browser/` mode `0700` and mounts only that subdirectory
read-write at `/mcp-browser`. Source, candidate, photo, token, and create-intent
files remain outside the mount. It mounts exactly one validated login file
read-only at `/mcp-credentials/login.env`; no credential directory is mounted.
Existing CA and spec mounts stay read-only.

Local mode reserves a synthetic account and writes its generated login values to
`<run>/synthetic-login.env`, a regular non-symbolic mode-`0600` file owned by
the current user. Only that file is mounted at the fixed container target. Local
mode must not resolve, stat, open, or mount `.dev/credentials/owner-test.env`.
The launcher removes the synthetic file with the fixture after the proof.
Production mode alone validates `.dev/credentials/owner-test.env` under the
credential rules below and mounts that exact file at the same container target.
The production launcher cannot start until the local SDK proof has recorded a
pass for the current runner and server build. Neither mode prints file content.

Playwright resolves Chromium from `ABOUTME_CHROMIUM_PATH` when the launcher
provides a validated executable, otherwise from `chromium.executablePath()` in
the pinned image. No browser executable path is a production workflow input.

QA owns `scripts/mcp-owner-workflow.sh` and its test. The script has closed
`local` and `production` modes, starts and joins the host runner and browser
container, and emits only fixed stage names. `make dev-https-mcp-sdk-check`
invokes local mode; the authorized run is
`scripts/mcp-owner-workflow.sh production`. CI runs only local mode.

After local success, the launcher atomically writes
`.dev/mcp-workflow/sdk-proof-attestation.json` mode `0600` under the mode-`0700`
root. Its closed version 1 object records scenario
`mcp-owner-workflow-local-v1`, result `passed`, completion time, release tag and
commit, SHA-256 of the runner, local server binary, launcher, browser runner,
and complete staged browser source manifest, plus immutable app, web, and
browser image digests. The browser-source digest hashes canonical ordered path,
expected mode, and byte entries for the config, spec, and every transitive local
import in the same closed manifest enforced by `run.sh` and its static tests.
These hashes are private control state and never enter evidence or output.

Production validates the regular non-symbolic attestation and recomputes every
local identity before it resolves or stats the owner credential path. It also
requires the release tag to name the recorded commit and the deployed immutable
image digests to match. Missing, malformed, time-reversed, older-than-24-hour,
or mismatched state exits before credential access. Any source, binary, image,
or release change requires a new local proof. Tests cover every rejection and
prove config, helper, mode, and manifest-membership changes invalidate the full
browser-source digest. They also prove local mode succeeds with no owner
credential file while its container arguments mount only the synthetic login
file.

The helper receives only `ABOUTME_MCP_BROWSER_DIR=/mcp-browser` and its mode. It
atomically creates `browser-ready` with fixed content `ready` and mode `0600`.
The Go `AuthorizationCodeFetcher` waits for that signal, then atomically writes
`browser-request.json` mode `0600` with this version 1 shape:

```json
{
  "version": 1,
  "authorization_url": "sensitive URL",
  "mode": "local or production",
  "expected_origin": "configured origin",
  "expected_login_path": "/login",
  "credential_file": "/mcp-credentials/login.env",
  "result_file": "/mcp-browser/browser-result.json"
}
```

Playwright reads and unlinks the request without printing it. It atomically
writes the result mode `0600` with only `completed`, `login_failed`,
`origin_rejected`, `consent_failed`, `second_factor_required`, or `timeout` as
raw ASCII bytes with no quotes or newline; only `completed` succeeds. Passkeys
are live in production and the owner workflow signs in with password only, so
a `202 secondFactorRequired` login reports `second_factor_required`, and the
runner maps it to its own word `second-factor-required` instead of the
generic `browser-handoff`. The fetcher waits at most five minutes. Go alone
validates callback code, state, issuer, host, port, and path, then hands the
SDK that already-verified issuer as `Iss`, never the raw query byte;
revocation needs no browser cookies.

Trace, video, screenshots, and browser console attachment are disabled. The URL
never enters an argument, environment variable, test title, stdout, report, or
retained artifact. The browser directory starts empty and permits only
`browser-ready`, `browser-request.json`, and `browser-result.json`, all regular
mode-`0600` files. The launcher and helper reject extra entries and symbolic
links. Handoff files and the subdirectory are removed after use.
Immediately before each saved credential fill, Playwright requires the top-level
page to have the configured origin and exact `/login` path. A `next` value may
name only a relative internal `/oauth/authorize` target. The helper rejects
child-frame forms, cross-origin frames, navigation, or redirects. It fills
credentials once and never refills after navigation.

## Inputs and private runtime

| Input           | Value or rule                                 |
| --------------- | --------------------------------------------- |
| MCP origin      | `https://aboutme.vn`                          |
| Credential file | `.dev/credentials/owner-test.env`             |
| Credential keys | `ABOUTME_TEST_EMAIL`, `ABOUTME_TEST_PASSWORD` |
| Source          | The only canonical resume with `lng: en`      |
| Target          | `lng: vi`, title `CV tiếng Việt`              |
| Scope           | `resumes:read resumes:write`                  |
| Limit           | One create intent, plus target photo writes   |

The account must have exactly one English resume and at most two total resumes.
Failure stops before mutation. The runner never deletes a resume to make space
and accepts no resume content, resume ID, credential, or token on the command
line.

The credential file must be regular, owned by the current user, non-symbolic,
and mode `0600`. The browser helper reads only the two named keys. Values never
enter arguments, output, errors, evidence, or the SDK. A current second factor
uses the existing browser flow; no second-factor secret is stored.

Production state lives only in the main checkout's `.dev/mcp-workflow`, found
from the Git common directory, so all worktrees share it. The runner refuses any
other production root and creates `<run>/` mode `0700` under it. Every file is
regular, non-symbolic, atomically written, and mode `0600`. Tokens, codes, OAuth
state, and PKCE verifiers stay in memory when possible. Failure cleanup removes
secret files but keeps unresolved create intent and payload.

Production also uses `.dev/mcp-workflow/owner-vi-complete.json` as a durable
one-shot control. Before OAuth or resume reads, the runner holds an exclusive
production lock and checks this file and the revocation journal. Either blocks
all create, translation, and resume work. A journal permits only revocation
recovery. A sentinel without a journal stops successfully. The synthetic proof
uses a separate root and never creates either file.

After proving the target and unchanged source, the runner first atomically
writes and syncs `owner-vi-revocation.json` mode `0600`. It contains only the
version `1`, origin, workflow `owner-vi-copy`, `target_confirmed: true`, a
same-origin server `target_confirmed_at` time, revoke endpoint, access token,
refresh token, exact token hint, server-bound access and refresh cutoffs, and
status `pending`. It then creates and syncs the completion sentinel from the
journal's version, origin, workflow, target confirmation, and time. Cleanup
never removes the sentinel.

The initial process or a restart may use the journal only for exact revocation,
expiry-bound access probing, and expiry-bound refresh-authority testing. It does
not initialize SDK OAuth, open a browser, or read resumes. Under the production
lock, recovery validates the journal's version, workflow, origin, target flag,
and confirmation time. If the sentinel is missing, recovery atomically creates
and syncs it from those attested fields before any revocation action. A
malformed or conflicting sentinel or a write or sync failure retains the journal
and blocks recovery.

The journal is deleted and its directory synced only after timely `401` or
timely `invalid_grant` and after the matching sentinel is durably present.
Uncertainty retains it. The manager never deletes the sentinel or unresolved
journal under this authorization.

## Translation handoff and contract

The generic runner does not translate or call a model API. It owns protocol
access, private artifacts, validation, and submission. The manager owns the
already authorized model translation.

The runner writes the canonical document to `source.json`. The active
manager-controlled model reads it and atomically writes only the translated
document to `candidate.json`. Owner content may pass through that active model
context and the tool calls needed to read and write these private files. It must
not appear in user-visible commentary, terminal output, logs, CI, tracked files,
screenshots, or evidence. No separate translation service, connector, provider
credential, or retained transcript is allowed.

The candidate sets `lng: vi` and omits `personalDetails.photo`, which is
server-owned. It preserves exactly:

- identity, contact values and types, email, phone, URLs, and visibility;
- organizations, products, projects, certifications, trademarks, and places;
- dates, numbers, currencies, percentages, units, and identifiers;
- schema and extension data, section and entry IDs, types, order, and flags;
- structure and customization values; and
- rich-text shape, marks, links, and link targets.

The model may translate headlines, prose, generic role names, display names,
labels, subtitles, and rich-text text nodes. It preserves official names and
ambiguous phrases. Translation must not add, remove, strengthen, or infer a
career fact.

Structural checks cannot prove meaning. The manager performs a separate semantic
pass comparing each statement's actor, action, responsibility, result, time,
quantity, and qualification. It rejects changes such as “assisted” to “led.” The
manager writes `candidate-review.json` atomically with source and candidate
digests and `facts_preserved: true`, but no resume text. The runner recomputes
both digests and requires an exact match. This is not an owner gate.

## Photo contract

When the source has a photo, the runner calls `get_photo`, sends those bytes to
`upload_photo` for the target, then applies the exact source crop. It never
writes or deletes the source photo. Photo bytes remain in memory or a transient
mode-`0600` file and never enter output or evidence.

Upload normalization may re-encode the image, so byte equality is not required.
Both decoded images must have the same media type, width, height, and alpha
behavior. A transparent PNG requires exact RGBA pixels. A JPEG requires at least
35 dB peak signal-to-noise ratio across RGB channels. Crops must both be absent
or have equal normalized numbers. Failure leaves one incomplete private target
and stops without deletion.

## Workflow

Local synthetic validation runs first:

1. Exercise discovery, registration, PKCE, browser consent, token exchange,
   Streamable HTTP, and exact tool discovery.
2. Prove private create, exact replay, revision conflict, photo parity, source
   immutability, durable recovery, revocation, server `401`, and blocked
   reauthorization.
3. Remove synthetic fixtures and safely resolved private runtime files.

After the accepted MCP server release is live, production runs once:

1. Lock production and require that no completion sentinel exists.
2. Authorize through the guarded helper. Login failure stops before resume read.
3. Enforce source and count constraints, then read source and optional photo.
4. Produce and semantically review the private candidate. Enforce preservation,
   schema, field, entry, aggregate, and request-size bounds.
5. Re-read the source. Any document, revision, photo, or publication change
   stops before mutation.
6. Persist the create intent, then submit it once.
7. Reconcile the outcome. Copy and validate any photo, chaining the latest
   target decimal revision.
8. Require one new private Vietnamese target, one unchanged English source, and
   resume count delta one.
9. Persist the revocation journal and completion sentinel, then inspect the
   target without retained media.
10. Revoke through `/oauth/revoke`, observe the next MCP request receive `401`,
    and require the one-shot gate to reject reauthorization.
11. Remove private state only after definitive create reconciliation. Retain
    redacted evidence.

## Mutation and recovery safety

Each mutation has one intent, canonical payload digest, and caller-generated
UUID idempotency key. Exact retry uses the same key and byte-equivalent payload.
An unresolved create forbids any new create intent.

Before `create_resume`, the runner atomically persists `create-intent.json` with
the canonical request payload, digest, key, account and origin binding,
source-count baseline, and first-attempt time. The journal survives crashes,
unknown responses, and failed-run cleanup.

Before the first send, the runner records same-origin server `Date` from a
successful authenticated response and sends within one minute. Recovery obtains
a fresh server `Date`. Exact replay is allowed only when fresh time is less than
12 hours after baseline, and the request deadline must end before that cutoff.
Missing, malformed, or reversed time forbids replay. This stays far inside the
server's 24-hour idempotency lifetime.

At or after the cutoff, recovery performs reads only. The manager reconciles the
account against the stored payload and count baseline. If one exact target
cannot be proved, the journal remains and all creates stay blocked. The runner
removes it only after one target is proved or a definitive response proves no
create was accepted.

Target mutations use the latest decimal revision. After `revision_conflict`, the
runner reads the target. It accepts an already exact result; otherwise it stops
without rebasing onto owner changes.

The runner enforces these caps outside model control:

- One create intent. Exact same-key replay is not a second intent.
- No `delete_resume` and no mutation naming the source ID.
- Only photo upload and crop may mutate the target after creation.
- No call outside the exact 15-tool registry.

## Privacy, revocation, and evidence

Owner content exists only in the authorized model context and private runtime.
Tracked files, terminal output, logs, CI, reports, and screenshots contain none.
Authorization URLs, credentials, tokens, codes, verifiers, identifiers, photos,
payloads, and hashes stay out of retained evidence. A failure prints one
closed-vocabulary line, `mcp-workflow: <word>`.

Evidence is an ignored mode-`0600` file of at most 4 KiB holding only fixed
labels, booleans, bounded counts, SDK and transport versions, tool count,
language, source and target status, count delta, create reconciliation,
revocation, and post-revocation `401` status.

`/oauth/revoke` invalidates the access and refresh families but leaves the
private resume. A failure before create leaves the account unchanged. A
confirmed or unknown create followed by failure leaves the target or intent for
safe reconciliation. Production never performs compensating deletion. Local
synthetic data may be deleted after proof.

## Compatibility, loss, and size

The change needs no migration, schema change, OpenAPI change, MCP tool change,
or dependency. OAuth gains optional canonical `resource` and native loopback DCR
handling. Older clients that omit both retain behavior. Deployments without the
fixes fail the SDK proof before owner data is read.

Removing the runner removes only verification tooling. It leaves both resumes
unchanged. The owner can edit or delete the new private resume normally. No
format conversion or downgrade occurs.

Existing limits stand: MCP body below 4 MiB, current schema and aggregate
bounds, decoded photo at most 2,097,152 bytes, and base64 photo request within
the MCP limit. The workflow adds one resume and at most one photo.

## Acceptance and release ownership

Acceptance requires:

- official-SDK discovery with exact protected-resource scopes, resource
  handling, native loopback DCR, PKCE, guarded browser consent, Streamable HTTP,
  and exact tool discovery;
- one private Vietnamese target, unchanged English source, factual and
  structural preservation, visual photo parity, and count delta one;
- exact idempotency, conservative recovery, revision handling, and mutation
  caps;
- no publication, deletion, or private-data leakage; and
- durable one-shot completion, grant revocation, server `401`, blocked
  reauthorization, restart before and after access expiry, safe refresh-token
  rotation, a crash after revocation-journal sync but before sentinel creation,
  safe cleanup, and allowlisted evidence.

Backend owns `apps/server/cmd/mcp-workflow/` and OAuth metadata, resource, and
DCR changes and tests in `apps/server/internal/oauthsrv/`. QA owns
`deploy/dev-https-browser/mcp-sdk.spec.ts`, `scripts/mcp-owner-workflow.sh`, its
test, the browser handoff, local proof, production procedure, and private
evidence. Devops owns mount and mode changes in
`deploy/dev-https-browser/run.sh`. The manager assigns shared `Makefile` edits,
and owns Git, CI, release order, and the authorized production run. The reviewer
performs the independent release review.

Owner inspection is optional and is not a release gate. Publication always
requires a separate explicit request.
