# Build evidence and verification

This page says what the release workflow signs, what "verified" means in the
[deployment document](document.md), who checks it, and how anyone can check it
without trusting aboutme.

## What the release workflow produces

`release-images.yml` builds `server`, `web`, `caddy`, and `observer` for a
version tag on a GitHub-hosted runner. For each image it already scans with
Trivy, pushes to `ghcr.io/<owner>/aboutme-<name>`, where `<owner>` is the
repository's owner, and runs `actions/attest-build-provenance`, which signs an
SLSA provenance v1 statement for the pushed digest with a short-lived Sigstore
certificate issued to the workflow's OIDC identity. The signature is recorded in
the public Sigstore transparency log (Rekor), stored in GitHub's attestation
store, and pushed to the registry beside the image.

A real statement for `aboutme-server` v0.5.21 names subject digest
`sha256:d1c33a0a…945c`, workflow ref `refs/tags/v0.5.21`, commit `d73f4043…`,
and run `actions/runs/36218940587/attempts/1`. The
[recorded bundle](../../../deploy/observer/testdata/verify/api-server-v0.5.21-provenance.json)
keeps its original `dannyota` signer.

### SBOM

For each image, after the push:

1. `trivy image --format spdx-json` on the pushed digest writes
   `aboutme-<name>.spdx.json`. Trivy is already installed and pinned for the
   scan, so this adds no new third-party action. It reads Go build information
   and the OS package database.
2. `actions/attest` (the action that `attest-sbom` now wraps) with
   `subject-name`, `subject-digest`, `sbom-path`, and `push-to-registry: true`
   signs an SPDX 2.3 attestation bound to the same digest. The job keeps its
   `id-token: write` and `attestations: write` permissions. It sets
   `create-storage-record: false`, so it needs no `artifact-metadata: write`;
   the action skips storage records for a user-owned repository anyway.
3. The SBOM and digest files are uploaded as job artifacts.

The observer publishes separate single-platform images: the plain version tag is
`linux/arm64`, which no deployment uses, and `<tag>-amd64` is `linux/amd64` for
the Vietnam host. Both build on native GitHub-hosted runners with the same
smoke, Trivy, provenance, and SBOM checks. An observer job failure does not
block the app images or their release.

**Owner approval (A3):** a final `release` job, which waits for every image job
and requires only the app image jobs to pass, runs with only `contents: write`,
downloads those artifacts, and runs `gh release create <tag> --verify-tag` with
the platform SBOMs, a `digests.txt` listing each image and digest, and fixed
notes holding the verify command below. The release gives the SBOM a permanent
public link; workflow artifacts expire. The release asset is a convenience copy:
the signed SBOM attestation is the evidence, and a changed asset would no longer
match it. Without a release, the signed SBOM remains available through the
GitHub attestations API.

### A separate cosign signature

A GitHub attestation is a Sigstore signature: a DSSE envelope signed with a
Fulcio certificate for the same workflow identity, logged in Rekor, and bound to
the digest. `cosign sign` would add a second signature from the same identity
over the same digest. It would matter only for a verifier that reads cosign's
tag-based signatures and not Sigstore bundles, such as an admission controller
on a future Kubernetes cluster. Revisit it then.

## What "verified" means

The [host reporter](host.md) supplies unchecked observations to an observer
container on the same host. The observer uses the verifier below. Independent
checks use the same GitHub and Sigstore commands.

For one running digest, the observer uses `sigstore-go` with the public-good
trust root (refreshed through TUF) and requires all of:

1. A bundle for the digest, fetched from the GitHub attestations API by
   repository ID
   (`https://api.github.com/repositories/1318874703/attestations/sha256:<hex>`)
   and filtered by predicate type. If the API returns no bundle or responds with
   403, 404, or 429, the observer looks for the bundle in both owners' registry
   namespaces. A namespace whose token the registry refuses holds no readable
   bundle.
