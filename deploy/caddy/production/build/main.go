// Command caddy builds production Caddy 2.11.9 with the Go toolchain in
// .tool-versions and the dependencies pinned by go.mod and go.sum.
// It mirrors upstream cmd/caddy/main.go for v2.11.9 with the standard modules,
// Coraza web application firewall, CrowdSec HTTP bouncer, and private log
// encoders used by the direct edge (docs/design/vietnam-production.md, "Edge").
package main

import (
	_ "time/tzdata"

	caddycmd "github.com/caddyserver/caddy/v2/cmd"

	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/corazawaf/coraza-caddy/v2"
	_ "github.com/hslatman/caddy-crowdsec-bouncer/http"
)

func main() {
	caddycmd.Main()
}
