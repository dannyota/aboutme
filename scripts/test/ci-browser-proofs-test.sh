#!/usr/bin/env bash

# Tests scripts/ci-browser-proofs.sh against a fake make: proof order and
# harness restarts, shard environment, shard evidence staging (including a
# TOTP shard list split into one staging directory per shard), the request
# peak in the summary, and that one failing proof neither stops the rest nor
# lets the group pass.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-ci-browser-proofs.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

fail() {
  printf 'ci-browser-proofs-test: %s\n' "$*" >&2
  exit 1
}

REPO=$WORK/repo
BIN=$WORK/bin
mkdir -p "$REPO/scripts" "$REPO/.dev/native-https/evidence" \
  "$REPO/.dev/native-https/log" "$BIN"
cp "$ROOT/scripts/ci-browser-proofs.sh" "$REPO/scripts/"

# The fake make logs each target with the shard variables it saw. A TOTP
# check leaves one enabled run directory holding each listed shard's own
# evidence directory, plus disabled evidence when epoch-disabled is listed; a
# passkey check leaves enabled evidence; the editor check logs 61 requests
# over 70 seconds, one of them answered 429; and dev-https-exports-check
# fails.
cat >"$BIN/make" <<'EOF'
#!/usr/bin/env bash
printf '%s totp=%s passkey=%s\n' "$1" "${ABOUTME_TOTP_SHARD-unset}" \
  "${ABOUTME_PASSKEY_SHARD-unset}" >>"$CALL_LOG"
evidence=.dev/native-https/evidence
case $1 in
dev-https-totp-check)
  IFS=, read -ra shards <<<"$ABOUTME_TOTP_SHARD"
  for shard in "${shards[@]}"; do
    mkdir -p "$evidence/totp-enabled.x/$shard"
    : >"$evidence/totp-enabled.x/$shard/totp-second-factor-proof.json"
    : >"$evidence/totp-enabled.x/$shard/totp-timing.json"
  done
  if [[ ,$ABOUTME_TOTP_SHARD, == *,epoch-disabled,* ]]; then
    mkdir -p "$evidence/totp-disabled.y"
  fi
  ;;
dev-https-editor-check)
  for second in $(seq 0 70); do
    status=200
    [ "$second" -ne 5 ] || status=429
    printf 'time=2026-09-25T17:%02d:%02d.000Z level=INFO msg=http_request method=GET path=/x status=%s\n' \
      $((10 + second / 60)) $((second % 60)) "$status"
  done >>.dev/native-https/log/server.log
  ;;
dev-https-passkey-check)
  mkdir -p "$evidence/passkey-enabled.$ABOUTME_PASSKEY_SHARD"
  ;;
dev-https-exports-check) exit 3 ;;
esac
exit 0
EOF
chmod +x "$BIN/make"

run_group() {
  local name=$1
  shift
  : >"$WORK/$name.calls"
  : >"$WORK/$name.summary"
  (
    cd "$REPO"
    PATH="$BIN:/usr/bin:/bin" CALL_LOG="$WORK/$name.calls" \
      GITHUB_STEP_SUMMARY="$WORK/$name.summary" \
      bash scripts/ci-browser-proofs.sh "$@"
  ) >"$WORK/$name.out" 2>&1
}

run_group ok totp:skew,epoch-disabled passkey:primary-disabled editor ||
  fail "a passing group failed: $(cat "$WORK/ok.out")"
expected='dev-https-totp-check totp=skew,epoch-disabled passkey=unset
dev-https-down totp=unset passkey=unset
dev-https totp=unset passkey=unset
dev-https-passkey-check totp=unset passkey=primary-disabled
dev-https-down totp=unset passkey=unset
dev-https totp=unset passkey=unset
dev-https-editor-check totp=unset passkey=unset'
[ "$(cat "$WORK/ok.calls")" = "$expected" ] ||
  fail "unexpected make calls: $(cat "$WORK/ok.calls")"
staged=$REPO/.dev/proof-evidence
for shard in skew epoch-disabled; do
  [ -f "$staged/totp-browser-proof-evidence-$shard/totp-enabled.x/totp-second-factor-proof.json" ] &&
    [ -f "$staged/totp-browser-proof-evidence-$shard/totp-enabled.x/totp-timing.json" ] ||
    fail "TOTP $shard evidence was not staged under its shard directory"
done
[ -d "$staged/totp-browser-proof-evidence-epoch-disabled/totp-disabled.y" ] ||
  fail "TOTP disabled-phase evidence was not staged with the epoch-disabled shard"
if [ -e "$staged/totp-browser-proof-evidence-skew/totp-disabled.y" ]; then
  fail "TOTP disabled-phase evidence was staged with a shard that did not prove it"
fi
[ -d "$staged/passkey-browser-proof-evidence-primary-disabled/passkey-enabled.primary-disabled" ] ||
  fail "passkey shard evidence was not staged under its shard directory"
if compgen -G "$REPO/.dev/native-https/evidence/*" >/dev/null; then
  fail "shard evidence was left in the evidence root"
fi
grep -Fq '| editor | passed |' "$WORK/ok.summary" ||
  fail "summary lacks the passing proof row"
grep -Eq '^\| editor \| passed \| [0-9]+ \| 60 \| 1 \|$' "$WORK/ok.summary" ||
  fail "summary lacks the editor proof's request peak and 429 count: $(cat "$WORK/ok.summary")"
grep -Eq '^\| passkey:primary-disabled \| passed \| [0-9]+ \| 0 \| 0 \|$' "$WORK/ok.summary" ||
  fail "summary counted another proof's requests: $(cat "$WORK/ok.summary")"

if run_group failing exports auth; then
  fail "a group with a failing proof passed"
fi
grep -Fq 'dev-https-auth-check' "$WORK/failing.calls" ||
  fail "a failing proof stopped the rest of its group"
grep -Fq '| exports | failed (exit 3) |' "$WORK/failing.summary" ||
  fail "summary lacks the failing proof row"
grep -Fq 'failed proofs: exports' "$WORK/failing.out" ||
  fail "the group did not name its failing proof"

for bad in '' 'totp:' 'Editor' '../x' 'totp:skew;id' 'totp:skew,' 'totp:,skew' \
  'passkey:primary-disabled,recovery-attempts' 'editor,exports'; do
  : >"$WORK/bad.calls"
  status=0
  (
    cd "$REPO"
    PATH="$BIN:/usr/bin:/bin" CALL_LOG="$WORK/bad.calls" \
      GITHUB_STEP_SUMMARY=/dev/null bash scripts/ci-browser-proofs.sh $bad
  ) >/dev/null 2>&1 || status=$?
  [ "$status" -eq 64 ] || fail "proof name '$bad' exited $status, want 64"
  [ ! -s "$WORK/bad.calls" ] || fail "proof name '$bad' reached make"
done

printf 'ci-browser-proofs tests passed\n'