2. A valid signature, a certificate chain to Fulcio, and a Rekor inclusion proof
   and signed timestamp inside the certificate's validity.
3. Issuer `https://token.actions.githubusercontent.com`.
4. Source repository identifier `1318874703` in the certificate extensions. The
   [repository transfer](../repository-transfer.md) moves the repository from
   `dannyota` to `dntmakers`, and the ID stays the same.
5. Certificate identity exactly matching
   `^https://github\.com/(dannyota|dntmakers)/aboutme/\.github/workflows/release-images\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`.
   The owner in it must equal the owner in the source repository URI and in the
   run URL. The source repository owner identifier must be that owner's account
   ID: 244348267 for `dannyota`, 342323513 for `dntmakers`. The runner
   environment must be `github-hosted`.
6. Statement subject digest equal to the running digest, and subject name equal
   to the running image's repository, under the signer's owner.
7. Predicate type `https://slsa.dev/provenance/v1` for the signature, and
   `https://spdx.dev/Document/v2.3` for the SBOM.

`version` comes from the certificate's source ref and `commit` from its source
repository digest. Both are set by Fulcio from GitHub's OIDC token, not from the
predicate the workflow wrote.

| Status      | Meaning                                                                  |
| ----------- | ------------------------------------------------------------------------ |
| `verified`  | Every check above passed                                                 |
| `not_found` | GitHub and the registry both answered, and neither holds a bundle        |
| `invalid`   | A bundle exists and a check failed                                       |
| `unchecked` | The observer could not finish (network, rate limit, trust root, timeout) |

A digest's result is cached in process memory for 24 hours, then checked again.
Digests are immutable, so the cache only saves API calls. An `unchecked` result
is not reused.

"Verified" therefore means: our observer, a program whose own source is public,
checked the public signature and found that GitHub built this exact digest from
this public commit. It does not mean aboutme vouches for anything the
[limits](README.md#what-this-proves) exclude.

## Verify it yourself

The page shows these commands with the live values filled in. Nothing in them
trusts aboutme: the digest comes from the document, and the check runs against
GitHub and Sigstore.

1. Read the running digests:

   ```sh
   curl -fsS https://aboutme.vn/.well-known/deployment.json \
     | jq -r '.components[] | .name as $n | .running_images[] | "\($n) \(.digest) \(.version)"'
   ```

2. Verify one digest's provenance with the GitHub CLI (signed in, since `gh`
   needs a token). `<owner>` is the owner in that component's `image` in the
   document, `dannyota` or `dntmakers`. For a `dannyota` image, add
   `--bundle-from-oci` after `--repo`: GitHub's API no longer returns
   attestations built before the
   [repository transfer](../repository-transfer.md#what-the-transfer-moves), and
   the bundle stored beside the image in GHCR carries the same signature.

   ```sh
   gh attestation verify \
     oci://ghcr.io/<owner>/aboutme-server@sha256:<digest> \
     --repo <owner>/aboutme \
     --signer-workflow <owner>/aboutme/.github/workflows/release-images.yml \
     --source-ref refs/tags/<version> \
     --deny-self-hosted-runners
   ```

3. Verify its SBOM the same way, adding
   `--predicate-type https://spdx.dev/Document/v2.3`.

4. Open the commit and read the source at that tag.

A person who does not run commands can open the transparency log entry and the
build run from the page's links.

`cosign verify-attestation` can check the same bundle without a GitHub account
if it reads bundles stored as OCI referrers (**Verify** the cosign version and
flags before the page shows it). Until then the page shows only `gh`.

## Who verifies

| Who      | What                                                                 |
| -------- | -------------------------------------------------------------------- |
| Operator | `observer.sh` and `deploy.sh` run `gh attestation verify` before use |
| Observer | Every new running digest, then daily                                 |
| Anyone   | The commands above, against GitHub and Sigstore directly             |

The page's own browser code does not verify signatures. A verifier served by
aboutme proves nothing a compromised aboutme could not fake; independent
verification needs the reader's own tools.
