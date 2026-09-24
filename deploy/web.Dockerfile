# Multi-stage build for apps/web (Nuxt 4 / Vue 3).
# Build context is the repository root: apps/web depends on packages/schema
# through a relative `file:` reference, so the build needs both directories.

# ---- build ----
# Node 24.21.0: pinned exactly to apps/web/.nvmrc. Digest pins the
# multi-arch index so the same reference resolves on amd64 and arm64.
FROM docker.io/library/node:24.21.0-alpine3.24@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS build

WORKDIR /src

# `npm ci` runs `nuxt prepare`, which loads nuxt.config.ts, and that config
# builds validators from app/, server/, the OpenAPI contract and the schema
# package. So every build input is copied before install. The list mirrors the
# non-test entries of scripts/web-e2e-source.manifest.
COPY apps/web/package.json apps/web/package-lock.json apps/web/nuxt.config.ts apps/web/tsconfig.json ./apps/web/
COPY apps/web/types/ ./apps/web/types/
COPY apps/web/app/ ./apps/web/app/
COPY apps/web/public/ ./apps/web/public/
COPY apps/web/server/ ./apps/web/server/
COPY docs/api/openapi.yaml ./docs/api/openapi.yaml
COPY packages/schema/package.json packages/schema/resume.schema.json ./packages/schema/
COPY packages/schema/gen/ts/ ./packages/schema/gen/ts/
COPY packages/schema/samples/ ./packages/schema/samples/
COPY packages/schema/fixtures/full.json packages/schema/fixtures/vn-full.json ./packages/schema/fixtures/
COPY packages/schema/validation/store.ts ./packages/schema/validation/

RUN npm --prefix apps/web ci

RUN npm --prefix apps/web run build

# ---- runtime ----
# Nitro's node-server preset (Nuxt's default) bundles its own dependencies
# into .output/, so the runtime stage needs no node_modules install.
FROM docker.io/library/node:24.21.0-alpine3.24@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS runtime

WORKDIR /app

# The runtime only runs `node .output/server/index.mjs`; it never invokes
# npm, npx, or corepack, so their bundled dependencies (npm ships tar,
# brace-expansion, ip-address, undici, and pacote, all with published CVEs)
# are removed rather than shipped unused.
RUN addgroup -S aboutme && adduser -S aboutme -G aboutme \
    && rm -rf /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/corepack \
    && rm -f /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack

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
