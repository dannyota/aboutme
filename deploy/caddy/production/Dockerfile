# Production Caddy image. Build context is the repository root.
FROM docker.io/library/alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS render
RUN apk add --no-cache bash grep openssl
COPY deploy/caddy/production/render.sh /render.sh
COPY deploy/caddy/public-roots.generated.caddy /generated.caddy
RUN bash /render.sh /generated.caddy >/routes.caddy
COPY deploy/caddy/production/maintenance-render.sh /maintenance-render.sh
COPY deploy/caddy/production/maintenance.html /maintenance.html
RUN bash /maintenance-render.sh /maintenance.html >/maintenance.generated.caddy

FROM docker.io/library/caddy:2.11.4-alpine@sha256:5f5c8640aae01df9654968d946d8f1a56c497f1dd5c5cda4cf95ab7c14d58648
COPY deploy/caddy/production/Caddyfile /etc/caddy/Caddyfile
COPY deploy/caddy/production/Caddyfile.maintenance /etc/caddy/Caddyfile.maintenance
COPY --from=render /routes.caddy /etc/caddy/routes.caddy
COPY --from=render /maintenance.generated.caddy /etc/caddy/maintenance.generated.caddy
# file_server's root holds nothing but this one file: no other config or
# secret material sits under it for a path rewrite to ever reach.
COPY deploy/caddy/production/maintenance.html /etc/caddy/maintenance-site/maintenance.html
COPY --chmod=0755 deploy/caddy/production/entrypoint.sh /usr/local/bin/aboutme-caddy
ENTRYPOINT ["/usr/local/bin/aboutme-caddy"]
