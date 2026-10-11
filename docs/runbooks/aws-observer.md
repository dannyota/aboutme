# AWS deployment observer reference

Status: **undeployed reference**. These procedures require a reviewed AWS ECS
rebuild serving `aboutme.vn` through CloudFront. They do not operate current
production, which uses the [Vietnam observer](observer.md) under
[ADR 0027](../adr/0027-vietnam-hosted-production.md) and
[ADR 0028](../adr/0028-deployment-transparency-observer.md).

The [observer design](../design/deployment-transparency/README.md) owns the AWS
mechanism and trust limits. Paths and commands below refer to `deploy/aws/`.

## AWS ECS setup

Run the AWS steps from the release worktree with the owner's `aws login`
session, following the
[AWS deployment design](../design/single-host-production.md#release-and-deploy).

1. With `observer_image_digest` empty in the ignored `prod.tfvars`, review
   `tofu plan` and apply. It creates the ECR repository, the bucket
   `aboutme-prod-transparency-<account id>`, the roles, and the CloudFront
   policies; no function yet.
2. Copy the observer image of a released tag into ECR:

   ```sh
   bash deploy/aws/scripts/observer.sh <tag>
   ```

   The script verifies the image's build provenance for that tag, copies it by
   digest, and, with no function yet, prints the `observer_image_digest` value
   to set.

3. Set `observer_image_digest` in `prod.tfvars`, review `tofu plan` (the
   function, schedule, and alarm, plus one in-place change to the CloudFront
   distribution), and apply.
4. Within two minutes,
   `curl -fsS https://aboutme.vn/.well-known/deployment.json` returns the
   document.

## AWS ECS update

`observer.sh <tag>` verifies, copies, and points the function at the new digest
by itself; OpenTofu ignores the function's image afterward. Then set the printed
digest as `observer_image_digest` in `prod.tfvars` and its backup, so a function
OpenTofu ever recreates starts on the same image. Updates are separate from
application deploys and needed only when the observer changes.

## AWS ECS production check

After a deploy or an observer update, the manager runs
`bash scripts/deployment-document-check.sh` from the release worktree. It checks
the headers, schema, and freshness of the apex and `www` documents, requires a
`verified` summary and the same images on both, then verifies the provenance and
SBOM attestation of every running image digest itself. It exits 0 only when
every check passes; during a rollout it fails until the rollout ends. Evidence
and a `summary.txt` land in `.dev/prod-checks/transparency/<UTC time>/`.

## AWS ECS failures

- **`aboutme-prod-observer-errors` or `-stopped` mails the owner.** Read the
  function's logs in `/aws/lambda/aboutme-prod-observer`. A failed platform read
  or an invalid document writes nothing, so the published document goes stale
  and the verify page says so.
- **The script stops before copying.** The tag is not on `main`, the tag has no
  single-platform observer image, or its provenance does not verify. Nothing
  changed in AWS.
- **ECR already holds the tag with another digest.** Tags are immutable; do not
  delete the tag to force it. Find out how a different digest got there first.

## AWS ECS stop publishing

Set `observer_image_digest = ""` and apply: the function, schedule, both alarms,
and the CloudFront behavior go away, and the path falls back to the host, which
answers 404. To only pause, disable the `aboutme-prod-observer` schedule; the
last document then ages into stale, and `aboutme-prod-observer-stopped` mails
the owner within ten minutes.
