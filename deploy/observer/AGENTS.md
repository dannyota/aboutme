# Deployment observer

## Source map and boundaries

|Path|Role|
|-|-|
|`cmd/observer/`|Binary modes and adapter wiring|
|`cmd/document-check/`|Fetched-document validation for operator checks|
|`internal/platform/`|Production Podman observation input and retained ECS and Kubernetes adapters|
|`internal/verify/`|Signed build evidence verification|
|`internal/document/`, `schema/`|Closed public report builder and JSON Schema|
|`internal/publish/`, `internal/leakscan/`|Atomic publication and forbidden identifier checks|
|`testdata/`|Adapter, verification, leak, and example fixtures|

`go.mod` defines an independent module outside root `go.work`. Podman mode reads the root reporter's observation file and writes the public document without cloud credentials or Podman socket access. ECS and Kubernetes adapters are retained alternatives, not the live host mode.

## Sources and outputs

Root reporter input flows through platform aggregation, signature verification, the schema-bound report builder, and atomic publication of `deployment.json`. `Dockerfile` builds the independent binary image. `internal/document/examples_test.go` generates the golden `testdata/examples/` documents used by the frontend; briefs must name affected consumers and example output.

## Authorities and checks

Read [observer README](README.md), [document design](../../docs/design/deployment-transparency/document.md), [host design](../../docs/design/deployment-transparency/host.md), [verification design](../../docs/design/deployment-transparency/verification.md), [ADR 0027](../../docs/adr/0027-deployment-transparency-observer.md), and [observer runbook](../../docs/runbooks/observer.md).

[Go rules](../../instructions/go.md) and [verification](../../instructions/verification.md) govern checks. The `server` job in `.github/workflows/ci.yml` builds, vets, tests, lints, checks module drift, and scans vulnerabilities for the observer with `GOWORK=off`. Package tests protect report leakage, strict Podman input, freshness, atomic publication, and byte-exact examples; [release rules](../../instructions/releases.md) own image checks.
