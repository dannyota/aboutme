# MCP owner workflow

The owner workflow drives the production Model Context Protocol (MCP) interface
as a real client. It reads the account's only English resume and creates exactly
one new private Vietnamese resume from a model translation. It never writes the
source, publishes, or deletes a resume; publication needs a separate owner
request through the web application.

The runner (`apps/server/cmd/mcp-workflow/`) uses the official Go MCP SDK. A
local proof with synthetic data (`make dev-https-mcp-sdk-check`) runs in CI; the
production run is `scripts/mcp-owner-workflow.sh production` and runs once. The
raw JSON-RPC browser proof stays a separate server regression test. The workflow
follows [ADR 0026](../adr/0026-mcp-agent-access.md) and changes no MCP tool,
resume schema, or public contract.

The exact tool registry has 15 tools: `list_resumes`, `get_resume`,
`create_resume`, `delete_resume`, `update_resume_metadata`, `upsert_entry`,
`delete_entry`, `update_section`, `update_structure`, `update_personal_details`,
`update_customization`, `get_photo`, `upload_photo`, `update_photo_crop`, and
`delete_photo`.

## OAuth server contract

The SDK needs three server behaviors, and each keeps older clients working.

`/.well-known/oauth-protected-resource` includes the exact ordered field
`"scopes_supported":["resumes:read","resumes:write"]`; the SDK derives its
requested scopes from it. The unauthenticated `/mcp` challenge still names only
the protected-resource metadata URL.

The authorization endpoint accepts zero or one `resource`, which must equal the
canonical public origin byte for byte. Missing keeps legacy behavior. Empty,
duplicate, malformed, non-canonical, or different values get the closed
`invalid_request`. An `authorization_code` token exchange applies the same rule
with `invalid_grant`; refresh-token requests reject `resource`. One protected
resource means no database or token-shape change.

Dynamic client registration (DCR) accepts zero or one `application_type`. A
present value must be exactly `native`, and every redirect must be an HTTP
loopback URI; any other shape fails closed. The response echoes `native` only
when supplied. The runner registers only `client_name`
`"aboutme MCP owner workflow"`, its exact loopback `redirect_uris`, and
`token_endpoint_auth_method: "none"`; the SDK adds `application_type`.

## SDK and HTTP boundary

The runner uses the pinned `github.com/modelcontextprotocol/go-sdk v1.7.0`,
`mcp.StreamableClientTransport`, and the SDK authorization-code handler, and
reimplements none of JSON-RPC, discovery, DCR, PKCE, or token exchange. Consent
requests exactly `resumes:read resumes:write`. The callback binds `127.0.0.1` on
an ephemeral port, accepts one matching callback, and closes.

One restricted HTTP client carries metadata, registration, token, refresh,
revocation, and MCP traffic. It allows only the configured local origin or the
canonical production origin, rejects cross-origin endpoints and redirects, uses
no ambient cookies or proxy credentials, and allows loopback only for the
callback. A one-shot gate wraps the SDK OAuth handler: after the first grant
succeeds, every later `Authorize` returns `reauthorization_disabled`, and the
gate never resets.

The SDK has no revocation helper, so the runner posts the current refresh token
to the discovered `/oauth/revoke` as the exact form `token=<value>` and
`token_type_hint=refresh_token`, with no cookie or bearer header. A transport
error or `5xx` allows one identical retry. The runner then probes `/mcp` with
the access token; `401` confirms revocation.

A probe counts only before an access cutoff 55 minutes after the token
response's server `Date`. Between that and a refresh cutoff of 29 days, recovery
submits the current refresh token: `invalid_grant` proves the authority dead,
and a successful rotation journals the successor tokens and new cutoffs before
revoking the successor. An unknown response, missing server time, or a passed
refresh cutoff keeps the journal and cannot report revocation.

## Browser helper interface

The Go runner runs on the host. `deploy/dev-https-browser/mcp-sdk.spec.ts` runs
in the pinned browser image through `deploy/dev-https-browser/run.sh` with host
networking, so Chromium reaches the loopback callback. The launcher creates
`<run>/browser/` mode `0700` and mounts only that directory read-write at
`/mcp-browser`, plus exactly one login file read-only at
`/mcp-credentials/login.env`. Source, candidate, photo, token, and intent files
stay outside the mounts.

