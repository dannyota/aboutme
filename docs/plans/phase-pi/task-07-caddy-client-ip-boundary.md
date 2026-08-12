# Task 7: **BLOCKING** — production Caddy client-IP boundary, fail-closed config, prod image, simulated-edge e2e test

AC-INF-001; mechanism half of AC-OPS-002. This task is the reason PI cannot
slip: promoting the dev Caddyfile unchanged recreates a cross-tenant DoS (master
plan, Phase 0 security review). The prod Caddy image (`deploy/caddy.Dockerfile`)
is authored **here**, not in Task 8, because `Caddyfile.prod` can only be
validated inside it (review blocking 11).

**Files:**
`deploy/caddy/{routes.caddy,boundary.caddy,Caddyfile.prod,Caddyfile.boundary-test,caddy-entrypoint.sh}`,
`deploy/caddy.Dockerfile`, `deploy/caddy/Caddyfile` (modify: import
`routes.caddy`), `apps/server/internal/routetable/prod_boundary_test.go`,
Makefile diff (`route-table-test-prod`) for the integration owner.

**Boundary contract (what `boundary.caddy` + global options + the entrypoint
guard must implement):**

1. **Fail-closed origin-secret gate (D8):** authenticate only when the presented
   `X-Origin-Secret` is a single header instance whose non-empty value equals a
   **non-empty** configured secret (`{$ORIGIN_SECRET_CURRENT}` or
   `{$ORIGIN_SECRET_NEXT}`, sourced from SSM via ECS secrets). The CEL
   expression must require non-emptiness on both sides — env placeholders
   substitute to `""` when unset and an absent header is also `""`, so bare
   equality would authenticate the world in the steady state (next unset).
   Multiple `X-Origin-Secret` instances never authenticate, regardless of order
   or whether one value is correct. Everything else ⇒ `403` before any routing.
2. Derive the viewer address via Caddy's trusted-proxy chain walk:
   `trusted_proxies static {$CLOUDFRONT_ORIGIN_CIDRS}` +
   `client_ip_headers X-Forwarded-For`; `{client_ip}` is then the first address
   (right-to-left) **outside** the trusted ranges — the entry CloudFront itself
   appended, i.e. the real viewer (D5/D6). An **empty/unset CIDR set must refuse
   service**: the entrypoint guard (`caddy-entrypoint.sh`) exits nonzero before
   starting Caddy when `ORIGIN_SECRET_CURRENT` or `CLOUDFRONT_ORIGIN_CIDRS` is
   unset/empty, and the boundary test pins the config layer's behavior with an
   empty set (row 15) so the deployed system is fail-closed at **both** layers.
3. Strip every inbound forwarding header (`X-Forwarded-For`, `X-Real-IP`,
   `Forwarded`, `X-Forwarded-Host`, `X-Forwarded-Proto`) and the
   `X-Origin-Secret` itself from what goes upstream, **and suppress
   `reverse_proxy`'s default re-addition of forwarding headers explicitly inside
   every `reverse_proxy` block**: `header_up -X-Forwarded-For`,
   `header_up -X-Forwarded-Proto`, `header_up -X-Forwarded-Host` (Caddy re-adds
   these upstream by default — inbound `request_header` stripping alone is
   insufficient and the strict count assertions would fail without the explicit
   `header_up` deletions). Emit **exactly one** `X-Real-IP: {client_ip}` via
   `header_up`.
4. Route per `routes.caddy` (identical table to dev, incl. `/print/*` denial);
   upstreams via `{$GO_UPSTREAM:server:8080}` / `{$WEB_UPSTREAM:web:3000}` env
   placeholders with dev defaults (D7).
5. **Internal SSR listener (D24):** a separate site block on
   `{$INTERNAL_API_LISTEN}` (prod: the bridge gateway address, port 8081)
   proxying only `/api/v1/*` to `{$GO_UPSTREAM}`, with **no origin-secret
   requirement**, stripping all inbound forwarding headers (with the same
   explicit `header_up` deletions) and emitting exactly one `X-Real-IP` set to
   the immediate peer (`{remote_host}`) — correct on this listener because the
   peer _is_ the client (the web container).
6. `Caddyfile.prod` sets **`admin off`** globally (under host networking the
   admin API would expose the adapted config — secrets included — to any
   host-namespace process), and defines a loopback-only health vhost
   `http://127.0.0.1:2020` answering `200` at `/caddy-health` (the ECS container
   health check target, Task 5).
7. `deploy/caddy.Dockerfile` (D13): pinned xcaddy builds Caddy **2.11.4**
   `--with github.com/caddy-dns/cloudflare@<pinned>`; runtime from the pinned
   alpine digest; non-root; entrypoint `caddy-entrypoint.sh` (the fail-closed
   env guard, itself unit-tested). The guard additionally verifies (Rev 3) that
   the address in `INTERNAL_API_LISTEN` exists on a host interface before
   starting Caddy — the bridge-gateway default (`172.17.0.1`, D24) is the plan's
   riskiest real-AWS assumption, and a missing address must refuse start rather
   than silently bind-fail into a restart loop (Caddy's own bind error is the
   second layer; Task 14 verifies the address live).
