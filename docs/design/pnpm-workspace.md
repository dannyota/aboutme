# pnpm workspace

The owner chose pnpm over npm on 2026-11-15. This page owns how the repository
installs JavaScript dependencies. It amends the npm install rules in
[local tooling](local-tooling-simplification.md#owner-choices-and-combined-jobs).

## Projects and lockfiles

One pnpm workspace holds the root, `apps/web`, and `packages/schema`, with one
`pnpm-lock.yaml` at the root. `apps/web` depends on `@aboutme/schema` through
`workspace:*`.

`deploy/dev-https-browser` stays a separate pnpm project with its own lockfile.
Its image installs from the closed staged source set in `spec-sources.txt`, and
the application graph must not enter that image.

No `package-lock.json` remains. Every `npm ci`, `npm install`, `npm run`, and
`npx` call becomes its pnpm form. The npm and corepack removal in the web
runtime image stays.

## Version and pinning

Use the latest pnpm 11 release that is at least seven days old, pinned exactly
in `.tool-versions` and in the root `packageManager` field with its integrity
hash. Every environment gets pnpm through corepack from that field; CI fails
when the running pnpm differs from the pin.

pnpm 12 waits. Dependabot reads pnpm 12's two-document lockfile as an empty
dependency graph ([dependabot-core#15904][dbc-15904]), which would hide security
alerts. Raise the pin once that issue is fixed.

## Install settings

`pnpm-workspace.yaml` sets, and every pnpm project uses:

- `minimumReleaseAge: 10080` (seven days) with `minimumReleaseAgeStrict: true`.
  A security fix newer than that goes in `minimumReleaseAgeExclude` by exact
  name, with the advisory in the commit message, and leaves once it is seven
  days old.
- Dependency build scripts run only for packages listed in `allowBuilds`.
  `strictDepBuilds` stays on, so a new build script fails the install until a
  reviewed change allows it.
- The isolated node_modules layout with an in-project virtual store. No hoisting
  settings, no global virtual store.
- `--frozen-lockfile` for every install outside a deliberate dependency change.

## Store, worktrees, and the dependency guard

pnpm keeps one content store per user at its default path. A checkout or
worktree installs with `pnpm install --frozen-lockfile --offline` once the store
holds its versions, so worktrees no longer borrow or link another checkout's
`node_modules`.

The pre-push and ci-local guard keeps refusing borrowed dependencies. A symlink
inside a `node_modules` tree is allowed only when it resolves inside the same
checkout: into that checkout's `node_modules/.pnpm`, a `node_modules/.bin`
entry, or a workspace member directory. Any other symlink, and any linked
`node_modules` or ancestor, is refused as before. Store files arrive as
hardlinks, which the guard does not follow.

`scripts/codex-agent.sh` adds the pnpm store and cache paths to the writable
roots it already grants for npm, and drops the npm cache once nothing uses it.

## Images and CI

The web image fetches from the root lockfile and installs only `apps/web` and
its workspace dependency, frozen and offline after the fetch. Its `.output` file
list must match the npm build apart from paths that name the package manager.
The browser harness image installs its own project the same way, with build
scripts off.

CI installs with the pinned pnpm and caches the pnpm store. Source manifests and
staging lists that name `package-lock.json` name `pnpm-lock.yaml` and
`pnpm-workspace.yaml` instead.

Dependabot keeps the `npm` ecosystem, which covers pnpm, for `/` and
`/deploy/dev-https-browser`, with a seven-day cooldown matching
`minimumReleaseAge` and the current ignore lists.

## Migration and acceptance

`pnpm import` converts each `package-lock.json`, so the first lockfile keeps the
resolved versions; the migration changes no dependency version. Then
`pnpm dedupe`.

The change is accepted when:

- every hosted CI job passes on the exact head;
- GitHub's dependency graph lists the workspace packages after the merge;
- the web image's `.output` matches as above;
- no `package-lock.json` and no npm install or run call remains, outside docs
  that record history;
- [resources](../../instructions/resources.md),
  [gotchas](../../instructions/gotchas.md), `CLAUDE.md`, and the component
  guides describe the pnpm commands.

`pnpm licenses list` later feeds the third-party licence notices in the images.

[dbc-15904]: https://github.com/dependabot/dependabot-core/issues/15904
