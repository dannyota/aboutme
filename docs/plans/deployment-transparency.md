# Deployment transparency (0.6.x)

Status: planned; waiting for owner approvals A1 to A5 in [the design](../design/deployment-transparency/README.md#owner-approval) and acceptance of [ADR 0057](../adr/0057-deployment-transparency-observer.md). Three small releases, one feature each, in order. The manager assigns version numbers after the [link previews](link-previews.md) releases (proposed 0.6.3, 0.6.4, 0.6.5). Briefs repeat the writing rules: code, comments, tests, and living docs cite the design pages or ADR 0057, never this plan or its release names.

|Release|Outcome|Risk|
|-|-|-|
|Release evidence|Signed SPDX SBOM per image; GitHub Release per tag with SBOMs and digests; deploy checks provenance before use|Medium: workflow permissions|
|Observer|Observer image, Lambda, bucket, edge path, alarm; `/.well-known/deployment.json` live|High: IAM, production infrastructure, sanitizing|
|Verify page|`/verify`, footer link, public-root `verify`|Medium: new public route and reserved slug|

## Before any code

1. Owner approves A1 to A5 and accepts ADR 0057; the manager updates the ADR status line and `docs/design/decisions.md`.
2. Owner settles the Kubernetes and Podman conflict for the Vietnam host (design README, "Kubernetes"). It blocks only the Vietnam observer, not these releases.

## Release evidence

|Role|Files|Work|
|-|-|-|
|devops|`.github/workflows/release-images.yml`|After Push: `trivy image --format spdx-json` on the pushed digest; `actions/attest` (pinned by SHA) with `sbom-path`, `subject-name`, `subject-digest`, `push-to-registry: true`; upload SBOM and digest artifacts. New `release` job (needs `image`, only `contents: write`): `gh release create <tag> --verify-tag` with four SBOMs, `digests.txt`, fixed notes holding the verify command. Add `artifact-metadata: write` only if the action requires it.|
|devops|new `deploy/aws/scripts/provenance.sh` and `provenance_test.sh`; `deploy/aws/scripts/deploy.sh` (source and call only; it is at 690 lines); `deploy/aws/scripts/deploy_test.sh`|Before registering task definitions, `gh attestation verify` each digest with `--repo`, `--signer-workflow`, `--source-ref refs/tags/<tag>`, `--deny-self-hosted-runners`; stop on failure. Tests with a stubbed `gh`.|
|devops|`docs/runbooks/production.md`; `docs/design/single-host-production.md` ("Release and deploy")|State the SBOM, the release, and the provenance check.|
|reviewer|read-only|Adversarial pass on the workflow permission change and the deploy check.|

Done: green `main` CI; the tag's `release-images` run green; `gh attestation verify --predicate-type https://spdx.dev/Document/v2.3` passes for all three new digests; the GitHub Release holds the assets; a deploy of the tag passes the provenance check.

## Observer

The devops lead runs the three devops slices in order; each reviews before the next.

|Role|Files|Work|
|-|-|-|
|devops|new Go module `deploy/observer/`: `go.mod`, `go.sum`, `cmd/observer/`, `internal/platform/ecs/`, `internal/platform/kubernetes/`, `internal/verify/`, `internal/document/`, `internal/publish/`, `schema/deployment.v1.json`, `testdata/` (adapter fixtures with canaries, a recorded public bundle for `aboutme-server` v0.5.21, example documents for every page state), `Dockerfile`, `README.md`|Tests first: `TestDocumentLeaksNothing` exactly as in [document](../design/deployment-transparency/document.md#sanitizer); summary and release rules; mapping; verification policy (wrong identity, wrong subject name, self-hosted runner, missing bundle, rate-limited API falls back to the registry); `stale_after`; 64 KiB cap; schema validation before write. Modes: `lambda` (AWS) and `run --interval 60s` (Kubernetes). Non-root static image.|
|devops|`.github/workflows/ci.yml`; `.github/workflows/release-images.yml` (observer matrix entry, single platform, `--provenance=false`); root `Makefile` (`observer-test`); `.tool-versions` only if a new tool is pinned|CI runs observer tests, vet, golangci-lint, and govulncheck. Report every changed line of shared files.|
|devops|new `deploy/aws/modules/observer/`; `deploy/aws/modules/edge/` (S3 origin with origin access control, behavior for `/.well-known/deployment.json`, cache policy 0/30/30, response headers policy); `deploy/aws/modules/identity/main.tf` (deploy role: ECR push on one repository, `lambda:GetFunction` and `UpdateFunctionCode` on one function); `deploy/aws/prod/main.tf`, `variables.tf`, `outputs.tf`, `prod.tfvars.example`; new `deploy/aws/scripts/observer.sh` and `observer_test.sh`|Exactly the IAM, bucket policy, ECR policy, schedule, alarm, and headers in the [design](../design/deployment-transparency/README.md#access-on-aws). Function `ignore_changes` on the image, as task definitions do. `observer.sh <tag>`: verify provenance, copy by digest to ECR, fail unless digests match, update the function.|
|devops|`docs/runbooks/production.md`; `docs/design/cloudfront-edge.md` (cache table and the `/_nuxt/*`-only statements); `docs/design/single-host-production.md` (observer, alarm, cost)|Living docs describe the built state; answer the six facts to verify with evidence.|
|reviewer|read-only|Adversarial pass: IAM scope and denials, bucket policy, edge headers, sanitizer allowlist, verification policy, no secret in Lambda environment.|
|qa|new `scripts/deployment-document-check.sh`, beside `scripts/totp-production-proof.sh`|After apply: fetch the live document; validate against the schema; scan the bytes for the forbidden patterns; confirm headers (HSTS, `nosniff`, no `Server`); confirm `gh attestation verify` passes for each listed digest. Evidence under `.dev/prod-checks/transparency/`.|

Apply order: ECR repository first (targeted apply), `observer.sh <tag>` copies the image, then the full apply creates the function and schedule. Done: green CI; reviewed plan with no unexpected destroy; live document passes the qa check within two minutes of a deploy; a denied-call test shows the role cannot describe another cluster.

## Verify page

|Step|Role|Files|Work|
|-|-|-|-|
|1|designer|`DESIGN.md` (new "Verify page" section; `/verify` in "Language coverage")|Spec before any build: status marks inside the color rules, the chain at desktop and at 360 px, rollout rows, digest and copy treatment, command block, dark theme. Owner reviews the Vietnamese copy. Frontend starts only after this.|
|2|devops or manager|none|Read-only production check that no resume or tombstone holds slug `verify`; if one does, stop for the owner.|
|3|backend|`packages/publicroots/` (version 9 adding `verify`, Nuxt dispatch); regenerated `apps/server/internal/publicroots/generated.go`, `deploy/caddy/public-roots.generated.caddy`, `deploy/caddy/testdata/public-roots.generated.json`, route parity fixtures; `docs/design/product.md` (registry version)|Tests first: `verify` is not claimable; dispatch goes to Nuxt. Report every regenerated line.|
|4|frontend|new `apps/web/app/pages/verify.vue`, `apps/web/app/components/verify/`, `apps/web/app/composables/useDeploymentDocument.ts`, `apps/web/app/i18n/verify.ts`; `apps/web/app/pages/index.vue` (footer); `apps/web/app/components/app/AppShell.vue` (marketing CTA); `apps/web/app/composables/useLocale.ts` (`useRouteLocale()`); `docs/design/localization.md` (localized routes); web source manifest; new tests under `apps/web/test/verify/`|Tests first, reading `deploy/observer/testdata/` documents: state order, staleness from `Date` plus `Age` with a skewed browser clock, summary recomputed from components, unknown status and schema version, closed decoding, no request to another origin. Footer label "Kiểm chứng" / "Verify"; title "Kiểm chứng phiên bản đang chạy" / "Verify what's running".|
|5|qa|new `deploy/dev-https-browser/verify.spec.ts` and its shard registration|Route the document fixture for every state; 390 and 1280 px; light and dark; vi and en; copy buttons; no-JavaScript view; screenshots under `.dev/personas/verify/`.|
|6|designer|read-only|Finish review of the built page against the spec in both themes and widths.|
|7|reviewer|read-only|One final review of the release.|

Done: green CI including the new spec; after deploy, qa confirms the footer link, the live page state matches the live document, and the page's command verifies a live digest.
