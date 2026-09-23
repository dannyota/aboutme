#!/usr/bin/env bash

# Author regression checks for fail-closed hosted append-only jobs.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORKFLOW=$ROOT/.github/workflows/ci.yml

fail() {
  printf 'workflow-safety-test: %s\n' "$*" >&2
  exit 1
}

if grep -Fq 'changed=$(git diff --name-status' "$WORKFLOW"; then
  fail "a hosted append-only job still filters git diff before checking its status"
fi

grep -Fq 'bash scripts/check-migrations-append-only.sh --commits "$BASE_SHA" HEAD' "$WORKFLOW" ||
  fail "migration job does not use the tested commit-to-commit guard"
grep -Fq 'base is not a commit' "$ROOT/scripts/check-migrations-append-only.sh" ||
  fail "migration guard does not reject an invalid base"
grep -Fq 'git diff --quiet "$base" "$head"' "$ROOT/scripts/check-migrations-append-only.sh" ||
  fail "migration guard does not compare push trees directly"

count=$(grep -Fc 'if ! diff=$(git diff --name-status' "$WORKFLOW" || true)
[ "$count" -eq 1 ] ||
  fail "want one inline fail-closed hosted diff capture, found $count"
grep -Fq 'Could not compare released schemas with base.' "$WORKFLOW" ||
  fail "released-schema job lacks an explicit diff-failure result"
grep -Fq 'git diff --name-status "$base" "$head"' "$WORKFLOW" ||
  fail "released-schema job does not compare event trees directly"
grep -Fq 'git rev-parse --verify --quiet "$BASE_SHA^{commit}"' "$WORKFLOW" ||
  fail "released-schema job does not validate the base commit"
grep -Fq 'git rev-parse --verify --quiet "HEAD^{commit}"' "$WORKFLOW" ||
  fail "released-schema job does not validate the head commit"
if grep -Fq 'git diff --name-status "$base"...HEAD' "$WORKFLOW"; then
  fail "released-schema job still uses a merge-base diff"
fi

grep -Fq 'semgrep ci --code --supply-chain --secrets --no-suppress-errors' "$WORKFLOW" ||
  fail "hosted Semgrep does not explicitly select every product and fail closed"
grep -Fq -- '- run: scripts/test/semgrep-sca-inputs-test.sh' "$WORKFLOW" ||
  fail "hosted Semgrep does not verify its dependency inputs"

grep -Fq 'runs-on: ubuntu-24.04' "$WORKFLOW" ||
  fail "hosted S3 conformance does not pin a runner with Podman"
grep -Fq -- '- run: make test-s3-up' "$WORKFLOW" ||
  fail "hosted S3 conformance does not start the pinned test service"
grep -Fq -- '- run: make server-test-s3' "$WORKFLOW" ||
  fail "hosted S3 conformance does not run the fail-closed suite"
grep -Fq 'run: make test-s3-down' "$WORKFLOW" ||
  fail "hosted S3 conformance does not tear down its disposable service"

grep -Fq '  web-e2e:' "$WORKFLOW" ||
  fail "hosted workflow lacks the pinned browser job"
grep -Fq '    needs: web' "$WORKFLOW" ||
  fail "pinned browser job does not wait for the web job"
grep -Fq '        WEB_E2E_RUN_ID: ci-${{ github.run_id }}-${{ github.run_attempt }}' \
  "$WORKFLOW" || fail "pinned browser job lacks the closed immutable run ID"
grep -Fq '        test -z "${UPDATE_GOLDEN+x}"' "$WORKFLOW" ||
  fail "pinned browser job does not reject UPDATE_GOLDEN by presence"
grep -Fq '        test -z "${PLAYWRIGHT_UPDATE_SNAPSHOTS+x}"' "$WORKFLOW" ||
  fail "pinned browser job does not reject PLAYWRIGHT_UPDATE_SNAPSHOTS by presence"
grep -Fq '        make web-e2e' "$WORKFLOW" ||
  fail "pinned browser job does not run the comparison target"
if grep -Fq 'make web-e2e-update' "$WORKFLOW"; then
  fail "hosted workflow can update browser baselines"
fi
if grep -Fq -- '--update-snapshots' "$WORKFLOW"; then
  fail "hosted workflow passes a browser baseline update flag"
fi

