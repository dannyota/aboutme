#!/usr/bin/env bash
# Vite reloads every open page when it optimizes a dependency first found at runtime (Vite dependency pre-bundling docs).
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
  local deadline=$((SECONDS + 5))
  while kill -0 -- "-$pid" 2>/dev/null && ((SECONDS < deadline)); do
    sleep 1
  done
  kill -KILL -- "-$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  pid=
}
cleanup() {
  stop_server
  rm -f -- "$log"
}
trap cleanup EXIT

rm -rf -- "$WEB/node_modules/.cache/vite"
# Job control gives the backgrounded server its own process group (bash manual, JOB CONTROL).
set -m
(cd "$WEB" && DEBUG=vite:deps exec node_modules/.bin/nuxt dev \
  --port "$PORT" --host 127.0.0.1 --logLevel verbose --no-tui) \
  >"$log" 2>&1 </dev/null &
pid=$!
set +m

deadline=$((SECONDS + 240))
until grep -aq 'Vite client warmed up' "$log"; do
  if ! kill -0 "$pid" 2>/dev/null; then
    refuse 'the dev server exited before its client warmup finished'
  fi
  ((SECONDS < deadline)) || refuse 'the client warmup did not finish within 240 seconds'
  sleep 1
done
# Vite optimizes each batch after a short debounce and Nuxt names new modules 3 seconds after the last one (Vite, Nuxt dev server).
sleep 5
stop_server

if grep -a 'Vite client warmed up' "$log" | grep -q 'abandoned'; then
  refuse 'the client warmup stopped early, so the crawl is incomplete'
fi
found=$(sed 's/\x1b\[[0-9;]*m//g' "$log" | grep -aE 'new dependencies found|discovered new dependencies at runtime|New dependencies found' || true)
if [ -n "$found" ]; then
  printf '%s\n' "$found" \
    | sed -E 's/^.*([Nn]ew dependencies found|discovered new dependencies at runtime):[[:space:]]*//' \
    | grep -E '^@?[a-zA-Z0-9_.~/-]+(,[[:space:]]*@?[a-zA-Z0-9_.~/-]+)*$' >&2 || true
  printf '%s: the dev server found client dependencies at runtime; add them to vite.optimizeDeps.include in apps/web/nuxt.config.ts\n' "$NAME" >&2
  exit 1
fi
printf '%s: ok (the client warmup found no dependency at runtime)\n' "$NAME"
