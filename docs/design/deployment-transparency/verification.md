# Build evidence and verification

This page says what the release workflow signs, what "verified" means in the
[deployment document](document.md), who checks it, and how anyone can check it
without trusting aboutme.

## What the release workflow produces

`release-images.yml` builds `server`, `web`, `caddy`, and `observer` for a
version tag on a GitHub-hosted runner. For each image it already scans with
Trivy, pushes to `ghcr.io/dannyota/aboutme-<name>`, and runs
`actions/attest-build-provenance`, which signs an SLSA provenance v1 statement
for the pushed digest with a short-lived Sigstore certificate issued to the
workflow's OIDC identity. The signature is recorded in the public Sigstore
transparency log (Rekor), stored in GitHub's attestation store, and pushed to
the registry beside the image.

A real statement for `aboutme-server` v0.5.21 names subject digest
`sha256:d1c33a0a…945c`, workflow ref `refs/tags/v0.5.21`, commit `d73f4043…`,
and run `actions/runs/36218940587/attempts/1`, and GitHub serves it without
authentication at
`https://api.github.com/repos/dannyota/aboutme/attestations/sha256:<hex>`.

### SBOM

Release 1 adds, per image, after the push:

1. `trivy image --format spdx-json` on the pushed digest writes
   `aboutme-<name>.spdx.json`. Trivy is already installed and pinned for the
   scan, so this adds no new third-party action. It reads Go build information
   and the OS package database.
2. `actions/attest` (the action that `attest-sbom` now wraps) with
   `subject-name`, `subject-digest`, `sbom-path`, and `push-to-registry: true`
   signs an SPDX 2.3 attestation bound to the same digest. The job keeps its
   `id-token: write` and `attestations: write` permissions and adds
   `artifact-metadata: write` if the pinned action version requires it
   (**Verify** on the first run).
3. The SBOM and digest files are uploaded as job artifacts.

**Owner approval (A3):** a final `release` job, which needs every image job,
runs with only `contents: write`, downloads those artifacts, and runs
`gh release create <tag> --verify-tag` with the four SBOMs, a `digests.txt`
listing each image and digest, and fixed notes holding the verify command below.
The release gives the SBOM a permanent public link; workflow artifacts expire.
The release asset is a convenience copy: the signed SBOM attestation is the
evidence, and a changed asset would no longer match it. Without the release, the
document's `links.sbom` points at the attestation API with
`?predicate_type=spdx` instead.

### A separate cosign signature

It adds nothing now. A GitHub attestation is already a Sigstore signature: a
DSSE envelope signed with a Fulcio certificate for the same workflow identity,
logged in Rekor, and bound to the digest. `cosign sign` would add a second
signature from the same identity over the same digest. It would matter only for
a verifier that reads cosign's tag-based signatures and not Sigstore bundles,
such as an admission controller on a future Kubernetes cluster. Revisit it then.

## What "verified" means

For one running digest, the observer uses `sigstore-go` with the public-good
trust root (refreshed through TUF) and requires all of:

1. A bundle for the digest, fetched from the GitHub attestations API filtered by
   predicate type. When the API answers 403 or 429 (its unauthenticated limit is
   60 requests an hour per address), the observer reads the same bundle from the
   registry.
2. A valid signature, a certificate chain to Fulcio, and a Rekor inclusion proof
   and signed timestamp inside the certificate's validity.
3. Issuer `https://token.actions.githubusercontent.com`.
4. Certificate identity exactly matching
   `^https://github\.com/dannyota/aboutme/\.github/workflows/release-images\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`.
5. Source repository `https://github.com/dannyota/aboutme` and runner
   environment `github-hosted` in the certificate extensions.
6. Statement subject digest equal to the running digest, and subject name equal
   to the component's image.
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

A digest's result is cached in the bucket under `verified/<digest>.json` for 24
hours, then checked again. Digests are immutable, so the cache only saves API
calls. An `unchecked` result is not cached.

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
   needs a token):

   ```sh
   gh attestation verify \
     oci://ghcr.io/dannyota/aboutme-server@sha256:<digest> \
     --repo dannyota/aboutme \
     --signer-workflow dannyota/aboutme/.github/workflows/release-images.yml \
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
