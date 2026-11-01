# Common tool gotchas

Read this file when running repository tools. Task-specific gotchas live in [Go](go.md), [web](web.md), [deployment](deploy.md), and [testing](testing.md). CI concurrency and exact-commit release evidence live in [verification](verification.md#github-ci).

- `.env` is ignored and holds credentials. Never print, document, or stage values; add names with empty values to `.env.example`.
- Run `make` at the repository root.
- Use `npx @redocly/cli`, never `npx redocly`.
- `make docs-lint` follows `package.json` and `.prettierignore`; [agent-only files](documentation.md#agent-file-formatting) are excluded.

## macOS local tooling

Install Homebrew `bash`, `coreutils`, `make`, and `shellcheck`, plus Python 3 and the repository's pinned Go, Node, Caddy, and Podman tools. Put Homebrew Bash's `bin`, coreutils' `libexec/gnubin`, and GNU make's `libexec/gnubin` before the system tools on PATH. Make recipes resolve `bash` from PATH; `make pre-push` and `make dev-https-browser-image` require Bash 4+. Native tooling checks GNU `realpath`, `stat`, `base64`, `sha256sum`, and `date` before using their options. Python 3 supplies managed locks and detach helpers; the current wrapper also supplies the RSS watchdog until its replacement lands. The accepted portable session/listener helpers remain pending; see [local tooling](../docs/design/local-tooling-simplification.md#session-contract) before assuming `lsof` can replace Linux-only probes.

Install Caddy 2.11.4 from its official macOS ARM64 release archive at `https://github.com/caddyserver/caddy/releases/download/v2.11.4/caddy_2.11.4_mac_arm64.tar.gz` and put its `caddy` binary on PATH; CI uses the matching pinned release binary.

Start `podman machine` before `make test-db-up`. The shared `aboutme-test-db` container publishes `127.0.0.1:20432`; verify that the VM forwards that port to the Mac before starting native services. Use the same container for test and native databases, and leave it running while another session uses it. The pending laptop RSS/nice removal does not change the VM memory setting or container limits.

On non-Linux hosts the native server starts with print and PDF rendering disabled and logs that limitation once. Owner PDF, public PDF and share-image render requests return 503 through their existing error envelopes. The private print listener also returns 503. Linux still requires the browser sandbox check to pass at startup; no platform launches an unsandboxed browser.

Native macOS HTTPS proofs remain pending runner integration and acceptance: use the owner-account trusted-commit boundary, attended disposable-profile CA import, strict TLS, and the exact whole-journey skips in [local tooling](../docs/design/local-tooling-simplification.md). Native HTTPS WebKit skips; display harness PDFs still run. Never install a CA in host trust stores or use TLS bypass.

## Portable scripts and tests

- macOS temp paths resolve `/var` to `/private/var`; compare canonical paths (`pwd -P`) wherever a tool canonicalizes.
- macOS has no `/proc`; `[ /dev/fd/N -ef FILE ]` stats the devfs node, so match descriptors with Python `os.fstat` (`st_dev`, `st_ino`).
- Homebrew `flock` (discoteq) reopens `/dev/fd/N`; lock an inherited descriptor with `fcntl.flock` on the descriptor itself.
- `python3 -I` ignores `PYTHONDONTWRITEBYTECODE`; tests that spawn Python pass `-B`.
- Darwin rejects read deadlines on FIFOs; use a pipe or a polled file in Go test handshakes.
- Podman on macOS runs in a VM: `--network=host` reaches the VM, not the Mac; use a Podman network or published loopback ports.
- GNU `sort` fails in some macOS locales; use `LC_ALL=C` where order matters.