8. **Credential-free structured access logs:** both origin listeners emit JSON
   to stdout with only timestamp/logger, listener (`external` or `internal`),
   route marker, host, method, path, status, request ID, canonical client IP,
   duration, and byte count. A Caddy filter strips the query from `request>uri`
   with a `\\?.*$` replacement, deletes the complete request and response header
   maps plus TLS/user/remote-port fields, and never enables `log_credentials`.
   Route/listener markers and the upstream request ID are appended before
   encoding. The pinned adapted-config test compares the exact field set. OAuth
   `code`/`state`, Basic `Authorization`, cookies, CSRF, origin secrets, and
   arbitrary query values never reach a log, even as hashes.

**Steps:**

- [ ] **Write the failing e2e test first** — same harness pattern as
      `route_table_test.go` (real `caddy` binary via `CADDY_BIN`, stub Go
      backends, `Caddyfile.boundary-test` importing the same snippets on a
      **test-chosen unprivileged listen port injected via env**, reusing the
      existing listen-port substitution pattern — never `:80` in CI). The stub
      backend records, per request, every
      `X-Real-IP`/`X-Forwarded-*`/`Forwarded`/`X-Origin-Secret` value received
      (multi-value aware — it must count header occurrences, not just read the
      first). Test matrix (table-driven; default env for the run:
      `CLOUDFRONT_ORIGIN_CIDRS=127.0.0.0/8`, `ORIGIN_SECRET_CURRENT=itest-cur`,
      `ORIGIN_SECRET_NEXT=itest-next` — loopback plays CloudFront; rows 11–15
      restart caddy with the stated env deltas):

