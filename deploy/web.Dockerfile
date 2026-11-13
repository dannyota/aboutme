FROM docker.io/library/node:24.22.2-alpine3.24@sha256:8a32c8cf649a49915a66e81e7a609223c708d0adabf288ee3f4077d765542f85 AS build

WORKDIR /src

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

# Busybox grep has no --exclude, so manifests are filtered afterward (BusyBox grep).
RUN set -e; out=apps/web/.output; test -f "$out/server/index.mjs"; \
    hits=$(find "$out" \( -ipath '*node-forge*' -o -ipath '*listhen*' \
      -o -ipath '*/node_modules/braces*' -o -ipath '*/node_modules/micromatch*' \
      -o -ipath '*/node_modules/fast-glob*' -o -ipath '*/node_modules/globby*' \)); \
    if [ -n "$hits" ]; then printf '%s\n' "$hits"; exit 1; fi; \
    rc=0; hits=$(grep -rliE 'node-forge|listhen|micromatch|fast-glob|globby' "$out") || rc=$?; [ "$rc" -le 1 ]; \
    hits=$(printf '%s\n' "$hits" | grep -v '/package\.json$' || true); \
    if [ -n "$hits" ]; then printf '%s\n' "$hits"; exit 1; fi

# Nitro's node-server preset bundles its dependencies into .output (Nitro node-server preset docs).
FROM docker.io/library/node:24.22.2-alpine3.24@sha256:8a32c8cf649a49915a66e81e7a609223c708d0adabf288ee3f4077d765542f85 AS runtime

WORKDIR /app

RUN addgroup -S aboutme && adduser -S aboutme -G aboutme \
    && rm -rf /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/corepack /opt/yarn-v* \
    && rm -f /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack \
        /usr/local/bin/yarn /usr/local/bin/yarnpkg \
    && for tool in npm npx corepack yarn yarnpkg; do \
        ! command -v "$tool" >/dev/null || exit 1; \
    done

COPY --from=build /src/apps/web/.output ./.output

USER aboutme
ENV HOST=0.0.0.0 \
    PORT=3000
EXPOSE 3000

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=10 \
    CMD wget -q -O /dev/null http://127.0.0.1:3000/ || exit 1

CMD ["node", ".output/server/index.mjs"]
