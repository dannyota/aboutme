# Delivery and releases

How work is reviewed and shipped. The primary, devops implementers, QA, and code reviewers read this file.

Delivery and independent review follow [workflow](workflow.md).

## Releases

The primary releases; devops implementers own the scripts and infrastructure. Details live in [the production runbook](../docs/runbooks/production.md).

The [pre-v1 compatibility policy](../AGENTS.md#how-we-work) permits breaking changes throughout application v0. Application SemVer is separate from resume document schema versions. The first application `v1.0.0` release candidate records supported API and document contracts and adds root `.v1-baseline`. Keep the marker absent in v0 releases. [ADR 0004](../docs/adr/0004-resume-document-contract.md) and [ADR 0005](../docs/adr/0005-database-migrations.md) own the resulting freeze. Deployments that change pre-v1 migration history reconcile existing database state explicitly.

1. Commit each feature as its own exact file set (see [Git](../AGENTS.md#git)).
2. A renderer change needs the display evidence and product assets for that exact candidate under [web rules](web.md). Until assertion cutover, keep the existing hosted comparisons and commit inspected baseline changes. After cutover, assertions replace pixel comparisons; product PNGs use their separate generator and reviewed manifest. Native proofs never replace exact-commit hosted release CI.
3. Order commits so each release commit holds only that release. Hold commits for a later release (for example routes for pages not yet built) above it.
4. Merge the reviewed pull request to `main` locally (see [Git](../AGENTS.md#how-we-work)) and push it alone, wait for green CI on that commit, tag `vX.Y.Z`, wait for the release-images workflow, then run `DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh vX.Y.Z` from a clean, current reviewed `main` checkout. Publication and deployment enforce the shared [release eligibility gate](../docs/runbooks/production.md#deploy); a branch or manual CI run never authorizes a release. A tag runs the release workflow from the tagged commit, so a tag on an off-main commit could carry a workflow without that gate; the `release-tags-create` ruleset closes this by letting only admins create `v*` tags, and `release-tags-immutable` blocks moving or deleting them. `DEPLOY_HOST` is required; `DEPLOY_SITE` defaults to `https://aboutme.vn` and accepts an HTTPS origin with no path. `SSH_PORT` defaults to 22922. Run a reviewed `tofu plan` in `deploy/vn/prod` only when infrastructure changes. When `deploy/vn/host/` changed, first copy the main checkout's `deploy/vn` to the host and run `sudo bash host/install.sh <bundle>` there; it restarts PostgreSQL for a few seconds, and `deploy.sh` refuses while the host's scripts differ from the checkout.
5. Verify in production, then report.

When a change touches release images or their dependencies, run `gh workflow run release-check.yml --ref <branch>` at the final branch head and require the server, web, and Caddy builds to pass before tagging; an observer-only failure follows the observer rule below.

If `release-images.yml` fails after a version tag is pushed, keep that tag, fix forward, and release the next patch version.

Release eligibility and signed provenance bind to commit IDs. Equal source trees do not transfer that evidence to rewritten commits; those commits need their own CI and release attestations.

`release-check.yml` supports manual checks on a branch with only `contents: read`. It builds, smoke-tests, and scans server, web, Caddy, and observer on native arm64 and amd64 runners through the shared `release-image-check` action. It never publishes or signs. `release-images.yml` runs only for version-tag pushes and keeps provenance and SBOM signing in that workflow to preserve verifier identities. Publication runs for the same tag share a concurrency group without cancelling an active run. Eligibility has a 5-minute timeout; app builds have 40 minutes; observer builds have 20 minutes; image indexes and release publication each have 10 minutes. Manual builds have 40 minutes. App build or scan failure blocks app indexes and the GitHub Release. Observer-only failure does not block app images or their release; a failed observer contributes no release assets.

A resume schema version bump ships with its renderer and editor support in one release, so no one can save a field the page does not show. Rolling back past a schema bump breaks documents saved at the new version.
