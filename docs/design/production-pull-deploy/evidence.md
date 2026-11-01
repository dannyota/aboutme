# Production pull deployment evidence

See the [design overview](README.md) and [protocol](protocol.md).

## Trust, release modes, and historical evidence

Enroll an immutable, versioned, root-owned control package containing verifier,
coordinator, journal reader, startup guard, and cleanup. Systemd names its exact
path. Automatic Ansible cannot replace controls, trust policy, or allowed
signers. Initially enroll the owner’s existing SSH signing public key through
trusted administration; `minhmq` may approve without becoming a source signer.

Signer addition, rotation, revocation, policy changes, and control upgrades
require paused attended administration and a state-revision increment before
polling resumes. Revoke administrative keys through trusted administration.
Never obtain trust keys from GitHub profiles or candidate checkouts.

For every authorization and mode, fetch canonical refs without hooks,
submodules, credential helpers, or repository-controlled execution. Verify the
coordinator’s SSH signature, require it to equal freshly fetched `main`, and
require the release source to be its ancestor. Authenticate the complete
execution bundle before Ansible reads it.

For both coordinator and release source, select the newest `ci.yml` run by run
ID matching exact SHA, `event=push`, and branch `main`, then its current
attempt. Require completed success. Missing, unreadable, pending, cancelled, or
failed evidence refuses admission. Historical SSH operations receive no CI
exemption.

Let `F` be the enrolled automatic eligibility floor: the first released version
whose serving and maintenance Caddy support deploy status and whose publication
satisfies modern evidence rules. Record its actual tag, commit, and digests
after attended installation and proof. Do not assume v0.8.13 qualifies. `F` is
distinct from the monotonic security floor.

| Authorization and mode          | Target restriction                                               | Evidence and `publication`                                  |
| ------------------------------- | ---------------------------------------------------------------- | ----------------------------------------------------------- |
| GitHub `deploy`                 | Target ≥ installed tag and ≥ `F`                                 | Modern; non-null                                            |
| GitHub `rollback` or `activate` | Refused; automatic rollback allowlist is empty                   | No admission                                                |
| SSH `deploy`                    | Target ≥ installed tag                                           | Modern and non-null at/above `F`; legacy and null below `F` |
| SSH `rollback`                  | Target < installed tag; reviewed compatibility evidence required | Modern and non-null at/above `F`; legacy and null below `F` |
| SSH `activate`                  | Exact installed release and application digests                  | Modern and non-null at/above `F`; legacy and null below `F` |

Every target must meet the current security floor and existing privacy,
document, migration, and CIMD guards. Recheck release ordering under admission
locks. Equal-tag deployment permits configuration reapplication but requires the
same tag object, source, and application digests. A lower target can never enter
through `deploy`, even when migration manifests match.

Common image checks require the published, non-draft, non-prerelease GitHub
Release identified by the request, exact tag/source binding, requested manifest
digests, and hashed manifest/configuration bytes proving platform identity. Each
application index contains exactly the requested arm64 and amd64 children; pull
only amd64 children. Other layouts or missing release identities are
unsupported.

Modern evidence selects the newest matching `release-images.yml` tag-push run
for the exact tag and source SHA. Require a completed first attempt; reject all
reruns, including partial reruns, and never fall back to an older successful
run. Fix forward with another patch release.

| Job ID and matrix                       | Required API-visible name and result             |
| --------------------------------------- | ------------------------------------------------ |
| `eligibility`                           | `Release eligibility`: success                   |
| `build`, server/web/caddy × arm64/amd64 | `Build <component> <arch>`: all six succeed      |
| `image`, server/web/caddy               | `Index <component>`: all three succeed           |
| `release`                               | `Publish release`: success                       |
| `observer-image`                        | `Build observer arm64`: optional                 |
| `observer-image-amd64`                  | `Build observer amd64`: required for replacement |

Assign these names before releasing `F`, retaining job IDs. Require exactly one
of each required job, with no skipped or missing result. Overall failure is
acceptable only when all non-observer jobs succeeded and failure is confined to
observer jobs. Reject cancellation or ambiguity. `Publication` identifies this
workflow, run, and attempt.

The unique `digests.txt` asset must match `digests_asset_hash` and contain
exactly the three requested index and six platform digests, plus optional
observer entries. Verify index provenance, both platform provenance records, and
platform SPDX 2.3 SBOM attestations through a deployment entry point reusing
`deploy/observer/internal/verify`.

Cryptographic verification uses authenticated Sigstore/TUF roots, Fulcio
certificates, Rekor inclusion and timestamps, exact subject names/digests,
issuer `https://token.actions.githubusercontent.com`, GitHub-hosted runners,
source repository/SHA, and signer
`https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/<tag>`.
Modern provenance identifies the selected publication run and attempt; SBOM
certificate source identity must match.

Legacy evidence applies only to SSH targets below `F`, with `publication`
exactly null. It waives the modern job-name/result contract, selected
publication run/attempt binding, `digests.txt`, mandatory index provenance,
unused arm64 provenance, and mandatory SBOM presence. It still requires valid
tag-bound SLSA provenance for every installed amd64 image with the cryptographic
identities above. Optional index/arm64 provenance or SBOM evidence, when
present, must verify; only confirmed absence permits omission. Errors and
invalid evidence refuse admission.

This is the complete legacy exception. Newer releases cannot claim it because
evidence is missing; null publication never skips common checks. Legacy
operations use current controls and the enrolled maintenance image, never
scripts from the old release.

For deploy and rollback, an absent target observer tag permits keeping the
installed observer. A present target requires replacement with valid amd64
configuration and provenance; modern evidence additionally requires its
successful job, release digest entry, and SBOM. Registry errors or invalid
evidence refuse admission. Activation keeps the observer without reinstalling
it.

Revalidate mutable run, approval, main-ref, and CI evidence within 120 seconds
before admission. Reverify cached immutable bytes under current policy and
commit/workflow/digest denial lists. No tag-signature requirement is added.
Release assets and observer display caches never replace cryptographic evidence.

## References

First-party references:
[review-history API](https://docs.github.com/en/rest/actions/workflow-runs#get-the-review-history-for-a-workflow-run),
[environment protections](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments),
[jobs API](https://docs.github.com/en/rest/actions/jobs),
[rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api),
[JSON canonicalization](https://www.rfc-editor.org/rfc/rfc8785),
[Ansible pull](https://docs.ansible.com/projects/ansible/latest/cli/ansible-pull.html),
[Git signature configuration](https://git-scm.com/docs/git-config), and
[systemd cleanup](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html#ExecStopPost=).
