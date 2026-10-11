# Delivery and releases

How work is reviewed and shipped. The manager, devops, and reviewer read this file.

## Delivery and review

[ADR 0024](../docs/adr/0024-delivery-gates.md) governs:

1. **One author per task.** Write the failing test first, make the smallest correct change, and use CI for the affected checks. A local red test is not required when it would violate [resource rules](resources.md); record that it was not run. Adversarial cases (write safety, races, bounds, hostile input, authz, CSRF) are the author's job.
2. **One fresh review per plan or release,** by the reviewer role, before push. Local-only and test-only changes skip it. Findings go back to the author; the reviewer confirms the fix.
3. **Copilot reviews every pull request** against `main` (the `copilot-review` ruleset), from another model family than the workers. Each finding is fixed on the branch or answered in its thread with the reason it does not apply before the merge. Copilot cannot approve and does not reply; a push after fixes starts its next review.

## Releases

The manager releases; devops owns the scripts and infrastructure. Details live in [the production runbook](../docs/runbooks/production.md).

1. Commit each feature as its own exact file set (see [Git](../AGENTS.md#git)).
2. A renderer change moves pixel baselines. Generate them in hosted CI from the exact candidate, inspect the artifacts, and commit the changed images. If no hosted generation path exists, add one instead of running an uncapped local build. Every release commit must carry its own baselines.
3. Order commits so each release commit holds only that release. Hold commits for a later release (for example routes for pages not yet built) above it.
4. Merge the reviewed pull request to `main` locally (see [Git](../AGENTS.md#how-we-work)) and push it alone, wait for green CI on that commit, tag `vX.Y.Z`, wait for the release-images workflow, then run `DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh vX.Y.Z` from the current reviewed `main` checkout. `DEPLOY_HOST` is required; `DEPLOY_SITE` defaults to `https://aboutme.vn` and accepts an HTTPS origin with no path. `SSH_PORT` defaults to 22922. Run a reviewed `tofu plan` in `deploy/vn/prod` only when infrastructure changes.
5. Verify in production, then report.

A resume schema version bump ships with its renderer and editor support in one release, so no one can save a field the page does not show. Rolling back past a schema bump breaks documents saved at the new version.
