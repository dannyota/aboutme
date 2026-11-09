# MCP CIMD failed-grant admission

[MCP CIMD authorization](mcp-cimd-authorization.md) owns the surrounding
boundary and the [error matrix](mcp-cimd-authorization.md#errors).

Technical amendment approved by the primary for the owner on 2026-10-14: isolate
failed-grant admission by the stored grant, for both CIMD and DCR. Naming a
public client identifier must not spend any owner's grant budget.

After the existing token-IP admission and form decoding, resolve the presented
credential with the existing strict parser and digest lookup. An authorization
code row supplies its `GrantID`; a refresh-token row supplies its `GrantID` only
when both the parsed token and stored row have refresh kind. Use that stored
grant UUID as the failed-grant key. Never derive it from the claimed client, a
cookie, a supplied grant ID, or metadata fetching. The same grant's codes and
refresh tokens share a bucket; different owners of one client have different
grant UUIDs. A supplied wrong client cannot redirect debt to that client's
grants. Internal UUID aliases still fail public client validation.

Missing, malformed, wrong-kind, or never-issued credentials reserve no grant
slot and create no failed-grant key, including no overflow debt. They remain
under the existing token-IP limit and return the existing token errors. An
admission lookup database failure returns generic `500` without a reservation or
token mutation; it must not mean an unknown credential. This lookup chooses
admission identity only. Token processing must still recheck all authority and
bindings under the existing locks. A consumed, expired, superseded, or revoked
credential whose row remains can select only its own stored grant.

Keep the existing bounded fixed-window reservation algorithm: reserve before
processing, count pending reservations against the limit, retain a failure only
for the existing counted `invalid_grant` outcomes, and release neutral outcomes.
Success clears only that tracked grant's window; stale completions cannot
restore cleared debt. Expiry, monotonic time, maximum keys, shared overflow,
overflow-success behavior stay unchanged. A refusal blocks token issue even when
its public response hides the refusal, as defined below. Numeric limits live in
[Budgets](budgets.md). Unknown guesses cannot fill this map; holders of enough
real grants can still reach the shared overflow boundary of
[ADR 0007](../adr/0007-bounded-rate-limiter.md). Requests sharing an IP still
share the separate token-IP budget.

## Refusal response and client recovery

A grant-bucket refusal must not reveal whether a candidate credential has a
stored row. Determine the response through the existing token validation rules,
without token mutations. A request that lacks valid authority keeps the error it
would receive without the grant limiter. In particular, stored and never-issued
candidates that fail with `invalid_grant` must have identical status, body
bytes, and headers before and after exhaustion:

- Status `400`; body exactly
  `{"error":"invalid_grant","error_description":"The request is invalid."}`.
- `Content-Type: application/json` and `Cache-Control: no-store`.
- No `Retry-After`, rate-limit header, cookie, or other signal of grant state.
  All other response headers follow the same path as ordinary `invalid_grant`;
  transport-generated dates and request IDs must be independent of credential
  existence and admission outcome.

The [error matrix](mcp-cimd-authorization.md#errors) still governs malformed
requests and unknown clients; grant refusal cannot override their errors either.
Token-IP refusal remains before lookup and keeps its `429` and `Retry-After` for
all candidates.

Return the existing `429 invalid_request` with its fixed error description and
positive `Retry-After` only if the request would otherwise be authorized to
receive tokens. Row existence alone is insufficient. For a code, require the
correct public client, exact redirect, S256 verifier, live unconsumed code, live
grant and user, and matching authentication epoch. For refresh, require a
current live refresh token and family, live grant and user, matching epoch, and
a matching public client when supplied; omission remains allowed. Both paths
must pass all existing form, resource, scope, and binding checks.

On refusal, run those checks under the existing token transaction lock order,
re-reading state rather than trusting the admission lookup. Use the same
validation predicates as issuance, but stop before any write. Do not consume a
code, issue or rotate tokens, revoke a code's tokens or a reused refresh family,
clear debt, or finish a nonexistent reservation. End the transaction without
business changes. A database failure remains generic `500`, never evidence of
valid authority. An admitted request keeps normal token processing, including
reuse revocation. A concurrent consume, rotation, revocation, or epoch change
that wins the locks must prevent a subsequent valid-authority classification.

A client presenting valid authority still learns to wait from `Retry-After`, the
remaining grant window rounded up to at least one second. It must not retry
before that delay. The delay does not extend credential lifetime: an expired
code requires fresh authorization, and refresh can be retried only while its
authority remains live. A client presenting invalid authority receives no
throttle hint; it must stop retrying the unchanged request and correct its
parameters or obtain fresh authorization through its ordinary `invalid_grant`
recovery. Older clients need no new error or header handling.

This preserves useful backoff without creating buckets for unknown guesses. The
cost is read-only authority validation even for a refused stored grant; token-IP
admission still bounds that work per address. It promises response equivalence,
not constant-time database access. Returning `400` for every refusal would also
hide existence but would tell a client with live authority to reconnect
unnecessarily. Returning `429` before authority validation would expose stored
credentials. Shared overflow follows the same response rules.

## Scope and rollout

DCR has the same flaw: its public UUID identifies a client, not the caller or
the grant owner. It therefore receives this same limiter correction; its
registration, redirects, consent, and token authority stay unchanged. Using
existing stored grant UUIDs avoids new composite keys, per-credential buckets,
or a second source-address policy. Counting arbitrary presented credentials
would let anonymous key churn fill the bounded map and spend shared overflow.

A stolen code or refresh token can spend its own grant's allowance, but cannot
select another grant. Existing revocation and recovery rules apply; this adds no
key, secret, persistent data, or telemetry field. Apply the correction with the
CIMD backend before enablement, including while CIMD is off. No migration,
client update, or data loss is required. Restart clears process-local limiter
state as before; older clients keep the same token forms and error bodies.
