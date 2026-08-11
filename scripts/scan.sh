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

echo "=== semgrep"
if [ -n "${SEMGREP_APP_TOKEN:-}" ]; then
  semgrep ci || STATUS=1
else
  echo "SEMGREP_APP_TOKEN not set -- falling back to the offline rule packs."
  echo "Supply Chain (SCA) and Pro cross-file analysis are NOT covered by this run."
  make semgrep || STATUS=1
fi

echo
echo "=== gitleaks (full history)"
gitleaks detect --redact --no-banner || STATUS=1

echo
if [ "$STATUS" -ne 0 ]; then
  echo "Scan found issues." >&2
  exit 1
fi
echo "Scan clean."
