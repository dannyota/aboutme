#!/usr/bin/env bash
# Fixed diagnostics survive helper redirection, explicit exits, and unset names.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
fixture=$root/deploy/vn/tests/crowdsec-retention-fixture.sh
for name in real-migration-switch real-migration-noop real-migration-rejects-all-old-credentials \
  real-migration-reset real-migration-failure-requires-explicit-reset; do
  for failure in command exit unset; do
    if bash -euo pipefail -c '
      source "$1"
      retention_fixture_case=$2
      integration_stage=credential-machine
      retention_fixture_diagnostics
      helper() {
        case $3 in
          command) return 1 ;;
          exit) exit 1 ;;
          unset) printf "%s" "$missing_name" ;;
        esac
      }
      helper "$@" >/dev/null 2>&1
    ' proof "$fixture" "$name" "$failure" >"$work/out" 2>"$work/err"; then
      printf 'FAIL migration-diagnostic-status\n'; exit 1
    fi
    [[ ! -s $work/out && $(cat "$work/err") == "FAIL $name stage=credential-machine" ]]
  done
done
printf 'PASS migration-case-and-stage-survive-hidden-helper-failures\n'

# Exercise the same PID reader installed in the namespace service adapter.
# shellcheck source=crowdsec-retention-fixture.sh
source "$fixture"
fixture=$work
unit=consumer.service
mkdir "$fixture/pids"
sleep 30 &
consumer=$!
printf '%s\n' "$consumer" >"$fixture/pids/$unit"
pid=0
retention_fixture_pid_for "$fixture" "$unit"
[[ $pid == "$consumer" ]]
kill -TERM "$consumer"
if wait "$consumer"; then exit 1; fi
retention_fixture_pid_for "$fixture" "$unit" 2>"$work/pid-err"
[[ $pid == 0 && ! -s $work/pid-err ]]
printf 'PASS exited-consumer-is-inactive-without-raw-proc-diagnostics\n'
