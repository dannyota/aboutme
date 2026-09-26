#!/usr/bin/env bash
# Checks observer.sh's order and failures against stubbed commands.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

digest=sha256:$(printf 'ab%062d' 5)
other=sha256:$(printf 'cd%062d' 5)
commit=0123456789abcdef0123456789abcdef01234567
registry=111122223333.dkr.ecr.ap-southeast-1.amazonaws.com

mkdir -p "$work/bin"
for cmd in aws gh git curl skopeo; do
  cat >"$work/bin/$cmd" <<'STUB'
#!/usr/bin/env bash
printf '[%s] %s %s\n' "${AWS_PROFILE:-none}" "$(basename "$0")" "$*" >>"$CALLS"
exec bash "$STUB_DIR/respond" "$(basename "$0")" "$@"
STUB
  chmod +x "$work/bin/$cmd"
done

# One responder; $STUB_CASE picks the scenario.
cat >"$work/respond" <<STUB
#!/usr/bin/env bash
cmd=\$1; shift; args="\$*"
digest=$digest other=$other commit=$commit registry=$registry
STUB
cat >>"$work/respond" <<'STUB'
state=$STUB_DIR/copied.$STUB_CASE
case "$cmd $args" in
  "git fetch"*) exit 0 ;;
  "git rev-list"*) echo "$commit" ;;
  "git merge-base"*) [[ $STUB_CASE != not_on_main ]] ;;
  "curl "*"ghcr.io/token"*) echo '{"token":"anonymous"}' ;;
  "curl "*"/manifests/"*)
    [[ $STUB_CASE == no_image ]] || printf 'docker-content-digest: %s\r\n' "$digest" ;;
  "gh attestation verify"*)
    [[ $STUB_CASE != provenance_fails ]] || exit 1
    printf '[{"verificationResult":{"statement":{"subject":[{"name":"ghcr.io/dannyota/aboutme-observer","digest":{"sha256":"%s"}}]}}}]\n' "${digest#sha256:}" ;;
  "aws sts get-caller-identity --query Arn"*) echo 'arn:aws:iam::111122223333:user/operator' ;;
  "aws sts get-caller-identity --query Account"*) echo 111122223333 ;;
  "aws "*"sts get-caller-identity --query Arn --output text")
    case $AWS_PROFILE in
      fence-operator) echo 'arn:aws:sts::111122223333:assumed-role/aboutme-prod-operator/s' ;;
      fence-deploy) echo 'arn:aws:sts::111122223333:assumed-role/aboutme-prod-deploy/s' ;;
    esac ;;
  "aws "*"ecr describe-images"*)
    if [[ -e $state ]]; then
      [[ $STUB_CASE == copy_changes_digest ]] && echo "$other" || echo "$digest"
    else
      case $STUB_CASE in
        already_copied) echo "$digest" ;;
        tag_taken) echo "$other" ;;
        ecr_denied) echo "AccessDeniedException" >&2; exit 254 ;;
        *) echo "An error occurred (ImageNotFoundException)" >&2; exit 254 ;;
      esac
    fi ;;
  "aws "*"ecr get-login-password"*) echo 'stub-password' ;;
  "skopeo copy"*) : >"$state" ;;
  "aws "*"lambda get-function --function-name aboutme-prod-observer --query Configuration.FunctionName"*)
    case $STUB_CASE in
      no_function) echo "An error occurred (ResourceNotFoundException)" >&2; exit 254 ;;
      *) echo aboutme-prod-observer ;;
    esac ;;
  "aws "*"lambda update-function-code"*) : >"$STUB_DIR/updated.$STUB_CASE" ;;
  "aws "*"lambda wait function-updated-v2"*) exit 0 ;;
  "aws "*"lambda get-function --function-name aboutme-prod-observer --query Code.ImageUri"*)
    if [[ $STUB_CASE == stale_function ]]; then echo "$registry/aboutme-prod-observer@$other"
    else echo "$registry/aboutme-prod-observer@$digest"; fi ;;
  *) echo "unexpected: $cmd $args" >&2; exit 99 ;;
esac
STUB