PASSKEY_JOB=$(sed -n '/^  passkey-browser-proof:/,/^  totp-browser-proof:/p' "$WORKFLOW")
[ -n "$PASSKEY_JOB" ] || fail "hosted workflow lacks the passkey-browser-proof job"
grep -Fq '    timeout-minutes: 45' <<<"$PASSKEY_JOB" ||
  fail "passkey-browser-proof job lacks a fixed 45-minute timeout"
grep -Fq -- '- run: make dev-https' <<<"$PASSKEY_JOB" ||
  fail "passkey-browser-proof job does not start the repository HTTPS harness"
grep -Fq -- '- run: make dev-https-browser-image' <<<"$PASSKEY_JOB" ||
  fail "passkey-browser-proof job does not build the pinned browser image"
grep -Fq -- '- run: make dev-https-passkey-check' <<<"$PASSKEY_JOB" ||
  fail "passkey-browser-proof job does not run the passkey proof target"
if grep -Fq 'secrets.' <<<"$PASSKEY_JOB"; then
  fail "passkey-browser-proof job references a repository secret"
fi

# Each cleanup step's own if: always() is asserted against its own named
# block, not a job-wide count, so a check cannot pass by attaching every
# always() to one step and none to the others.
step_block() { # step-name job-body
  awk -v name="      - name: $1" '
    $0 == name { found = 1; print; next }
    found && /^      - / { exit }
    found { print }
  ' <<<"$2"
}

UPLOAD_STEP=$(step_block "Upload passkey proof evidence" "$PASSKEY_JOB")
[ -n "$UPLOAD_STEP" ] || fail "passkey-browser-proof job lacks the named upload step"
grep -Fq 'if: always()' <<<"$UPLOAD_STEP" ||
  fail "passkey-browser-proof job's upload step does not run on every exit"
grep -Fq 'path: .dev/native-https/evidence/passkey-*' <<<"$UPLOAD_STEP" ||
  fail "passkey-browser-proof job does not upload the bounded passkey evidence path"
grep -Fq 'include-hidden-files: true' <<<"$UPLOAD_STEP" ||
  fail "passkey-browser-proof job's upload step excludes the hidden .dev evidence path by default"

STOP_HARNESS_STEP=$(step_block "Stop the HTTPS harness" "$PASSKEY_JOB")
[ -n "$STOP_HARNESS_STEP" ] || fail "passkey-browser-proof job lacks the named stop-harness step"
grep -Fq 'if: always()' <<<"$STOP_HARNESS_STEP" ||
  fail "passkey-browser-proof job's stop-harness step does not run on every exit"
grep -Fq -- 'run: make dev-https-down' <<<"$STOP_HARNESS_STEP" ||
  fail "passkey-browser-proof job's stop-harness step does not stop the HTTPS harness"

STOP_DB_STEP=$(step_block "Stop the runner-local database" "$PASSKEY_JOB")
[ -n "$STOP_DB_STEP" ] || fail "passkey-browser-proof job lacks the named stop-database step"
grep -Fq 'if: always()' <<<"$STOP_DB_STEP" ||
  fail "passkey-browser-proof job's stop-database step does not run on every exit"
grep -Fq -- 'run: make test-db-down' <<<"$STOP_DB_STEP" ||
  fail "passkey-browser-proof job's stop-database step does not stop the runner-local database"

# The TOTP proof mirrors the passkey job's shape and blocks the release.
TOTP_JOB=$(sed -n '/^  totp-browser-proof:/,/^  web-source-build:/p' "$WORKFLOW")
[ -n "$TOTP_JOB" ] || fail "hosted workflow lacks the totp-browser-proof job"
grep -Fq '    timeout-minutes: 60' <<<"$TOTP_JOB" ||
  fail "totp-browser-proof job lacks a fixed 60-minute timeout"
! grep -Fq 'continue-on-error' <<<"$TOTP_JOB" ||
  fail "totp-browser-proof job must block the release"
grep -Fq -- '- run: make dev-https' <<<"$TOTP_JOB" ||
  fail "totp-browser-proof job does not start the repository HTTPS harness"
grep -Fq -- '- run: make dev-https-browser-image' <<<"$TOTP_JOB" ||
  fail "totp-browser-proof job does not build the pinned browser image"
grep -Fq -- '- run: make dev-https-totp-check' <<<"$TOTP_JOB" ||
  fail "totp-browser-proof job does not run the TOTP proof target"
if grep -Fq 'secrets.' <<<"$TOTP_JOB"; then
  fail "totp-browser-proof job references a repository secret"
