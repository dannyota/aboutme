#!/usr/bin/env bash
# Fails when the Nuxt dev server that the HTTPS proofs run finds a client
# dependency at runtime instead of pre-bundling it at startup. Vite then
# optimizes again, and when that changes a dependency a page already loaded,
# it reloads every open page. Add each reported module to
# vite.optimizeDeps.include in apps/web/nuxt.config.ts.
#
# The check starts the dev server on an empty client dependency cache and
# sends it no request, so the startup warmup crawls the whole client module
# graph, lazy routes included. It reads the crawl's verbose log line and
# Vite's dependency debug log. Run it in a checkout with no dev stack: like
# any Nuxt command it writes .nuxt, and it clears node_modules/.cache/vite.
#
# Exit status: 0 when nothing was found at runtime, 1 when something was,
# 2 when the check could not run or the crawl did not finish.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WEB=$ROOT/apps/web
PORT=${DEV_HTTPS_DEPS_CHECK_PORT:-20450}
NAME=dev-https-deps-check

refuse() {
  printf '%s: %s\n' "$NAME" "$1" >&2
  exit 2
}

[ -x "$WEB/node_modules/.bin/nuxt" ] || refuse 'apps/web dependencies are missing'
[[ $PORT =~ ^[0-9]+$ ]] || refuse 'DEV_HTTPS_DEPS_CHECK_PORT must be a port number'
if (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null; then
  refuse "port $PORT is already in use"
fi

log=$(mktemp "${TMPDIR:-/tmp}/$NAME.XXXXXX")
pid=
stop_server() {
  [ -n "$pid" ] || return 0
  kill -TERM -- "-$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  pid=
}
cleanup() {
  stop_server
  rm -f -- "$log"
}
trap cleanup EXIT

rm -rf -- "$WEB/node_modules/.cache/vite"
# Job control gives the server its own process group, so stopping it also
# stops the forked Nuxt child.
set -m
(cd "$WEB" && DEBUG=vite:deps exec node_modules/.bin/nuxt dev \
  --port "$PORT" --host 127.0.0.1 --logLevel verbose --no-tui) \
  >"$log" 2>&1 </dev/null &
pid=$!
set +m

deadline=$((SECONDS + 240))
until grep -aq 'Vite client warmed up' "$log"; do
  if ! kill -0 "$pid" 2>/dev/null; then
    tail -n 20 -- "$log" >&2
    refuse 'the dev server exited before its client warmup finished'
  fi
  ((SECONDS < deadline)) || refuse 'the client warmup did not finish within 240 seconds'
  sleep 1
done
# Vite optimizes each batch it finds after a short debounce, and Nuxt names
# the new modules 3 seconds after the last one.
sleep 5
stop_server

if grep -a 'Vite client warmed up' "$log" | grep -q 'abandoned'; then
  refuse 'the client warmup stopped early, so the crawl is incomplete'
fi
found=$(sed 's/\x1b\[[0-9;]*m//g' "$log" | grep -aE 'new dependencies found|discovered new dependencies at runtime|New dependencies found' || true)
if [ -n "$found" ]; then
  printf '%s\n' "$found" >&2
  printf '%s: the dev server found client dependencies at runtime; add them to vite.optimizeDeps.include in apps/web/nuxt.config.ts\n' "$NAME" >&2
  exit 1
fi
printf '%s: ok (the client warmup found no dependency at runtime)\n' "$NAME"
