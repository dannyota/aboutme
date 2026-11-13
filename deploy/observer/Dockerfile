FROM docker.io/library/golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build

WORKDIR /src

# Alpine's base image ships no CA bundle (Alpine docker image).
RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN for attempt in 1 2 3; do \
      go mod download && exit 0; \
      status=$?; \
      if [ "$attempt" -eq 3 ]; then exit "$status"; fi; \
      echo "go mod download failed (attempt $attempt/3); retrying in $((attempt * 5))s" >&2; \
      sleep "$((attempt * 5))" || exit $?; \
    done

COPY deploy/observer/ ./deploy/observer/

ARG VERSION=""
ARG COMMIT=""
RUN CGO_ENABLED=0 go -C deploy/observer build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/observer ./cmd/observer

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/observer /observer

USER 65532:65532

ENTRYPOINT ["/observer"]
CMD ["lambda"]
