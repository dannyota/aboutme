#!/usr/bin/env bash
# Release checks use only stubbed GitHub, Git, registry, and SSH commands.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
export ELIGIBILITY_TEST_DIR=$work
export TEST_COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export TEST_OTHER=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

fail() { echo "release-eligibility-test: $*" >&2; exit 1; }
job_body() {
  awk -v job="  $1:" '$0 == job { found=1; next } /^  [a-z]/ { found=0 } found' \
    "$root/.github/workflows/release-images.yml"
}

write_jobs=$(awk '
  /^  [a-z][a-z0-9_-]*:$/ { job=$1; sub(/:$/, "", job) }
  /^      [a-z-]+: write$/ { print job }
' "$root/.github/workflows/release-images.yml" | sort -u)
[[ $write_jobs == $'build\nimage\nobserver-image\nobserver-image-amd64\nrelease' ]] ||
  fail 'write-capable jobs changed without eligibility coverage'

# These edges put the read-only gate before every job holding write authority.
# The release job's image dependency preserves the optional observer policy.
for job in build observer-image observer-image-amd64; do
  body=$(job_body "$job")
  grep -Eq '^    needs: eligibility$' <<<"$body" || fail "$job can start before eligibility"
  ! grep -Eq '^    (if:|continue-on-error:)' <<<"$body" || fail "$job overrides dependency success"
done
body=$(job_body image)
grep -Eq '^    needs: build$' <<<"$body" || fail 'image must wait for build'
grep -Fxq "    if: github.event_name == 'push'" <<<"$body" || fail 'image bypasses dependency success'
body=$(job_body release)
grep -Fxq "    if: \${{ !cancelled() && needs.image.result == 'success' }}" <<<"$body" ||
  fail 'release must require image success'
grep -Fxq '    needs: [image, observer-image, observer-image-amd64]' <<<"$body" ||
  fail 'release must wait for image and collect observer evidence'
body=$(job_body eligibility)
[[ -n $body ]] || fail 'missing eligibility job'
! grep -Eq '^    if:' <<<"$body" || fail 'eligibility must not skip the dependency gate'
grep -Fxq "        if: github.event_name == 'push'" <<<"$body" || fail 'tag pushes must run eligibility'
! grep -Eq ': write|continue-on-error:' <<<"$body" || fail 'eligibility must be read-only and fail closed'
grep -Eq '^      actions: read$' <<<"$body" || fail 'eligibility cannot read CI evidence'
grep -Eq '^          fetch-depth: 0$' <<<"$body" || fail 'eligibility needs main ancestry'
grep -Fq 'TAG: ${{ github.ref_name }}' <<<"$body" || fail 'eligibility must use the event tag'
grep -Fq 'COMMIT: ${{ github.sha }}' <<<"$body" || fail 'eligibility must bind the event commit'
# Run the actual workflow command, so bypassing the helper is a regression.
gate_command() {
  job_body "$1" | awk '
    /^      - name: Check release eligibility$/ { found=1; next }
    /^      - / { found=0 }
    found && /^        run: / { sub(/^        run: /, ""); print }
  '
}
command=$(gate_command eligibility)
[[ -n $command ]] || fail 'missing shared eligibility command'

cat >"$work/bin/git" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$ELIGIBILITY_TEST_DIR/git.log"
[[ $1 != -C ]] || shift 2
case $1 in
  fetch)
    [[ " $* " == *' https://github.com/dannyota/aboutme.git '* &&
       " $* " == *' +refs/heads/main:refs/remotes/origin/main '* &&
       " $* " == *" refs/tags/$TAG:refs/tags/$TAG "* ]] || exit 2
    [[ ${TEST_CASE:-} != fetch-error ]]
    ;;
  rev-parse)
    [[ ${TEST_CASE:-} != missing-tag ]] || exit 1
    printf '%s\n' "$TEST_COMMIT"
    ;;
  merge-base) [[ ${TEST_CASE:-} != off-main ]] ;;
  *) echo "unexpected git command: $*" >&2; exit 2 ;;
esac
STUB
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ $1 == release ]]; then echo release >>"$ELIGIBILITY_TEST_DIR/writes"; exit 0; fi
[[ " $* " == *' --repo dannyota/aboutme '* && " $* " == *' --workflow ci.yml '* &&
   " $* " == *" --commit $TEST_COMMIT "* && " $* " == *' --event push '* &&
   " $* " == *' --branch main '* ]] || exit 2
[[ ${TEST_CASE:-} != api-error ]] || exit 1
cat "$ELIGIBILITY_TEST_DIR/runs.json"
STUB
for name in docker ssh; do
  cat >"$work/bin/$name" <<'STUB'
