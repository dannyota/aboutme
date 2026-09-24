# Delivery and releases

How work is reviewed and shipped. The manager, devops, and reviewer read this file.

## Delivery and review

[ADR 0024](../docs/adr/0024-single-pass-delivery-gates.md) governs:

1. **One author per task.** Write the failing test first, make the smallest correct change, and use CI for the affected checks. A local red test is not required when it would violate [resource rules](resources.md); record that it was not run. Adversarial cases (write safety, races, bounds, hostile input, authz, CSRF) are the author's job.
2. **One fresh review per plan or release,** by the reviewer role, before push. Local-only and test-only changes skip it. Findings go back to the author; the reviewer confirms the fix.

## Releases

The manager releases; devops owns the scripts and infrastructure. Details live in [the production runbook](../docs/runbooks/production.md).

1. Commit each feature as its own exact file set (see [Git](../AGENTS.md#git)).
2. A renderer change moves pixel baselines. Generate them in hosted CI from the exact candidate, inspect the artifacts, and commit the changed images. If no hosted generation path exists, add one instead of running an uncapped local build. Every release commit must carry its own baselines.
3. Order commits so each release commit holds only that release. Hold commits for a later release (for example routes for pages not yet built) above it.
4. Push the commit alone to `main`, wait for green CI, tag `vX.Y.Z`, wait for the release-images workflow, run `tofu plan` from the release worktree, then `deploy/aws/scripts/deploy.sh vX.Y.Z`.
5. Verify in production, then report.

A resume schema version bump ships with its renderer and editor support in one release, so no one can save a field the page does not show. Rolling back past a schema bump breaks documents saved at the new version.
