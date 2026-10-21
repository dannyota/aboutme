# CrowdSec privacy notice

Use this procedure for the first, never-run CrowdSec notice campaign on the
Vietnam host. The [notice design](../design/crowdsec-privacy-notification.md)
owns copy, delivery, counts, and activation; the
[initialization contract](../design/crowdsec-privacy-notice-initialization.md)
owns initial reconciliation and its failure boundaries. This procedure does not
reconcile a restored or existing campaign.

## Release prerequisites

Require independent review, exact-commit green main CI, published compatible
images, and the reviewed host bundle containing `reconcile-initial` and the
bootstrap helper fix. Follow the
[production release gate](production.md#deploy). Use one published notice tag
throughout. Set both public languages and both compiled manifests to the actual
publication date before publishing the release. Verify their shared digest; do
not change the terms date.

Keep expanded target-path collection and PostgreSQL attack ingestion disabled
through notice activation. Never lower the release floor. Check secrets by name
only. Never export recipients, inspect secret values, edit receipts, delete
notice state, or remove authority markers by hand.

Commands marked host run through the owner's existing SSH access with its
verified host key. `<bundle>` is the copied, reviewed `deploy/vn` directory;
`<notice-tag>` is the eligible published tag. These placeholders select the
ordinary install/deploy inputs, not notice command overrides.

The incoming bundle, its files, and all ancestor directories must be root-owned
and not group- or world-writable.

## First adoption, installation, and deployment

On an existing pre-notice application host, invoke adoption directly from the
incoming bundle before ordinary installation. Its compiled legacy release and
helper pins must match; unknown or already adopted hosts refuse.

```sh
# Host, incoming reviewed bundle:
sudo bash <bundle>/host/crowdsec-privacy-notice-host.sh adopt
sudo bash <bundle>/host/install.sh <bundle>
```

Adoption establishes the canonical helper before replacing guards. Exit 0 is
silent; refusal is `invalid_state`, exit 1. Stop on refusal. Interrupted
adoption may resume only its recorded operation from the same reviewed bundle.
Do not install first, downgrade helpers, or change pins to force eligibility.

A new app-absent host uses ordinary installation, which runs `initialize`; skip
`adopt` there. PostgreSQL-only installation does not authorize application
reconciliation. Both bootstrap paths leave reconciliation required and receipts
absent. Installation preserves that state.

Deploy the compatible release using the existing deployer:

```sh
# Operator computer, clean current reviewed main:
DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh <notice-tag>
```

For a genuinely new application database, use the production runbook's
`--first-deploy` procedure. Do not use that flag to repair an existing database.
Verify both languages at `https://aboutme.vn/privacy` show the approved copy and
actual publication date before any send authority. Deployment can serve the new
notice while the reconciliation marker keeps notice mail disabled.

## Initial reconciliation and capture

On the host:

```sh
sudo /usr/local/lib/aboutme/crowdsec-privacy-notice.sh reconcile-initial
sudo /usr/local/lib/aboutme/crowdsec-privacy-notice.sh prepare
```

Run prepare only after reconcile-initial exits 0 with `initial_reconciled` and
all three zero counts. Its full closed output is defined by the initialization
contract. This action commits the empty-state proof, durably clears only
reconciliation, and leaves maintenance active with writers stopped.

Prepare returns `pending`, exit 2, while capturing. Invoke the same command
again until it returns `sealed`, exit 0. Do not restart the app between batches.
The wrapper keeps the capture marker until exact membership is sealed, including
for zero accounts. Any exit 1 stops the procedure; never change the cutoff or
delete a partial campaign.

After sealing, use the restart sequence below. Accounts created after sealing
see the new notice and remain outside the captured cohort.

## Restart the pinned application

Prepare and activate quiesce the host; their success does not restart serving or
the mail worker. Run this after sealed prepare, and again after successful
activation. Run it only with no retained operation or capture marker. Keep
changed-ingestion controls disabled until their separate cutover gate passes.

On the host, run the following as one root shell. It uses existing deployment
steps and releases ownership only after each step succeeds. A failed step
retains ownership for [failed-deploy recovery](production.md#failed-deploy).

```sh
sudo bash <<'SH'
set -euo pipefail
lib=/usr/local/lib/aboutme
tag=$("$lib/deploy-host.sh" - - release-json | jq -er .release_tag)
operation=$("$lib/fence.sh" lock deploy "$tag")
exec 9>>/run/aboutme/workload.lock
flock -x -w 300 9
"$lib/deploy-host.sh" "$operation" "$tag" app-up
"$lib/deploy-host.sh" "$operation" "$tag" maintenance-down
"$lib/deploy-host.sh" "$operation" "$tag" timers-start
exec 9>&-
"$lib/fence.sh" release "$operation"
SH
```

Confirm public `/healthz` and `/readyz` return 200 and both privacy languages
still show the published date. Do not put the private operation ID in evidence.

## Authorize, enqueue, and activate

After the sealed campaign's app restart, raise the release floor to the capable
notice release before any enqueue. This existing deployment action checks the
running candidate; it does not activate the notice or enable changed ingestion.

```sh
# Operator computer, clean current reviewed main:
DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh --activate <notice-tag>
```

Require success before issuing send authority and enqueueing on the host:

```sh
sudo /usr/local/lib/aboutme/crowdsec-privacy-notice.sh authorize-send
sudo /usr/local/lib/aboutme/crowdsec-privacy-notice.sh enqueue
sudo /usr/local/lib/aboutme/crowdsec-privacy-notice.sh status
```

Authorize-send succeeds silently. Enqueue/status return the existing campaign
aggregates. `pending` and `queue_full` use exit 2; allow the worker to progress,
then explicitly enqueue another batch or read status. Do not automate a new
timer or treat an empty outbox as completion. Any `blocked` or `invalid_state`
requires the design's reviewed recovery, not another initial reconciliation.

Once status is `ready`, exit 0, check Bizfly failures through the
[email runbook](email.md#feedback), keeping only aggregate evidence. Then:

```sh
sudo /usr/local/lib/aboutme/crowdsec-privacy-notice.sh activate
```

Require `activated`, exit 0, and accepted plus deleted equal expected. The
wrapper revokes send authority and writes activation authority. Perform the
restart sequence above. Only then proceed with the
[CrowdSec storage switch](../design/crowdsec-http-privacy.md#migration-loss-and-release-order),
which still requires retention, fresh provisioning, and consumer proofs. Notice
activation alone does not start changed ingestion.

## Refusals and interrupted work

`invalid_state` has empty stdout, one fixed stderr line, and exit 1. Stop; do
not interpret a partial or lost SSH reply as success. Save only release digests,
publication date, campaign/content digest, cutoff, aggregate counts,
acceptance/activation times, and final fence result.

For initial reconciliation interrupted under retained cutover ownership, keep
maintenance active. Prove the old wrapper, child container/database backends,
descendants, mail sends, and systemd jobs have stopped. Under the existing
workload lock, inspect the exact cutover owner and `quiesce` mutation. The
[existing fence recovery](production.md#passkey-release-fence) uses
`fence.sh recover-cutover <operation-id> quiesce <reason>` followed by exact
`fence.sh release <operation-id>`; if the mutation was already cleared, only the
verified owner's release remains. Keep IDs private and never use generic clear
to bypass a mutation. Do not resume the app during this recovery.

Then explicitly rerun `reconcile-initial`. It repeats the full database proof,
even if the host marker is absent after a crash. If ownership is already absent,
invoke that same action directly. Do not replay saved success output.

An existing campaign, any notice job, a restore/epoch mismatch, missing initial
anchor, changed release, unknown owner, or uncertain quiescence is outside this
procedure. Keep notices and changed ingestion disabled and obtain separately
reviewed recovery. The sealed-campaign recovery helper cannot reconcile a
first-run database; initial reconciliation cannot recover a sealed campaign.
