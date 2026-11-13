# shellcheck shell=bash
# shellcheck disable=SC2154

assert_waf_stderr_private() {
  local container=$1 mode=$2 stdout_file stderr_file
  case $mode in
    serving|maintenance) ;;
    *) echo 'coraza: unknown stderr check mode' >&2; return 1 ;;
  esac
  stdout_file=$work/waf-$mode.stdout
  stderr_file=$work/waf-$mode.stderr
  if ! podman logs "$container" >"$stdout_file" 2>"$stderr_file"; then
    echo 'coraza: container logs could not be read' >&2
    return 1
  fi
  python3 -B "$root/deploy/caddy/production/test/waf_stderr_diagnostic.py" \
    --mode "$mode" --stdout "$stdout_file" --stderr "$stderr_file" >&2
}
