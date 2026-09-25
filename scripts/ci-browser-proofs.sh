#!/usr/bin/env bash
# Runs a group of trusted-browser proofs one after another against the running
# native HTTPS harness, for the hosted dev-https-proofs job. Each proof after
# the first gets a freshly restarted harness (same database and image), so no
# proof inherits another proof's in-memory server state such as rate limits.
# A failing proof does not stop the rest; the script fails after all of them
# ran, and the job summary lists each proof's result.
#
# A proof is either NAME, which runs make dev-https-NAME-check, or
# passkey:SHARD, which runs one shard of the sharded passkey journey
# (second-factor.spec.ts "Enabled-proof sharding"), or
# totp:SHARD[,SHARD...], which runs the listed shards of the TOTP journey as
# parallel tests against one harness (totp.spec.ts "Enabled-proof
# sharding"). Each shard's evidence moves to
# .dev/proof-evidence/KIND-browser-proof-evidence-SHARD/, the directory name
# the shard coverage checks read the shard from.
#
# Every request the proofs send reaches the server from one client IP, so
# the job summary and log also list each proof's peak server requests in any
# 60 seconds and its 429 answers: the headroom under the per-IP whole-server
# rate budget that parallel shards share.
set -Euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

usage() {
  echo 'usage: scripts/ci-browser-proofs.sh PROOF...' >&2
  echo '  PROOF is NAME, totp:SHARD[,SHARD...], or passkey:SHARD' >&2
  exit 64
}

[ "$#" -gt 0 ] || usage
for proof in "$@"; do
  [[ $proof =~ ^(totp:[a-z][a-z-]*(,[a-z][a-z-]*)*|(passkey:)?[a-z][a-z-]*)$ ]] || {
    echo "ci-browser-proofs: invalid proof name: $proof" >&2
    usage
  }
done

evidence_root=.dev/native-https/evidence
staging=.dev/proof-evidence
server_log=.dev/native-https/log/server.log
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

# stage_totp_evidence moves each listed TOTP shard's /evidence/SHARD/ output
# out of every totp-enabled.* run directory into that shard's staging
# directory under the run directory's own name. Whatever is left, such as
# the disabled-phase run, then goes to epoch-disabled when it is listed, or
# to the first listed shard.
stage_totp_evidence() {
  local list=$1 shard run owner dest
  local -a shards=()
  IFS=, read -ra shards <<<"$list"
  owner=${shards[0]}
  for shard in "${shards[@]}"; do
    [ "$shard" != epoch-disabled ] || owner=$shard
    dest=$staging/totp-browser-proof-evidence-$shard
    install -d -m 0700 "$staging" "$dest"
    for run in "$evidence_root"/totp-enabled.*; do
      [ -d "$run/$shard" ] || continue
      mv -- "$run/$shard" "$dest/${run##*/}"
    done
  done
  for run in "$evidence_root"/totp-enabled.*; do
    [ ! -d "$run" ] || rmdir -- "$run" 2>/dev/null || true
  done
  move_shard_evidence totp "$owner"
}

# log_lines prints how many lines the server log holds now.
log_lines() {
  if [ -f "$server_log" ]; then wc -l <"$server_log"; else echo 0; fi
}

# api_load FROM prints "PEAK | 429S" for the server log lines after line
# FROM: the most requests the server logged in any 60 seconds, and how many
# it answered 429. Only counts leave the log.
api_load() {
  local from=$1 lines peak
  lines=$(tail -n "+$((from + 1))" "$server_log" 2>/dev/null |
    grep -F ' msg=http_request ' || true)
  peak=$(sed -nE 's/^time=([^ ]+) .*/\1/p' <<<"$lines" | { date -f - +%s 2>/dev/null || true; } |
    sort -n | awk '{ t[NR] = $1 }
      END { j = 1; max = 0
        for (i = 1; i <= NR; i++) { while (t[i] - t[j] >= 60) j++; if (i - j + 1 > max) max = i - j + 1 }
        print max }')
  printf '%s | %s' "$peak" "$(grep -cE ' status=429( |$)' <<<"$lines" || true)"
}

failed=()
first=1
{
  echo "| Proof | Result | Seconds | Peak requests in 60 s | 429 answers |"
  echo "|-|-|-|-|-|"
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
  log_start=$(log_lines)
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
  case $kind in
  totp) stage_totp_evidence "$shard" || status=1 ;;
  passkey) move_shard_evidence "$kind" "$shard" || status=1 ;;
  esac
  echo "::endgroup::"
  if [ "$status" -eq 0 ]; then
    result=passed
  else
    result="failed (exit $status)"
    failed+=("$proof")
    echo "::error title=$proof::$target failed with exit $status"
  fi
  row="| $proof | $result | $((SECONDS - start)) | $(api_load "$log_start") |"
  echo "$row" >>"$summary"
  echo "ci-browser-proofs: $row"
done
if [ "${#failed[@]}" -ne 0 ]; then
  echo "failed proofs: ${failed[*]}" >&2
  exit 1
fi
