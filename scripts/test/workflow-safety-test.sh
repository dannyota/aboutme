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

grep -Fq 'runs-on: ubuntu-24.04' "$WORKFLOW" ||
  fail "hosted S3 conformance does not pin a runner with Podman"
grep -Fq 'run: make test-s3-up' "$WORKFLOW" ||
  fail "hosted S3 conformance does not start the pinned test service"
grep -Fq 'SERVER_TEST_S3_RUN: ^TestNormalizeAcceptsFrozenCorpusDeterministically$' "$WORKFLOW" ||
  fail "hosted S3 conformance does not isolate the slow corpus test"
grep -Fq 'SERVER_TEST_S3_SKIP: ^(TestNormalizationBudget|TestNormalizeAcceptsFrozenCorpusDeterministically)$' "$WORKFLOW" ||
  fail "hosted S3 conformance does not run the rest of the suite with both split-off tests skipped"
grep -Fq 'run: make test-s3-down' "$WORKFLOW" ||
  fail "hosted S3 conformance does not tear down its disposable service"

WEB_E2E_JOB=$(awk '/^  web-e2e:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$WORKFLOW")
[ -n "$WEB_E2E_JOB" ] || fail "hosted workflow lacks the pinned browser job"
if grep -Fq 'needs:' <<<"$WEB_E2E_JOB"; then
  fail "pinned browser job depends on a job whose artifact it does not use"
fi
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

grep -Fq '        shard: [preview-gap, rest]' <<<"$WEB_E2E_JOB" ||
  fail "pinned browser job does not run both shards that cover every spec"
grep -Fq '          WEB_E2E_SHARD: ${{ matrix.shard }}' <<<"$WEB_E2E_JOB" ||
  fail "pinned browser job does not pass its shard to make web-e2e"

PROOF_JOB=$(awk '/^  dev-https-proofs:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$WORKFLOW")
[ -n "$PROOF_JOB" ] || fail "hosted workflow lacks the dev-https-proofs job"
PROOF_SETUP=$ROOT/.github/actions/browser-proof-setup/action.yml
[ -f "$PROOF_SETUP" ] || fail "the browser-proof-setup action is missing"
PROOF_RUNNER=$ROOT/scripts/ci-browser-proofs.sh
grep -Fq '    runs-on: ubuntu-24.04' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job does not pin a runner with Podman"
grep -Fq '    timeout-minutes: 40' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job lacks a fixed 40-minute timeout"
! grep -Fq 'continue-on-error' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job must block the release"
grep -Fq 'uses: ./.github/actions/browser-proof-setup' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job does not use the shared proof setup"
grep -Fxq '      run: make test-db-up' "$PROOF_SETUP" ||
  fail "proof setup does not start the shared database container"
grep -Fxq '      run: make dev-https' "$PROOF_SETUP" ||
  fail "proof setup does not start the repository HTTPS harness"
grep -Fxq '      run: make dev-https-browser-image' "$PROOF_SETUP" ||
  fail "proof setup does not build the pinned browser image"
grep -Fq 'bash scripts/ci-browser-proofs.sh "${proofs[@]}"' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job does not run its group through the proof runner"
grep -Fq 'ABOUTME_TOTP_SHARD=$shard make "$target"' "$PROOF_RUNNER" ||
  fail "proof runner does not run a TOTP shard through its make target"
grep -Fq 'ABOUTME_PASSKEY_SHARD=$shard make "$target"' "$PROOF_RUNNER" ||
  fail "proof runner does not run a passkey shard through its make target"
if grep -Fq 'secrets.' <<<"$PROOF_JOB" || grep -Fq 'secrets.' "$PROOF_SETUP"; then
  fail "dev-https-proofs job references a repository secret"
fi
if grep -Fq '    services:' <<<"$PROOF_JOB"; then
  fail "dev-https-proofs job must not add a second database service beside dev-https"
fi
if grep -Eq 'ABOUTME_RELEASE_APP_IMAGE|ABOUTME_RELEASE_WEB_IMAGE|owner-test\.env' \
  <<<"$PROOF_JOB"; then
  fail "dev-https-proofs job leaks a release image or owner credential input"
fi

# Every proof runs exactly once: each dev-https check below, and every shard
# of the two sharded second-factor journeys, read from their shard maps.
shard_proofs=$(
  cd "$ROOT/deploy/dev-https-browser" &&
    node --input-type=module -e "
      import { PASSKEY_SHARD_ROLES, TOTP_SHARD_ROLES } from './proof-shards.mjs';
      for (const shard of Object.keys(TOTP_SHARD_ROLES)) console.log('totp:' + shard);
      for (const shard of Object.keys(PASSKEY_SHARD_ROLES)) console.log('passkey:' + shard);
    "
) || fail "cannot read the proof shard maps"
[ -n "$shard_proofs" ] || fail "the proof shard maps are empty"
expected_proofs=$(
  printf '%s\n' auth editor entry exports mcp mcp-sdk password privacy public \
    publish sample-start transport "$shard_proofs" | LC_ALL=C sort
)
# A TOTP proof may list several shards, comma-separated; each counts once.
actual_proofs=$(awk '$1 == "proofs:" { for (i = 2; i <= NF; i++) {
    if ($i ~ /^totp:/) { n = split(substr($i, 6), s, ","); for (j = 1; j <= n; j++) print "totp:" s[j] }
    else print $i } }' <<<"$PROOF_JOB" | LC_ALL=C sort)
[ "$actual_proofs" = "$expected_proofs" ] ||
  fail "dev-https-proofs groups do not run every proof exactly once: $(tr '\n' ' ' <<<"$actual_proofs")"

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

UPLOAD_STEP=$(step_block "Upload shard proof evidence" "$PROOF_JOB")
[ -n "$UPLOAD_STEP" ] || fail "dev-https-proofs job lacks the named upload step"
grep -Fq 'if: always()' <<<"$UPLOAD_STEP" ||
  fail "dev-https-proofs job's upload step does not run on every exit"
grep -Fq 'path: .dev/proof-evidence/' <<<"$UPLOAD_STEP" ||
  fail "dev-https-proofs job does not upload the bounded shard evidence path"
grep -Fq 'include-hidden-files: true' <<<"$UPLOAD_STEP" ||
  fail "dev-https-proofs job's upload step excludes the hidden .dev evidence path by default"
grep -Fq 'staging=.dev/proof-evidence' "$PROOF_RUNNER" ||
  fail "proof runner does not stage shard evidence where the job uploads it"

STOP_HARNESS_STEP=$(step_block "Stop the HTTPS harness" "$PROOF_JOB")
[ -n "$STOP_HARNESS_STEP" ] || fail "dev-https-proofs job lacks the named stop-harness step"
grep -Fq 'if: always()' <<<"$STOP_HARNESS_STEP" ||
  fail "dev-https-proofs job's stop-harness step does not run on every exit"
grep -Fq -- 'run: make dev-https-down' <<<"$STOP_HARNESS_STEP" ||
  fail "dev-https-proofs job's stop-harness step does not stop the HTTPS harness"

STOP_DB_STEP=$(step_block "Stop the runner-local database" "$PROOF_JOB")
[ -n "$STOP_DB_STEP" ] || fail "dev-https-proofs job lacks the named stop-database step"
grep -Fq 'if: always()' <<<"$STOP_DB_STEP" ||
  fail "dev-https-proofs job's stop-database step does not run on every exit"
grep -Fq -- 'run: make test-db-down' <<<"$STOP_DB_STEP" ||
  fail "dev-https-proofs job's stop-database step does not stop the runner-local database"

# The gate checks that the shards' evidence together proves every step.
CI_JOB=$(awk '/^  ci:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$WORKFLOW")
[ -n "$CI_JOB" ] || fail "hosted workflow lacks the ci gate job"
grep -Fq 'pattern: shard-evidence-*' <<<"$CI_JOB" ||
  fail "ci gate does not download every group's shard evidence"
grep -Fq 'node deploy/dev-https-browser/check-shard-coverage.mjs totp \' <<<"$CI_JOB" ||
  fail "ci gate does not check TOTP shard coverage"
grep -Fq 'node deploy/dev-https-browser/check-shard-coverage.mjs passkey \' <<<"$CI_JOB" ||
  fail "ci gate does not check passkey shard coverage"
grep -Fq '      - dev-https-proofs' <<<"$CI_JOB" ||
  fail "ci gate does not require the dev-https-proofs job"

# Hosted Semgrep runs one scan per product set, each in its own job. Together
# the jobs select every product and fail closed, the gate requires each job,
# and the gate lets exactly these jobs, and no other, be skipped.
semgrep_jobs=(semgrep-code semgrep-supply-chain)
semgrep_if="    if: github.event_name == 'push' || github.event_name == 'workflow_dispatch' || github.event.pull_request.head.repo.full_name == github.repository"
actual_semgrep_jobs=$(sed -n 's/^  \(semgrep[a-z-]*\):$/\1/p' "$WORKFLOW" | LC_ALL=C sort)
[ "$actual_semgrep_jobs" = "$(printf '%s\n' "${semgrep_jobs[@]}" | LC_ALL=C sort)" ] ||
  fail "hosted Semgrep jobs are not exactly ${semgrep_jobs[*]}: $(tr '\n' ' ' <<<"$actual_semgrep_jobs")"
semgrep_job_body() { # job-id
  awk -v job="  $1:" '$0 == job { flag = 1; next } /^  [a-z]/ { flag = 0 } flag' "$WORKFLOW"
}
# Each scan writes its JSON, and the step right after it blocks on that JSON
# in the repository, whatever the Semgrep platform policy marks as blocking.
semgrep_json='"$RUNNER_TEMP/semgrep.json"'
SEMGREP_CODE_JOB=$(semgrep_job_body semgrep-code)
grep -Fxq "      - run: semgrep ci --code --secrets --no-suppress-errors --json-output=$semgrep_json" <<<"$SEMGREP_CODE_JOB" ||
  fail "semgrep-code does not select Code and Secrets, fail closed, and write its JSON"
grep -Fxq -- '      - run: scripts/test/semgrep-gate-test.sh' <<<"$SEMGREP_CODE_JOB" ||
  fail "semgrep-code does not test its blocking gate"
SEMGREP_SCA_JOB=$(semgrep_job_body semgrep-supply-chain)
grep -Fxq "      - run: semgrep ci --supply-chain --no-suppress-errors --json-output=$semgrep_json" <<<"$SEMGREP_SCA_JOB" ||
  fail "semgrep-supply-chain does not select Supply Chain, fail closed, and write its JSON"
semgrep_gate_follows_scan() { # job-body mode
  awk -v gate="        run: scripts/semgrep-gate.sh $2 $semgrep_json" '
    /^      - run: semgrep ci / { scan = NR }
    /^      - / && scan && NR > scan && !next_step { next_step = NR }
    $0 == gate { gates++; at = NR }
    END { exit !(gates == 1 && scan && next_step && at == next_step + 1) }
  ' <<<"$1"
}
semgrep_gate_follows_scan "$SEMGREP_CODE_JOB" code ||
  fail "semgrep-code does not block on Code findings in the step after its scan"
semgrep_gate_follows_scan "$SEMGREP_SCA_JOB" supply-chain ||
  fail "semgrep-supply-chain does not block on Supply Chain findings in the step after its scan"
grep -Fxq -- '      - run: scripts/test/semgrep-sca-inputs-test.sh' <<<"$SEMGREP_SCA_JOB" ||
  fail "semgrep-supply-chain does not verify its dependency inputs"
for job in "${semgrep_jobs[@]}"; do
  body=$(semgrep_job_body "$job")
  [ "$(grep -c -- '- run: semgrep ci ' <<<"$body")" -eq 1 ] ||
    fail "$job does not run exactly one semgrep ci scan"
  grep -Fxq "$semgrep_if" <<<"$body" ||
    fail "$job is not skipped only when a fork pull request withholds the token"
  grep -Fxq '          SEMGREP_APP_TOKEN: ${{ secrets.SEMGREP_APP_TOKEN }}' <<<"$body" ||
    fail "$job does not scan with the Semgrep platform token"
  ! grep -Fq 'continue-on-error' <<<"$body" ||
    fail "$job must block the release"
  grep -Fxq "      - $job" <<<"$CI_JOB" ||
    fail "ci gate does not require the $job job"
done
[ "$(grep -Fc '"skipped"' <<<"$CI_JOB")" -eq 1 ] ||
  fail "ci gate accepts a skipped result in more than one place"
grep -Fq "or (.key | IN(\"semgrep-code\", \"semgrep-supply-chain\") | not)))" <<<"$CI_JOB" ||
  fail "ci gate does not limit skipped jobs to exactly the Semgrep jobs"

printf 'hosted workflow safety tests passed\n'
