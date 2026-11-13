FROM docker.io/library/golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN for attempt in 1 2 3; do \
      go mod download && exit 0; \
      status=$?; \
      if [ "$attempt" -eq 3 ]; then exit "$status"; fi; \
      echo "go mod download failed (attempt $attempt/3); retrying in $((attempt * 5))s" >&2; \
      sleep "$((attempt * 5))" || exit $?; \
    done

COPY apps/server/cmd/ ./apps/server/cmd/
COPY apps/server/internal/ ./apps/server/internal/
COPY apps/server/migrations/ ./apps/server/migrations/
COPY packages/schema/gen/go/ ./packages/schema/gen/go/

RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/render-browser-supervisor ./cmd/render-browser-supervisor
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/db-setup ./cmd/db-setup

# AWS RDS CA bundle for sslmode=verify-full. A changed upstream bundle fails the
# build until this hash is reviewed and updated.
ARG RDS_BUNDLE_SHA256=90a6f1a7978a1b1b096dca6c57511ca48d3b8cf052f8e704baf358f34ea2f21b
RUN wget -qO /out/rds-global-bundle.pem https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem \
    && echo "${RDS_BUNDLE_SHA256}  /out/rds-global-bundle.pem" | sha256sum -c -

FROM mcr.microsoft.com/playwright:v1.64.0-noble@sha256:06a9939e57531807f8d5fd76ce44b53165ffb7d7501d87ab10e285c20b1e971f AS runtime

USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget \
    && apt-get install -y --no-install-recommends --only-upgrade libssl3t64 openssl libbrotli1 apt libapt-pkg6.0t64 \
    && rm -rf /var/lib/apt/lists/* \
    && chromium=/ms-playwright/chromium-1248/chrome-linux64 \
    && { [ -d "$chromium" ] || chromium=/ms-playwright/chromium-1248/chrome-linux-arm64; } \
    && { [ -d "$chromium" ] || chromium=/ms-playwright/chromium-1248/chrome-linux; } \
    && ln -s "$chromium" /opt/chromium \
    && test -x /opt/chromium/chrome \
    && rm -rf /usr/lib/node_modules/npm /usr/lib/node_modules/corepack \
    && rm -f /usr/bin/npm /usr/bin/npx /usr/bin/corepack \
    && for tool in npm npx corepack; do \
        ! command -v "$tool" >/dev/null || exit 1; \
    done
ENV CHROMIUM_PATH=/opt/chromium/chrome TZ=UTC LANG=C.UTF-8 LC_ALL=C.UTF-8

COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/render-browser-supervisor /usr/local/bin/render-browser-supervisor
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY --from=build /out/db-setup /usr/local/bin/db-setup
COPY --from=build /out/rds-global-bundle.pem /etc/ssl/rds/global-bundle.pem

USER pwuser
EXPOSE 8080 8081

HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=10 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/server"]
