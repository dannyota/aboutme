# Runtime handoff after Phase 8

Phase 10 must wire the completed runtime before deploying it. This inventory was
checked against Phase 8 merge `58d8e62` on 2026-09-06. It records work to
complete in Phase 10, not evidence that infrastructure is ready.

The [cost recommendation](../../research/aws-cost/recommendation.md) owns
selected sizes, UAT lifetime, and spending assumptions. Existing infrastructure
defaults cannot override it. The remaining runtime checks below still apply to
the selected topology.

Task 10.18 is the first runtime dependency despite its numeric identifier. It
must replace or coordinate process-local publication fences, render jobs and
capabilities, SSE fanout, and limiters before dependent infrastructure tasks
wire a second replica or autoscaling.

## Application and private print

| Input or boundary     | Current source and required handoff                                                                                                                                                                                                                              |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Public render origin  | `apps/server/internal/config/config.go` reads `PUBLIC_RENDER_ORIGIN`, not `NUXT_RENDER_ORIGIN`. Wire the direct Nuxt origin for bounded `POST /internal-render/public`.                                                                                          |
| Build identity        | Go also reads `APP_BUILD_DIGEST` and `PUBLIC_RENDERER_BUILD_DIGEST`. Derive these from the tested release and renderer inputs.                                                                                                                                   |
| Private redemption    | `apps/server/internal/config/print.go` permits `PRINT_LISTEN_ADDR=127.0.0.1:8081` in staging and production. This listener serves `POST /internal-render/print/redeem`; the public API listener does not. Task 10.18 preserves this trust boundary across tasks. |
| Nuxt print caller     | `apps/web/nuxt.config.ts` declares `printOrigin`, set through `NUXT_PRINT_ORIGIN`. `server/utils/print/redemption.ts` has an exact origin allowlist. Its local default is not an AWS address.                                                                    |
| Web network isolation | D24 places Nuxt outside Go's host namespace. Its proposed Caddy bridge listener routes only `/api/v1/*` to Go's public listener. That does not provide a private redemption path.                                                                                |
| Provider login        | `PROVIDER_LOGIN_ENABLED=false` is the v1 default, but config currently validates provider credentials before reading that flag. Phase 10 must fix and test disabled-provider startup without adding unused credentials.                                          |
| MCP access            | Enable the shipped MCP surface for v1. The edge must preserve Bearer authorization and protocol discovery while enforcing the resolved UAT access policy.                                                                                                        |

Task 10.18 chooses the replica-safe render and redemption contract. Tasks 10.5
and 10.7 then wire the private redemption path together and update its exact
origin allowlist and topology tests. Keep Nuxt outside the trusted Go loopback
namespace. Permit only the private redemption operation on the selected internal
route. External `/print/**` and `/internal-render/**` remain denied, and the
one-use capability remains mandatory. Do not expose the private listener or make
the origin allowlist accept arbitrary URLs.

`NUXT_INTERNAL_API_BASE` is an older proposed deployment input; the current Nuxt
runtime config does not declare it. Inspect the actual server-side callers
before wiring a new variable. The direct frozen public render and private print
redemption paths are separate contracts.

## ARM64 and resource evidence

`deploy/server.Dockerfile` currently links Chromium from
`/ms-playwright/chromium-1234/chrome-linux64`. Task 10.8 must resolve the
architecture-specific path and base digest for native ARM64. Merely changing the
runner label does not prove the browser executable works. Keep the Chromium
sandbox enabled and preserve the pinned fonts and rendering inputs.

