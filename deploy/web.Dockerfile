# Multi-stage build for apps/web (Nuxt 4 / Vue 3).
# Build context is the REPO ROOT (set in deploy/compose.yml), not apps/web/:
# apps/web now has a real dependency on @aboutme/schema, a local `file:`
# reference to packages/schema (see apps/web/package.json — this repo has no
# npm workspaces, so a plain relative file: dependency is what npm resolves
# and symlinks), so the build needs to see both directories, not just
# apps/web/. This file lives in deploy/ per the exclusive-ownership split for
# Task C2.

# ---- build ----
# Node 24.18.1: the current Node 24 "Krypton" LTS patch as of 2026-08-01.
# apps/web has no committed Node version pin yet (no .nvmrc / package.json
# "engines") — this is a manual match to what's used locally; keep it in
# sync until apps/web adds a committed pin.
FROM docker.io/library/node:24.18.1-alpine3.24 AS build

WORKDIR /src

# apps/web's package.json/lock plus the config files `postinstall: nuxt
# prepare` needs, and @aboutme/schema's own package.json + the generated TS
# types its "exports"/"types" point at (packages/schema/gen/ts/) — nothing
# else from packages/schema is needed to install or build (resume.schema.json
# and the Go/generator/test trees are irrelevant to `npm ci`/`nuxt build`).
# Copied before `npm ci` so this layer only invalidates on dependency/config
# changes, not every source edit. `nuxt prepare` doesn't require app/ to
# exist (verified: an isolated `npm ci` with only these files succeeds).
COPY apps/web/package.json apps/web/package-lock.json apps/web/nuxt.config.ts apps/web/tsconfig.json ./apps/web/
COPY packages/schema/package.json ./packages/schema/
COPY packages/schema/gen/ts/ ./packages/schema/gen/ts/

RUN npm --prefix apps/web ci

COPY apps/web/app/ ./apps/web/app/

RUN npm --prefix apps/web run build

# ---- runtime ----
# Nitro's node-server preset (Nuxt's default) bundles its own dependencies
# into .output/, so the runtime stage needs no node_modules install.
FROM docker.io/library/node:24.18.1-alpine3.24 AS runtime

WORKDIR /app

RUN addgroup -S aboutme && adduser -S aboutme -G aboutme

COPY --from=build /src/apps/web/.output ./.output

USER aboutme
ENV HOST=0.0.0.0 \
    PORT=3000
EXPOSE 3000

# Plain GET with output discarded, not `--spider` (HEAD) — consistent with
# server.Dockerfile's healthcheck; avoids depending on HEAD support.
HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=10 \
    CMD wget -q -O /dev/null http://127.0.0.1:3000/ || exit 1

CMD ["node", ".output/server/index.mjs"]
