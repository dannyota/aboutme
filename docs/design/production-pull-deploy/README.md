# Production pull deployment

Status: Accepted with conditions (2026-11-01), under the
[ADR 0026 amendment](../../adr/0026-vietnam-hosted-production.md#amendment-anonymous-approval-pull-deployment-2026-11-01).
Implementation is planned; the [live probe](../../research/approval-probe.md)
records the evidence and its limits.

Runtime decision (owner, 2026-11-01): keep the Podman containers on the host and
use Ansible pull for deployment and host configuration. A read-only measurement
that day found about 11 MiB and no measurable CPU for the container runtime
helpers on the 2 vCPU, 4 GiB host, so native services would save little while
losing signed image digests, read-only roots, and container isolation.

The production host verifies an explicitly approved request, then runs
`ansible-pull` at its exact signed coordinator commit. GitHub builds and signs
public images. The host holds no GitHub credential. Deployment jobs receive no
production credential or administrative network access.

The first version retains attended laptop proofs and an empty automatic rollback
allowlist. It adds no monitoring VM. The separate
[Synthetic quota-renewal amendment](../../adr/0026-vietnam-hosted-production.md#amendment-synthetic-quota-renewal-in-github-actions-2026-11-01)
governs vMonitor credentials and automation. Pull removes CI’s administrative
access at the cost of host verification, polling delay, and durable recovery
state. Laptop deployment stays available in parallel and shares the same
controls. A push service or unattended proof service requires a separate
decision.

## Pages

| Page                                        | Holds                                                             |
| ------------------------------------------- | ----------------------------------------------------------------- |
| [Protocol](protocol.md)                     | Closed schemas and durable state                                  |
| [Evidence](evidence.md)                     | Trust, release modes, image verification, and historical evidence |
| [Execution](execution.md)                   | Discovery, admission, transitions, cleanup, and Ansible           |
| [Attendance](attendance.md)                 | Administrative commands and external proofs                       |
| [Recovery](recovery.md)                     | Configuration revisions, activation, rollback, and recovery       |
| [Status and threats](status-and-threats.md) | Public status, custody, threats, and network boundaries           |

## Anonymous verification prerequisite

The owner accepted the measured approval path with the
[cache wait, single-page history, and single-review conditions](evidence.md#anonymous-approval-and-freshness).
The host must apply those conditions and the mapping below. Live multi-page
history and approval by a second maintainer remain unverified; the restricted
contract permits implementation without claiming either was proved. Keep
[approval enrollment](#approval-enrollment) owner-only until distinct-maintainer
approval is verified. Solo owner approval is an accepted supplied requirement.

| Response            | Host acceptance rule                                                                                                                                         |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Run                 | Pinned repository and workflow IDs; `event=workflow_dispatch`; `head_branch=main`; `head_sha=request.coordinator.commit`; `run_attempt=1`; completed success |
| Actors              | Both `actor.id` and `triggering_actor.id` belong to enrolled maintainer IDs                                                                                  |
| Approval history    | Exactly one relevant approval: `state=approved`, enrolled human `user.id`, exact approval comment, pinned production ID in `environments[].id`               |
| Conflicting history | Any production rejection, conflicting approval comment, or ambiguous identity refuses admission                                                              |
| Attempt-one jobs    | Exactly one `Prepare request` and one `Record approval`, both completed successfully                                                                         |
| Environment         | Enrolled numeric ID and name exactly `production`                                                                                                            |

Read `/actions/runs/<id>`, `/actions/runs/<id>/approvals`, and
`/actions/runs/<id>/attempts/1/jobs`. Deployment records and workflow-written
approval files never substitute for review history. The runbook owns fixture
capture and API inspection procedures; sanitized observations belong in research
evidence.

Before enabling pull, record live or controlled proof that review history with
more than one page or more than one relevant review refuses admission. Also
prove a complete cycle within the anonymous 60-request hourly budget per IP,
including discovery, post-cache revalidation, and observer use. Missing fields,
ambiguous history, or insufficient budget stop admission. The
[evidence contract](evidence.md#anonymous-approval-and-freshness) owns timing
and deferral; keep attended laptop deployment available without silently adding
a credential or weaker verification.

## Approval API proof

The [2026-11-01 live evidence](../../research/approval-probe.md) records
accepted self-approval and refusal of rejection, wrong comments, missing
approval, reruns, and cancellation after cache expiry. The successful
approval-only check used four requests. It did not measure the full deployment
cycle or prove multi-page history or distinct dispatcher/approver identities.
The owner chose the restricted anonymous transport on that evidence;
implementation and runtime acceptance remain separate.

## Approval and independent inspection

Use `.github/workflows/production-approve.yml`, dispatched on `main`.
`Prepare request` constructs the closed request and publishes
`deploy-<run-id>-1.json` on the target GitHub Release. `Record approval` depends
on preparation and uses `environment: production`; after approval it validates
the comment and reports “Request approved; host execution pending.”

Only preparation receives `contents: write`. The gated job receives repository
and Actions reads. Neither receives production secrets, deployment OIDC
authority, SSH identities, or a host connection. Preserve full-SHA action pins
and cache trust boundaries.

| Control      | Required setting                                                                                                                                                                                      |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Main         | [ADR 0024](../../adr/0024-delivery-gates.md#decision): required `ci`, signed commits, pull requests, owner local-gate admin bypass only; no force push or deletion; zero mandatory human PR approvals |
| Release tags | Admin-only `v*` creation; updates and deletion prohibited                                                                                                                                             |
| Environment  | `production`, enrolled numeric ID; selected branch `main` only, no tag rule; no secrets                                                                                                               |
| Reviewers    | `dannyota` only until the [enrollment gate](#approval-enrollment) passes; self-review allowed; administrator bypass disabled                                                                          |
| Concurrency  | `production-approval`, `cancel-in-progress: false`; no FIFO assumption                                                                                                                                |
| Reruns       | Refused; use a new dispatch                                                                                                                                                                           |

Before approving, the maintainer uses the independently installed read-only
checker: `aboutme-deploy-check inspect --tag <tag> --run <run-id> --attempt 1`.
The exact approval comment is
`approve production <mode> <tag> run <run-id> attempt 1 sha256:<request-hash>`.

The checker fetches the uniquely named asset, validates canonical bytes and
bindings, computes the hash, and displays the complete escaped JSON, release
source commit, coordinator commit, every digest, mode, expected state, expiry,
and comment. A mismatch with the human-selected tag refuses comment output.

Install the checker from separately reviewed source authenticated with the
enrolled source-signing key. Never execute checker code supplied by preparation
or its artifacts. Logs and summaries are advisory. Asset replacement before
inspection becomes visible; replacement after approval changes the approved
hash.

The continuous deployment runbook will own checker installation, API fixture
procedures, enrollment steps, operational commands, and the supporting reference
catalog. Deployment and quota documents link to their owning contracts.

### Approval enrollment

Initially enroll only `dannyota` in the production environment's reviewer list
and the host's maintainer-ID allowlist. Before adding `minhmq` to either list,
verify live approval by a maintainer distinct from the dispatcher in a separate
proof, including numeric actor and reviewer IDs against the host acceptance
rules. Record that evidence before expanding production enrollment. Either
maintainer may approve after that gate passes; one approval suffices.

## Release order and owner decisions

The owner accepted the evidence and transport. Ship the remaining changes in
this order, with independent review and exact-commit CI for each release:

1. **Retire the probe.** Disable the workflow and cancel/drain its runs. In a
   reviewed change remove `.github/workflows/approval-probe.yml`, its assertions
   in `scripts/test/workflow-contract.test.mjs` and
   `scripts/test/workflow-safety-test.sh`, `scripts/approval-probe.sh`,
   `scripts/approval-probe.json`, `scripts/test/approval-probe-test.sh`, their
   three `Makefile` lines, and `docs/runbooks/approval-probe.md`. Keep the
   research evidence and Git history. The checker is removed, not retained as
   the host reference: it permits multi-page history and does not implement the
   accepted cache or full host verification contract. After that change lands
   green and no pending run can recreate it, delete the `approval-probe`
   environment and confirm absence. Environment deletion is a separate primary
   operation.
2. **Release status and publication names.** Own `deploy/caddy/production/`,
   `deploy/vn/host/quadlet/`, `deploy/vn/tests/`,
   `.github/workflows/release-images.yml`, and its assertions in
   `scripts/test/workflow-contract.test.mjs`. Add serving/maintenance status and
   the [publication names](evidence.md); publish and prove a candidate for `F`.
3. **Durable state and admission.** Own `deploy/vn/pull/`,
   `deploy/vn/scripts/fence.sh`, and `deploy/vn/tests/`. Implement closed
   schemas, atomic state/journals, generation and revision checks, configuration
   barriers, and admission locks. Keep pull disabled.
4. **Execution and shared laptop protocol.** Own `deploy/vn/pull/`,
   `deploy/vn/host/`, `deploy/vn/scripts/`, `deploy/vn/tests/`, and
   `docs/runbooks/production.md`. Integrate lifecycle guards, attendance,
   cleanup, recovery, and laptop entry points; prove interruption safety in a
   disposable Ubuntu VM. Keep pull disabled and laptop deployment usable.
5. **Pinned Ansible.** Own `deploy/vn/ansible/`, `deploy/vn/host/install.sh`,
   `deploy/vn/pull/`, `deploy/vn/tests/`, and `.github/workflows/vn-infra.yml`.
   Pin toolchain hashes, converge unchanged enrolled configuration, prove no
   changes/restarts on repeat convergence, and verify host memory fit.
6. **Host verifier and independent checker.** Own `deploy/vn/pull/`,
   `deploy/vn/tests/`, `deploy/observer/internal/verify/`, and
   `docs/runbooks/continuous-deployment/`. Implement approval/freshness, release
   and image checks, request inspection, denial lists, and budget accounting.
   Use the accepted contract and evidence, not the retired probe as authority.
7. **Approval workflow and polling integration.** Own
   `.github/workflows/production-approve.yml`,
   `scripts/test/workflow-contract.test.mjs`,
   `scripts/test/workflow-safety-test.sh`, `deploy/vn/pull/`,
   `deploy/vn/host/etc/systemd/system/`, `deploy/vn/tests/`, and
   `docs/runbooks/continuous-deployment/`. Prove the full anonymous cycle and
   failure/deferral paths, including live or controlled refusal evidence for
   multi-page history and more than one relevant review. Keep production polling
   disabled until those proofs pass.
8. **Attended enrollment and first pull.** Own
   `docs/runbooks/continuous-deployment/` and private host enrollment state;
   separately reviewed ingress changes own `deploy/vn/prod/` and
   `deploy/vn/host/`. Require the preceding full-cycle and refusal evidence,
   prove remote/console access, and enroll controls, keys and actual `F`. Apply
   the [owner-only enrollment gate](#approval-enrollment), then enable polling
   and perform the first approved pull with attended proofs. Preserve parallel
   laptop deployment.
9. **Transfer further configuration separately.** Each responsibility owns its
   exact files in `deploy/vn/ansible/`, `deploy/vn/host/`, and
   `deploy/vn/tests/`. Remove its former writer together with
   unchanged-convergence proof.

Hosted CI must cover schema rejection, approval substitution,
publication/evidence profiles, reruns, replay, downgrade refusal, challenge
pending/delivery/expiry/reconnect, wrong-key receipts, egress/session erasure,
configuration interruption/drift, bound recovery repair, baseline/revision crash
boundaries, locks, activation, recovery, and faults at every
intent/result/commit boundary. Run Ansible lint, syntax, and
unchanged-convergence checks without production access. A disposable Ubuntu VM
must prove systemd cleanup, successful exit preserving service, reboot guards,
and blocked admission during cleanup; container tests alone are insufficient.

Owner decisions remain: approve concrete VPN ingress rules after remote-access
and console proofs, and approve the actual enrolled release identity after
evidence exists. If the restricted anonymous contract cannot be met, keep pull
disabled and return any transport change to the owner. Ansible adoption, solo
approval, parallel laptop deployment, attended proofs, and no automatic rollback
are supplied requirements.
