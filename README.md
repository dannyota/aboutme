# aboutme

[![CI](https://github.com/dannyota/aboutme/actions/workflows/ci.yml/badge.svg)](https://github.com/dannyota/aboutme/actions/workflows/ci.yml)

Build a resume, share its link, and export a PDF. aboutme is a free, open-source
resume builder for Vietnamese and English content. Each account can hold up to
three resumes, each with its own public URL. Accounts stay private.

[Use aboutme](https://aboutme.vn) ·
[Browse templates](https://aboutme.vn/templates) ·
[Read the docs](docs/README.md) · [Contribute](CONTRIBUTING.md)

## Features

- Twenty templates, a public gallery, and fictional samples to start from.
- An editor with autosave, live preview, layout controls, and photo crop.
- Shared rendering across the editor, public resume, and PDF, with self-hosted
  fonts that support Vietnamese.
- Per-resume publishing with separate controls for public access, PDF download,
  and search discovery. Unpublishing revokes public access.
- Live updates, PDF export, and public share images.
- Optional connections to MCP-compatible assistants for private resume editing.
  Connected assistants cannot publish resumes.
- Account export and deletion, session controls, and connected-agent revocation.

The hosted service runs at [aboutme.vn](https://aboutme.vn). Email-and-password
sign-up and Google sign-in are enabled, with optional passkey and authenticator
app second factors. The whole product, including the editor and settings,
supports Vietnamese and English.

## Project status

Production runs on AWS in Singapore behind Cloudflare. See
[tags](https://github.com/dannyota/aboutme/tags) for published versions,
[Actions](https://github.com/dannyota/aboutme/actions) for build results, and
the [production runbook](docs/runbooks/production.md) for operations.

The [architecture](docs/architecture.md) describes implemented behavior. The
[design](docs/design/README.md) records intended behavior and the
[roadmap](docs/plans/README.md) records remaining work. Flutter is deferred.

## Repository

| Path              | Responsibility                                                                  |
| ----------------- | ------------------------------------------------------------------------------- |
| `apps/server`     | Go API: authentication, resume domain, media, publish, public read, OAuth, MCP  |
| `apps/web`        | Nuxt SSR, authenticated editor, renderer, public pages, generated API client    |
| `apps/mobile`     | Deferred Flutter client                                                         |
| `packages/schema` | Resume JSON Schema, immutable releases, generated types, fixtures, and presets  |
| `deploy`          | Compose deployment, Caddy images, AWS OpenTofu modules, and the browser harness |
| `docs`            | Design, ADRs, API contract, plans, guides, standards, and runbooks              |

See the [documentation map](docs/README.md) for authority and lifecycle rules.

## Development

Use the exact local and CI tool versions in [`.tool-versions`](.tool-versions).
The Node and Go declarations in component manifests mirror that file.

```sh
npm ci
(cd apps/web && npm ci)
make tools-check ARGS=dev
make dev-native
```

Open `http://localhost:20080`. The command starts the one shared PostgreSQL
container and native Go, Nuxt, and Caddy processes. Stop only the native
processes with `make dev-native-down`.

See the [native development runbook](docs/runbooks/native-development.md) for
status, logs, ports, and database rules. Run `make check` for the fast local
gate. Independent contributors run `make ci` before a pull request. In a
coordinated worker session, GitHub CI on `main` is the full gate.

Report bugs through [GitHub issues](https://github.com/dannyota/aboutme/issues).
Report vulnerabilities privately using the [security policy](SECURITY.md).

## Self-hosting

The current Compose artifact supports local deployment evaluation. It does not
yet provide the HTTPS/443 path required for authentication checks or safe public
exposure. See the [self-hosting guide](docs/guides/self-hosting.md) for its
exact scope and limits.

## License

[AGPL-3.0](LICENSE). A modified version offered as a network service must make
its corresponding source available to that service's users.
