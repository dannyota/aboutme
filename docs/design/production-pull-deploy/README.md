# Production pull deployment

Status: Proposed 2026-11-01; ADR 0026 amendment pending owner acceptance after
the approval API proof

The production host verifies an explicitly approved request, then runs
`ansible-pull` at its exact signed coordinator commit. GitHub builds and signs
public images. The host holds no GitHub credential. CI receives no production
credential or administrative network access.

The first version retains attended laptop proofs, manual vMonitor quota renewal,
and an empty automatic rollback allowlist. It adds no monitoring VM. ADRs 0024
and 0025 remain unchanged;
[ADR 0026](../../adr/0026-vietnam-hosted-production.md) needs the proposed
amendment. Pull removes CI’s administrative access at the cost of host
verification, polling delay, and durable recovery state. Laptop deployment
shares the same controls. A push service or unattended proof service requires a
separate decision.

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

The approval API proof below establishes that one approval is visible
anonymously, but does not prove all required approval semantics. Before building
deployment machinery, prove approval, self-approval, rejection, cancellation,
rerun rejection, pagination, and the following mapping using a harmless workflow
with production-equivalent protections and no production access or secrets.

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

Prove a complete cycle within the anonymous 60-request hourly budget per IP,
including discovery and observer use. Missing fields, ambiguous ordering, or
insufficient budget stop the automatic path before implementation. Return the
transport decision to the owner and retain attended laptop deployment; introduce
no credential or weaker verification silently.

## Approval API proof

The first milestone was met on 2026-11-01. A throwaway environment with required
reviewers and a push-triggered run was approved once. Unauthenticated
`GET https://api.github.com/repos/dannyota/aboutme/actions/runs/<run-id>/approvals`
returned state `approved`, the exact approval comment, the approving user's
login and numeric ID, and the environment name and numeric ID. An
unauthenticated GET of the run returned event, `run_attempt`, `head_sha`,
workflow path, actor, and `triggering_actor`.

The unauthenticated core rate limit is 60 requests per hour per IP, so the
host's polling budget must stay well under that limit. The throwaway environment
and branch were deleted afterwards. This observation does not replace the
remaining prerequisite cases or the complete-cycle budget proof above.

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

| Control      | Required setting                                                                                                    |
| ------------ | ------------------------------------------------------------------------------------------------------------------- |
| Main         | Required `ci`, signed commits, pull requests, no force push, deletion, or bypass; zero mandatory human PR approvals |
| Release tags | Admin-only `v*` creation; updates and deletion prohibited                                                           |
| Environment  | `production`, enrolled numeric ID; selected branch `main` only, no tag rule; no secrets                             |
| Reviewers    | `dannyota`, `minhmq`; one suffices; self-review allowed; administrator bypass disabled                              |
| Concurrency  | `production-approval`, `cancel-in-progress: false`; no FIFO assumption                                              |
| Reruns       | Refused; use a new dispatch                                                                                         |

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

## Release order and owner decisions

Ship separate reviewed changes: anonymous feasibility evidence; status-capable
release and publication job names; durable coordinator and shared laptop
protocol with pull disabled; pinned Ansible with unchanged convergence;
verification and approval workflow; then attended enrollment and first approved
pull. Transfer further configuration responsibilities separately.

Hosted CI must cover schema rejection, approval substitution,
publication/evidence profiles, reruns, replay, downgrade refusal, challenge
pending/delivery/expiry/reconnect, wrong-key receipts, egress/session erasure,
configuration interruption/drift, bound recovery repair, baseline/revision crash
boundaries, locks, activation, recovery, and faults at every
intent/result/commit boundary. Run Ansible lint, syntax, and
unchanged-convergence checks without production access. A disposable Ubuntu VM
must prove systemd cleanup, successful exit preserving service, reboot guards,
and blocked admission during cleanup; container tests alone are insufficient.

Owner decisions remain: accept the ADR amendment; approve concrete VPN ingress
rules after remote-access and console proofs; approve the actual enrolled
release identity after evidence exists; and choose another transport if
anonymous verification fails, retaining laptop deployment meanwhile. Ansible
adoption, solo approval, attended proofs, manual renewal, and no automatic
rollback are supplied requirements.
