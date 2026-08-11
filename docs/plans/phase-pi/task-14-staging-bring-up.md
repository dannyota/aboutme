# Task 14: Authorized staging bring-up (real AWS), runbooks complete, evidence

Closes "modules apply cleanly to a staging environment". **This task is
explicitly real-AWS**, operator-run after the human AWS-authorization
checkpoint, with spend visibility (D21 sizing).

**Preconditions (hard gate — do not start without all four):**

- [ ] P9 local-UAT report is `PASS` at the exact candidate commit, and a fresh
      independent evidence reviewer returned `PASS` for that same commit.
- [ ] **Recorded human-owner AWS resource-creation authorization**, including
      staging-scale spend and approved AWS/Cloudflare mutation scope (see
      "Escalations pending human owner" #2). **No bootstrap apply, ECR push, DNS
      mutation, staging apply, or workflow dispatch without it.**
- [ ] Owner-provided inputs in hand: AWS account/role access, Cloudflare API
      token (Zone:DNS:Edit, `aboutme.vn` only), `var.oncall_email` value.
- [ ] Base commit + image digests from Task 8's first build recorded;
      `secrets-bootstrap.sh --check` green after bootstrap-write.

**Steps:**

- [ ] Sequence per Task 11's ordering: `terraform apply` in `envs/staging` →
      `dns-apply.sh --apply` → ACM issued → CloudFront deployed →
      `deploy-staging.yml` dispatch → services stable → synthetic smoke green
      (CloudFront→Caddy→app→DB via `/readyz`, with staging-gate credentials —
      D25).
- [ ] **Bridge-gateway + SSR path end-to-end (Rev 3, D24):** before/at caddy
      start, verify the bridge gateway address live — record
      `ip addr show docker0` (or the ECS AMI's equivalent bridge) on the
      instance proving `var.bridge_gateway_ip` exists, and confirm the caddy
      task started (its entrypoint guard verifies the `INTERNAL_API_LISTEN`
      address and refuses otherwise). Then fetch a Nuxt-SSR-rendered page
      through the staging CloudFront URL (gate credentials) and confirm the full
      chain **web → internal listener (bridge gateway :8081) → Go**: the caddy
      internal-listener access log shows the SSR fetch from the web container's
      bridge address, and the Go server log shows the same request with
      canonical `X-Real-IP` = that bridge address (the D24 keying ruling
      observed live). Record both log excerpts in the ledger.
- [ ] Fail-closed spot-checks (cheap, staging-only, recorded in the ledger):
      direct-to-EIP request **without** the origin secret → connection refused
      or 403 (prefix-list + secret, AC-OPS-002 mechanism); `curl` with a forged
      `X-Forwarded-For` through CloudFront → application rate-limit keying
      unaffected (observed via server logs' canonical IP); a viewer-sent
      `X-Origin-Secret` through CloudFront → not forwarded by the ORP, request
      served normally with exactly one origin-side secret instance (Task 6 Rev 3
      assertion observed live); unauthenticated request to `staging.aboutme.vn`
      → gate challenge, not product content (D25); response headers carry HSTS +
      `X-Robots-Tag: noindex` (Task 6); `terraform plan` immediately after apply
      → **zero changes** (the cheapest idempotency proof); `terraform destroy` +
      re-`apply` once → clean both ways (D23), then leave staging **up or down
      per the integration owner's cost call**, recorded.
- [ ] Author the `docs/architecture.md` current-state update (staging exists,
      module map, one Mermaid diagram — the spec's intended-design diagram is
      not duplicated) **as a diff handed to the integration owner**
      (owner-serialized file); complete all four runbook seeds;
      `make docs-fmt && make docs-lint` on files PI owns.
- [ ] Hand the integration owner: evidence ledger path, filled test references
      for AC-INF-001…008 in `../traceability/ac-inf.md` (as a diff —
      owner-serialized), and the master-plan Phase-status row update text ("PI:
      staging applied at `<commit>`").

**Verification:** the recorded command outputs above; every prior task's CI
checks green at the final commit. What PI does **not** claim: ops drills,
CloudFront matrix live probing (AC-OPS-015), rotation drill (AC-OPS-016), live
two-runner migration (AC-OPS-017), real restore timing (AC-OPS-018), alarm
receipt (AC-OPS-019), SSE soak — all P9A, against this staging environment.

> **Constraint inherited from Phase 1 (DD-C16) — do not apply a blanket
> `Referrer-Policy: no-referrer` at the edge.** The
> `/api/v1/auth/{provider}/start` endpoints reject cross-site initiation for
> `purpose=link|reauth`: they accept `Sec-Fetch-Site: same-origin` and otherwise
> fall back to an Origin/Referer same-origin check, failing closed. On browsers
> that do not send `Sec-Fetch-Site`, stripping the same-origin `Referer` from
> the app's HTML document leaves the fallback with no signal and **provider
> linking breaks for those users**. The response-headers policy may set
> `no-referrer` on `/api/v1/*` (the Go server already does), but the policy
> applied to the Nuxt document must preserve same-origin referrers —
> `strict-origin-when-cross-origin` (the browser default) or `same-origin`. This
> is not discoverable from `apps/web`, which sets no policy of its own; assert
> it in Task 6's behavior tests and spot-check it in Task 14's bring-up.

---