Local mode writes a synthetic login to `<run>/synthetic-login.env` (regular,
non-symbolic, mode `0600`, owned by the user), mounts only that file, removes it
after the proof, and never resolves `.dev/credentials/owner-test.env`.
Production mode alone validates and mounts that owner file. Neither mode prints
file content. Chromium comes from a validated `ABOUTME_CHROMIUM_PATH` or the
pinned image; no browser path is a production input.

After local success, the launcher writes
`.dev/mcp-workflow/sdk-proof-attestation.json` (mode `0600`, version 1, scenario
`mcp-owner-workflow-local-v1`, result `passed`). It records completion time,
release tag and commit, SHA-256 of the runner, local server binary, launcher,
browser runner, and the full staged browser-source manifest, and the app, web,
and browser image digests. The browser-source digest hashes ordered path, mode,
and bytes for the config, spec, and every transitive local import in the closed
manifest that `run.sh` enforces. Production recomputes every identity and checks
that the tag names the recorded commit and the deployed digests match, before it
touches the owner credential path. A missing, malformed, time-reversed,
over-24-hour-old, or mismatched attestation stops the run; any source, binary,
image, or release change needs a new local proof.

The helper gets only `ABOUTME_MCP_BROWSER_DIR=/mcp-browser` and its mode. It
writes `browser-ready` (content `ready`). The Go `AuthorizationCodeFetcher`
waits for that file, then writes `browser-request.json`:

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

Playwright reads and unlinks the request without printing it, then writes the
result as raw ASCII with no quotes or newline: `completed`, `login_failed`,
`origin_rejected`, `consent_failed`, `second_factor_required`, or `timeout`.
Only `completed` succeeds. The owner signs in with password only, so a `202`
second-factor response reports `second_factor_required`, which the runner maps
to its own word `second-factor-required`. The fetcher waits at most five
minutes. Go alone validates the callback code, state, issuer, host, port, and
path, and hands the SDK the verified issuer as `Iss`.

Every handoff file is regular and mode `0600`, written atomically. The directory
starts empty, allows only the three named files, rejects extras and symbolic
links, and is removed after use. Trace, video, screenshots, and console capture
are off, and the URL never enters an argument, environment variable, title,
output, or artifact. Before filling credentials, Playwright requires the
top-level page to be the configured origin at exactly `/login`; a `next` value
may name only a relative `/oauth/authorize`. It rejects child-frame forms,
cross-origin frames, and redirects, and fills credentials once.

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

The account must hold exactly one English resume and at most two resumes, or the
run stops before any mutation. The runner never deletes to make room and takes
no content, ID, credential, or token on the command line. The credential file
must be regular, user-owned, non-symbolic, and mode `0600`; its values never
reach arguments, output, errors, evidence, or the SDK.

Production state lives only in the main checkout's `.dev/mcp-workflow`, found
from the Git common directory, with a mode-`0700` `<run>/` per run. Every file
is regular, non-symbolic, atomic, and mode `0600`. Tokens, codes, state, and
verifiers stay in memory when possible. Failure cleanup removes secret files but
keeps an unresolved create intent and payload.

`.dev/mcp-workflow/owner-vi-complete.json` is the durable one-shot sentinel.
Before OAuth or any resume read, the runner takes an exclusive production lock
and checks the sentinel and the revocation journal. A journal permits only
revocation recovery; a sentinel without a journal stops successfully. The local
proof uses a separate root and never creates either file.

After proving the target and the unchanged source, the runner writes and syncs
`owner-vi-revocation.json`: version `1`, origin, workflow `owner-vi-copy`,
`target_confirmed: true`, server `target_confirmed_at`, revoke endpoint, access
and refresh tokens, token hint, server-bound cutoffs, and status `pending`. It
then creates and syncs the sentinel from those fields. Recovery, on a first run
or restart, may only revoke, probe, and test refresh authority; it opens no
browser and reads no resume. It recreates a missing sentinel from the journal
before acting, and stops on a malformed or conflicting sentinel or a failed
write. The journal is deleted only after a timely `401` or `invalid_grant` with
the sentinel durably present. Nothing deletes the sentinel.

## Translation handoff and contract

The runner does not translate or call a model API. It writes the canonical
document to `source.json`; the operator's authorized model writes only the
translated document to `candidate.json`. Owner content may pass through that
model context and the file tool calls, but never into commentary, terminal
output, logs, CI, tracked files, screenshots, or evidence. No translation
service, connector, provider credential, or retained transcript is allowed.

