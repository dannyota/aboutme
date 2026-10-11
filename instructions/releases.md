# Delivery and releases

How work is reviewed and shipped. The primary, devops implementers, QA, and code reviewers read this file.

Delivery and independent review follow [workflow](workflow.md).

## Releases

The primary releases; devops implementers own the scripts and infrastructure. Details live in [the production runbook](../docs/runbooks/production.md).

1. Commit each feature as its own exact file set (see [Git](../AGENTS.md#git)).
2. A renderer change moves pixel baselines. Generate them in hosted CI from the exact candidate, inspect the artifacts, and commit the changed images. If no hosted generation path exists, add one instead of running an uncapped local build. Every release commit must carry its own baselines.
3. Order commits so each release commit holds only that release. Hold commits for a later release (for example routes for pages not yet built) above it.
4. Merge the reviewed pull request to `main` locally (see [Git](../AGENTS.md#how-we-work)) and push it alone, wait for green CI on that commit, tag `vX.Y.Z`, wait for the release-images workflow, then run `DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh vX.Y.Z` from a clean, current reviewed `main` checkout. Publication and deployment enforce the shared [release eligibility gate](../docs/runbooks/production.md#deploy); a branch or manual CI run never authorizes a release. `DEPLOY_HOST` is required; `DEPLOY_SITE` defaults to `https://aboutme.vn` and accepts an HTTPS origin with no path. `SSH_PORT` defaults to 22922. Run a reviewed `tofu plan` in `deploy/vn/prod` only when infrastructure changes. When `deploy/vn/host/` changed, first copy the main checkout's `deploy/vn` to the host and run `sudo bash host/install.sh <bundle>` there; it restarts PostgreSQL for a few seconds, and `deploy.sh` refuses while the host's scripts differ from the checkout.
5. Verify in production, then report.

A resume schema version bump ships with its renderer and editor support in one release, so no one can save a field the page does not show. Rolling back past a schema bump breaks documents saved at the new version.
