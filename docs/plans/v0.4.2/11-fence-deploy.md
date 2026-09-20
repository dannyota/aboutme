# Fence-aware deployment brief

Role: devops. Model: `gpt-5.6-terra`.

## Objective and authority

Make normal deploy, rollback, and failed-deploy restoration enforce the durable
minimum release before any image starts. Document the accepted flag-off, fence,
and activation sequence. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`,
`docs/design/passkey-release-fence.md`, `docs/design/deployment.md`, ADR 0048,
the production runbook, and the verified infrastructure report before editing.
Start only after the deployment alarm-suppression maintenance commit is
integrated into the v0.4.2 base. Read its deploy-script tests and preserve its
notification pause, restore, and cleanup contract.

## Owned paths

- Modify `deploy/aws/scripts/deploy.sh`.
- Modify `deploy/aws/scripts/deploy_test.sh`.
- Modify `deploy/aws/scripts/testdata/respond`.
- Modify `docs/runbooks/production.md`.
- Modify `.github/workflows/ci.yml` only for the hosted passkey browser proof.
- Modify `scripts/test/workflow-safety-test.sh` only for that job's contract.

Do not edit Terraform, application code, another workflow, secrets, or design
sources. Do not run AWS, OpenTofu plan or apply, production commands, local
tests, builds, lint, installs, browsers, database writes, or development stacks.

## Required behavior

- Run the current `main` checkout's fence-aware deploy script with the target
  tag. Never use the target tag's older script.
- Read the fence consistently before task registration and recheck it before
  every normal, rollback, and restoration service start named by the contract.
- Assume the operator role, strongly read the fence, then assume the deploy role
  through the accepted refreshable profile. Acquire the nonexpiring DynamoDB
  operation lock before deploy, rollback, restoration, fence raise, or
  enrollment activation. Resolve an uncertain acquisition with one strong read
  for the exact operation ID. Continue only when that read proves ownership;
  otherwise stop without an AWS mutation. No retry may replace another owner.
  The script never expires or steals a lock.
- A missing fence item is zero only while the running app task definition proves
  passkey enrollment false. Missing or malformed state otherwise blocks every
  deployment mutation.
- Reject a lower target before any AWS mutation. Accept the equal capable
  release without lowering or rewriting the fence.
- Assume the dedicated deploy role only in the accepted order. An old script
  using the operator identity cannot mutate ECS or schedules.
- Raise the fence with the exact accepted conditional update only after the
  flag-off release is healthy and while holding the operation lock. A failed
  condition or unknown result stops.
- If a raise succeeds but later flag enablement or redeploy fails, preserve the
  higher fence. The running flag stays off, or restoration uses the capable
  flag-off revision.
- Record the exact tag and numeric release in each task definition. Before task
  registration and every service, one-shot, schedule, alarm, or EventBridge
  mutation, conditionally prove the exact operation ID and release floor.
- Preserve maintenance mode, snapshot, migration, health, schedule, Cloudflare,
  secret-metadata checks, and every deployment alarm and task-stopped email-rule
  suppression cleanup path.
- Every normal exit restores alarm actions and the task-stopped rule before it
  conditionally releases the exact owned lock. A crash leaves the lock closed;
  the runbook owns the privileged manual-clear proof and reason record.
- Document the privileged administration bypass, forward-fix rule, flag-off
  proof, fence raise, flag enablement, synthetic account proof, and failure
  recovery without printing secrets or fence internals that the contract marks
  private.
- Add a `passkey-browser-proof` job to `.github/workflows/ci.yml`. Give it a
  fixed 45-minute timeout. Use the pinned repository toolchain and repository
  lifecycle targets to start `make dev-https`, build the pinned browser image,
  and execute `make dev-https-passkey-check` on the exact candidate. Upload only
  bounded, secret-free evidence from fictional accounts at
  `.dev/native-https/evidence/passkey-*`. Use `if: always()` steps to stop the
  HTTPS stack and its runner-local database on success, failure, and
  cancellation.
- Add workflow-safety assertions for the named job, exact repository targets,
  fixed timeout, bounded artifact path, lack of production secrets, and
  unconditional stack and database cleanup. A compile or Playwright list result
  does not satisfy the hosted proof.
- Document the two manager-owned production browser proofs from the release
  plan. GitHub CI cannot observe the deployed fence, enrollment flag, live
  identity providers, or production origin. Each proof uses only the named
  fictional account and the exact bounded command from the plan, starts no
  product stack, and removes the proof's factors, recovery plaintext, and live
  sessions on every exit.

## Test-first cycle and checks

Start with `git status --short`. Add failing shell-stub tests for missing fence,
lower target, equal target, missing and malformed items, lock contention,
uncertain acquisition resolved as exact-owner success, uncertain acquisition
resolved as absent or foreign ownership, conditional raise race, exact-owner
release, crash-closed state, normal deploy, rollback, pre-migration restoration,
post-migration failure, raise-before-flag order, and notification restoration
before release on each new fence exit path. Add the failing workflow-safety
contract before the hosted job. Report these checks as unrun pending GitHub CI
on the exact candidate:

```bash
bash -n deploy/aws/scripts/deploy.sh deploy/aws/scripts/deploy_test.sh
bash deploy/aws/scripts/deploy_test.sh
node_modules/.bin/prettier --check docs/runbooks/production.md
npx markdownlint-cli2 docs/runbooks/production.md
```

The exact CI run must also show the docs job, workflow-safety test, and
`passkey-browser-proof` job green. Do not repeat any hosted check locally.

Definition of done: every supported image-start path enforces the same durable
minimum, the runbook gives one safe release and recovery order, and the hosted
job executes the synthetic passkey proof with unconditional cleanup. Report
exact files and hunks, checks and results or awaiting CI, skipped checks with
command and reason, stub and workflow coverage, hosted artifact path or `none`,
and open items. Do not perform Git operations or external writes. Use short
plain text with no em dash.
