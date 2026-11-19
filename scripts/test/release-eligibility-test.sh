#!/usr/bin/env bash
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

for job in build observer-image observer-image-amd64; do
  body=$(job_body "$job")
  grep -Fxq '    needs: [eligibility, trivy-db]' <<<"$body" || fail "$job can start before eligibility"
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
body=$(job_body trivy-db)
[[ -n $body ]] || fail 'missing trivy-db job'
! grep -Eq ': write|^    if:|continue-on-error:' <<<"$body" ||
  fail 'trivy-db must be read-only and fail closed'
body=$(job_body eligibility)
[[ -n $body ]] || fail 'missing eligibility job'
! grep -Eq '^    if:' <<<"$body" || fail 'eligibility must not skip the dependency gate'
grep -Fxq "        if: github.event_name == 'push'" <<<"$body" || fail 'tag pushes must run eligibility'
! grep -Eq ': write|continue-on-error:' <<<"$body" || fail 'eligibility must be read-only and fail closed'
grep -Eq '^      actions: read$' <<<"$body" || fail 'eligibility cannot read CI evidence'
grep -Eq '^          fetch-depth: 0$' <<<"$body" || fail 'eligibility needs main ancestry'
grep -Fq 'TAG: ${{ github.ref_name }}' <<<"$body" || fail 'eligibility must use the event tag'
grep -Fq 'COMMIT: ${{ github.sha }}' <<<"$body" || fail 'eligibility must bind the event commit'
grep -Fq 'OWNER: ${{ github.repository_owner }}' <<<"$body" || fail 'eligibility must bind the repository owner'
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
  status)
    case ${CHECKOUT_CASE:-} in
      dirty) echo ' M scripts/release-eligibility.sh' ;;
      staged) echo 'M  scripts/release-eligibility.sh' ;;
      untracked) echo '?? scripts/local.sh' ;;
      status-error) exit 1 ;;
    esac
    ;;
  symbolic-ref)
    case ${CHECKOUT_CASE:-} in
      branch) echo feature ;;
      detached) exit 1 ;;
      *) echo main ;;
    esac
    ;;
  fetch)
    [[ " $* " == *" https://github.com/${EXPECT_REPO:-dannyota/aboutme}.git "* &&
       " $* " == *' +refs/heads/main:refs/remotes/origin/main '* ]] || exit 2
    # The tagless checkout proof fetches main alone.
    if [[ -n ${TAG:-} ]]; then [[ " $* " == *" refs/tags/$TAG:refs/tags/$TAG "* ]] || exit 2
    else [[ $* != *refs/tags/* ]] || exit 2; fi
    [[ ${TEST_CASE:-} != fetch-error ]]
    ;;
  rev-parse)
    [[ ${TEST_CASE:-} != missing-tag ]] || exit 1
    if [[ $* == *HEAD && ${CHECKOUT_CASE:-} == stale ]]; then
      printf '%s\n' "$TEST_OTHER"
    else
      printf '%s\n' "$TEST_COMMIT"
    fi
    ;;
  merge-base) [[ ${TEST_CASE:-} != off-main ]] ;;
  ls-tree)
    [[ $* == "ls-tree --full-tree $TEST_COMMIT^{commit} -- scripts/lib/repository.sh" ]] || exit 2
    case ${TAG_REPOSITORY:-absent} in
      absent) ;;
      tree-error) exit 128 ;;
      symlink) printf '120000 blob %s\tscripts/lib/repository.sh\n' "$TEST_OTHER" ;;
      executable) printf '100755 blob %s\tscripts/lib/repository.sh\n' "$TEST_OTHER" ;;
      *) printf '100644 blob %s\tscripts/lib/repository.sh\n' "$TEST_OTHER" ;;
    esac
    ;;
  show)
    [[ $* == "show $TEST_COMMIT:scripts/lib/repository.sh" ]] || exit 2
    cat "$ELIGIBILITY_TEST_DIR/tag-repository"
    ;;
  *) echo "unexpected git command: $*" >&2; exit 2 ;;
esac
STUB
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ $1 == release ]]; then echo release >>"$ELIGIBILITY_TEST_DIR/writes"; exit 0; fi
[[ " $* " == *" --repo ${EXPECT_REPO:-dannyota/aboutme} "* && " $* " == *' --workflow ci.yml '* &&
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
export TAG=v0.7.5 COMMIT=$TEST_COMMIT OWNER=dannyota
repository_text() { printf '# shellcheck shell=bash disable=SC2034\nowner=%s\nname=aboutme\n' "$1"; }
malformed_repositories=(
  $'# shellcheck shell=bash disable=SC2034\nowner=evil\nname=aboutme\n'
  $'# shellcheck shell=bash disable=SC2034\nowner=Dannyota\nname=aboutme\n'
  $'# shellcheck shell=bash disable=SC2034\nowner=dannyota\nname=other\n'
  $'owner=dannyota\nname=aboutme\n'
  $'# shellcheck shell=bash disable=SC2034\r\nowner=dannyota\r\nname=aboutme\r\n'
  $'# shellcheck shell=bash disable=SC2034\nowner=dannyota\nowner=dntmakers\nname=aboutme\n'
  $'# shellcheck shell=bash disable=SC2034\nname=aboutme\nowner=dannyota\n'
  $'# shellcheck shell=bash disable=SC2034\nowner=$(id)\nname=aboutme\n'
  $'# shellcheck shell=bash disable=SC2034\nowner=dannyota\nname=aboutme'
  $'# shellcheck shell=bash disable=SC2034\nowner=dannyota\nname=aboutme\nextra=1\n'
  $'# shellcheck shell=bash disable=SC2034\nowner=dannyota\nname=aboutme\n\n'
  $' # shellcheck shell=bash disable=SC2034\nowner=dannyota\nname=aboutme\n'
  $'# shellcheck shell=bash disable=SC2034\nowner= dannyota\nname=aboutme\n'
  ''
)
write_malformed() { printf '%s' "${malformed_repositories[$1]}" >"$2"; }
mkdir -p "$work/workspace/scripts/lib" "$work/workspace/.release-trusted/scripts/lib"
cp "$root/scripts/release-eligibility.sh" "$work/workspace/scripts/"
cp "$root/scripts/release-eligibility.sh" "$work/workspace/.release-trusted/scripts/"
repository_text dannyota >"$work/workspace/scripts/lib/repository.sh"
repository_text dannyota >"$work/workspace/.release-trusted/scripts/lib/repository.sh"
cd "$work/workspace"

owner_of() {
  bash scripts/release-eligibility.sh --owner "$@" >"$work/out" 2>&1
}
for owner in dannyota dntmakers; do
  TAG_REPOSITORY=present
  export TAG_REPOSITORY
  repository_text "$owner" >"$work/tag-repository"
  owner_of "$TEST_COMMIT" || { cat "$work/out" >&2; fail "--owner refused $owner"; }
  [[ $(cat "$work/out") == "$owner" ]] || fail "--owner did not print $owner"
done
TAG_REPOSITORY=absent
owner_of "$TEST_COMMIT" || { cat "$work/out" >&2; fail '--owner refused a commit without the file'; }
[[ $(cat "$work/out") == dannyota ]] || fail 'a commit without the file must mean dannyota'
for TAG_REPOSITORY in tree-error symlink executable; do
  if owner_of "$TEST_COMMIT"; then fail "--owner accepted $TAG_REPOSITORY"; fi
  grep -q 'release-eligibility:' "$work/out" || { cat "$work/out" >&2; fail "$TAG_REPOSITORY failed outside the reader"; }
done
TAG_REPOSITORY=present
for i in "${!malformed_repositories[@]}"; do
  write_malformed "$i" "$work/tag-repository"
  if owner_of "$TEST_COMMIT"; then fail "--owner accepted malformed file $i"; fi
  grep -q 'release-eligibility:' "$work/out" || { cat "$work/out" >&2; fail "malformed file $i failed outside the reader"; }
done
for args in "$TEST_COMMIT extra" HEAD "${TEST_COMMIT:0:39}" "-$TEST_COMMIT" "${TEST_COMMIT^^}"; do
  # shellcheck disable=SC2086
  if owner_of $args; then fail "--owner accepted arguments: $args"; fi
done
TAG_REPOSITORY=absent
rm scripts/lib/repository.sh
owner_of || { cat "$work/out" >&2; fail '--owner refused a checkout without the file'; }
[[ $(cat "$work/out") == dannyota ]] || fail 'a checkout without the file must mean dannyota'
for i in "${!malformed_repositories[@]}"; do
  write_malformed "$i" scripts/lib/repository.sh
  if owner_of; then fail "checkout reader accepted malformed file $i"; fi
done
rm scripts/lib/repository.sh
ln -s "$work/tag-repository" scripts/lib/repository.sh
repository_text dannyota >"$work/tag-repository"
if owner_of; then fail 'checkout reader followed a symbolic link'; fi
rm scripts/lib/repository.sh
repository_text dannyota >scripts/lib/repository.sh
mkdir -p "$work/tracked/scripts/lib"
cp "$root/scripts/release-eligibility.sh" "$work/tracked/scripts/"
cp "$root/scripts/lib/repository.sh" "$work/tracked/scripts/lib/" 2>/dev/null ||
  fail 'scripts/lib/repository.sh is missing'
tracked=$(bash "$work/tracked/scripts/release-eligibility.sh" --owner 2>&1) ||
  fail "the tracked repository file is refused: $tracked"
[[ $tracked == dannyota || $tracked == dntmakers ]] || fail "the tracked repository file names $tracked"
[[ -f $root/scripts/lib/repository.sh && ! -L $root/scripts/lib/repository.sh && ! -x $root/scripts/lib/repository.sh ]] ||
  fail 'scripts/lib/repository.sh must be a regular, non-executable file'

echo 'repository owner reader accepts both owners and refuses malformed files'

runs() {
  jq -n --arg sha "$TEST_COMMIT" --arg event "${1:-push}" --arg branch "${2:-main}" \
    --arg conclusion "${3:-success}" --arg status "${4:-completed}" \
    '[{databaseId: 20, headSha: $sha, event: $event, headBranch: $branch,
       conclusion: $conclusion, status: $status}]' >"$work/runs.json"
}
publication() {
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
runs
for CHECKOUT_CASE in dirty staged untracked stale branch detached status-error; do
  export CHECKOUT_CASE
  if bash scripts/release-eligibility.sh --deploy-checkout "$TAG" >"$work/out" 2>&1; then
    fail "$CHECKOUT_CASE checkout was accepted"
  fi
  grep -q 'release-eligibility:.*checkout' "$work/out" || {
    cat "$work/out" >&2; fail "$CHECKOUT_CASE failed outside checkout validation"
  }
  echo "rejected $CHECKOUT_CASE checkout"
done
unset CHECKOUT_CASE
bash scripts/release-eligibility.sh --deploy-checkout "$TAG" >"$work/out" 2>&1 || {
  cat "$work/out" >&2; fail 'clean current main checkout refused'
}
[[ $(cat "$work/out") == "$TEST_COMMIT" ]] || fail 'checkout gate did not return the resolved commit'

for CHECKOUT_CASE in dirty staged untracked stale branch detached status-error; do
  export CHECKOUT_CASE
  if TAG='' bash scripts/release-eligibility.sh --deploy-checkout >"$work/out" 2>&1; then
    fail "tagless $CHECKOUT_CASE checkout was accepted"
  fi
  grep -Eq 'release-eligibility: (cannot inspect )?deployment checkout' "$work/out" || {
    cat "$work/out" >&2; fail "tagless $CHECKOUT_CASE failed outside checkout validation"
  }
  echo "rejected tagless $CHECKOUT_CASE checkout"
done
unset CHECKOUT_CASE
if TAG='' TEST_CASE=fetch-error bash scripts/release-eligibility.sh --deploy-checkout >"$work/out" 2>&1; then
  fail 'tagless checkout passed without fetching main'
fi
grep -q 'release-eligibility: cannot fetch main' "$work/out" || { cat "$work/out" >&2; fail 'tagless fetch error'; }
if TAG='' bash scripts/release-eligibility.sh >"$work/out" 2>&1; then fail 'tagless publication gate accepted'; fi
grep -q 'release-eligibility: usage' "$work/out" || { cat "$work/out" >&2; fail 'tagless publication usage'; }
runs push main failure
: >"$work/git.log"
TAG='' TEST_CASE=api-error bash scripts/release-eligibility.sh --deploy-checkout >"$work/out" 2>&1 || {
  cat "$work/out" >&2; fail 'clean current main tagless checkout refused'
}
[[ ! -s $work/out ]] || fail 'tagless checkout printed output'
grep -q '^-C .* fetch ' "$work/git.log" || fail 'tagless checkout did not fetch main'
! grep -qE 'refs/tags/|merge-base' "$work/git.log" || fail 'tagless checkout read a tag'
echo 'accepted tagless clean current main checkout'
runs

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
for TAG in v01.2.3 v1.2 v1.2.3-rc1 'v1.2.3;false' main v1000.0.0 v0.1000.0 v0.0.1000; do
  TEST_CASE=malformed-tag
  reject
done
TEST_CASE=success
runs
for TAG in v0.7.5 v999.999.999; do
  : >"$work/writes"
  publication >"$work/out" 2>&1 || { cat "$work/out" >&2; fail 'valid release refused'; }
  [[ $(cat "$work/out") == "$TEST_COMMIT" ]] || fail 'gate did not return the resolved commit'
  grep -q 'docker push' "$work/writes" || fail 'valid release did not reach publication'
done
TAG=v0.7.5
export TAG_REPOSITORY=present
repository_text dannyota >"$work/tag-repository"
publication >"$work/out" 2>&1 || { cat "$work/out" >&2; fail 'a dannyota tag refused under dannyota'; }
repository_text dntmakers >.release-trusted/scripts/lib/repository.sh
repository_text dntmakers >"$work/tag-repository"
: >"$work/writes"
OWNER=dntmakers EXPECT_REPO=dntmakers/aboutme publication >"$work/out" 2>&1 || {
  cat "$work/out" >&2; fail 'a dntmakers tag refused under dntmakers'
}
grep -q 'docker push' "$work/writes" || fail 'a dntmakers release did not reach publication'
said() { grep -Fq -- "$1" "$work/out" || { cat "$work/out" >&2; fail "$TEST_CASE: expected '$1'"; }; }
TEST_CASE=dannyota-repository-for-dntmakers-tag
OWNER=dannyota EXPECT_REPO=dntmakers/aboutme reject
said 'names dntmakers in scripts/lib/repository.sh, not the publishing owner dannyota'
repository_text dannyota >.release-trusted/scripts/lib/repository.sh
TEST_CASE=wrong-window
TAG_REPOSITORY=absent OWNER=dntmakers reject
said 'names dannyota in scripts/lib/repository.sh, not the publishing owner dntmakers'
TEST_CASE=unknown-owner-context
TAG_REPOSITORY=absent OWNER=evil reject
said 'not the publishing owner evil'
for OWNER in '' 'dannyota/aboutme' '-x' "$(printf 'a%.0s' {1..40})"; do
  TEST_CASE="invalid owner argument '$OWNER'"
  reject
  said 'expected owner must be a GitHub login'
done
OWNER=dannyota
for TAG_REPOSITORY in tree-error symlink executable; do
  TEST_CASE=tag-repository-$TAG_REPOSITORY
  reject
  case $TAG_REPOSITORY in
    tree-error) said 'cannot inspect scripts/lib/repository.sh' ;;
    *) said 'scripts/lib/repository.sh at '"$TEST_COMMIT"' is not a regular file' ;;
  esac
done
TAG_REPOSITORY=present
for i in "${!malformed_repositories[@]}"; do
  TEST_CASE=malformed-tag-repository-$i
  write_malformed "$i" "$work/tag-repository"
  reject
  said "scripts/lib/repository.sh at $TEST_COMMIT must hold exactly"
done
repository_text dannyota >"$work/tag-repository"
for i in "${!malformed_repositories[@]}"; do
  TEST_CASE=malformed-trusted-repository-$i
  write_malformed "$i" .release-trusted/scripts/lib/repository.sh
  : >"$work/git.log"
  reject
  said 'scripts/lib/repository.sh must hold exactly'
  ! grep -q ' fetch ' "$work/git.log" || fail "malformed trusted repository $i reached fetch"
done
rm .release-trusted/scripts/lib/repository.sh
TAG_REPOSITORY=absent
publication >"$work/out" 2>&1 || { cat "$work/out" >&2; fail 'a trusted checkout without the file refused'; }
repository_text dannyota >.release-trusted/scripts/lib/repository.sh
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
TEST_CASE=rerun-after-transfer
runs
for job in eligibility $write_jobs; do
  TAG_REPOSITORY=absent OWNER=dntmakers expect_job_rejected "$job"
done
[[ $failures == 0 ]] || fail "$failures publication bypasses"

expected_prefix=$(cat <<'YAML'
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          repository: ${{ github.repository }}
          ref: refs/heads/main
          path: .release-trusted
          fetch-depth: 0
          persist-credentials: false
      - name: Check release eligibility
        if: github.event_name == 'push'
        env:
          TAG: ${{ github.ref_name }}
          COMMIT: ${{ github.sha }}
          OWNER: ${{ github.repository_owner }}
          GH_TOKEN: ${{ github.token }}
        run: bash .release-trusted/scripts/release-eligibility.sh "$TAG" "$COMMIT" "$OWNER"
YAML
)
for job in eligibility $write_jobs; do
  body=$(job_body "$job")
  prefix=$(awk '/^    steps:$/ { found=1; next } found { print; if (/^        run: /) exit }' <<<"$body")
  [[ $prefix == "$expected_prefix" ]] || fail "$job does not start with the trusted main gate"
  grep -Eq '^      actions: read$' <<<"$body" || fail "$job cannot refresh CI evidence"
done
echo 'release eligibility test passed'
