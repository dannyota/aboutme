# Repository transfer to dntmakers

Accepted 2026-11-19 (owner): the owner moves `github.com/dannyota/aboutme` to
the organization `github.com/dntmakers/aboutme`. New release images use the
organization namespace. Deployed images and rollback targets keep their original
signers and require verifiable provenance.

## What the transfer moves

| Item                                        | After the transfer                                                                                                                                                          |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Issues, pull requests, stars, watchers      | Move ([transfer docs][transfer])                                                                                                                                            |
| Releases, Actions history, attestations     | Releases and Actions history moved. The API returned 404 for sampled pre-transfer attestations under both names and the repository ID.                                      |
| Secrets, webhooks, deploy keys              | Stay associated ([transfer docs][transfer])                                                                                                                                 |
| Rulesets, environments, Actions settings    | Not stated in the docs; checked after the transfer. The organization's default repository permissions apply ([transfer docs][transfer])                                     |
| Old web, Git and API URLs                   | Redirect: the API answers 301 to `/repositories/<id>/…`, which `gh` and `git` follow and plain `curl` does not. A new repository or fork at the old name ends the redirects |
| Container packages under `ghcr.io/dannyota` | Stay owned by `dannyota` and lose their repository link; the new repository's workflows lose write access ([packages][perm]). Anonymous pulls keep working                  |
| Repository ID 1318874703                    | Unchanged: every signature carries it as `SourceRepositoryIdentifier` (OID 1.3.6.1.4.1.57264.1.15). The owner ID is 244348267 in old signatures and 342323513 in new ones   |
| Existing signatures                         | Keep the `dannyota` signer identity and cannot be re-signed                                                                                                                 |
| Go module `danny.vn/aboutme`                | Unaffected: no import names GitHub, and `danny.vn` serves no `go-import` metadata                                                                                           |
| Packages created by the first new release   | `GITHUB_TOKEN` links them to the workflow repository; initial visibility can be private ([Container registry][pub]), so deployment requires anonymous pulls                 |

[transfer]:
  https://docs.github.com/en/repositories/creating-and-managing-repositories/transferring-a-repository
[perm]:
  https://docs.github.com/en/packages/learn-github-packages/about-permissions-for-github-packages
[pub]:
  https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry

## Decisions

1. **Release images move; tool images stay.**
   - Releases built after the transfer publish
     `ghcr.io/dntmakers/aboutme-{server,web,caddy,observer}`.
   - Earlier releases stay in `ghcr.io/dannyota`, where their signatures name
     them.
   - `ci-tools`, `vn-infra-debs` and `aboutme-browser-base` are public and
     pinned by digest. Each moves when it is next rebuilt.
2. **Trust is the repository ID, owner is per image.**
   - A signature is accepted only when its `SourceRepositoryIdentifier` is
     `1318874703`.
   - The owner in its signer identity must equal the owner in its source
     repository URI and in the image namespace, and that owner must be
     `dannyota` or `dntmakers`.
   - Its `SourceRepositoryOwnerIdentifier` must be that owner's account ID:
     244348267 for `dannyota`, 342323513 for `dntmakers`.
   - The ID pin makes both owners safe to accept permanently. No boundary tag
     and no tag freeze are needed.
3. **Host records check shape only, and accept both owners permanently.**
   - In `release.json`, `release.previous.json`, `maintenance.json` and stored
     operation states, server, web and caddy share one owner. The maintenance
     image may have either owner, because a rollback keeps the newer maintenance
     image.
   - Operation states are never deleted and are re-validated by the daily
     retention timer, so these checks never drop `dannyota`.
   - Image pruning matches both namespaces.