run_case() { # name want(0|fail) [args]
  local name=$1 want=$2 got
  shift 2
  : >"$work/$name.calls"
  set +e
  CALLS="$work/$name.calls" STUB_DIR="$work" STUB_CASE="$name" PATH="$work/bin:$PATH" \
    AWS_CONFIG_FILE="$work/no-such-aws-config" AWS_PROFILE=test-base \
    bash "$here/observer.sh" "${@:-v0.6.5}" >"$work/$name.out" 2>&1
  got=$?
  set -e
  if [[ $want == fail ]]; then
    ((got != 0)) || { echo "$name: exit 0, want failure" >&2; cat "$work/$name.out" >&2; exit 1; }
  elif ((got != want)); then
    echo "$name: exit $got, want $want" >&2
    cat "$work/$name.out" >&2
    exit 1
  fi
  if grep -q '^unexpected' "$work/$name.out"; then
    echo "$name: an unstubbed command ran" >&2
    cat "$work/$name.out" >&2
    exit 1
  fi
}
line() { { grep -n -m1 -F -- "$2" "$1" || true; } | cut -d: -f1; }
before() {
  local a b
  a=$(line "$1" "$2")
  b=$(line "$1" "$3")
  [[ -n $a && -n $b && $a -lt $b ]] || { echo "$(basename "$1"): '$2' must precede '$3'" >&2; exit 1; }
}
absent() { ! grep -q -F -- "$2" "$1" || { echo "$(basename "$1"): unexpected '$2'" >&2; exit 1; }; }

# A normal update: provenance for this tag and commit first, then every AWS
# mutation as the deploy role, the copy by digest, and the function by digest.
run_case ok 0
f=$work/ok.calls
policy="gh attestation verify oci://ghcr.io/dannyota/aboutme-observer@$digest --repo dannyota/aboutme"
policy+=" --cert-identity https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/v0.6.5"
policy+=" --source-ref refs/tags/v0.6.5 --source-digest $commit --deny-self-hosted-runners --format json"
grep -qF -- "$policy" "$f" || { echo "ok: provenance not verified with the full policy" >&2; exit 1; }
before "$f" "$policy" "sts get-caller-identity"
before "$f" "$policy" "skopeo copy"
grep -qF -- "skopeo copy --preserve-digests --retry-times 3 --dest-authfile" "$f" ||
  { echo "ok: the copy does not preserve digests" >&2; exit 1; }
grep -qF -- "docker://ghcr.io/dannyota/aboutme-observer@$digest docker://$registry/aboutme-prod-observer:v0.6.5" "$f" ||
  { echo "ok: the copy is not by digest into the observer repository" >&2; exit 1; }
grep -qF -- "[fence-deploy] aws --region ap-southeast-1 lambda update-function-code --function-name aboutme-prod-observer --image-uri $registry/aboutme-prod-observer@$digest" "$f" ||
  { echo "ok: the function was not updated by digest as the deploy role" >&2; exit 1; }
before "$f" "skopeo copy" "lambda update-function-code"
grep -qF "observer_image_digest = \"$digest\"" "$work/ok.out" ||
  { echo "ok: the digest to record was not printed" >&2; exit 1; }
absent "$f" "stub-password"
absent "$f" " ecs "
for mutation in "ecr get-login-password" "lambda update-function-code" "ecr describe-images"; do
  ! grep -F -- "$mutation" "$f" | grep -qv '^\[fence-deploy\]' ||
    { echo "ok: $mutation ran outside the deploy role" >&2; exit 1; }
done

# An image already in ECR with the same digest is not copied again.
run_case already_copied 0
absent "$work/already_copied.calls" "skopeo copy"
grep -qF "lambda update-function-code" "$work/already_copied.calls" ||
  { echo "already_copied: the function was not updated" >&2; exit 1; }

# Before OpenTofu creates the function, the copy happens and the script
# prints the digest to set, without an update.
run_case no_function 0
absent "$work/no_function.calls" "lambda update-function-code"
grep -qF "observer_image_digest = \"$digest\"" "$work/no_function.out" ||
  { echo "no_function: the digest to set was not printed" >&2; exit 1; }

# Every refusal happens before anything reaches ECR or Lambda.
for name in not_on_main no_image provenance_fails; do
  run_case "$name" fail
  absent "$work/$name.calls" "skopeo copy"
  absent "$work/$name.calls" "lambda update-function-code"
  absent "$work/$name.calls" "sts get-caller-identity"
done
# An immutable tag that already names another digest, or an unreadable
# repository, stops before the copy.
for name in tag_taken ecr_denied; do
  run_case "$name" fail
  absent "$work/$name.calls" "skopeo copy"
  absent "$work/$name.calls" "lambda update-function-code"
done
# A copy whose ECR digest differs never reaches the function.
run_case copy_changes_digest fail
absent "$work/copy_changes_digest.calls" "lambda update-function-code"
# A function that does not end up on the digest fails.
run_case stale_function fail
# Malformed and extra arguments.
run_case bad_tag fail v0.6
absent "$work/bad_tag.calls" "git"
run_case extra_args 2 v0.6.5 v0.6.6

echo "observer-script-test: ok"