#!/usr/bin/env bash
echo "${0##*/} $*" >>"$ELIGIBILITY_TEST_DIR/writes"
if [[ ${0##*/} == docker && $1 == push && ${TEST_PUSH_FAIL:-0} == 1 ]]; then exit 1; fi
STUB
done
chmod +x "$work/bin/"*
export PATH=$work/bin:$PATH
export TAG=v0.7.5 COMMIT=$TEST_COMMIT
# Model the candidate checkout and the separate trusted main checkout. Static
# checks below bind the trusted fixture to the workflow's checkout source.
mkdir -p "$work/workspace/scripts" "$work/workspace/.release-trusted/scripts"
cp "$root/scripts/release-eligibility.sh" "$work/workspace/scripts/"
cp "$root/scripts/release-eligibility.sh" "$work/workspace/.release-trusted/scripts/"
cd "$work/workspace"

runs() {
  jq -n --arg sha "$TEST_COMMIT" --arg event "${1:-push}" --arg branch "${2:-main}" \
    --arg conclusion "${3:-success}" --arg status "${4:-completed}" \
    '[{databaseId: 20, headSha: $sha, event: $event, headBranch: $branch,
       conclusion: $conclusion, status: $status}]' >"$work/runs.json"
}
publication() {
  # A failed gate stops before registry login, image push, or release creation.
  bash -euo pipefail -c "${1-$command}" || return
  docker login ghcr.io || return
  docker push test-image || return
  gh release create "$TAG"
}
reject() {
  : >"$work/writes"
  if publication >"$work/out" 2>&1; then fail "$TEST_CASE passed"; fi
  [[ ! -s $work/writes ]] || fail "$TEST_CASE reached publication"
  grep -q 'release-eligibility:' "$work/out" || { cat "$work/out" >&2; fail "$TEST_CASE failed outside eligibility"; }
  echo "rejected $TEST_CASE before publication"
}
for TEST_CASE in off-main fetch-error missing-tag api-error wrong-sha branch-only dispatch-only cancelled pending red no-run malformed-evidence newer-red newer-pending; do
  export TEST_CASE
  runs
  case $TEST_CASE in
    wrong-sha) sed "s/$TEST_COMMIT/$TEST_OTHER/g" "$work/runs.json" >"$work/other"; mv "$work/other" "$work/runs.json" ;;
    branch-only) runs push feature ;;
    dispatch-only) runs workflow_dispatch main ;;
    cancelled) runs push main cancelled ;;
    pending) runs push main success in_progress ;;
    red) runs push main failure ;;
    no-run) echo '[]' >"$work/runs.json" ;;
    malformed-evidence) echo '{' >"$work/runs.json" ;;
    newer-red|newer-pending)
      # Old success comes first to prove selection does not trust array order.
      jq --arg status "$TEST_CASE" '. + [.[0] | .databaseId = 21 |
        .conclusion = (if $status == "newer-red" then "failure" else null end) |
        .status = (if $status == "newer-red" then "completed" else "queued" end)]' \
        "$work/runs.json" >"$work/newer"
      mv "$work/newer" "$work/runs.json"
      ;;
  esac
  reject
done
TEST_CASE=event-sha-mismatch COMMIT=$TEST_OTHER
runs
reject
COMMIT=$TEST_COMMIT
for TAG in v01.2.3 v1.2 v1.2.3-rc1 'v1.2.3;false' main; do
  TEST_CASE=malformed-tag
  reject
done
TAG=v0.7.5 TEST_CASE=success
runs
publication >"$work/out" 2>&1 || { cat "$work/out" >&2; fail 'valid release refused'; }
[[ $(cat "$work/out") == "$TEST_COMMIT" ]] || fail 'gate did not return the resolved commit'
grep -q 'docker push' "$work/writes" || fail 'valid release did not reach publication'
# A candidate can replace its own helper, but cannot replace the main helper.
printf '#!/usr/bin/env bash\nexit 0\n' >scripts/release-eligibility.sh
failures=0
expect_job_rejected() {
  local job=$1
  : >"$work/writes"
  if publication "$(gate_command "$job")" >"$work/out" 2>&1 || [[ -s $work/writes ]]; then
    echo "release-eligibility-test: $TEST_CASE reached publication in $job" >&2
    failures=$((failures + 1))
    return
  fi
  grep -q 'release-eligibility:' "$work/out" || { cat "$work/out" >&2; fail "$job failed outside eligibility"; }
  echo "rejected $TEST_CASE in $job before publication"
}
TEST_CASE=off-main
runs
for job in eligibility $write_jobs; do
  expect_job_rejected "$job"
done

# A partial rerun reuses the successful prerequisite, but reruns the failed
# publishing job's own steps. Main CI can turn red between those attempts.
cp "$root/scripts/release-eligibility.sh" scripts/release-eligibility.sh
TEST_CASE=success
runs
bash -euo pipefail -c "$command" >/dev/null || fail 'first attempt eligibility failed'
for job in $write_jobs; do
  runs
  : >"$work/writes"
  if TEST_PUSH_FAIL=1 publication "$(gate_command "$job")" >"$work/out" 2>&1; then
    fail "$job did not fail its first push"
  fi
  grep -q 'docker push' "$work/writes" || fail "$job first attempt never reached push"
  ! grep -qx release "$work/writes" || fail "$job created a release after push failed"
  TEST_CASE=partial-rerun-red
  runs push main failure
  expect_job_rejected "$job"
done
[[ $failures == 0 ]] || fail "$failures publication bypasses"

# Every job checks trusted code before candidate checkout or artifact handling.
# Exact prefix comparison also rejects conditional or fail-open gate steps.
expected_prefix=$(cat <<'YAML'
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          repository: dannyota/aboutme
          ref: refs/heads/main
          path: .release-trusted
          fetch-depth: 0
          persist-credentials: false
      - name: Check release eligibility
        if: github.event_name == 'push'
        env:
          TAG: ${{ github.ref_name }}
          COMMIT: ${{ github.sha }}
          GH_TOKEN: ${{ github.token }}
        run: bash .release-trusted/scripts/release-eligibility.sh "$TAG" "$COMMIT"
YAML
)
for job in eligibility $write_jobs; do
  body=$(job_body "$job")
  prefix=$(awk '/^    steps:$/ { found=1; next } found { print; if (/^        run: /) exit }' <<<"$body")
  [[ $prefix == "$expected_prefix" ]] || fail "$job does not start with the trusted main gate"
  grep -Eq '^      actions: read$' <<<"$body" || fail "$job cannot refresh CI evidence"
done
echo 'release eligibility test passed'
