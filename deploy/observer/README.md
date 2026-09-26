# Deployment observer

`deploy/observer` is the deployment transparency observer: a program, separate
from the application, that asks the platform which image digests are running,
checks each one against the signed GitHub build record, and publishes the result
to `https://aboutme.vn/.well-known/deployment.json`. It is its own Go module
(`github.com/dannyota/aboutme/deploy/observer`, go 1.27.1), outside the
repository's `go.work`, because it ships as a separate container image and never
shares a dependency graph with `apps/server`.

The design lives in
[`docs/design/deployment-transparency/`](../../docs/design/deployment-transparency/README.md):
where the observer runs, what it proves, and what it does not prove. Field
sources and the sanitizer are in
[`document.md`](../../docs/design/deployment-transparency/document.md). What
"verified" means is in
[`verification.md`](../../docs/design/deployment-transparency/verification.md).
[ADR 0057](../../docs/adr/0057-deployment-transparency-observer.md) records the
choice.

## Modes

`cmd/observer` builds one binary with three subcommands:

- `observer lambda`: calls `lambda.Start` with a handler that performs one run
  per invocation. On AWS, EventBridge Scheduler invokes the function every
  minute; the verifier and its Sigstore trust root are built once and reused
  across warm invocations.
- `observer run --interval 60s`: runs forever, performing one run per tick on a
  Kubernetes Deployment. A failed run is logged and the loop continues; it never
  exits on a single failed run.
- `observer version`: prints the binary's own version and commit and exits 0,
  for a smoke check that needs no cloud access.

One run lists the running images, checks every distinct digest against the
signed build record, builds and encodes the document, and publishes it. A
platform read failure or a document that fails to build or encode writes
nothing, so the last good document ages into stale rather than being replaced by
a wrong one
([README.md, "Run, freshness, and staleness"](../../docs/design/deployment-transparency/README.md#run-freshness-and-staleness)).

## Configuration

Every value comes from an environment variable, and every value is non-secret:
the AWS access policy in the design restricts the observer's role to reading
platform state and writing one bucket, so nothing it reads needs to be kept out
of its own environment.

| Variable                       | Required                 | Meaning                                                  |
| ------------------------------ | ------------------------ | -------------------------------------------------------- |
| `OBSERVER_PLATFORM`            | always                   | `ecs` or `kubernetes`                                    |
| `OBSERVER_CLUSTER`             | `ecs`                    | ECS cluster name                                         |
| `OBSERVER_APP_SERVICE`         | `ecs`                    | Service running `server` and `caddy`                     |
| `OBSERVER_WEB_SERVICE`         | `ecs`                    | Service running `web`                                    |
| `OBSERVER_MAINTENANCE_SERVICE` | `ecs`                    | Service running maintenance `caddy`                      |
| `OBSERVER_BUCKET`              | always                   | Bucket holding the document and the verification cache   |
| `OBSERVER_REGION`              | always (or `AWS_REGION`) | AWS region, or the GreenNode region code                 |
| `OBSERVER_NAMESPACE`           | no (`aboutme`)           | Kubernetes namespace to list pods in                     |
| `OBSERVER_S3_ENDPOINT`         | no                       | S3-compatible endpoint override, for path-style requests |
| `OBSERVER_TUF_CACHE`           | no (`/tmp/sigstore-tuf`) | Sigstore TUF metadata cache directory                    |

The environment (`production`) and site (`https://aboutme.vn`) are constants,
not configuration: the design fixes one observer to one site.

## Package layout

- `internal/platform`: the one type every adapter returns (`platform.Image`),
  the component constants, and `platform.Aggregate`, which counts observations
  by component and digest and fails on anything it cannot describe.
- `internal/platform/ecs`: an `aws-sdk-go-v2/service/ecs` adapter. Lists
  `RUNNING` tasks per serving service, describes them in one call, and maps each
  running container to a component by which service listed its task, never by
  the task's own group string
  ([document.md, "Component mapping"](../../docs/design/deployment-transparency/document.md#component-mapping)).
- `internal/platform/kubernetes`: a small `net/http` client for the in-cluster
  API, with no `client-go` dependency. Reads pods labeled
  `app.kubernetes.io/component` and takes the `@sha256:<hex>` suffix of a
  running container's `imageID`.
- `internal/verify`: checks one running digest against the Sigstore-signed
  provenance and SBOM attestations `release-images.yml` produced for it, and a
  cache so the check runs about once a day per digest.
- `internal/document`: builds the closed `Document` type from platform images,
  verification evidence, and constants only, and encodes it against the JSON
  Schema in `schema/`.
- `internal/publish`: writes the document and the verification cache to one S3
  (or S3-compatible) bucket.
- `cmd/observer`: the three subcommands, environment configuration, and the
  wiring that builds one adapter, one verifier, and one publisher per process.

## Tests

Run from this directory, with `GOWORK=off` so the module builds outside
`go.work`:

```sh
cd deploy/observer && GOWORK=off go test ./...
```

`make observer-test` from the repository root runs the same command. GitHub CI
additionally runs `go build`, `go vet`, `golangci-lint`, a `go mod tidy` no-op
check, and `govulncheck` (`.github/workflows/ci.yml`, the `observer` job).

The platform adapters are tested against `httptest` servers standing in for the
ECS control plane and the in-cluster Kubernetes API, using recorded-shape
fixtures under `testdata/ecs/` and `testdata/kubernetes/`. `internal/publish` is
tested against an `httptest` server standing in for S3. `internal/document`
carries `TestDocumentLeaksNothing`, which asserts that no infrastructure
identifier a platform adapter might read can reach the published bytes.

## Example documents

`internal/document/examples_test.go` builds one example document per state the
`/verify` page renders, from fixed inputs, and compares it byte for byte against
a golden file under `testdata/examples/`. Run the test with `-update` to
regenerate a golden file after an intentional change:

```sh
cd deploy/observer && GOWORK=off go test ./internal/document/... -run TestExamples -update
```

The frontend reads these files by name, so the set is stable:

- `verified.json`: every component runs one verified digest of the same release.
- `verified-with-maintenance.json`: as above, with a maintenance task also
  running.
- `rolling-out.json`: a component runs two verified digests during a deploy.
- `unverified.json`: one running digest has not been checked yet.
- `mismatch-not-found.json`: a running digest has no signed record.
- `mismatch-invalid.json`: a signed record exists but names a different image.
- `mismatch-missing-component.json`: a required component runs nothing.
- `sbom-not-found.json`: a release built before SBOM attestations shipped, still
  verified.
- `stale.json`: a verified document whose `observed_at` is far in the past.
- `future-version.json`: hand-written, `schema_version: 2`, never validated
  against the version 1 schema.
