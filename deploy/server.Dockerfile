# Go server image. The repository-root build context includes apps/server and
# its packages/schema/gen/go workspace dependency.

# ---- build ----
# Match the exact Go version declared by apps/server/go.mod.
FROM docker.io/library/golang:1.27.1-alpine3.24 AS build

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

# The API server and one-shot migration runner share one image.
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# ---- runtime ----
FROM mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac AS runtime

USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && ln -s /ms-playwright/chromium-1234/chrome-linux64 /opt/chromium
ENV CHROMIUM_PATH=/opt/chromium/chrome TZ=UTC LANG=C.UTF-8 LC_ALL=C.UTF-8

COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/migrate /usr/local/bin/migrate

USER pwuser
EXPOSE 8080 8081

# Probe the same GET response operators use. HEAD is also supported by the
# route contract, but wget's explicit output target keeps this probe portable.
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=10 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

# Compose overrides this entrypoint for its one-shot migrate service.
ENTRYPOINT ["/usr/local/bin/server"]