4. **The document follows the running images.**
   - The reporter keeps writing today's exact `source_repository` and provenance
     link strings. Only `components[].image` follows the running pin; a stopped
     component gets `ghcr.io/dannyota/aboutme-<name>`, which every observer
     accepts.
   - The observer carries each component's `image` from the observation into the
     document and takes `build` and `signer_workflow` from each verified
     certificate. It rebuilds `source_repository` and the commit, release, SBOM
     and provenance links from its build-time owner, the
     `github.repository_owner` that built it. The build refuses any other owner,
     and the observer refuses to start without one. It fetches attestations from
     `https://api.github.com/repositories/1318874703/attestations/<digest>` and
     falls back to GHCR if the API has no bundle or is rate limited. The
     provenance link names its build-time owner.
   - A provenance link for a pre-transfer digest returns 404 whichever owner the
     observer that wrote it has, because the GitHub API returns 404 for
     pre-transfer attestations. A pre-transfer digest runs only after a rollback
     to a pre-transfer release, whose own web then serves `/verify`, or briefly
     during a deploy from one. A later web therefore shows these links
     unchanged. That is accepted: `--bundle-from-oci` still verifies those
     images.
   - From step 1, the schema and the web decoder accept both owners in
     `source_repository`, `components[].image`, `signer_workflow` and every link
     pattern. A new web can then read the previous observer's document during a
     deploy, and any observer from step 1 on accepts a `dntmakers` image.
   - An observer older than step 1 rejects a `dntmakers` image. After a rollback
     to such a release, `deployment.json` stays stale while a newer `dntmakers`
     maintenance image runs. That is accepted.
   - Older open `/verify` tabs show "unavailable" for a `dntmakers` document
     instead of the "out of date" message. That is accepted before 1.0.
5. **Commands that start from a tag read the owner from the tagged commit.**
   - `deploy.sh`, `operations.sh` and `mcp-owner-workflow.sh` read the namespace
     from `scripts/lib/repository.sh` in the tag's commit, or `dannyota` when
     that file is absent. They parse its one owner line with a strict pattern
     and never source a file from an old commit. The signature check then binds
     the owner.
   - For `dannyota` images, the deploy and operations verifier and
     `deployment-document-check.sh` read provenance from GHCR using
     `gh attestation verify --bundle-from-oci`, and the verify page's copy-paste
     command adds the same flag. They use the GitHub API for `dntmakers` images.
     The scripts require the same signature, repository, owner, and image checks
     from both sources. OCI retrieval does not check who currently holds the
     repository name; the scripts' repository and owner ID pins enforce that
     identity. The page's command binds the repository by name only, as it does
     for `dntmakers` images. After a rollback to a pre-transfer release, that
     release's own web serves the page with its API-only command, which returns
     404; that is accepted on the same grounds as the provenance links.
     Verification fails if the CLI lacks the flag or the legacy image lacks a
     verifiable bundle.
   - `release-images.yml`'s eligibility step fails when
     `github.repository_owner` differs from that owner, so a tag pushed in the
     wrong window publishes nothing.
   - `security-scan.sh` reads each scanned release's `digests.txt`.
   - Host scripts never read `scripts/lib/repository.sh`.
6. **Workflows use the context.**
   - Image names come from `github.repository_owner`.
   - Trusted checkouts use `github.repository`.
   - Run guards compare `github.repository_id` with `1318874703`, so they work
     under both names and need no later change.
   - Provenance steps set `create-storage-record: false`: the pinned
     `actions/attest` otherwise tries organization storage records and warns
     without `artifact-metadata: write`.
7. **Links switch after the transfer.**
   - The links switch to `dntmakers` in the first release after the transfer:
     before it the new URL is 404, after it the old one redirects.
   - Where they are: the AGPL source link, navigation, landing and JSON-LD,
     `llms.txt`, the verify page, Telegram runbook links, the issue template,
     README, SECURITY, `instructions/verification.md`, the runbooks
     (`deploy.md`, `approval-probe.md`), the deployment transparency and
     production pull deployment designs, and the verify page's command owner
     when no document is available.
   - `CLAUDE.md`'s commands and notification filter change on transfer day.

## Changes by component

