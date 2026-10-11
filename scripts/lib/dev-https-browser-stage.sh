#!/usr/bin/env bash
# Shared per-run spec staging for the dev-https browser image, sourced by
# scripts/dev-https-check.sh and scripts/totp-production-proof.sh. Copies the
# exact spec fileset run.sh's validate_spec_dir expects into a fresh, owner-
# matched, mode-0700 directory: the live source tree always carries extra
# files (Dockerfile, run.sh itself, and so on), which validate_spec_dir
# rejects (deploy/dev-https-browser/run.sh "validate_spec_dir").
set -Eeuo pipefail

# stage_dev_https_browser_specs <context-dir> <staging-dir>
#
# <staging-dir> must already exist (mktemp -d); this sets it to mode 0700
# and copies every spec-sources.txt entry into it at mode
# 0600. The caller owns removing <staging-dir> on every exit.
stage_dev_https_browser_specs() {
  local context=$1 dest=$2 path
  local -a sources=()
  mapfile -t sources <"$context/spec-sources.txt" || return 1
  for path in "${sources[@]}"; do
    if [[ ! $path =~ ^[a-z0-9][a-z0-9.-]*\.(ts|mjs)$ ]]; then
      printf 'dev-https-browser-stage: spec source %s is not a plain .ts or .mjs basename\n' \
        "$path" >&2
      return 1
    fi
  done
  chmod 0700 "$dest" || return 1
  for path in "${sources[@]}"; do
    if [ ! -f "$context/$path" ] || [ -L "$context/$path" ]; then
      printf 'dev-https-browser-stage: spec source %s is missing or not a regular file\n' \
        "$path" >&2
      return 1
    fi
    cp -- "$context/$path" "$dest/$path" || return 1
    chmod 0600 "$dest/$path" || return 1
  done
}
