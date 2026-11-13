FROM docker.io/library/golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS caddy-build
WORKDIR /src
COPY deploy/caddy/production/build/ ./
RUN for attempt in 1 2 3; do \
      go mod download && exit 0; \
      status=$?; \
      if [ "$attempt" -eq 3 ]; then exit "$status"; fi; \
      echo "go mod download failed (attempt $attempt/3); retrying in $((attempt * 5))s" >&2; \
      sleep "$((attempt * 5))" || exit $?; \
    done
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/caddy .

FROM docker.io/library/alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS render
RUN apk add --no-cache bash grep openssl
COPY deploy/caddy/production/render.sh /render.sh
COPY deploy/caddy/public-roots.generated.caddy /generated.caddy
RUN bash /render.sh /generated.caddy >/routes.caddy
COPY deploy/caddy/production/maintenance-render.sh /maintenance-render.sh
COPY deploy/caddy/production/maintenance.html /maintenance.html
RUN bash /maintenance-render.sh /maintenance.html >/maintenance.generated.caddy

FROM docker.io/library/alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache ca-certificates mailcap \
    && addgroup -S -g 10001 caddy \
    && adduser -S -D -H -u 10001 -G caddy -h /data -s /sbin/nologin caddy \
    && mkdir -p /config/caddy /data/caddy /etc/caddy /run/caddy /var/log/caddy/waf /var/log/caddy/crowdsec \
    && chown 10001:10001 /config/caddy /data/caddy /var/log/caddy/waf /var/log/caddy/crowdsec \
    && chmod 0700 /config/caddy /data/caddy /var/log/caddy/waf /var/log/caddy/crowdsec \
    && chmod 1777 /run/caddy
ENV XDG_CONFIG_HOME=/config XDG_DATA_HOME=/data
COPY --from=caddy-build /out/caddy /usr/bin/caddy
COPY deploy/caddy/production/Caddyfile /etc/caddy/Caddyfile
COPY deploy/caddy/production/Caddyfile.maintenance /etc/caddy/Caddyfile.maintenance
COPY deploy/caddy/production/edges/ /etc/caddy/edges/
COPY deploy/caddy/production/coraza/ /etc/caddy/coraza/
COPY --from=render /routes.caddy /etc/caddy/routes.caddy
COPY --from=render /maintenance.generated.caddy /etc/caddy/maintenance.generated.caddy
# file_server's root holds nothing but this one file: no other config or
# secret material sits under it for a path rewrite to ever reach.
COPY deploy/caddy/production/maintenance.html /etc/caddy/maintenance-site/maintenance.html
COPY --chmod=0755 deploy/caddy/production/entrypoint.sh /usr/local/bin/aboutme-caddy
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/aboutme-caddy"]
