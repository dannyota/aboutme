#!/usr/bin/env bash
# Scan secrets over full Git history with the pinned gitleaks toolchain.
set -Eeuo pipefail

cd "$(dirname "$0")/.."

STATUS=0
TOOLS_OK=1

echo "=== tool versions"
if ! make tools-check ARGS=scan; then
  STATUS=1
  TOOLS_OK=0
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
