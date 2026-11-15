# Deployment observer

`deploy/observer` is the deployment transparency observer: a program, separate
from the application, that reads the host's observation of the running image
digests, checks each one against the signed GitHub build record, and publishes
the result to `https://aboutme.vn/.well-known/deployment.json`. Its packages
belong to the repository's root Go module (`danny.vn/aboutme`, go 1.27.4), under
the package prefix `danny.vn/aboutme/deploy/observer`. It ships as a separate
container image and imports no `apps/server` package, so the observer binary
links only its own dependencies.

The design lives in
[`docs/design/deployment-transparency/`](../../docs/design/deployment-transparency/README.md):
where the observer runs, what it proves, and what it does not prove. Field
sources and the sanitizer are in
[`document.md`](../../docs/design/deployment-transparency/document.md). What
"verified" means is in
[`verification.md`](../../docs/design/deployment-transparency/verification.md).
[ADR 0027](../../docs/adr/0027-deployment-transparency-observer.md) records the
choice.

## Modes

`cmd/observer` builds one binary with two subcommands:

- `observer run --interval 60s`: runs forever, performing one run per tick in
  the Podman host container. A failed run is logged and the loop continues; it
  never exits on a single failed run. The image's default command is
  `run --interval 60s`.
- `observer version`: prints the binary's own version and commit and exits 0,
  for a smoke check that needs no network access.

One run reads the observation, checks every distinct digest against the signed
build record, builds and encodes the document, and publishes it. A platform read
failure or a document that fails to build or encode writes nothing, so the last
good document ages into stale rather than being replaced by a wrong one
([README.md, "Run, freshness, and staleness"](../../docs/design/deployment-transparency/README.md#run-freshness-and-staleness)).

## Configuration

Every configuration value comes from a non-secret environment variable. The
observer reads an observation file and publishes to a host directory without
cloud credentials.

| Variable                    | Required                             | Meaning                               |
| --------------------------- | ------------------------------------ | ------------------------------------- |
| `OBSERVER_PLATFORM`         | always                               | `podman`                              |
| `OBSERVER_REGION`           | always                               | GreenNode region code                 |
| `OBSERVER_TUF_CACHE`        | no (`/tmp/sigstore-tuf`)             | Sigstore TUF metadata cache directory |
| `OBSERVER_OBSERVATION_FILE` | no (`/observation/observation.json`) | Root reporter's observation file      |
| `OBSERVER_OUTPUT_DIR`       | no (`/public`)                       | Directory receiving `deployment.json` |

The environment (`production`) and site (`https://aboutme.vn`) are constants,
not configuration: the design fixes one observer to one site.

## Podman host

`OBSERVER_PLATFORM=podman` reads the root reporter's observation file. It
requires the closed version 1 document shape with `greennode` / `podman`, known
components in document order, valid digests, replica counts, and RFC 3339
timestamps. Unknown or repeated JSON fields are rejected. Maintenance is omitted
when stopped. The read is capped at 64 KiB. An observation older than 180
seconds or more than 60 seconds in the future is a platform read failure, so the
run publishes nothing. The public document preserves the root reporter's
`observed_at`, so signature checks cannot extend an old snapshot's freshness.

Only the runtime fields enter the aggregator, verifier, and document builder.
The reporter's signature statuses and build claims are not evidence.
Verification results stay in memory for the process lifetime, with a 24-hour
recheck policy. The observer requires no bucket, cloud configuration, or secret.

The publisher writes a temporary file in `OBSERVER_OUTPUT_DIR`, sets mode
`0644`, fsyncs and closes it, renames it to `deployment.json`, and fsyncs the
directory. A failure before rename removes the temporary file and keeps the
previous document. A directory fsync failure after rename returns an error; the
complete new document may be visible. The run loop logs that error.

The [host design](../../docs/design/deployment-transparency/host.md) defines the
root timer, Quadlet restrictions, Caddy mounts, and weaker host trust limit. The
host image is the single-platform amd64 `<tag>-amd64` tag. The plain version tag
is a single-platform arm64 image that no deployment uses.

## Package layout

- `internal/platform`: the one type the adapter returns (`platform.Image`), the
  component constants, and `platform.Aggregate`, which counts observations by
  component and digest and fails on anything it cannot describe.
- `internal/platform/podman`: reads and validates the root reporter's file,
  without Podman or socket access.
- `internal/verify`: checks one running digest against the Sigstore-signed
  provenance and SBOM attestations `release-images.yml` produced for it, and an
  in-memory cache so the check runs about once a day per digest.
- `internal/document`: builds the closed `Document` type from platform images,
  verification evidence, and constants only, and encodes it against the JSON
  Schema in `schema/`.
- `internal/publish`: atomically replaces the host document file.
- `internal/leakscan`: the forbidden identifier patterns. The leak test and
  `cmd/document-check` share them.
- `cmd/observer`: the two subcommands, environment configuration, and the wiring
  that builds the adapter, the verifier, and the publisher once per process.
- `cmd/document-check`: checks a fetched copy of the live document (size,
  schema, forbidden patterns, staleness) and lists its running images. The
  production check script runs it; it is not part of the image.

## Tests

Run from this directory:

```sh
cd deploy/observer && go test ./...
```

`make observer-test` from the repository root runs the same command. GitHub CI
additionally runs `go build`, `go vet`, `golangci-lint`, and `govulncheck` here,
and the root module's `go mod tidy` no-op check (`.github/workflows/ci.yml`, the
`server` job).

`internal/publish` is tested against temporary directories for atomic file
replacement. Podman tests cover strict parsing, size bounds, timestamps, and the
maintenance rule. `TestPublishedDocument_MatchesGolden` reads a fixed host
observation through the Podman adapter, publishes the document through the file
publisher, and compares the bytes with `testdata/podman/deployment.json`.
`internal/document` carries `TestDocumentLeaksNothing`, which asserts that the
reporter's build claims and no infrastructure identifier can reach the published
bytes.

## Example documents

`internal/document/examples_test.go` builds one example document per state the
`/verify` page renders, from fixed inputs, and compares it byte for byte against
a golden file under `testdata/examples/`. Run the test with `-update` to
regenerate a golden file after an intentional change:

```sh
cd deploy/observer && go test ./internal/document/... -run TestExamples -update
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
