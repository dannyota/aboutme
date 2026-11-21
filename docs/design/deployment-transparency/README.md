# Deployment transparency

Anyone can read the image digests reported for `https://aboutme.vn`. The
[Vietnam host reporter](host.md) reads Podman, and an observer container checks
each running digest against the signed GitHub build record. The observer
publishes the result at `https://aboutme.vn/.well-known/deployment.json`. The
page `/verify` shows the same document to people and tells them how to check it
themselves. [ADR 0027](../../adr/0027-deployment-transparency-observer.md)
records the choice.

Status: accepted; the owner approved A1 to A5 on 2026-09-27. **Owner approval**
marks a product-visible choice the owner makes before the work that depends on
it starts ([list](#owner-approval)).

| Page                            | Holds                                                  |
| ------------------------------- | ------------------------------------------------------ |
| This page                       | Evidence model, freshness, serving, limits, approvals  |
| [Document](document.md)         | `deployment.json` schema, field sources, sanitizer     |
| [Verification](verification.md) | Provenance, SBOM, what "verified" means, user commands |
| [Verify page](page.md)          | `/verify` states, layout, copy, footer link            |
| [Vietnam host](host.md)         | Podman source, Caddy publication, weaker host trust    |

## Evidence model

The critical field is the image digest the reporter reads, not a tag. A tag can
move; a digest names exact bytes. A checked running digest is joined to a
Sigstore-signed provenance record that GitHub Actions produced when it built
that digest from a tagged commit. The document shows four steps, and each must
match:

1. **Source:** the public repository, tag, and commit.
2. **Build:** the `release-images` workflow run that built from that commit.
3. **Image:** the digest the build signed, in `ghcr.io/<owner>/aboutme-*`. The
   [repository transfer](../repository-transfer.md) defines the two owners.
4. **Running:** the digest the platform reports for each serving container.

Build fields come from the signed record, never from the application, the
reporter, or deploy scripts. The host reporter leaves build fields null, and the
observer fills them only after their signatures have been checked. Running
fields come from the host reporter. Application self-reporting would be weaker
evidence: a compromised application can say anything about itself.

## Where the observer runs

Production on the Vietnam vServer uses the [host reporter](host.md). A root
timer writes an unchecked observation of the running Podman containers, and an
observer container without Podman socket access verifies the digests and
publishes the document.

## Run, freshness, and staleness

The root reporter writes an observation every minute. Each observer run reads
it, verifies every digest not in its cache ([verification](verification.md)),
and writes the document.

- **Freshness:** the document keeps the reporter's `observed_at`. The observer
  refuses an observation older than 180 seconds or more than 60 seconds in the
  future, and clients may cache the document for 30 seconds.
- **Stale:** the observer writes `stale_after` as `observed_at` plus 180
  seconds. The page treats the document as stale after `stale_after`, or more
  than 600 seconds after `observed_at` whatever `stale_after` says. It measures
  "now" from the response's `Date` plus `Age` headers, so a wrong visitor clock
  cannot make old data look current.
- **Failure:** a failed platform read, including a refused observation, writes
  nothing, so the last good document ages into stale. A failed or impossible
  verification still writes the document, with that image marked `unchecked`,
  `not_found`, or `invalid`. A document that fails the output schema is never
  written.

The page never shows "verified" for a stale document, an unknown schema version,
or any image whose status is not `verified`.

## Serving and caching

Caddy serves `/.well-known/deployment.json` from the observer's public directory
with `Cache-Control: public, max-age=30`
([host publication](host.md#source-and-publication)). The document holds no
personal data and no publication state, so ADR 0010 does not apply. The page is
an ordinary Nuxt route through Caddy. The browser fetches the document from the
same origin, so no Content Security Policy change is needed.

## What this proves

It proves:

- At `observed_at`, the host reporter reported these running digests and replica
  counts. The reporter is the host itself, not an independent platform API, so
  the trust claim is weaker than an off-host observer's.
- For an image marked verified, GitHub Actions, running the public
  `release-images.yml` workflow for that tag on a GitHub-hosted runner, built
  and signed the digest from the named commit, and the signature is in the
  public Sigstore transparency log.

It does not prove:

- **That the host is honest.** A compromised host could run other code and
  report the expected digests.
- **Runtime integrity.** Code injected into a running container after start is
  not visible.
- **Configuration.** Environment values, secrets, feature flags, and container
  settings are not shown, by design.
- **Good source.** A verified build of a buggy or malicious commit is still
  verified. The source is public for review.
- **Build isolation.** Provenance comes from the same job that built the image
  (SLSA Build Level 2), so a compromised build step could influence it.
- **Operator honesty.** The operator can change the reporter and its published
  document. The reporter and observer share the host's failure domain.
- **Jobs.** One-shot containers (`migrate`, `db-setup`, and scheduled jobs) are
  not listed ([approval](#owner-approval)).

The page states the first three limits in plain words ([page](page.md#limits)).

## Compatibility

- There is no database change and nothing to migrate. The document is a
  snapshot; no history is kept, so nothing is lost when it is overwritten.
- Digests without an SBOM attestation have no checked SBOM. The document marks
  their SBOM `not_found`, and the page shows that step without failing the build
  chain.
- A browser holding an older page reads a newer document by `schema_version`. An
  unknown major version shows "This page is out of date" and never a status.
- Self-hosted instances without a reporter answer the path with 404, and the
  page shows that the server publishes no deployment record.
- Rollback: disabling the reporter timer or the observer leaves the last
  document to go stale, which the page shows honestly. Removing the public file
  makes the path answer 404.

## Owner approval

| ID  | Choice                                                                                                                     | Decision                                 |
| --- | -------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| A1  | Page path `/verify` or `/transparency` ([page](page.md#path))                                                              | Approved                                 |
| A2  | Lambda observer every minute, off the host, under USD 0.50 a month; edge caches the document 30 seconds                    | Superseded by the [host design](host.md) |
| A3  | `release-images.yml` creates a GitHub Release per tag holding the SBOMs and digests ([verification](verification.md#sbom)) | Approved                                 |
| A4  | List serving services only (server, web, caddy, maintenance while it runs), not one-shot jobs                              | Approved                                 |
| A5  | Show the observer's verdict and the command to verify it yourself, with the limits above ([page](page.md))                 | Approved                                 |

The footer label ("Kiểm chứng" and "Verify") and page title ("Kiểm chứng phiên
bản đang chạy" and "Verify what's running") are decided.
