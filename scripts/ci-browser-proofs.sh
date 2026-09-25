#!/usr/bin/env bash
# Runs a group of trusted-browser proofs one after another against the running
# native HTTPS harness, for the hosted dev-https-proofs job. Each proof after
# the first gets a freshly restarted harness (same database and image), so no
# proof inherits another proof's in-memory server state such as rate limits.
# A failing proof does not stop the rest; the script fails after all of them
# ran, and the job summary lists each proof's result.
#
# A proof is either NAME, which runs make dev-https-NAME-check, or
# totp:SHARD or passkey:SHARD, which runs one shard of the sharded
# second-factor journey (totp.spec.ts and second-factor.spec.ts,
# "Enabled-proof sharding"). A shard's evidence moves to
# .dev/proof-evidence/KIND-browser-proof-evidence-SHARD/, the directory name
# the shard coverage checks read the shard from.
set -Euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

usage() {
  echo 'usage: scripts/ci-browser-proofs.sh PROOF...' >&2
  echo '  PROOF is NAME, totp:SHARD, or passkey:SHARD' >&2
  exit 64
}

[ "$#" -gt 0 ] || usage
for proof in "$@"; do
  [[ $proof =~ ^((totp|passkey):)?[a-z][a-z-]*$ ]] || {
    echo "ci-browser-proofs: invalid proof name: $proof" >&2
    usage
  }
done

evidence_root=.dev/native-https/evidence
staging=.dev/proof-evidence
summary=${GITHUB_STEP_SUMMARY:-/dev/null}

# move_shard_evidence moves every KIND-* evidence directory the shard left in
# the evidence root into the shard's own staging directory.
move_shard_evidence() {
  local kind=$1 shard=$2 dest dir
  dest=$staging/$kind-browser-proof-evidence-$shard
  install -d -m 0700 "$staging" "$dest"
  for dir in "$evidence_root/$kind"-*; do
    [ -d "$dir" ] || continue
    mv -- "$dir" "$dest/"
  done
}

failed=()
first=1
{
  echo "| Proof | Result | Seconds |"
  echo "|-|-|-|"
} >>"$summary"
for proof in "$@"; do
  kind=
  shard=
  case $proof in
  totp:* | passkey:*)
    kind=${proof%%:*}
    shard=${proof#*:}
    target=dev-https-$kind-check
    ;;
  *) target=dev-https-$proof-check ;;
  esac
  echo "::group::$proof"
  start=$SECONDS
  status=0
  if [ "$first" -eq 0 ]; then
    { make dev-https-down && make dev-https; } || status=$?
  fi
  first=0
  if [ "$status" -eq 0 ]; then
    case $kind in
    totp) ABOUTME_TOTP_SHARD=$shard make "$target" || status=$? ;;
    passkey) ABOUTME_PASSKEY_SHARD=$shard make "$target" || status=$? ;;
    *) make "$target" || status=$? ;;
    esac
  fi
  if [ -n "$kind" ]; then
    move_shard_evidence "$kind" "$shard" || status=1
  fi
  echo "::endgroup::"
  if [ "$status" -eq 0 ]; then
    result=passed
  else
    result="failed (exit $status)"
    failed+=("$proof")
    echo "::error title=$proof::$target failed with exit $status"
  fi
  echo "| $proof | $result | $((SECONDS - start)) |" >>"$summary"
done
if [ "${#failed[@]}" -ne 0 ]; then
  echo "failed proofs: ${failed[*]}" >&2
  exit 1
fi
