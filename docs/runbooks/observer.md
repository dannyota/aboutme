# Deployment observer

The observer publishes `https://aboutme.vn/.well-known/deployment.json`: the
image digests production runs and whether each one's signed build record checks
out. The [design](../design/deployment-transparency/README.md) explains it; this
runbook holds the operator steps. Run every command from the release worktree
with the owner's `aws login` session, as for a [deploy](production.md#deploy).

## Set up

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

## Update

`observer.sh <tag>` verifies, copies, and points the function at the new digest
by itself; OpenTofu ignores the function's image afterward. Then set the printed
digest as `observer_image_digest` in `prod.tfvars` and its backup, so a function
OpenTofu ever recreates starts on the same image. Updates are separate from
application deploys and needed only when the observer changes.

## Failures

- **`aboutme-prod-observer-errors` or `-stopped` mails the owner.** Read the
  function's logs in `/aws/lambda/aboutme-prod-observer`. A failed platform read
  or an invalid document writes nothing, so the published document goes stale
  and the verify page says so.
- **The script stops before copying.** The tag is not on `main`, the tag has no
  single-platform observer image, or its provenance does not verify. Nothing
  changed in AWS.
- **ECR already holds the tag with another digest.** Tags are immutable; do not
  delete the tag to force it. Find out how a different digest got there first.

## Stop publishing

Set `observer_image_digest = ""` and apply: the function, schedule, both alarms,
and the CloudFront behavior go away, and the path falls back to the host, which
answers 404. To only pause, disable the `aboutme-prod-observer` schedule; the
last document then ages into stale, and `aboutme-prod-observer-stopped` mails
the owner within ten minutes.
