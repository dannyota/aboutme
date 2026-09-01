#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
cd "$ROOT"

fail() {
  printf 'render-topology-test: %s\n' "$*" >&2
  exit 1
}

node --input-type=module - "$ROOT" <<'NODE'
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { parse } from 'yaml';

const root = process.argv[2];
const compose = parse(readFileSync(join(root, 'deploy/compose.yml'), 'utf8'));
const services = compose.services;
const networkNames = (service) =>
  Array.isArray(service.networks) ? service.networks : Object.keys(service.networks ?? {});

assert.deepEqual(compose.networks.render, {
  internal: true,
  ipam: { config: [{ subnet: '10.91.0.0/28' }] },
});
assert.deepEqual(
  Object.entries(services)
    .filter(([, service]) => networkNames(service).includes('render'))
    .map(([name]) => name)
    .sort(),
  ['server', 'web'],
);
assert.deepEqual(networkNames(services.server).sort(), ['db', 'edge', 'media', 'render']);
assert.equal(services.server.networks.edge.ipv4_address, '10.90.0.2');
assert.equal(services.server.environment.LISTEN_HOST, '10.90.0.2');
assert.equal(services.server.environment.PUBLIC_RENDER_ORIGIN, 'http://web:3000');
assert.equal(services.server.environment.MCP_ENABLED, '${MCP_ENABLED:-false}');
assert.match(services.server.environment.APP_BUILD_DIGEST, /^\$\{APP_BUILD_DIGEST:/);
assert.match(
  services.server.environment.PUBLIC_RENDERER_BUILD_DIGEST,
  /^\$\{PUBLIC_RENDERER_BUILD_DIGEST:/,
);
assert.equal(
  services.server.healthcheck.test.at(-1),
  'http://10.90.0.2:8080/healthz',
);
assert.deepEqual(networkNames(services.web).sort(), ['frontend', 'render']);
assert.equal(networkNames(services.web).includes('edge'), false);
for (const name of ['postgres', 'migrate', 'media', 'media-init', 'caddy']) {
  assert.equal(networkNames(services[name]).includes('render'), false, `${name} joined render`);
}
for (const name of ['server', 'web']) {
  assert.equal('ports' in services[name], false, `${name} publishes a render listener`);
}
assert.doesNotMatch(
  String(services.server.environment.TRUSTED_PROXY_CIDRS),
  /10\.91\./,
);
NODE

node scripts/generate-public-roots.mjs --check >/dev/null ||
  fail 'generated public-root outputs are stale'

native=$(<scripts/dev-native.sh)
https=$(<scripts/dev-https.sh)
dockerfile=$(<deploy/web.Dockerfile)

[[ $native == *'PUBLIC_RENDER_ORIGIN=http://127.0.0.1:20030'* ]] ||
  fail 'native direct render origin is missing'
[[ $https == *'PUBLIC_RENDER_ORIGIN=http://127.0.0.1:20440'* ]] ||
  fail 'HTTPS direct render origin is missing'
for name in APP_BUILD_DIGEST PUBLIC_RENDERER_BUILD_DIGEST; do
  [[ $native == *"$name"* ]] || fail "native $name is missing"
  [[ $https == *"$name"* ]] || fail "HTTPS $name is missing"
done
for name in ABOUTME_DEV_STATE_DIR ABOUTME_DEV_MEDIA_DIR; do
  [[ $native == *"$name"* ]] || fail "native $name override is missing"
done
[[ $dockerfile == *'COPY apps/web/server/ ./apps/web/server/'* ]] ||
  fail 'web image does not copy server routes before build'

path_fixture=$(mktemp -d "$ROOT/render-topology.XXXXXX")
cleanup() {
  rm -rf -- "$path_fixture"
}
trap cleanup EXIT
mkdir "$path_fixture/target"
ln -s target "$path_fixture/link"
for value in / "$ROOT" /tmp/aboutme-native-outside "$path_fixture/link"; do
  if ABOUTME_DEV_STATE_DIR="$value" bash scripts/dev-native.sh status >/dev/null 2>&1; then
    fail "native state override accepted unsafe path: $value"
  fi
done
set +e
path_output=$(ABOUTME_DEV_STATE_DIR="${path_fixture#"$ROOT"/}/state" \
  ABOUTME_DEV_MEDIA_DIR="${path_fixture#"$ROOT"/}/media" \
  bash scripts/dev-native.sh status 2>&1)
path_status=$?
set -e
[ "$path_status" -ne 0 ] || fail 'empty isolated native state unexpectedly reported running'
[[ $path_output == *'SERVICE'* ]] || fail 'safe repository-rooted native overrides were rejected'
[[ $path_output != *'must stay below'* && $path_output != *'must not traverse'* ]] ||
  fail 'safe repository-rooted native overrides failed validation'

printf '%s\n' 'render topology tests: PASS'
