# pnpm workspace

The owner chose pnpm over npm on 2026-11-15. This page owns how the repository
installs JavaScript dependencies. It amends the npm install rules in
[local tooling](local-tooling-simplification.md#owner-choices-and-combined-jobs).

## Projects and lockfiles

One pnpm workspace holds the root, `apps/web`, and `packages/schema`, with one
`pnpm-lock.yaml` at the root. `apps/web` depends on `@aboutme/schema` through
`workspace:*`.

`deploy/dev-https-browser` stays a separate pnpm project with its own
`package.json`, `pnpm-workspace.yaml`, and lockfile. Its image installs from a
closed staged source set, and the application graph must not enter that image. A
static test fails when its pnpm pin or install settings differ from the root's.

No `package-lock.json` remains. Every `npm ci`, `npm install`, `npm run`, and
`npx` call becomes its pnpm form. The npm and corepack removal in the web
runtime image stays.

## Version and installation

Use the latest pnpm 12 release that is at least seven days old, pinned exactly
in `.tool-versions` and in the `packageManager` field of both project roots.
`make tools-check` fails on drift. pnpm refuses to run when its version differs
from `packageManager` and never downloads another pnpm; the implementer names
the pnpm 12 settings that do this and tests both refusals.

Every environment installs that exact version with
`npm install --global --ignore-scripts pnpm@<pin>`: CI, image build stages, the
Mac, and the Ubuntu host. Corepack is not used, because Node.js 25 and later no
longer bundle it.

## Install settings

Both `pnpm-workspace.yaml` files set:

- `minimumReleaseAge: 10080` (seven days) with `minimumReleaseAgeStrict: true`.
  A security fix newer than that goes in `minimumReleaseAgeExclude` by exact
  name, with the advisory in the commit message, and leaves once it is seven
  days old.
- Dependency build scripts run only for packages listed in `allowBuilds`.
  `strictDepBuilds` stays on, so a new build script fails the install until a
  reviewed change allows it.
- `verifyDepsBeforeRun: error`, so `pnpm run` and `pnpm exec` refuse stale
  dependencies and never install. Static tooling keeps its REFUSED and exit 2
  result for missing or stale tools.
- `packageImportMethod: clone-or-copy`, so editing a file in one checkout's
  `node_modules` cannot change the store or another checkout. APFS clones keep
  this cheap on the Mac; ext4 copies.
- The isolated node_modules layout with an in-project virtual store, no hoisting
  settings, and no global virtual store.
- `--frozen-lockfile` for every install outside a deliberate dependency change.

## Store, worktrees, and the dependency guard

pnpm keeps one content store per user at its default path. Only installs that
the owner's sessions run under the managed lock write it. A checkout or worktree
installs with `pnpm install --frozen-lockfile --offline` once the store holds
its versions; when a version is missing, the offline install fails and the
session runs a normal frozen install. Codex sandboxes get no writable store or
cache: the dispatching session installs a worktree before it dispatches a
worker, and `scripts/codex-agent.sh` drops the npm cache root once nothing uses
it.

The pre-push and ci-local guard keeps refusing borrowed dependencies. It walks
the root `node_modules`, including `.pnpm` as a real directory, and every
member's `node_modules`. For each symlink it resolves the full target, and the
target and all its ancestors must be real directories inside the same checkout,
in one of these places:

- the root `node_modules/.pnpm`;
- a `.bin` entry whose target stays in the same `node_modules` tree;
- a workspace member listed in `pnpm-workspace.yaml` and named by a `workspace:`
  dependency in the linking project's `package.json`.

Any other symlink, and any linked `node_modules` or ancestor, is refused as
before.

## Images, source maps, and CI

The web image fetches from the root lockfile and installs only `apps/web` and
its workspace dependency, frozen and offline after the fetch. The harness image
installs its own project the same way, with build scripts off.

The web image's closed copy map, `scripts/web-e2e-source.manifest`, and the
template pages' input closure in `apps/web/scripts/validate-template-pages.mts`
name the root `package.json`, `pnpm-workspace.yaml`, and `pnpm-lock.yaml` in
place of `package-lock.json`. The template fingerprint covers the lockfile's
importer entries for `apps/web` and `packages/schema` and every package and
snapshot entry they reach, and follows `workspace:` sources the way it follows
`file:` sources today.

CI installs with the pinned pnpm and caches the pnpm store.

Dependabot keeps the `npm` ecosystem, which covers pnpm, for `/` and
`/deploy/dev-https-browser`, with a seven-day cooldown matching
`minimumReleaseAge` and the current ignore lists.

## Migration and acceptance

The migration changes no resolved dependency version. The implementer seeds the
lockfiles from the four `package-lock.json` files with `pnpm import` where it
applies. Then, for each original root, it compares the name and version set npm
installed against pnpm's resolution for that importer, and lists any difference
with its cause for review. `pnpm dedupe` waits for a later dependency change.

The change is accepted when:

- every hosted CI job passes on the exact head;
- GitHub's dependency graph lists the workspace and harness packages after the
  merge;
- the linux/amd64 web image built from the head has the same `.output` path list
  and file hashes as the one built from the migration's base commit. The report
  names every differing file and its cause, and only build metadata may differ;
- no `package-lock.json` and no npm install or run call remains, outside docs
  that record history and the pnpm bootstrap line;
- [resources](../../instructions/resources.md),
  [gotchas](../../instructions/gotchas.md), `CLAUDE.md`, and the component
  guides describe the pnpm commands.

`pnpm licenses list` later feeds the third-party licence notices in the images.