| #   | Simulated request (edge = loopback client)                                                                                                           | Expected at origin/backend                                                                                                                                                                                                                                                                                            |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Secret=cur; edge-appended `X-Forwarded-For: 203.0.113.10` (viewer A)                                                                                 | 200; backend sees exactly one `X-Real-IP: 203.0.113.10`; no XFF/Forwarded/secret leak                                                                                                                                                                                                                                 |
| 2   | Secret=cur; `X-Forwarded-For: 198.51.100.7` (viewer B, same edge socket)                                                                             | 200; exactly one `X-Real-IP: 198.51.100.7` — two viewers through one edge keyed apart                                                                                                                                                                                                                                 |
| 3   | Secret=cur; viewer-forged prefix: `X-Forwarded-For: 10.0.0.1, 172.16.0.1, 203.0.113.10` (edge appended last)                                         | `X-Real-IP: 203.0.113.10` — forged left-hand entries ignored by the chain walk                                                                                                                                                                                                                                        |
| 4   | Secret=cur; viewer-forged `X-Real-IP: 6.6.6.6` **and** `Forwarded: for=6.6.6.6` plus edge XFF `203.0.113.10`                                         | Exactly one `X-Real-IP: 203.0.113.10`; forged headers stripped, count == 1                                                                                                                                                                                                                                            |
| 5   | No `X-Origin-Secret`; well-formed XFF                                                                                                                | `403`; backend never receives the request                                                                                                                                                                                                                                                                             |
| 6   | Wrong secret                                                                                                                                         | `403`; backend never receives the request                                                                                                                                                                                                                                                                             |
| 7   | Secret=**next**                                                                                                                                      | 200 (rotation window: current+next both accepted)                                                                                                                                                                                                                                                                     |
| 8   | Re-run subtests 1/3 with `CLOUDFRONT_ORIGIN_CIDRS=192.0.2.0/24` (loopback now untrusted)                                                             | `X-Real-IP` = the socket peer (127.0.0.1), **never** an XFF-supplied value                                                                                                                                                                                                                                            |
| 9   | Secret=cur; malformed XFF (`X-Forwarded-For: not-an-ip`)                                                                                             | Upstream `X-Real-IP` is a valid IP (peer fallback) or the request is rejected — never garbage forwarded (Go rejects malformed values regardless, AC-OPS-008; assert the boundary doesn't rely on that)                                                                                                                |
| 10  | Secret=cur; route checks through the boundary: `/api/v1/x` → go-backend, `/` → web-backend, `/print/x` → 404                                         | Route table identical to dev (shared `routes.caddy` proven in the prod wrapper too)                                                                                                                                                                                                                                   |
| 11  | **NEXT unset** (env absent); secret=cur                                                                                                              | 200 — steady state (no rotation in progress) still authenticates the real secret                                                                                                                                                                                                                                      |
| 12  | **NEXT unset**; no `X-Origin-Secret` header                                                                                                          | `403` — absent header must not match the empty NEXT placeholder (the `"" == ""` fail-open, review blocking 1)                                                                                                                                                                                                         |
| 13  | **NEXT unset**; header present with empty value (`X-Origin-Secret:`)                                                                                 | `403` — an empty presented value never authenticates                                                                                                                                                                                                                                                                  |
| 14  | **CURRENT and NEXT both unset**; any request, correct-looking or not                                                                                 | Every request `403` at the config layer; **separately**, `caddy-entrypoint.sh` with the same env exits nonzero before starting Caddy (bash unit test in this task)                                                                                                                                                    |
| 15  | **`CLOUDFRONT_ORIGIN_CIDRS` unset/empty**; secret correct; forged XFF                                                                                | Caddy refuses to load the config **or** every derived `X-Real-IP` equals the socket peer and never an XFF value — the test pins whichever the pinned Caddy version does with an explicit assertion; **and** the entrypoint guard exits nonzero on this env, so the deployed image can never reach the ambiguous state |
| 16  | Secret=cur; **duplicated `X-Forwarded-For` header instances** (line 1: `10.0.0.1`; line 2: `203.0.113.10` edge-appended)                             | Exactly one `X-Real-IP: 203.0.113.10` — instance duplication is equivalent to the comma form; the forged first instance is ignored                                                                                                                                                                                    |
| 17  | **Duplicated `X-Origin-Secret` instances**: (a) wrong then right; (b) right then wrong                                                               | `403` in both orders — multiple credential instances never authenticate (D8)                                                                                                                                                                                                                                          |
| 18  | **Internal listener (D24):** request to `{$INTERNAL_API_LISTEN}` `/api/v1/x` with forged `X-Real-IP`/XFF/`Forwarded`, no origin secret               | 200; upstream sees exactly one `X-Real-IP` == the socket peer address; all forged headers absent                                                                                                                                                                                                                      |
| 19  | Connection attempt to the admin API port                                                                                                             | Refused — `admin off` is in effect (config asserted AND connection probed)                                                                                                                                                                                                                                            |
| 20  | `GET http://127.0.0.1:2020/caddy-health`                                                                                                             | `200` — the loopback health vhost the ECS health check targets (Task 5)                                                                                                                                                                                                                                               |
| 21  | External OAuth callback with sentinel query values plus Cookie, Authorization, CSRF, forwarding, and origin-secret headers; one internal SSR request | Captured JSON has the exact allowlisted keys, path without query, distinct listener markers, canonical IP and request ID; no sentinel, raw query, request/response header map, cookie, credential, or secret appears anywhere                                                                                         |
| 22  | Run the production container with Task 5's security options and host port 443 in the staging-shaped harness                                          | Caddy UID is 10001, only `CAP_NET_BIND_SERVICE` remains, writable Caddy/log mounts have the pinned owner/mode, and the listener accepts TLS on 443                                                                                                                                                                    |

Run:
`cd apps/server && CADDY_BIN=caddy go test ./internal/routetable/   -run ProdBoundary -count=1`
→ **FAIL** (config files don't exist yet).

- [ ] Implement `routes.caddy` (extracted verbatim from the dev Caddyfile,
      upstreams parameterized with dev defaults — D7), `boundary.caddy`,
      `Caddyfile.prod` (`admin off`; site block for `{$ORIGIN_FQDN}`:
      `tls { dns cloudflare {$CLOUDFLARE_API_TOKEN} }`; internal listener;
      health vhost; credential-free access-log filter; imports boundary +
      routes), `Caddyfile.boundary-test` (HTTP-only, env-substituted port, same
      imports), `caddy-entrypoint.sh` + a small bash unit test for its refusal
      cases, `deploy/caddy.Dockerfile`, and the dev `Caddyfile` import swap.
- [ ] Existing dev route-table test **must stay green unmodified**
      (`make route-table-test`) — it is the regression gate for the D7
      extraction. If it needs editing, stop and report; do not adapt the test to
      the refactor.
- [ ] Build the prod image locally with podman (amd64 locally is fine; CI builds
      arm64 natively) and run the config-validity check inside it:
      `podman run --rm -v "$PWD/deploy/caddy:/cfg:ro,Z" <image> caddy     validate --config /cfg/Caddyfile.prod`
      with dummy env — the vanilla caddy binary cannot validate `Caddyfile.prod`
      (unknown `dns.providers.cloudflare` module), which is why the image lives
      in this task (review blocking 11).
- [ ] Author the Makefile diff (`route-table-test-prod` mirroring
      `route-table-test` with `-run ProdBoundary`) and hand it to the
      integration owner. Task 13 wires the CI job.

**Verification:** both Caddy test suites green with a pinned caddy 2.11.4
binary; the entrypoint-guard bash unit test green; the in-image `caddy validate`
command recorded as the config-validity check. Entirely CI-runnable; no AWS.
