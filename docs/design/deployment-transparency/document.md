# Deployment document

`/.well-known/deployment.json` is one JSON object, version 1. The observer
writes it; the [verify page](page.md) and anyone else read it. This page is the
contract: the fields, where each value comes from, the sanitizer, and the
version rules. The [overview](README.md) covers the observer itself.

## Example

A rollout of `web` from v0.6.4 to v0.6.5 on AWS, with `server` and `caddy`
already on v0.6.5 and maintenance stopped. Digests and commits are shortened,
and `"…"` stands for fields that repeat the `server` shape; the document always
carries full values.

```json
{
  "schema_version": 1,
  "project": "aboutme",
  "environment": "production",
  "site": "https://aboutme.vn",
  "source_repository": "https://github.com/dannyota/aboutme",
  "platform": {
    "provider": "aws",
    "orchestrator": "ecs",
    "region": "ap-southeast-1"
  },
  "observed_at": "2026-10-02T03:14:05Z",
  "stale_after": "2026-10-02T03:17:05Z",
  "summary": "rolling_out",
  "release": null,
  "components": [
    {
      "name": "server",
      "image": "ghcr.io/dannyota/aboutme-server",
      "running_images": [
        {
          "digest": "sha256:9f2c…41ab",
          "replicas": 1,
          "running_since": "2026-10-02T03:10:51Z",
          "version": "v0.6.5",
          "commit": "3b1f…9e07",
          "signature": {
            "status": "verified",
            "checked_at": "2026-10-02T03:11:05Z",
            "signer_workflow": "https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/v0.6.5",
            "transparency_log_index": 2965228543
          },
          "sbom": { "status": "verified", "format": "spdx-2.3" },
          "links": {
            "commit": "https://github.com/dannyota/aboutme/commit/3b1f…9e07",
            "release": "https://github.com/dannyota/aboutme/releases/tag/v0.6.5",
            "build": "https://github.com/dannyota/aboutme/actions/runs/36218940587/attempts/1",
            "provenance": "https://api.github.com/repos/dannyota/aboutme/attestations/sha256:9f2c…41ab?predicate_type=provenance",
            "sbom": "https://github.com/dannyota/aboutme/releases/download/v0.6.5/aboutme-server.spdx.json",
            "transparency_log": "https://search.sigstore.dev/?logIndex=2965228543"
          }
        }
      ]
    },
    {
      "name": "web",
      "image": "ghcr.io/dannyota/aboutme-web",
      "running_images": [
        {
          "digest": "sha256:77e0…c2d1",
          "replicas": 1,
          "version": "v0.6.4",
          "…": "…"
        },
        {
          "digest": "sha256:a41b…0f9e",
          "replicas": 1,
          "version": "v0.6.5",
          "…": "…"
        }
      ]
    }
  ],
  "observer": {
    "version": "v0.6.4",
    "commit": "5d0e…a771",
    "source": "self-reported"
  }
}
```

`caddy` follows the same shape. `maintenance` appears only while its service
runs a task.

## Fields