The [export baseline](../../runbooks/exports.md#local-baseline-2026-09-06)
measured Go's render queue and Chromium at 309.5 MiB peak under a 512 MiB,
half-CPU cgroup. Nuxt was outside that measurement. The
[SSE baseline](../../runbooks/realtime.md#local-connection-measurement) measured
2,000 streams with clients and server in one test process. Neither result proves
the combined application at hosted load. Phase 10 must measure each server, web,
and Caddy task, host headroom, fleet limits, pgx connection budgets, and the
selected database together.

Task 10.8 first proves ARM64 image and render compatibility without AWS
credentials. Hosted Phase 10 then proves latency, mixed render/media/SSE load,
queue saturation, and cleanup within the unchanged
[numeric budgets](../../design/budgets.md). Failure requires a tested correction
or a reviewed design change before promotion.

## Jobs, email, and retention

Task 10.10 owns the seven schedules in its
[schedule contract](infrastructure/task-10-jobs-module.md#schedule-contract).
The restore job runs nightly and creates a real temporary RDS instance. Price
its storage and runtime separately, including the four-hour failure bound and
stale-target adjudication. Deletion and idempotency jobs run hourly; missed
heartbeats and overdue deletions must alarm.

Task 10.3's former seven-day UAT backup default conflicted with the design's
30-day retention and is corrected to 30 days. The cost model uses that retention
in both environments. Disposable synthetic UAT teardown remains a separate,
explicit data-loss operation; retained backups continue to incur charges until
deleted or expired.

The selected UAT teardown model leaves no final/manual snapshot and deletes
automated backups. Task 10.3 must pin `delete_automated_backups = true` for UAT;
Task 10.15 verifies that no owned backup remains. Any retained exception needs a
priced lifetime before cleanup is called complete.

The [email runbook](../../runbooks/email.md) is the owner's existing SES
handoff. Keep `aboutme-email`, its identity, feedback queue, alarms, and DNS in
a persistent shared ownership scope outside disposable UAT. Adoption as a stack
unit leaves CloudFormation as sole owner of its children. Individual import
requires the documented retention and ownership transfer first.

Phase 10 still needs runtime SES IAM, verified UAT recipients, evidence of
current sandbox limits, and a decision and implementation for feedback
processing. Queue delivery alone is not application processing. Keep those
inputs separate from simulator delivery proof. Preserve Google Workspace DNS and
the existing mail resources during UAT cleanup.

## Dispatch and activation checks

- Task 10.18 completes its design, bounded implementation-task split, runtime
  implementation, and local proof before tasks 10.2, 10.5–10.7, 10.9–10.12,
  10.14, or 10.15 dispatch. It does not treat two uncoordinated processes as
  scaling evidence.
- Tasks 10.5, 10.7, and 10.8 consume the exact runtime inputs above and replace
  conflicting baseline text before implementation.
- Task 10.1 resolves the new private repository's actual OIDC subject format and
  immutable owner/repository IDs before bootstrap activation. GitHub's
  [AWS OIDC guide](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws),
  checked 2026-09-06, warns that new repositories use ID-bearing subjects. Pin
  the exact private environment subject; public identities remain rejected.
- Task 10.6 resolves the UAT access mechanism across discovery, `/oauth/*`,
  `/authorize`, and `/mcp`. A blanket Basic Authorization gate cannot consume or
  remove the MCP Bearer header. Noindex does not provide access control.
- Task 10.13 checks workflow protection against the actual private repository
  plan. Public builds and smoke are free on standard runners under ADR 0033.
  Private validation/publication/deployment need sufficient remaining included
  minutes and metadata storage; no paid Actions usage is selected. Account
  allowances do not prove private deployment protection features.
- GitHub's
  [environment rules](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments),
  checked 2026-09-06, restrict required reviewers to public repositories on
  Free, Pro, and Team. Tasks 10.1 and 10.12 require that feature for the private
  infrastructure repository. Verify Enterprise Cloud eligibility and the
  repository namespace, or review a replacement approval contract before
  enabling AWS workflow credentials. No plan upgrade or weaker approval path is
  implied by the UAT authorization.
- Task 10.15 records the approved spending amount, expiry, cleanup inventory,
  regional/global resources, and remaining retained charges before activation.
  Region, hostname, and UAT DNS scope are already authorized by ADR 0031.
