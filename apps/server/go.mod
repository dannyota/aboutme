module github.com/dannyota/aboutme/apps/server

go 1.26.5

require (
	github.com/dannyota/aboutme/packages/schema/gen/go v0.0.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/pressly/goose/v3 v3.27.3
	golang.org/x/oauth2 v0.36.0
	golang.org/x/time v0.15.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/rogpeppe/go-internal v1.15.0 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

// packages/schema/gen/go is an unpublished, in-repo module (design spec
// §3 "Codegen fidelity"): there is no tagged release to depend on, so this
// replace points the require above at its real path instead of a fabricated
// version. Kept even though the repo-root go.work also lists this
// directory, so `go build`/`go test` resolve correctly with GOWORK=off too
// (e.g. a build environment that doesn't propagate go.work) — see go.work's
// own comment for the workspace half of this wiring.
replace github.com/dannyota/aboutme/packages/schema/gen/go => ../../packages/schema/gen/go