| Component                                                                                        | Change                                                                                                                                                                                                                      |
| ------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `release-images.yml`, `release-check.yml`                                                        | Decision 6; the release-notes verify command from `github.repository`                                                                                                                                                       |
| `approval-probe.yml`, `vmonitor-renew.yml`                                                       | Guards on `github.repository_id`                                                                                                                                                                                            |
| `deploy.sh`, `operations.sh`, `provenance.sh`                                                    | Owner from the tagged commit; `provenance.sh` adds the repository ID and owner-consistency checks to `gh attestation verify`'s JSON                                                                                         |
| `deploy-host.sh`, `operation-state.sh`, `operation-run.sh`, `crowdsec-privacy-notice*.sh`        | Decision 3, with a test of a rollback record: `dannyota` app images and a `dntmakers` maintenance image                                                                                                                     |
| `deployment-document.sh`, observer `verify`, `document`, `platform`, `leakscan`, `fetch`, schema | Decision 4; the leak scan allows both owners' prefixes; the registry fallback reads both namespaces                                                                                                                         |
| Web `deploymentDocument.ts`, `verifyView.ts`, `VerifyChain.vue`                                  | Decode decision 4; the copy-paste verify command takes each image's owner for `oci://`, `--repo` and `--signer-workflow`, and adds `--bundle-from-oci` for `dannyota`                                                       |
| `release-eligibility.sh`, `security-scan.sh`, `mcp-owner-workflow.sh`                            | Repository from `scripts/lib/repository.sh`, or as decision 5 says                                                                                                                                                          |
| `deployment-document-check.sh`                                                                   | Each image's owner for `oci://`, `--repo` and `--signer-workflow`, `--bundle-from-oci` for `dannyota`, and the repository ID from `gh`'s JSON                                                                               |
| `approval-probe.sh`                                                                              | Requests `https://api.github.com/repositories/1318874703/actions/runs/<id>` and drops the `full_name` pin; the repository ID is already pinned                                                                              |
| Tests                                                                                            | Every verifier accepts both owners under the repository ID, and rejects another ID and a signer owner that differs from the namespace. Host records reject mixed app owners; the observer reports them with `release: null` |

Unchanged:

- the recorded `dannyota` bundle `api-server-v0.5.21-provenance.json`;
- the hash-pinned legacy fixture `legacy-notice-adoption/deploy-host.sh.txt`;
- the legacy image digests in `crowdsec-privacy-notice-host.sh`;
- `docs/research/`;
- the owner's personal login (sample resume, approval reviewer);
- references to `dannyota/vngcloud`.

## Sequence

1. **Prepare.**
   - Land every change in the table except the links.
   - Release, install the host bundle, and deploy as usual. The deploy output
     must not say "observer image absent".
   - Release from `dannyota` as needed afterwards; no freeze.
2. **Transfer (owner).**
   1. Before:
      - main CI is green, and no tag build or deploy is running;
      - the `dntmakers` Actions policy allows this repository's actions
        (GitHub-owned, `golangci-lint-action`, `setup-opentofu`), with SHA
        pinning required and read-only default workflow permissions;
      - Packages allows members to create public packages.
   2. Transfer the repository. Switch `CLAUDE.md`'s commands the same day.
   3. Check what survived:
      - the 5 rulesets, the 3 environments with their secrets, variable and
        `approval-probe` reviewer;
      - the repository's Actions settings: the allowed-actions list, SHA pinning
        and the read-only default token;
      - the repository, workflow and environment IDs pinned in
        `scripts/approval-probe.json`;
      - private vulnerability reporting, vulnerability alerts, Dependabot,
        CodeQL and app installations;
      - the last release's attestations, listed under both names.
   4. Point the Mac and Ubuntu checkouts' `origin` at `dntmakers/aboutme`.
   5. From now on:
      - never re-run a release workflow run started before the transfer;
      - tag nothing until the owner switch in step 3.1 lands;
      - never create a repository or fork named `dannyota/aboutme`.
3. **First `dntmakers` release.**
   1. Set `scripts/lib/repository.sh` to `dntmakers`, then land the links. The
      switch may land alone when a hotfix cannot wait for the links.
   2. Tag. `release-images` creates the four packages. Check that they are
      public with the anonymous pull check, and linked to `dntmakers/aboutme`.
   3. Before deploying, run `provenance.sh` on the new server, web, caddy and
      observer digests. Save one new attestation from the repository-ID URL as a
      scratch bundle and run the observer's `verify` tests against it.
   4. Install the host bundle and deploy.
   5. Read back: `/verify` verified, `deployment.json` naming `dntmakers`, and
      the observer image deployed.
4. **Afterwards.**
   - Commit a recorded `dntmakers` bundle beside the old one.
   - Move the tool images when they are rebuilt.
   - Nothing narrows: the repository ID pin keeps `dannyota` safe to accept.

## Timing

The vMonitor renewal window runs from 2026-11-24T06:54:54Z to
2026-12-01T06:54:54Z ([production runbook](../runbooks/production.md#alarms)).
The ID guard makes `vmonitor-renew.yml` run under either name. Before
dispatching it after a transfer, check that its environment kept its secrets.

## Owner-only steps

- Set the `dntmakers` Actions and package policies, and transfer the repository.
- Make a new package public if the anonymous pull check fails.
- Optional: move the GitHub OAuth apps to the organization. Their client IDs and
  callbacks do not change, and the product does not need it.