Every object is closed: the schema sets `additionalProperties: false` at every
level. Readers ignore fields they do not know (see [versions](#versions)).

| Field                                 | Type and rule                                                               | Source                                   |
| ------------------------------------- | --------------------------------------------------------------------------- | ---------------------------------------- |
| `schema_version`                      | Integer, `1`                                                                | Constant                                 |
| `project`                             | `"aboutme"`                                                                 | Constant                                 |
| `environment`                         | `"production"`                                                              | Observer configuration                   |
| `site`                                | `"https://aboutme.vn"`                                                      | Observer configuration                   |
| `source_repository`                   | `"https://github.com/dannyota/aboutme"`                                     | Constant                                 |
| `platform.provider`                   | `aws` or `greennode`                                                        | Observer platform adapter                |
| `platform.orchestrator`               | `ecs` or `kubernetes`                                                       | Observer platform adapter                |
| `platform.region`                     | `ap-southeast-1`, or a GreenNode region code such as `HCM03`                | Observer configuration                   |
| `observed_at`, `stale_after`          | RFC 3339 UTC, whole seconds; `stale_after` is `observed_at` plus 180 s      | Observer clock                           |
| `summary`                             | `verified`, `rolling_out`, `unverified`, or `mismatch` (rules below)        | Computed from the components             |
| `release`                             | Object or `null` (rules below)                                              | Computed from the components             |
| `components[].name`                   | `server`, `web`, `caddy`, or `maintenance`, in that order                   | Fixed mapping from service and container |
| `components[].image`                  | `ghcr.io/dannyota/aboutme-server`, `-web`, or `-caddy`                      | Fixed mapping, never the platform text   |
| `running_images[]`                    | One entry per distinct digest, ordered by `running_since`, at most 8        | Platform                                 |
| `running_images[].digest`             | `^sha256:[0-9a-f]{64}$`                                                     | Platform                                 |
| `running_images[].replicas`           | Integer 1 to 64                                                             | Platform, counted by the observer        |
| `running_images[].running_since`      | RFC 3339 UTC; earliest start among that digest's running tasks or pods      | Platform                                 |
| `running_images[].version`            | `^v[0-9]+\.[0-9]+\.[0-9]+$` or `null`                                       | Signed certificate's source ref          |
| `running_images[].commit`             | `^[0-9a-f]{40}$` or `null`                                                  | Signed certificate's source digest       |
| `signature.status`                    | `verified`, `not_found`, `invalid`, or `unchecked`                          | Observer verification                    |
| `signature.checked_at`                | RFC 3339 UTC or `null`                                                      | Observer clock                           |
| `signature.signer_workflow`           | The exact certificate identity, matching the pattern in [verification][ver] | Signed certificate                       |
| `signature.transparency_log_index`    | Integer or `null`                                                           | Bundle's Rekor entry                     |
| `sbom.status`, `sbom.format`          | Status as above; format `spdx-2.3` or `null`                                | Observer verification                    |
| `links.*`                             | Built from constants plus verified values; each matches a fixed URL pattern | Observer                                 |
| `observer.version`, `observer.commit` | The observer binary's own build information                                 | Self-reported, and labelled so           |

`version`, `commit`, `signer_workflow`, `transparency_log_index`, and every link
except `provenance` are `null` unless `signature.status` is `verified`, so an
unverified digest never carries a claimed version.

[ver]: verification.md#what-verified-means

### Summary and release

The observer computes `summary` from the components:

| Value         | When                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------ |
| `mismatch`    | Any image is `not_found` or `invalid`, or `server`, `web`, or `caddy` has no running image |
| `unverified`  | Otherwise, any image is `unchecked`                                                        |
| `rolling_out` | Otherwise, a component runs more than one digest                                           |
| `verified`    | Every component runs one verified digest                                                   |

`release` is set only when `summary` is `verified` and `server`, `web`, and
`caddy` share one version and commit. It holds `version`, `commit`, and
`deployed_at`: the latest `running_since` among those three, the moment the
whole release was running. `maintenance` may run an older Caddy after a rollback
and does not affect `release`.

Staleness is not in `summary`, because only the reader knows the current time.
The page applies the [staleness rule](README.md#run-freshness-and-staleness)
before it shows any summary.

### Component mapping

| Platform unit                                            | Container | Component     |
| -------------------------------------------------------- | --------- | ------------- |
| ECS service `aboutme-prod-app`                           | `server`  | `server`      |
| ECS service `aboutme-prod-app`                           | `caddy`   | `caddy`       |
| ECS service `aboutme-prod-web`                           | `web`     | `web`         |
| ECS service `aboutme-prod-maintenance`                   | `caddy`   | `maintenance` |
| Kubernetes pod labelled `app.kubernetes.io/component: X` | named X   | X             |

A container outside this table is ignored. A digest whose signed subject names a
different image than the component's (for example, the web image running as
`server`) is `invalid`.

## Size

A document with four components and two images each is about 8 KiB. The observer
refuses to write a document over 64 KiB, which also bounds a platform that
reports many digests.

## Sanitizer

The sanitizer is an allowlist. The observer never serializes a platform object.
Platform adapters return one internal type holding only component name, digest,
replica count, and start time; the document builder fills a closed output type
from that type, the verification results, and constants. Before writing, the
observer validates the bytes against the JSON Schema file
`deploy/observer/schema/deployment.v1.json`, and every string against its field
pattern. A value that fails its pattern drops the whole write; it is never
passed through or truncated into place.

Never written, by construction and by test: task, pod, and container IDs and
names; cluster names and ARNs; account IDs; node and host names; private and
public IP addresses; availability zones; environment variables; labels and
annotations; service accounts; task definition families, revisions, and
settings; registry credentials; image references as the platform wrote them;
commit authors; and the GitHub attestation `initiator`.

The test `TestDocumentLeaksNothing` in `deploy/observer/internal/document`:

1. Feeds recorded-shape fixtures to both adapters: a `DescribeTasks` response
   and a Kubernetes `PodList`, each with every field above filled with a unique
   canary value (for example `canary-cluster-7f3a`, `10.9.8.7`, `123456789012`,
   `ap-southeast-1b`, `containerd://canary…`).
2. Builds the document and asserts no canary string appears in its bytes.
3. Asserts the bytes match none of these patterns: a 12-digit number, `arn:`, an
   IPv4 or IPv6 address, an availability zone name, `://` outside the allowed
   URL prefixes, a 32-hex task ID not inside a 64-hex digest, and a UUID.
4. Walks every key path in the document and asserts each is in the allowlist
   generated from the JSON Schema, so a new output field fails until the schema
   and this test list it.
5. Adds a fixture field the adapters do not know (a new AWS response member) and
   asserts the output is unchanged.

## Versions

- A field added to version 1 must be optional for readers, and is added to the
  schema, the allowlist test, and the page in one change. Readers ignore unknown
  fields.
- Removing, renaming, or changing the meaning of a field is version 2. The
  observer then writes version 2 at the same path, and the page is updated in
  the same release, before the observer.
- A reader that sees a higher `schema_version` than it knows shows no status and
  says the page is out of date.
