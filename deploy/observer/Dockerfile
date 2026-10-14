# Deployment transparency observer image
# (docs/design/deployment-transparency/README.md). The build context is the
# repository root; this image copies only deploy/observer/, which is its own
# Go module outside go.work.

# ---- build ----
# Match the module's own go.mod (go 1.27.2). Digest pins the multi-arch index
# so the same reference resolves on amd64 and arm64.
FROM docker.io/library/golang:1.27.2-alpine3.24@sha256:85dc1069ac644ea3c527b177303a406eb3358192816cd7f9e5848eb658851673 AS build

WORKDIR /src

# alpine's base image ships no CA bundle; the runtime stage below has no
# package manager to install one itself.
RUN apk add --no-cache ca-certificates

COPY deploy/observer/go.mod deploy/observer/go.sum ./
# Retry proxy transport failures without changing module checksum verification.
RUN for attempt in 1 2 3; do \
      GOWORK=off go mod download && exit 0; \
      status=$?; \
      if [ "$attempt" -eq 3 ]; then exit "$status"; fi; \
      echo "go mod download failed (attempt $attempt/3); retrying in $((attempt * 5))s" >&2; \
      sleep "$((attempt * 5))" || exit $?; \
    done

COPY deploy/observer/ ./

ARG VERSION=""
ARG COMMIT=""
RUN GOWORK=off CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/observer ./cmd/observer

# ---- runtime ----
# No shell and no package manager: the observer makes only outbound HTTPS
# calls, writes only its TUF cache under /tmp, and never runs another program
# (docs/design/deployment-transparency/README.md, "Where the observer runs").
FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/observer /observer

USER 65532:65532

ENTRYPOINT ["/observer"]
CMD ["lambda"]