The candidate sets `lng: vi` and omits the server-owned `personalDetails.photo`.
It preserves exactly: identity, contact values and types, URLs, and visibility;
organizations, products, projects, certifications, trademarks, and places;
dates, numbers, currencies, percentages, units, and identifiers; schema data,
section and entry IDs, types, order, and flags; structure and customization; and
rich-text shape, marks, and link targets. The model may translate headlines,
prose, generic role names, display names, labels, subtitles, and rich-text text
nodes. It keeps official names and must not add, remove, strengthen, or infer a
career fact.

A separate semantic pass compares each statement's actor, action,
responsibility, result, time, quantity, and qualification, and rejects changes
such as "assisted" to "led". It writes `candidate-review.json` with source and
candidate digests and `facts_preserved: true`, and no resume text. The runner
recomputes both digests and requires a match.

## Photo contract

When the source has a photo, the runner reads it with `get_photo`, uploads those
bytes to the target, and applies the exact source crop. Photo bytes stay in
memory or a transient mode-`0600` file. Normalization may re-encode, so bytes
need not match. The decoded images must match media type, size, and alpha
behavior: exact RGBA for a transparent PNG, at least 35 dB PSNR across RGB for a
JPEG. Crops must both be absent or equal. A failure leaves one incomplete
private target and stops without deleting.

## Workflow

The local proof:

1. Exercises discovery, registration, PKCE, browser consent, token exchange,
   Streamable HTTP, and exact tool discovery.
2. Proves private create, exact replay, revision conflict, photo parity, source
   immutability, durable recovery, revocation, server `401`, and blocked
   reauthorization.
3. Removes synthetic fixtures and resolved private files.

The production run:

1. Locks production and requires that no sentinel exists.
2. Authorizes through the guarded helper; a login failure stops before any read.
3. Checks the source and count rules, then reads the source and optional photo.
4. Produces and reviews the candidate and enforces preservation, schema, field,
   entry, aggregate, and request-size bounds.
5. Re-reads the source; any document, revision, photo, or publication change
   stops before mutation.
6. Persists the create intent, then submits it once.
7. Reconciles the outcome and copies any photo, chaining the latest target
   revision.
8. Requires one new private Vietnamese target, an unchanged source, and a count
   delta of one.
9. Persists the revocation journal and sentinel.
10. Revokes through `/oauth/revoke`, sees the next MCP request get `401`, and
    requires the gate to refuse reauthorization.
11. Removes private state after definitive reconciliation and keeps redacted
    evidence.

## Mutation and recovery safety

Each mutation has one intent, a canonical payload digest, and a caller-generated
UUID idempotency key; a retry reuses both. Before `create_resume`, the runner
persists `create-intent.json` with the payload, digest, key, account and origin
binding, count baseline, and first-attempt time. It survives crashes, unknown
responses, and cleanup. While a create is unresolved, no new create may start.

The first send happens within one minute of a server `Date` from an
authenticated response. Recovery takes a fresh server `Date`; exact replay is
allowed only under 12 hours after the baseline, with the request deadline before
that cutoff, well inside the 24-hour idempotency lifetime. Missing, malformed,
or reversed time forbids replay. After the cutoff, recovery only reads and
reconciles the account against the stored payload and baseline; the journal
stays until one target is proved or a definitive response proves no create.

Target mutations use the latest decimal revision. After `revision_conflict`, the
runner reads the target, accepts an already exact result, and otherwise stops
without rebasing onto owner changes. The runner enforces these caps outside
model control: one create intent; no `delete_resume` and no mutation naming the
source; only photo upload and crop on the target after creation; no call outside
the 15-tool registry.

## Privacy, revocation, and evidence

Owner content exists only in the authorized model context and the private
runtime. Authorization URLs, credentials, tokens, codes, verifiers, identifiers,
photos, payloads, and hashes never reach retained evidence. A failure prints one
closed-vocabulary line, `mcp-workflow: <word>`.

Evidence is an ignored mode-`0600` file of at most 4 KiB with fixed labels,
booleans, bounded counts, SDK and transport versions, tool count, language,
source and target status, count delta, create reconciliation, revocation, and
post-revocation `401` status.

Revocation kills the access and refresh families and leaves the private resume.
A failure before any create intent best-effort revokes the grant in a deferred
call without changing the reported word. A confirmed or unknown create followed
by a failure keeps the grant and the intent for reconciliation. Production never
deletes to compensate; local synthetic data may be deleted after the proof.

The workflow adds one resume and at most one photo within the existing MCP body,
schema, aggregate, and photo limits. Removing the runner removes only tooling.
