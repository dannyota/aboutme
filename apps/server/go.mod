module github.com/dannyota/aboutme/apps/server

go 1.27.1

require (
	github.com/aws/aws-sdk-go-v2 v1.47.0
	github.com/aws/aws-sdk-go-v2/config v1.33.5
	github.com/aws/aws-sdk-go-v2/credentials v1.20.5
	github.com/aws/aws-sdk-go-v2/service/rds v1.129.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.113.3
	github.com/aws/aws-sdk-go-v2/service/sesv2 v1.75.0
	github.com/aws/smithy-go v1.28.2
	github.com/chromedp/cdproto v0.0.0-20260922220944-a19bff23514f
	github.com/chromedp/chromedp v0.16.0
	github.com/coreos/go-oidc/v3 v3.21.0
	github.com/dannyota/aboutme/packages/schema/gen/go v0.0.0
	github.com/go-jose/go-jose/v4 v4.1.5
	github.com/go-webauthn/webauthn v0.18.2
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/pressly/goose/v3 v3.28.0
	github.com/rivo/uniseg v0.4.7
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	golang.org/x/crypto v0.57.0
	golang.org/x/image v0.46.0
	golang.org/x/net v0.59.0
	golang.org/x/oauth2 v0.37.0
	golang.org/x/sys v0.48.0
	golang.org/x/text v0.42.0
	golang.org/x/time v0.16.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.20 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.0 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.10.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.38.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.51.0 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-json-experiment/json v0.0.0-20260820222146-c27c302e5fc3 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/rogpeppe/go-internal v1.15.0 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
)

// packages/schema/gen/go is an unpublished, in-repo module (design spec
// §3 "Codegen fidelity"): there is no tagged release to depend on, so this
// replace points the require above at its real path instead of a fabricated
// version. Kept even though the repo-root go.work also lists this
// directory, so `go build`/`go test` resolve correctly with GOWORK=off too
// (e.g. a build environment that doesn't propagate go.work) — see go.work's
// own comment for the workspace half of this wiring.
replace github.com/dannyota/aboutme/packages/schema/gen/go => ../../packages/schema/gen/go
