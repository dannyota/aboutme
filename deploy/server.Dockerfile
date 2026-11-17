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


FROM mcr.microsoft.com/playwright:v1.64.0-noble@sha256:06a9939e57531807f8d5fd76ce44b53165ffb7d7501d87ab10e285c20b1e971f AS chromium

RUN find /ms-playwright/chromium-1248 -type d -exec chmod 0755 {} + \
    && find /ms-playwright/chromium-1248 -type f -exec sh -c 'for file; do \
        case "$(head -c 4 "$file" | od -An -tx1 | tr -d " \n")" in \
            7f454c46|2321*) chmod 0755 "$file" ;; \
            *) chmod 0644 "$file" ;; \
        esac; \
    done' sh {} +

FROM docker.io/library/ubuntu:noble-20260917@sha256:534baea6a22c03a63003dbc8dbe78fe34bc0d7e595d9a9dc9834884ff530eb55 AS runtime

ARG DEBIAN_FRONTEND=noninteractive
ARG UBUNTU_SNAPSHOT=20261114T000000Z
# snapshot.ubuntu.com redirects HTTP to HTTPS, and ubuntu:noble has no CA bundle until ca-certificates installs.
RUN --mount=type=bind,from=chromium,source=/etc/ssl/certs/ca-certificates.crt,target=/run/bootstrap-ca.crt \
    --mount=type=bind,source=deploy/apt-retry.sh,target=/run/apt-retry.sh \
    printf '%s\n' 'Types: deb' 'URIs: https://snapshot.ubuntu.com/ubuntu/'"${UBUNTU_SNAPSHOT}"/ \
        'Suites: noble noble-updates noble-backports noble-security' \
        'Components: main universe restricted multiverse' \
        'Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg' \
        > /etc/apt/sources.list.d/ubuntu.sources \
    && apt="apt-get -o Acquire::Retries=5 -o Acquire::https::CAInfo=/run/bootstrap-ca.crt" \
    && sh /run/apt-retry.sh $apt update \
    && sh /run/apt-retry.sh $apt -y --no-install-recommends upgrade \
    && sh /run/apt-retry.sh $apt -y --no-install-recommends install \
        ca-certificates fonts-freefont-ttf fonts-ipafont-gothic fonts-liberation fonts-noto-color-emoji \
        fonts-tlwg-loma-otf fonts-unifont fonts-wqy-zenhei libasound2t64 libatk-bridge2.0-0t64 \
        libatk1.0-0t64 libatspi2.0-0t64 libcairo2 libcups2t64 libdbus-1-3 libdrm2 libfontconfig1 \
        libfreetype6 libgbm1 libglib2.0-0t64 libnspr4 libnss3 libpango-1.0-0 libx11-6 libxcb1 \
        libxcomposite1 libxdamage1 libxext6 libxfixes3 libxkbcommon0 libxrandr2 wget \
        xfonts-cyrillic xfonts-scalable \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1001 pwuser \
    && useradd --uid 1001 --gid 1001 --groups users --comment ',,,' --create-home --shell /bin/bash pwuser

COPY --from=chromium --chown=0:0 /ms-playwright/chromium-1248 /ms-playwright/chromium-1248
RUN chromium=/ms-playwright/chromium-1248/chrome-linux64 \
    && { [ -d "$chromium" ] || chromium=/ms-playwright/chromium-1248/chrome-linux-arm64; } \
    && { [ -d "$chromium" ] || chromium=/ms-playwright/chromium-1248/chrome-linux; } \
    && ln -s "$chromium" /opt/chromium \
    && test -x /opt/chromium/chrome
ENV CHROMIUM_PATH=/opt/chromium/chrome TZ=UTC LANG=C.UTF-8 LC_ALL=C.UTF-8

COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/render-browser-supervisor /usr/local/bin/render-browser-supervisor
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY --from=build /out/db-setup /usr/local/bin/db-setup

USER pwuser
EXPOSE 8080 8081

HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=10 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/server"]
