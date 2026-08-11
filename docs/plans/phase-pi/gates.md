# Escalations, interfaces, and exit criteria

## Escalations pending human owner

These are **not** resolved by the design-decisions table; each lists the default
this plan assumes if approved. The integration owner routes them; the human
owner decides.

| #   | Escalation                                                                                                     | Default assumed if approved                                                                                                                               |
| --- | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | D21 sizing — recurring real-money commitment with no budget row                                                | Staging `t4g.small`/`db.t4g.micro`; production `t4g.medium`/`db.t4g.small`; tfvars-only change if overturned                                              |
| 2   | AWS/Cloudflare resource creation, mutation scope, and recurring staging spend                                  | After P9 and independent evidence verification pass, the owner records one explicit AWS resource-creation authorization; no real-cloud action precedes it |
| 3   | AWS account, Cloudflare API token, `var.oncall_email` — human-provided inputs with no acquisition path in-plan | Owner supplies via `.env`/`secrets-bootstrap.sh` only after the AWS authorization checkpoint                                                              |
| 4   | D1 — Terraform (BUSL) tooling in an AGPL-3.0 public repo                                                       | Terraform retained: repo ships only HCL (no BUSL binaries); OpenTofu fallback preserved by plain-HCL constraint                                           |
| 5   | D14 — closing port 80 deviates from spec §6's literal "Caddy 80/443"                                           | 443-only ingress from the CloudFront prefix list; port 80 closed                                                                                          |
| 6   | Web-tier trust posture (review blocking 5)                                                                     | The D24 redesign (web outside the host namespace; no risk acceptance) — approving D24 closes this                                                         |
| 7   | D9 — origin secret unavoidably in CloudFront distribution config + TF state                                    | Accepted with mitigations: SSE-KMS state + noncurrent expiration, scoped state-read role, rotation runbook                                                |
| 8   | Public `staging.aboutme.vn` + ACM cert in CT logs while the spec §10 trademark item is open                    | Staging gated (D25: basic auth + blanket noindex); CT-log residue accepted — owner may instead direct a neutral staging domain (tfvars-only change)       |

## Interfaces PI leaves behind (consumed by later phases)

| Consumer | Interface                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P9A      | `envs/staging` applies from empty state (D23); alarm-trigger method table (Task 9 README) for AC-OPS-019; restore-drill runbook + manually invocable job for AC-OPS-018; secret-rotation runbook (D9) for AC-OPS-016; deploy/rollback workflow + runbook for AC-OPS-017 (two concurrent dispatches); direct-EIP bypass check procedure (AC-OPS-002); live behavior-matrix probe surface (AC-OPS-015); synthetic CloudFront→Caddy→app→DB smoke in the deploy workflow; staging-gate credentials for drill tooling |
| P10      | `envs/production` root (byte-identical to staging, D4) + `production.auto.tfvars` (gate disabled, noindex off); the deploy workflow shape re-pointed at production with the **staging-proven digest manifest**; `dns-apply.sh --check/--apply` for cutover records; P10 authors **no new Terraform** (master plan)                                                                                                                                                                                               |
| P7A      | Chromium seam in `server.Dockerfile` (untouched); 512 MiB task-level memory + render-queue custom-metric alarm slot (Task 9)                                                                                                                                                                                                                                                                                                                                                                                     |
| P8-priv  | `retention-sweep` server subcommand contract + disabled schedule (Task 10, D20)                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| P6A      | ulimit/fd headroom in task definitions; SSE origin timeout margins (D22)                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| P2B+/web | `NUXT_INTERNAL_API_BASE` (Caddy internal listener, D24) as the SSR-internal API base contract                                                                                                                                                                                                                                                                                                                                                                                                                    |

## Phase exit criteria

- [ ] Every module (incl. bootstrap) and both env roots: `terraform fmt -check`,
      `validate`, `terraform test` (mock providers with explicit `override_data`
      where data sources feed wiring), tflint — green in CI on the PR gate with
      **zero AWS credentials**; parity check (byte diff + tfvars key-set) proves
      staging/production differ only by `backend.hcl` + variable values.
- [ ] The **BLOCKING** boundary test (`prod_boundary_test.go`, all 20 rows — two
      viewers through one simulated edge; forged AND duplicated
      `X-Forwarded-For`/`X-Real-IP`/`Forwarded`/`X-Origin-Secret`; fail-closed
      rows 11–15 for empty/unset secrets and CIDRs; internal listener;
      `admin off`; health vhost) passes in CI against the pinned caddy binary;
      the entrypoint-guard unit test passes; the dev route-table test is green
      **unmodified**; `Caddyfile.prod` validates inside the Task 7 custom Caddy
      image.
- [ ] `envs/staging` applied cleanly to real AWS **after P9 local UAT and
      independent evidence verification passed at the candidate commit and human
      AWS resource-creation authorization was recorded**: services stable,
      synthetic CloudFront→Caddy→app→DB smoke green (through the D25 gate),
      post-apply `plan` shows zero changes, destroy/re-apply cycle proven once,
      direct-to-origin no-secret request rejected, gate challenge + noindex
      headers observed, bridge gateway address verified live, and the SSR chain
      (web → internal listener → Go) exercised end to end with log evidence —
      all in the phase ledger.
- [ ] Secrets: `secrets-bootstrap.sh --check` green; no secret value in repo,
      tfvars, outputs, or workflow logs; no plan-time reads of secret values
      except the two consuming sites (D10 ephemeral, D9 sensitive data source);
      IAM scoping tests prove per-service SSM path isolation; the CloudFront
      header exception is documented with mitigation (D9, incl. state-version
      expiration).
- [ ] Alarm inventory (incl. prefix-list drift + heartbeat alarms) +
      dashboards + SNS subscription provisioned with the stated default
      thresholds; each alarm's deliberate-trigger method documented for P9A's
      AC-OPS-019.
- [ ] Scheduled jobs provisioned: restore-verify (nightly, overlap-guarded,
      heartbeat-alarmed incl. missing-data), cidr-drift-check (6-hourly,
      alarmed), TLS-expiry check, retention schedule wired but disabled pending
      P8-priv.
- [ ] Image pipeline: four arm64 images built natively and pushed to the
      bootstrap-owned ECR by digest via OIDC (no static keys); deploy workflow
      with **verified pre-migration RDS snapshot**, plan-approval gate,
      drain→readiness, circuit-breaker rollback, the migration-init-container
      sequence riding AC-OPS-001's advisory lock, and code-back/schema-forward
      rollback documented in the runbook.
- [ ] All four runbooks seeded and docs gates green; `docs/architecture.md`
      update handed to the integration owner as a diff (owner-serialized).
- [ ] Traceability: the adoption-time patches (AC-INF-001…008, AC-OPS-015…019,
      master-plan corrections incl. Edit 3) are committed to `main` by the
      integration owner; PI's rows carry filled test references handed as a diff
      at Task 14.
- [ ] Opus 5 has reviewed every task diff; blocking findings resolved. Task 7
      (client-IP boundary) additionally gets independent adversarial tests
      derived from the spec **before** the reviewer reads the implementation
      diff (security-sensitive per the master-plan workflow table), and
      `make semgrep` runs clean on the touched configuration.
