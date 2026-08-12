#!/usr/bin/env bash
# Batched security scan: Semgrep (SAST + Supply Chain SCA + secrets) and
# gitleaks over full history. Run this at a phase gate, not on every commit --
# per-commit secret protection is the .githooks/pre-commit hook instead.
#
# Semgrep's connected mode needs SEMGREP_APP_TOKEN. It is read from the
# environment, or from .env if present. The token is never printed.
set -Eeuo pipefail

cd "$(dirname "$0")/.."

if [ -z "${SEMGREP_APP_TOKEN:-}" ] && [ -f .env ]; then
  # Load .env without echoing it. `set -a` exports everything it defines.
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

STATUS=0
TOOLS_OK=1
SEMGREP_TMP_ROOT=

cleanup_semgrep_tmp() {
  case "$SEMGREP_TMP_ROOT" in
  "$PWD"/.semgrep-tmp.*)
    rm -rf -- "$SEMGREP_TMP_ROOT"
    ;;
  esac
}
trap cleanup_semgrep_tmp EXIT

echo "=== tool versions"
if ! make tools-check ARGS=scan; then
  STATUS=1
  TOOLS_OK=0
fi

echo
echo "=== semgrep"
if [ "$TOOLS_OK" -ne 1 ]; then
  echo "Semgrep skipped because the pinned scanner toolchain is unavailable." >&2
elif [ -n "${SEMGREP_APP_TOKEN:-}" ]; then
  if SEMGREP_TMP_ROOT=$(mktemp -d "$PWD/.semgrep-tmp.XXXXXX"); then
    TMPDIR="$SEMGREP_TMP_ROOT" semgrep ci --no-suppress-errors || STATUS=1
    cleanup_semgrep_tmp
    SEMGREP_TMP_ROOT=
  else
    echo "Could not create the Semgrep work directory; this gate will fail." >&2
    STATUS=1
  fi
else
  echo "SEMGREP_APP_TOKEN is required for the phase gate." >&2
  echo "Connected Supply Chain and Pro analysis did not run; this gate will fail." >&2
  echo "Running the offline rule packs only as supplemental diagnostics."
  STATUS=1
  make semgrep || STATUS=1
fi

echo
echo "=== gitleaks (full history)"
if [ "$TOOLS_OK" -eq 1 ]; then
  gitleaks detect --redact --no-banner || STATUS=1
else
  echo "gitleaks skipped because the pinned scanner toolchain is unavailable." >&2
fi

echo
if [ "$STATUS" -ne 0 ]; then
  echo "Scan gate failed." >&2
  exit 1
fi
echo "Scan gate passed."
