# Deployment observer

The reporter publishes `https://aboutme.vn/.well-known/deployment.json`: the
running image digests and their signature check statuses. The
[design](../design/deployment-transparency/README.md) explains the evidence.
Production on the Vietnam host uses the host reporter below. The AWS observer
remains available for an ECS deployment.

## Vietnam host

Run `deploy/vn/host/install.sh <bundle-dir>` on the host with the reviewed
`deploy/vn` bundle. It installs the generator, creates the public directory,
and enables `aboutme-deployment-document.timer`. Rendering the Caddy Quadlet
units adds the read-only directory mount. Deploy the Caddy and web images that
support the host document before checking `/verify`.

The timer observes Podman every minute, including container restarts outside a
deploy. To refresh once and inspect service state on the host:

```sh
systemctl start aboutme-deployment-document.service
systemctl status aboutme-deployment-document.timer
journalctl -u aboutme-deployment-document.service --since '-10 minutes'
```

The generator writes `/var/lib/aboutme/deployment/deployment.json` atomically.
Caddy serves the exact well-known path with the
[document headers](../design/deployment-transparency/host.md#source-and-publication).
It does not serve the rest of the directory. The `www` host redirects to the
apex.

After installation and deploy, fetch the document and open `/verify` in both
languages:

```sh
curl -fsS -D /tmp/aboutme-deployment.headers \
  https://aboutme.vn/.well-known/deployment.json \
  | jq '{platform, observed_at, stale_after, summary, components}'
```

Expect `greennode` / `podman`, current timestamps, and the digests Podman
reports. A complete report has summary `unverified`, since the host generator
does not check attestations. The page must show the digests and the host trust
limit, with no AWS claim. Use the page's GitHub CLI commands to check each
image independently.

A generator failure leaves the last document to become stale and invokes
`aboutme-alert@`. Inspect the fixed error message and Podman state. A missing
required container produces `mismatch`. A missing document produces 404.
Disabling the timer leaves the document to become stale; removing the file
stops publication. Never substitute `release.json` for a failed Podman read.

## AWS ECS setup

Run the AWS steps from the release worktree with the owner's `aws login`
session, as for an [AWS deploy](production.md#deploy).

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
