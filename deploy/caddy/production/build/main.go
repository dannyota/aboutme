// Command caddy builds the production Caddy binary from source instead of
// using the upstream release binary. The upstream caddy:2.11.4-alpine image
// ships a binary built with an older Go toolchain and older module versions
// than this repository pins (.tool-versions: golang 1.27.1), so it carries
// stdlib and dependency vulnerabilities that this module's pinned, newer
// versions fix. It mirrors upstream's own cmd/caddy/main.go for v2.11.4,
// with the standard module set and no third-party plugins.
package main

import (
	caddycmd "github.com/caddyserver/caddy/v2/cmd"

	_ "github.com/caddyserver/caddy/v2/modules/standard"
)

func main() {
	caddycmd.Main()
}
