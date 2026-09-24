# Go server image. The repository-root build context includes apps/server and
# its packages/schema/gen/go workspace dependency.

# ---- build ----
# Match the exact Go version declared by apps/server/go.mod. Digest pins the
# multi-arch index so the same reference resolves on amd64 and arm64.
FROM docker.io/library/golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build

WORKDIR /src

# Copy manifests first so source edits retain the dependency layer.
COPY go.work ./
COPY apps/server/go.mod apps/server/go.sum ./apps/server/
COPY packages/schema/gen/go/go.mod ./packages/schema/gen/go/

RUN go -C apps/server mod download

# Migrations are embedded in cmd/migrate. Generated schema sources are a real
# module dependency, not build-time input only.
COPY apps/server/cmd/ ./apps/server/cmd/
COPY apps/server/internal/ ./apps/server/internal/
COPY apps/server/migrations/ ./apps/server/migrations/
COPY packages/schema/gen/go/ ./packages/schema/gen/go/

# The API server and the one-shot database commands share one image.
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/render-browser-supervisor ./cmd/render-browser-supervisor
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/db-setup ./cmd/db-setup

# AWS RDS CA bundle for sslmode=verify-full. A changed upstream bundle fails the
# build until this hash is reviewed and updated.
ARG RDS_BUNDLE_SHA256=e5bb2084ccf45087bda1c9bffdea0eb15ee67f0b91646106e466714f9de3c7e3
RUN wget -qO /out/rds-global-bundle.pem https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem \
    && echo "${RDS_BUNDLE_SHA256}  /out/rds-global-bundle.pem" | sha256sum -c -

# ---- runtime ----
# Multi-architecture index digest, so the same pin builds amd64 and arm64.
FROM mcr.microsoft.com/playwright:v1.62.1-noble@sha256:dcc5531e97840b9b5e794f2814476b21571c5124a3fca2267d73041f56e7580e AS runtime

USER root
# Nothing in this image runs npm at runtime; the base's Node install ships it
# unused (tar, brace-expansion, ip-address, undici, and pacote, all with
# published CVEs). Node itself stays: it's Playwright's base.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && chromium=/ms-playwright/chromium-1234/chrome-linux64 \
    && { [ -d "$chromium" ] || chromium=/ms-playwright/chromium-1234/chrome-linux; } \
    && ln -s "$chromium" /opt/chromium \
    && test -x /opt/chromium/chrome \
    && rm -rf /usr/lib/node_modules/npm \
    && rm -f /usr/bin/npm /usr/bin/npx
ENV CHROMIUM_PATH=/opt/chromium/chrome TZ=UTC LANG=C.UTF-8 LC_ALL=C.UTF-8

COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/render-browser-supervisor /usr/local/bin/render-browser-supervisor
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY --from=build /out/db-setup /usr/local/bin/db-setup
COPY --from=build /out/rds-global-bundle.pem /etc/ssl/rds/global-bundle.pem

USER pwuser
EXPOSE 8080 8081

# Probe the same GET response operators use. HEAD is also supported by the
# route contract, but wget's explicit output target keeps this probe portable.
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=10 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

# Compose overrides this entrypoint for its one-shot migrate service.
ENTRYPOINT ["/usr/local/bin/server"]