fi

TOTP_UPLOAD_STEP=$(step_block "Upload TOTP proof evidence" "$TOTP_JOB")
[ -n "$TOTP_UPLOAD_STEP" ] || fail "totp-browser-proof job lacks the named upload step"
grep -Fq 'if: always()' <<<"$TOTP_UPLOAD_STEP" ||
  fail "totp-browser-proof job's upload step does not run on every exit"
grep -Fq 'path: .dev/native-https/evidence/totp-*' <<<"$TOTP_UPLOAD_STEP" ||
  fail "totp-browser-proof job does not upload the bounded TOTP evidence path"
grep -Fq 'include-hidden-files: true' <<<"$TOTP_UPLOAD_STEP" ||
  fail "totp-browser-proof job's upload step excludes the hidden .dev evidence path by default"

TOTP_STOP_HARNESS_STEP=$(step_block "Stop the HTTPS harness" "$TOTP_JOB")
[ -n "$TOTP_STOP_HARNESS_STEP" ] || fail "totp-browser-proof job lacks the named stop-harness step"
grep -Fq 'if: always()' <<<"$TOTP_STOP_HARNESS_STEP" ||
  fail "totp-browser-proof job's stop-harness step does not run on every exit"
grep -Fq -- 'run: make dev-https-down' <<<"$TOTP_STOP_HARNESS_STEP" ||
  fail "totp-browser-proof job's stop-harness step does not stop the HTTPS harness"

TOTP_STOP_DB_STEP=$(step_block "Stop the runner-local database" "$TOTP_JOB")
[ -n "$TOTP_STOP_DB_STEP" ] || fail "totp-browser-proof job lacks the named stop-database step"
grep -Fq 'if: always()' <<<"$TOTP_STOP_DB_STEP" ||
  fail "totp-browser-proof job's stop-database step does not run on every exit"
grep -Fq -- 'run: make test-db-down' <<<"$TOTP_STOP_DB_STEP" ||
  fail "totp-browser-proof job's stop-database step does not stop the runner-local database"

grep -Fq '  mcp-proofs:' "$WORKFLOW" ||
  fail "hosted workflow lacks the MCP proofs job"
grep -Fq '    runs-on: ubuntu-24.04' "$WORKFLOW" ||
  fail "MCP proofs job does not pin a runner with Podman"
grep -Fq '    timeout-minutes: 40' "$WORKFLOW" ||
  fail "MCP proofs job lacks a bounded timeout"
grep -Fxq '      - run: make test-db-up' "$WORKFLOW" ||
  fail "MCP proofs job does not start the shared database container"
grep -Fq '        run: make test-db-down' "$WORKFLOW" ||
  fail "MCP proofs job does not remove its database container"
grep -Fxq '      - run: make dev-https' "$WORKFLOW" ||
  fail "MCP proofs job does not start the native HTTPS harness"
grep -Fq '      - run: make dev-https-browser-image' "$WORKFLOW" ||
  fail "MCP proofs job does not build the pinned browser image"
grep -Fq '      - run: make dev-https-mcp-check' "$WORKFLOW" ||
  fail "MCP proofs job does not run the raw JSON-RPC MCP proof"
grep -Fq '      - run: make dev-https-mcp-sdk-check' "$WORKFLOW" ||
  fail "MCP proofs job does not run the SDK owner workflow proof"
grep -Fq '        run: make dev-https-down' "$WORKFLOW" ||
  fail "MCP proofs job does not tear down its native HTTPS harness"
mcp_proofs_body=$(awk '/^  mcp-proofs:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$WORKFLOW")
[ -n "$mcp_proofs_body" ] || fail "MCP proofs job body is missing"
printf '%s\n' "$mcp_proofs_body" | grep -Fq 'if: always()' ||
  fail "MCP proofs job does not always tear down the stack"
if printf '%s\n' "$mcp_proofs_body" | grep -Fq '    services:'; then
  fail "MCP proofs job must not add a second database service beside dev-https"
fi
if printf '%s\n' "$mcp_proofs_body" | grep -Eq 'upload-artifact|ABOUTME_RELEASE_APP_IMAGE|ABOUTME_RELEASE_WEB_IMAGE|owner-test\.env'; then
  fail "MCP proofs job leaks an artifact, release image, or owner credential input"
fi

printf 'hosted workflow safety tests passed\n'
