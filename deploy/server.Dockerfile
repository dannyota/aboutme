# Go server image. The repository-root build context includes apps/server and
# its packages/schema/gen/go workspace dependency.

# ---- build ----
# Match the exact Go version declared by apps/server/go.mod.
FROM docker.io/library/golang:1.27.0-alpine3.24 AS build

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
RUN go -C apps/server build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN go -C apps/server build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# ---- runtime ----
FROM docker.io/library/alpine:3.24 AS runtime

# Chromium and its pinned fonts arrive with the render phase. Until then this
# remains a minimal runtime. See docs/design/system.md and docs/design/fonts.md.
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S aboutme \
    && adduser -S aboutme -G aboutme

COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/migrate /usr/local/bin/migrate

USER aboutme
EXPOSE 8080

# Probe the same GET response operators use. HEAD is also supported by the
# route contract, but wget's explicit output target keeps this probe portable.
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=10 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

# Compose overrides this entrypoint for its one-shot migrate service.
ENTRYPOINT ["/usr/local/bin/server"]
