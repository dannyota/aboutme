// Command caddy builds the production Caddy binary from source instead of
// using the upstream release binary. The upstream caddy:2.11.4-alpine image
// ships a binary built with an older Go toolchain and older module versions
// than this repository pins (.tool-versions: golang 1.27.1), so it carries
// stdlib and dependency vulnerabilities that this module's pinned, newer
// versions fix. It mirrors upstream's own cmd/caddy/main.go for v2.11.4 with
// the standard module set, plus the two plugins the direct edge uses
// (docs/design/vietnam-production.md, "Edge"): the Coraza web application
// firewall and the CrowdSec bouncer's HTTP handler. This file and go.mod are
// what `xcaddy build --with` generates, kept in the repository so go.sum pins
// every module.
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
