#!/usr/bin/env bash
# Overlaps independent work inside one hosted CI job. `start NAME CMD...` runs
# CMD detached, with its output in a log, and returns at once; a later step
# runs `wait NAME [SECONDS]`, which waits for CMD, prints its log in a
# collapsed group, and exits with CMD's status. Files live under
# CI_BACKGROUND_DIR (default $RUNNER_TEMP/ci-background).
set -Eeuo pipefail

usage() {
  echo 'usage: scripts/ci-background.sh start NAME CMD... | wait NAME [SECONDS]' >&2
  exit 64
}

[ "$#" -ge 2 ] || usage
action=$1
name=$2
shift 2
[[ $name =~ ^[a-z0-9][a-z0-9-]*$ ]] || usage
if [ -n "${CI_BACKGROUND_DIR-}" ]; then
  dir=$CI_BACKGROUND_DIR
elif [ -n "${RUNNER_TEMP-}" ]; then
  dir=$RUNNER_TEMP/ci-background
else
  echo 'ci-background: set CI_BACKGROUND_DIR or RUNNER_TEMP' >&2
  exit 64
fi
log=$dir/$name.log
status_file=$dir/$name.status

case $action in
start)
  [ "$#" -ge 1 ] || usage
  mkdir -p "$dir"
  [ ! -e "$log" ] || {
    echo "ci-background: $name was already started" >&2
    exit 1
  }
  : >"$log"
  # The status file appears only once CMD has exited, by rename, so a waiter
  # never reads a partial write.
  nohup bash -c '"$@" >>"$0.log" 2>&1; echo $? >"$0.status.tmp"; mv "$0.status.tmp" "$0.status"' \
    "$dir/$name" "$@" </dev/null >/dev/null 2>&1 &
  disown
  ;;
wait)
  [ "$#" -le 1 ] || usage
  limit=${1:-900}
  [[ $limit =~ ^[0-9]+$ ]] || usage
  [ -e "$log" ] || {
    echo "ci-background: $name was never started" >&2
    exit 1
  }
  for ((i = 0; i < limit; i++)); do
    [ -f "$status_file" ] && break
    sleep 1
  done
  echo "::group::$name output"
  cat -- "$log"
  echo "::endgroup::"
  [ -f "$status_file" ] || {
    echo "::error title=$name::$name did not finish within ${limit}s"
    exit 1
  }
  status=$(<"$status_file")
  if [ "$status" -ne 0 ]; then
    echo "::error title=$name::$name failed with exit $status"
    exit "$status"
  fi
  ;;
*) usage ;;
esac
