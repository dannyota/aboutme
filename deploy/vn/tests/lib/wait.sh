# shellcheck shell=bash

test_wait() {
  local name=$1 seconds=$2 interval=$3 deadline
  shift 3
  deadline=$((SECONDS + seconds))
  until "$@"; do
    if ((SECONDS >= deadline)); then
      printf 'test wait timed out after %ss: %s\n' "$seconds" "$name" >&2
      return 1
    fi
    "${ABOUTME_TEST_SLEEP:-sleep}" "$interval"
  done
}
