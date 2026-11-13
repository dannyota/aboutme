#!/usr/bin/env bash
set -Eeuo pipefail
export LC_ALL=C
cd "$(dirname "${BASH_SOURCE[0]}")/.."
# shellcheck source=scripts/lib/platform.sh
source scripts/lib/platform.sh
ensure_dev_ignored

commit=$(git rev-parse --verify 'HEAD^{commit}')
out=".dev/web-source-build/$commit/source.tar"
rm -rf ".dev/web-source-build/$commit"
install -d -m 0700 ".dev/web-source-build/$commit"

scripts/web-e2e-source.sh "$commit" scripts/web-e2e-source.manifest "$out" >/dev/null

work=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-web-source-build.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/aboutme"
tar -xf "$out" -C "$work/aboutme"

cd "$work/aboutme/apps/web"
npm ci --ignore-scripts >/dev/null
NUXT_HARNESS=1 node node_modules/nuxt/bin/nuxt.mjs build
node node_modules/nuxt/bin/nuxt.mjs build
printf 'web-source-build: source manifest is complete\n'
