# Task 10.6: Edge module — CloudFront, ALB origin, ORPs, HSTS, and UAT gate

AC-INF-002; AC-INF-007/008 (the D25 gate + noindex, Phase 10
infrastructure-attributed rows).

**Task gate:** One author writes the failing checks first and runs the affected
checks. The fresh Phase 10 review covers viewer access, canonical-origin
redirects, cache invalidation, and the CloudFront-to-ALB trust path. Task
10.18's approved TLS, forwarding-header, and bypass contract is a prerequisite.

**Files:** `deploy/aws/modules/edge/**` (incl. the viewer-gate CloudFront
Function code, D25) (+ tests), env-root wiring (aliased `us-east-1` provider
passed from the roots).

The behavior matrix, reconciled with the
[deployment design](../../../design/deployment.md#cloudfront-behavior) and
[ADR 0019](../../../adr/0019-private-media-delivery.md), has only the
application ALB origin. There is no S3 origin, OAC, or `/assets/*` behavior.
Public and owner photo requests reach Go through the applicable API behavior.

The D25 access behavior below is a proposed baseline only. Phase 9 and the
pre-dispatch refresh must resolve the UAT access mechanism and its interaction
with Basic Authorization before this task is dispatched; this task does not
choose that mechanism silently.

The matrix includes the **origin-request-policy (ORP) column**, because
cookie/header/query forwarding is governed by the ORP, not the cache policy
(with CachingDisabled and no ORP, `/api/v1/*` would forward **no** cookies and
authenticated traffic would break — a header-forwarding hazard). Two custom ORPs
are defined in this module. `orp-no-cookie`: all query strings, zero cookies,
header allowlist including `Accept`, `Content-Type`, and `Last-Event-ID` (SSE
resume). `orp-auth-api` (Rev 3, replaces managed AllViewerExceptHostHeader): all
cookies, all query strings, and viewer headers forwarded **except `Host`,
`X-Real-IP`, and `Forwarded`**. Removing viewer `Host` makes CloudFront send the
ALB origin hostname selected by Task 10.18. CloudFront overwrites a viewer
header that has the same name as a configured custom origin header, so a viewer
`X-Origin-Secret` becomes exactly the configured value. The ORP must not also
name that custom header. The trust-header exclusions are pinned by `tofu test`,
and Task 10.15 proves the `allExcept` policy applies successfully. This table is
the review artifact — any deviation is a design question, not an implementation
choice:

| Precedence | Path pattern                   | Origin  | Allowed methods              | Cached methods | Cache policy                                                         | Origin request policy    | Cookies to origin | Notes                                                           |
| ---------- | ------------------------------ | ------- | ---------------------------- | -------------- | -------------------------------------------------------------------- | ------------------------ | ----------------- | --------------------------------------------------------------- |
| 1          | `/api/v1/live/*`               | app-alb | `GET`, `HEAD`, `OPTIONS`     | `GET`, `HEAD`  | CachingDisabled                                                      | `orp-no-cookie` (custom) | none              | SSE; timeout exceeds heartbeat and drain contract               |
| 2          | `/api/v1/events`               | app-alb | `GET`, `HEAD`, `OPTIONS`     | `GET`, `HEAD`  | CachingDisabled                                                      | `orp-auth-api` (custom)  | all               | authenticated SSE; timeout exceeds heartbeat and drain contract |
| 3          | `/api/v1/public/resumes/*/pdf` | app-alb | `GET`, `HEAD`, `OPTIONS`     | `GET`, `HEAD`  | TTL min 0 / default 0 / **max 60 s**, respect origin `Cache-Control` | `orp-no-cookie` (custom) | none              | live/download-gated public PDF; publish-state invalidation      |
| 4          | `/api/v1/public/*`             | app-alb | `GET`, `HEAD`, `OPTIONS`     | `GET`, `HEAD`  | CachingDisabled                                                      | `orp-no-cookie` (custom) | **none**          | public JSON/photo; ETag revalidation from app                   |
| 5          | `/api/v1/*`                    | app-alb | All seven CloudFront methods | `GET`, `HEAD`  | CachingDisabled                                                      | `orp-auth-api` (custom)  | all               | authenticated API; viewer trust headers excluded                |
| default    | `*` (HTML/md/og/PDF/sitemap)   | app-alb | `GET`, `HEAD`, `OPTIONS`     | `GET`, `HEAD`  | TTL min 0 / default 0 / **max 60 s**, respect origin `Cache-Control` | `orp-no-cookie` (custom) | none              | never cache `Set-Cookie`; minimal key                           |

**HSTS placement:** the
[production topology](../../../design/deployment.md#production-topology)
requires HTTP Strict Transport Security. A CloudFront **response-headers
policy** attached to every behavior sets
`Strict-Transport-Security: max-age=31536000; includeSubDomains` — edge-owned so
it also covers edge-cached responses; app/route-level security headers (CSP
etc.) remain the owning route phase's scope and are not duplicated here. The
same policy carries the blanket `X-Robots-Tag: noindex, nofollow` **when
`var.noindex_all` is true** (UAT baseline; D25).

**One viewer-request function serves both host canonicalization and the UAT
gate.** It runs on every behavior in both environments. First, a request whose
host is in `var.redirect_hosts` (production: only `www.aboutme.vn`) returns
`308` to `https://aboutme.vn` with the path and semantic query parameters
preserved. This happens before authentication or origin processing, so no
cookie, authorization header, or request reaches Caddy. Other hosts proceed to
the optional UAT gate. The gate uses CloudFront Functions JavaScript runtime 2.0
and `crypto.createHash("sha256")` over the exact single `Authorization` header
value. It compares the 64-character lowercase digest against the SSM- stored
digest with a fixed-length accumulator, rejects missing, malformed, or repeated
credentials, and returns `401` with
`WWW-Authenticate: Basic realm="aboutme-uat", charset="UTF-8"` and
`Cache-Control: no-store`. On success it deletes `Authorization` before origin
forwarding. Plaintext username/password values exist only as protected GitHub
environment/operator secrets for smoke requests; they never enter SSM, OpenTofu
state, function code, logs, or evidence.

**Steps:**

- [ ] Failing `tofu test` (mocked, `override_data` for the SSM origin-secret
      read) first: behaviors exist in exactly this precedence order; every
      behavior's `allowed_methods` and `cached_methods` match the table, so
      authenticated `POST`, `PUT`, `PATCH`, and `DELETE` reach Go while only
      `GET` and `HEAD` are cache-eligible; **each behavior's
      `origin_request_policy_id` matches the ORP column** (`orp-no-cookie`
      forwards zero cookies, all query strings, and includes `Last-Event-ID` in
      its header allowlist; `orp-auth-api` forwards all cookies and all query
      strings); **a dedicated assertion pins that neither custom ORP's header
      configuration can forward viewer-supplied `Host`, `X-Real-IP`, or
      `Forwarded` to the origin**, and that no ORP lists the custom
      `X-Origin-Secret` header. Assert CloudFront's custom header overwrites a
      same-named viewer header and removing viewer `Host` supplies the ALB
      origin hostname; the two SSE behaviors point at the `app-alb-sse` origin
      whose timeout follows Task 10.18; the default origin uses its approved
      timeout; no behavior other than `/api/v1/*` and `/api/v1/events` forwards
      cookies; every origin request to the ALB carries the `X-Origin-Secret`
      custom header. Both custom origins set
      `origin_protocol_policy = "https-only"`, `https_port = 443`,
      `origin_ssl_protocols = ["TLSv1.2"]`, and SNI/hostname validation against
      the ALB certificate hostname; viewer protocol policy `redirect-to-https`;
      `minimum_protocol_version` ≥ TLSv1.2_2021; the response-headers policy
      with HSTS is attached to **every** behavior, and adds
      `X-Robots-Tag: noindex, nofollow` iff `var.noindex_all`; **the
      viewer-request function association exists on every behavior in both
      environments**. Unit fixtures prove the `www` 308 runs before the gate,
      preserves path/query semantics, and never yields an origin request; the
      UAT gate applies iff `var.viewer_gate_enabled` (true in the staging-shaped
      UAT environment, false in production), rejects wrong/missing/repeated
      headers, uses the exact runtime-2.0 SHA-256 contract above, and strips
      `Authorization` after success. The distribution contains only `app-alb`
      and `app-alb-sse` origins pointing to the same active ALB and has no OAC
      or `/assets/*` behavior; aliases and ACM certificate ARN come from
      variables. Scheduled UAT cannot enable the distribution without its ALB.
- [ ] Implement distribution + custom ORP + response-headers policy + the
      basic-auth viewer-request CloudFront Function (credential hash injected
      from SSM at apply; function code contains the hash, never the plaintext) +
      `aws_acm_certificate` (us-east-1 provider alias) with DNS validation
      records **exported as outputs** for the D19 `cf` script — no Cloudflare
      provider. Export the distribution ID, ARN, domain, and certificate
      validation records; the ID/ARN feed only Task 10.5's server environment
      and Task 10.4's exact invalidation policy.
- [ ] Origin-secret value: **regular `data "aws_ssm_parameter"` marked
      `sensitive`** feeding the custom origin header (an ephemeral value cannot
      populate a persistent CloudFront argument — this is D9's documented,
      mitigated exception; keep it out of _outputs_ and mark related variables
      `sensitive`).

**Verification:** `tofu test`, `validate`, parity. Real CloudFront behavior
(matrix observed through a live edge) is **Phase 10 operational rehearsal's row
AC-OPS-015** (plus AC-OPS-002 live bypass rejection); Phase 10 infrastructure's
cheapest safe check is the mocked assertion set above, which pins the
configuration content. Task 10.15 must prove AWS accepted the custom `allExcept`
policy before Phase 10 infrastructure can close. The test also fails if any S3
origin, OAC, or `/assets/*` behavior appears.
