# Multi-stage build for apps/server (module github.com/dannyota/aboutme/apps/server).
# Build context is the REPO ROOT (set in deploy/compose.yml), not apps/server/:
# apps/server now has a real module dependency on packages/schema/gen/go (see
# apps/server/go.mod's require + replace and the repo-root go.work), so the
# build needs to see both directories, not just apps/server/. This file lives
# in deploy/ per the exclusive-ownership split for Task C2.

# ---- build ----
# Pinned to the exact go.mod version (go 1.26.5) so the toolchain matches
# what CI/local `go build` uses — no implicit toolchain download.
FROM docker.io/library/golang:1.26.5-alpine3.24 AS build

WORKDIR /src

# Dependency manifests only — go.work, apps/server's go.mod/go.sum, and the
# schema module's own go.mod (its replace target; `go mod download` needs
# that file present to resolve the module graph, but not its .go sources
# yet) — copied before any application source so this layer only
# invalidates on dependency changes, not every source edit.
COPY go.work ./
COPY apps/server/go.mod apps/server/go.sum ./apps/server/
COPY packages/schema/gen/go/go.mod ./packages/schema/gen/go/

RUN go -C apps/server mod download

# Application source, including migrations/ (embedded via go:embed for
# cmd/migrate — see apps/server/migrations/migrations.go) and the schema
# module's real sources (its go.mod alone, copied above, is only enough for
# dependency resolution, not for compiling against its types).
COPY apps/server/cmd/ ./apps/server/cmd/
COPY apps/server/internal/ ./apps/server/internal/
COPY apps/server/migrations/ ./apps/server/migrations/
COPY packages/schema/gen/go/ ./packages/schema/gen/go/

# Two binaries: the API server, and cmd/migrate (deploy/compose.yml's
# one-shot migration service applies embedded goose migrations with this
# before the server ever starts — spec §3 "Prod migration sequence").
RUN go -C apps/server build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN go -C apps/server build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# ---- runtime ----
FROM docker.io/library/alpine:3.24 AS runtime

# --- Chromium seam (do not fill in yet) -----------------------------------
# A later phase adds chromedp-driven PDF/og-image rendering here (spec §2,
# §6): "Chromium runs inside the Go server task ... whole-task cgroup
# budget, 1 render at a time initially." When that phase lands, install
# Chromium in this stage (e.g. `apk add --no-cache chromium` plus its font/
# fontconfig deps) and wire CHROMEDP_* / render-queue config. Until then
# this image stays a minimal static-binary runtime with no browser.
# ---------------------------------------------------------------------------

RUN apk add --no-cache ca-certificates wget \
    && addgroup -S aboutme \
    && adduser -S aboutme -G aboutme

COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/migrate /usr/local/bin/migrate

USER aboutme
EXPOSE 8080

# Plain GET with output discarded, not `--spider` (HEAD): apps/server's
# router requires an exact method match per route rather than stdlib
# ServeMux's implicit GET-matches-HEAD, so a HEAD probe gets 405.
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=10 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

# Default entrypoint is the API server; deploy/compose.yml's one-shot
# "migrate" service overrides this (entrypoint: ["/usr/local/bin/migrate"])
# to run the same image's migrate binary instead.
ENTRYPOINT ["/usr/local/bin/server"]
