# Escalations, interfaces, and exit criteria

## Escalations pending human owner

These are **not** resolved by the design-decisions table; each lists the default
this plan assumes if approved. The integration owner routes them; the human
owner decides.

| #   | Escalation                                                                                                                          | Default assumed if approved                                                                                                                               |
| --- | ----------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | D21 sizing — recurring real-money commitment with no budget row                                                                     | Staging `t4g.small`/`db.t4g.micro`; production `t4g.medium`/`db.t4g.small`; tfvars-only change if overturned                                              |
| 2   | AWS/Cloudflare resource creation, mutation scope, and recurring staging spend                                                       | After P9 and independent evidence verification pass, the owner records one explicit AWS resource-creation authorization; no real-cloud action precedes it |
| 3   | AWS account, Cloudflare API token, `var.oncall_email` — human-provided inputs with no acquisition path in-plan                      | Owner supplies via `.env`/`secrets-bootstrap.sh` only after the AWS authorization checkpoint                                                              |
| 4   | D1 — Terraform (BUSL) tooling in an AGPL-3.0 public repo                                                                            | Terraform retained: repo ships only HCL (no BUSL binaries); OpenTofu fallback preserved by plain-HCL constraint                                           |
| 5   | Web-tier trust posture (review blocking 5)                                                                                          | The D24 redesign (web outside the host namespace; no risk acceptance) — approving D24 closes this                                                         |
| 6   | D9 — origin secret unavoidably in CloudFront distribution config + TF state                                                         | Accepted with mitigations: SSE-KMS state + noncurrent expiration, scoped state-read role, rotation runbook                                                |
| 7   | Public `staging.aboutme.vn` + ACM cert in CT logs while the [trademark gate](../../design/decisions.md#open-approval-gates) is open | Staging gated (D25: basic auth + blanket noindex); CT-log residue accepted — owner may instead direct a neutral staging domain (tfvars-only change)       |

## Interfaces PI leaves behind (consumed by later phases)

| Consumer    | Interface                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P9A         | `envs/staging` applies from empty state (D23); alarm-trigger method table (Task 9 README) for AC-OPS-019; restore-drill runbook + manually invocable job for AC-OPS-018; secret-rotation runbook (D9) for AC-OPS-016; deploy/rollback workflow + runbook for AC-OPS-017 (two concurrent dispatches); direct-EIP bypass check procedure (AC-OPS-002); live behavior-matrix probe surface (AC-OPS-015); synthetic CloudFront→Caddy→app→DB smoke in the deploy workflow; staging-gate credentials for drill tooling |
| P9A media   | Re-run P2B's frozen normalization manifest and corpus on its selected production Graviton class and exact 512 MiB task cgroup; any >5 s sample or >192 MiB RSS delta blocks launch                                                                                                                                                                                                                                                                                                                               |
| P10         | `envs/production` root (byte-identical to staging, D4) + `production.auto.tfvars` (gate disabled, noindex off); the deploy workflow shape re-pointed at production with the **staging-proven digest manifest**; `dns-apply.sh --check/--apply` for cutover records; P10 authors **no new Terraform** (master plan)                                                                                                                                                                                               |
| P7A         | Chromium seam in `server.Dockerfile` (untouched); 512 MiB task-level memory + render-queue custom-metric alarm slot (Task 9)                                                                                                                                                                                                                                                                                                                                                                                     |
| P8 privacy  | PI consumes the final `idempotency-expiry-sweep`, `media-orphan-sweep`, and `privacy-retention-sweep` commands and activates their exact Task 10 schedules; a mismatch blocks dispatch rather than creating an alias in IaC                                                                                                                                                                                                                                                                                      |
| P6A         | ulimit/fd headroom in task definitions; SSE origin timeout margins (D22)                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| P2B/P8-priv | Private media bucket identity and fixed `resumes/` prefix; the server task role alone can list that prefix and get, put, or delete its objects                                                                                                                                                                                                                                                                                                                                                                   |
| P2B+/web    | `NUXT_INTERNAL_API_BASE` (Caddy internal listener, D24) as the SSR-internal API base contract                                                                                                                                                                                                                                                                                                                                                                                                                    |

## Phase exit criteria

- [ ] Every module (incl. bootstrap) and both env roots: `terraform fmt -check`,
      `validate`, `terraform test` (mock providers with explicit `override_data`
      where data sources feed wiring), tflint — green under `make ci` locally
      (ADR 0011 gate of record), and on the PR gate for fork PRs, with **zero
      AWS credentials** either way; parity check (byte diff + tfvars key-set)
      proves staging/production differ only by `backend.hcl` + variable values.
- [ ] The **BLOCKING** boundary test (`prod_boundary_test.go`, all 22 rows — two
      viewers through one simulated edge; forged AND duplicated
      `X-Forwarded-For`/`X-Real-IP`/`Forwarded`/`X-Origin-Secret`; fail-closed
      rows 11–15 for empty/unset secrets and CIDRs; internal listener;
      `admin off`; health vhost; credential-free log schema; non-root 443 bind)
      passes in CI against the pinned caddy binary; the entrypoint-guard unit
      test passes; the dev route-table test is green **unmodified**;
      `Caddyfile.prod` validates inside the Task 7 custom Caddy image.
- [ ] `envs/staging` applied cleanly to real AWS **after P9 local UAT and
      independent evidence verification passed at the candidate commit and human
      AWS resource-creation authorization was recorded**: services stable, the
      staged foundation → secrets/DNS/certificate → DB bootstrap → full
      services/edge sequence and synthetic CloudFront→Caddy→app→DB smoke green,
      post-apply `plan` shows zero changes, stateful-safe destroy/re-apply cycle
      proven once, direct-to-origin no-secret request rejected, gate challenge +
      noindex headers observed, bridge gateway address verified live, and the
      SSR chain (web → internal listener → Go) exercised end to end with log
      evidence — all in the phase ledger. The ledger also proves the media
      bucket is not public, CloudFront has no direct media path, and live IAM
      simulation matches Task 4's exact server-only media policy.
- [ ] Secrets: decrypting `secrets-bootstrap.sh --check` green against the
      bootstrap-retained environment key; no secret value in repo, tfvars,
      outputs, or workflow logs; no plan-time reads of secret values except the
      three consuming sites (D10 ephemeral master password, D9 sensitive origin
      secret, and D25 sensitive Basic-header digest); IAM scoping tests prove
      per-service SSM path isolation; the CloudFront header exception is
      documented with mitigation (D9, incl. state-version expiration).
- [ ] Media storage is private: all public-access-block settings are true; no
      website, ACL, CloudFront principal, OAC, or `/assets/*` behavior exists.
      Exact-policy tests prove that only the server task role can list
      `resumes/` and can get, put, or delete `<media-bucket-arn>/resumes/*`.
      Every other role is denied media access.
- [ ] Alarm inventory (incl. prefix-list drift + heartbeat alarms) +
      dashboards + SNS subscription provisioned with the stated default
      thresholds; each alarm's deliberate-trigger method documented for P9A's
      AC-OPS-019.
- [ ] Scheduled jobs enabled: hourly idempotency expiry, weekly media orphan,
      daily privacy retention, nightly restore verification, daily TLS expiry,
      and six-hour CIDR drift. Exact task/PassRole policies, ownership-safe
      overlap, zero silent retry, and failure plus missing-heartbeat alarms
      pass.
- [ ] Image pipeline: four arm64 images built natively and pushed to the
      bootstrap-owned ECR by digest through the build-only OIDC role (no static
      keys); deploy workflow with **drain → zero-writer proof → verified RDS
      snapshot → fresh saved plan**, plan-approval gate, drain→readiness,
      circuit-breaker rollback, the migration-init-container sequence riding
      AC-OPS-001's advisory lock, and a passing previous-digest smoke against
      the migrated schema before code-back/schema-forward rollback is documented
      as available.
- [ ] All four runbooks seeded and docs gates green; `docs/architecture.md`
      update handed to the integration owner as a diff (owner-serialized).
- [ ] Traceability: the adoption-time patches (AC-INF-001…008, AC-OPS-015…019,
      master-plan corrections incl. Edit 3) are committed to `main` by the
      integration owner; PI's rows carry filled test references handed as a diff
      at Task 14.
- [ ] Every task delivered per its ADR 0011 risk tier: high-risk tasks (1, 2, 4,
      6, 7, 10, 12) get author TDD, then a fresh worker deriving tests from the
      owning design pages and acceptance IDs before reading the diff, then a
      fresh reviewer; normal-risk tasks get author TDD plus `make ci`; blocking
      findings resolved. Task 7 (client-IP boundary) additionally gets
      independent adversarial tests derived from the
      [client-IP design](../../design/deployment.md#client-ip-boundary)
      **before** the reviewer reads the implementation diff (security-sensitive
      per the master-plan workflow table), and `make semgrep` runs clean on the
      touched configuration.
