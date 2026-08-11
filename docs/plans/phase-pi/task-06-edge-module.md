# Task 6: Edge module — CloudFront behavior matrix, ORPs, HSTS, staging gate, ACM

AC-INF-002; AC-INF-007/008 (the D25 gate + noindex, PI-attributed rows).

**Files:** `deploy/aws/modules/edge/**` (incl. the viewer-gate CloudFront
Function code, D25) (+ tests), env-root wiring (aliased `us-east-1` provider
passed from the roots).

The behavior matrix, transcribed from spec §6 — now with the
**origin-request-policy (ORP) column**, because cookie/header/query forwarding
is governed by the ORP, not the cache policy (with CachingDisabled and no ORP,
`/api/v1/*` would forward **no** cookies and authenticated traffic would break —
review blocking 7). Two custom ORPs are defined in this module. `orp-no-cookie`:
all query strings, zero cookies, header allowlist including `Accept`,
`Content-Type`, and `Last-Event-ID` (SSE resume). `orp-auth-api` (Rev 3,
replaces managed AllViewerExceptHostHeader): all cookies, all query strings, and
viewer headers forwarded **except `X-Origin-Secret`, `X-Real-IP`, and
`Forwarded`** — a viewer-sent `X-Origin-Secret` reaching the origin alongside
CloudFront's custom origin header would arrive as a second instance and trip the
D8 multi-instance-403 rule on the authenticated API path (the D25 viewer-request
function cannot do this stripping: it is disabled in production). Both ORPs'
exclusion of those three headers is pinned by a `terraform test` assertion. This
table is the review artifact — any deviation is a spec question, not an
implementation choice:

| Precedence | Path pattern                 | Origin    | Cache policy                                                         | Origin request policy    | Cookies to origin | Notes                                                                                                    |
| ---------- | ---------------------------- | --------- | -------------------------------------------------------------------- | ------------------------ | ----------------- | -------------------------------------------------------------------------------------------------------- |
| 1          | `/api/v1/live/*`             | caddy-sse | CachingDisabled                                                      | `orp-no-cookie` (custom) | none              | SSE; 60 s origin read timeout (D22)                                                                      |
| 2          | `/api/v1/events`             | caddy-sse | CachingDisabled                                                      | `orp-no-cookie` (custom) | none              | SSE                                                                                                      |
| 3          | `/api/v1/public/*`           | caddy     | CachingDisabled                                                      | `orp-no-cookie` (custom) | **none**          | public JSON; ETag/no-cache from app                                                                      |
| 4          | `/api/v1/*`                  | caddy     | CachingDisabled                                                      | `orp-auth-api` (custom)  | all               | authenticated API; `no-store` from app; viewer `X-Origin-Secret`/`X-Real-IP`/`Forwarded` never forwarded |
| 5          | `/assets/*`                  | S3 (OAC)  | CachingOptimized                                                     | none (OAC/S3 default)    | none              | media/avatars                                                                                            |
| default    | `*` (HTML/md/og/PDF/sitemap) | caddy     | TTL min 0 / default 0 / **max 60 s**, respect origin `Cache-Control` | `orp-no-cookie` (custom) | none              | never cache `Set-Cookie`; minimal key                                                                    |

**HSTS placement (spec §6 requires HSTS):** a CloudFront **response-headers
policy** attached to every behavior sets
`Strict-Transport-Security: max-age=31536000; includeSubDomains` — edge-owned so
it also covers edge-cached objects; app/route-level security headers (CSP etc.)
remain P8-sec's scope and are not duplicated here. The same policy carries the
blanket `X-Robots-Tag: noindex, nofollow` **when `var.noindex_all` is true**
(staging; D25).

**Steps:**

- [ ] Failing `terraform test` (mocked, `override_data` for the SSM
      origin-secret read) first: behaviors exist in exactly this precedence
      order; **each behavior's `origin_request_policy_id` matches the ORP
      column** (`orp-no-cookie` forwards zero cookies, all query strings, and
      includes `Last-Event-ID` in its header allowlist; `orp-auth-api` forwards
      all cookies and all query strings); **a dedicated assertion pins that
      neither custom ORP's header configuration can forward viewer-supplied
      `X-Origin-Secret`, `X-Real-IP`, or `Forwarded` to the origin** (Rev 3 —
      protects the D8 multi-instance-403 rule on the authenticated path); the
      two SSE behaviors point at the `caddy-sse` origin whose
      `origin_read_timeout = 60`; default origin timeout 30; no behavior other
      than `/api/v1/*` forwards cookies; every origin request to Caddy carries
      the `X-Origin-Secret` custom header; viewer protocol policy
      `redirect-to-https`; `minimum_protocol_version` ≥ TLSv1.2_2021; the
      response-headers policy with HSTS is attached to **every** behavior, and
      adds `X-Robots-Tag: noindex, nofollow` iff `var.noindex_all`; **the
      viewer-request function association exists on every behavior iff
      `var.viewer_gate_enabled`** (true in `staging.auto.tfvars`, false in
      `production.auto.tfvars` — env-varying variable, same code path, parity
      preserved — D25); S3 origin uses OAC and the default/S3 behaviors forward
      no cookies; aliases + ACM certificate ARN come from variables; the origin
      domain is `var.origin_fqdn` (`origin-staging.aboutme.vn` /
      `origin.aboutme.vn`).
- [ ] Implement distribution + custom ORP + response-headers policy + the
      basic-auth viewer-request CloudFront Function (credential hash injected
      from SSM at apply; function code contains the hash, never the plaintext) +
      `aws_acm_certificate` (us-east-1 provider alias) with DNS validation
      records **exported as outputs** for the D19 `cf` script — no Cloudflare
      provider.
- [ ] Origin-secret value: **regular `data "aws_ssm_parameter"` marked
      `sensitive`** feeding the custom origin header (an ephemeral value cannot
      populate a persistent CloudFront argument — this is D9's documented,
      mitigated exception; keep it out of _outputs_ and mark related variables
      `sensitive`).

**Verification:** `terraform test`, `validate`, parity. Real CloudFront behavior
(matrix observed through a live edge) is **P9A's row AC-OPS-015** (plus
AC-OPS-002 live bypass rejection); PI's cheapest safe check is the mocked
assertion set above, which pins the configuration content.
