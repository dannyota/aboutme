# Production Caddy image. Build context is the repository root.

# Caddy 2.11.4 built from source with our pinned Go toolchain (.tool-versions)
# and patched modules instead of the upstream release binary, which lags
# behind both; see deploy/caddy/production/build/main.go. Multi-architecture
# index digest, so the same pin builds amd64 and arm64.
FROM docker.io/library/golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS caddy-build
WORKDIR /src
COPY deploy/caddy/production/build/ ./
RUN GOWORK=off go mod download
RUN CGO_ENABLED=0 GOWORK=off go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/caddy .

FROM docker.io/library/alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS render
RUN apk add --no-cache bash grep openssl
COPY deploy/caddy/production/render.sh /render.sh
COPY deploy/caddy/public-roots.generated.caddy /generated.caddy
RUN bash /render.sh /generated.caddy >/routes.caddy
COPY deploy/caddy/production/maintenance-render.sh /maintenance-render.sh
COPY deploy/caddy/production/maintenance.html /maintenance.html
RUN bash /maintenance-render.sh /maintenance.html >/maintenance.generated.caddy

# Same base and packages the upstream caddy:2.11.4-alpine image runs on, minus
# curl and libcap: curl is not used by our entrypoint or config, and libcap's
# setcap grant is only useful for a non-root process binding a privileged
# port, but this image always runs as root.
FROM docker.io/library/alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk add --no-cache ca-certificates mailcap \
    && mkdir -p /config/caddy /data/caddy /etc/caddy \
    && chmod 1777 /config/caddy /data/caddy
ENV XDG_CONFIG_HOME=/config XDG_DATA_HOME=/data
COPY --from=caddy-build /out/caddy /usr/bin/caddy
COPY deploy/caddy/production/Caddyfile /etc/caddy/Caddyfile
COPY deploy/caddy/production/Caddyfile.maintenance /etc/caddy/Caddyfile.maintenance
COPY --from=render /routes.caddy /etc/caddy/routes.caddy
COPY --from=render /maintenance.generated.caddy /etc/caddy/maintenance.generated.caddy
# file_server's root holds nothing but this one file: no other config or
# secret material sits under it for a path rewrite to ever reach.
COPY deploy/caddy/production/maintenance.html /etc/caddy/maintenance-site/maintenance.html
COPY --chmod=0755 deploy/caddy/production/entrypoint.sh /usr/local/bin/aboutme-caddy
ENTRYPOINT ["/usr/local/bin/aboutme-caddy"]
