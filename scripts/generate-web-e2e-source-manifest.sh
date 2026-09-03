#!/usr/bin/env bash

# Generate the reviewed browser source set from fixed, Git-visible roots.
set -Eeuo pipefail
export LC_ALL=C

fail() {
  printf 'web-e2e-source-manifest: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 1 ] ||
  fail 'usage: scripts/generate-web-e2e-source-manifest.sh --check|--update'
case $1 in
  --check | --update) mode=$1 ;;
  *) fail 'usage: scripts/generate-web-e2e-source-manifest.sh --check|--update' ;;
esac

root=$(git rev-parse --show-toplevel 2>/dev/null) ||
  fail 'current directory is not a Git worktree'
root=$(realpath "$root")
cd "$root"

manifest=scripts/web-e2e-source.manifest
candidate=$(mktemp "$root/scripts/.web-e2e-source.manifest.XXXXXX")
paths=$(mktemp "$root/scripts/.web-e2e-source.paths.XXXXXX")
cleanup() {
  rm -f -- "$candidate" "$paths"
}
trap cleanup EXIT

source_roots=(
  apps/web/app
  apps/web/e2e
  apps/web/nuxt.config.ts
  apps/web/package-lock.json
  apps/web/package.json
  apps/web/public
  apps/web/server
  apps/web/test/sanitizer/neutralization.ts
  apps/web/tsconfig.json
  apps/web/types
  docs/api/openapi.yaml
  packages/schema/fixtures/full.json
  packages/schema/fixtures/vn-full.json
  packages/schema/gen/ts
  packages/schema/package.json
  packages/schema/resume.schema.json
  packages/schema/validation/store.ts
)

required_files=(
  apps/web/nuxt.config.ts
  apps/web/package-lock.json
  apps/web/package.json
  apps/web/test/sanitizer/neutralization.ts
  apps/web/tsconfig.json
  docs/api/openapi.yaml
  packages/schema/fixtures/full.json
  packages/schema/fixtures/vn-full.json
  packages/schema/package.json
  packages/schema/resume.schema.json
  packages/schema/validation/store.ts
)

git ls-files --cached --others --exclude-standard -z -- \
  "${source_roots[@]}" >"$paths" || fail 'could not enumerate source paths'

is_secret_like() {
  local path=$1 component lower
  local -a components=()
  IFS=/ read -r -a components <<<"$path"
  for component in "${components[@]}"; do
    lower=${component,,}
    case $lower in
      .env* | .git* | .dev | .superpowers | node_modules | .nuxt | .output | \
        dist | coverage | test-results | playwright-report | credentials* | \
        secrets* | id_rsa* | id_ed25519* | *.key | *.pem)
        return 0
        ;;
    esac
  done
  return 1
}

while IFS= read -r -d '' path; do
  case $path in
    apps/web/app/assets/fonts/README.md | \
      apps/web/app/assets/fonts/tools/*)
      continue
      ;;
  esac
  [ -e "$path" ] || continue
  [ -f "$path" ] && [ ! -L "$path" ] ||
    fail "source path is not a regular file: $path"
  [[ $path != *[$'\001'-$'\037'$'\177']* ]] ||
    fail 'source paths cannot contain control characters'
  is_secret_like "$path" &&
    fail "secret-like source path is forbidden: $path"
  printf '%s\n' "$path"
done <"$paths" | sort -u >"$candidate"

for path in "${required_files[@]}"; do
  grep -Fxq -- "$path" "$candidate" || fail "required source file missing: $path"
done

if [ "$mode" = --check ]; then
  if [ ! -f "$manifest" ] || ! cmp -s "$manifest" "$candidate"; then
    if [ -f "$manifest" ]; then
      diff -u -- "$manifest" "$candidate" || true
    fi
    fail 'source manifest drifted; run make web-source-manifest-update'
  fi
  printf 'web-e2e-source-manifest: source manifest is current\n'
  exit 0
fi

chmod 0644 "$candidate"
mv -f -- "$candidate" "$manifest"
printf 'web-e2e-source-manifest: updated %s\n' "$manifest"
