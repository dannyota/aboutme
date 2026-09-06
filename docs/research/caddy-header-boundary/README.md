# Native Caddy trust-header experiment

Pinned Caddy 2.11.4 can reject duplicate origin-secret and client-IP values
using native expression matchers. The isolated raw-HTTP experiment passes all 21
cases with [the strict configuration](green.Caddyfile). The permissive
[header-matcher baseline](red.Caddyfile) fails 17 cases.

Run `python3 -B docs/research/caddy-header-boundary/probe.py` from the
repository root with localhost port 20961 free. The harness runs each Caddy
process in turn, stops it, and writes synthetic results under ignored
`.dev/phase-10/`. It does not load application configuration or credentials.

The request-header placeholder joins all physical values with commas in
[Caddy's pinned source](https://github.com/caddyserver/caddy/blob/v2.11.4/modules/caddyhttp/replacer.go#L66-L73).
Exact membership checks on that combined value reject duplicate and comma-list
secrets. Client-IP checks reject empty or comma-valued input and require the
value to equal Caddy's parsed address from the trusted immediate peer.

The cases cover current/next synthetic secrets, IPv4/IPv6, missing and wrong
secrets, repeated equal/mixed/empty values, comma lists, malformed addresses,
ports, zones, internal whitespace and literal placeholder input. Normal HTTP
optional whitespace is trimmed by the HTTP parser; this does not claim raw-byte
whitespace rejection.

This proves native configuration feasibility. The eventual route harness must
also prove real upstream header replacement, additional IPv6 forms, untrusted
peers, CloudFront overwriting viewer input, and ALB routing. It does not yet
prove the deployed trust chain.
