#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=vng-lib.sh
. "$here/vng-lib.sh"

# vStorage encryption covers only objects uploaded after it is on (vngcloud wiki, Storage-Bucket-Policy.md).
BUCKET_ENCRYPTION=${BUCKET_ENCRYPTION:-required}

BUCKETS=(aboutme-media aboutme-backups aboutme-tfstate)

usage() {
  say "usage: buckets.sh --storage-project-id <id> [--rotate-key <bucket>]" >&2
  exit 2
}

project=
rotate=
while (($#)); do
  case $1 in
    --storage-project-id)
      (($# >= 2)) || usage
      project=$2
      shift 2
      ;;
    --rotate-key)
      (($# >= 2)) || usage
      rotate=$2
      shift 2
      ;;
    *) usage ;;
  esac
done
[[ -n $project ]] || usage
if [[ -n $rotate ]]; then
  ok=0
  for b in "${BUCKETS[@]}"; do [[ $b == "$rotate" ]] && ok=1; done
  ((ok)) || usage
fi
case $BUCKET_ENCRYPTION in required | off) ;; *) die "BUCKET_ENCRYPTION must be required or off" ;; esac

need_tools
umask 077
[[ -n ${XDG_RUNTIME_DIR:-} ]] || die "XDG_RUNTIME_DIR is not set; key files need a tmpfs directory"

scratch=$(mktemp -d "$XDG_RUNTIME_DIR/aboutme-buckets.XXXXXX")
trap 'rm -rf "$scratch"' EXIT

vst() {
  local cmd=$1
  shift
  vng storage "$cmd" --project-id "$project" "$@"
}

key_file() {
  if [[ $1 == aboutme-tfstate ]]; then
    say "$HOME/.config/aboutme/vn/tfstate-s3-credentials"
  else
    say "$XDG_RUNTIME_DIR/aboutme-vn/$1-s3-credentials"
  fi
}

normalize() {
  jq -S -c '
    def arr: if type == "array" then sort else [.] end;
    .Statement |= (map(.Action |= arr | .Resource |= arr
      | if (.Principal | type) == "object"
        then .Principal.AWS |= arr else . end)
      | sort_by(.Sid))'
}

# vStorage checks a batch delete against the bucket, so backups also need s3:DeleteObject on it (vngcloud wiki, Storage-Bucket-Policy.md).
policy_for() {
  jq -n -c --arg p "$1" --arg b "$2" '
    {Version: "2012-10-17", Statement: [
      {Sid: "Bucket", Effect: "Allow", Principal: {AWS: [$p]},
       Action: (["s3:ListBucket", "s3:GetBucketLocation",
                 "s3:ListBucketMultipartUploads"]
                + (if $b == "aboutme-backups" then ["s3:DeleteObject"] else [] end)),
       Resource: ["arn:aws:s3:::" + $b]},
      {Sid: "Objects", Effect: "Allow", Principal: {AWS: [$p]},
       Action: ["s3:GetObject", "s3:PutObject", "s3:DeleteObject",
                "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"],
       Resource: ["arn:aws:s3:::" + $b + "/*"]}]}'
}

encryption_on() {
  [[ $(vst get-bucket-encryption --bucket "$1" | jq -r '.Enabled') == true ]]
}

existing_buckets=$(vst list-buckets | jq -r '.Items[]? | (.Name // .name)')
missing=()
for b in "${BUCKETS[@]}"; do
  grep -qx -- "$b" <<<"$existing_buckets" || missing+=("$b")
done

if [[ $BUCKET_ENCRYPTION == required ]]; then
  if ((${#missing[@]})); then
    vng_ro storage create-bucket --help 2>&1 | grep -q -e '--encryption' ||
      die "create-bucket offers no --encryption flag; install vngcloud v0.66.0 or later. Missing: ${missing[*]}"
  fi
  for b in "${BUCKETS[@]}"; do
    grep -qx -- "$b" <<<"$existing_buckets" || continue
    encryption_on "$b" ||
      die "bucket $b exists without default encryption; objects already in it stay unencrypted, so empty it, run put-bucket-encryption --enabled=true, and check it before running this again"
  done
fi

ensure_bucket() {
  local b=$1
  if grep -qx -- "$b" <<<"$existing_buckets"; then
    say "bucket $b: exists"
    return 0
  fi
  local extra=()
  [[ $BUCKET_ENCRYPTION == off ]] || extra=(--encryption)
  # With --encryption the vngcloud CLI returns only after encryption reads on (vngcloud CLI).
  vst create-bucket --bucket "$b" "${extra[@]}" >/dev/null
  say "bucket $b: created${extra:+, encrypted}"
}

ensure_service_account() {
  local name=$1 id
  id=$(vng iam list-service-accounts --name "$name" |
    jq -r --arg n "$name" '[.Items[]? | select(.Name == $n) | .ID] | .[0] // empty')
  if [[ -z $id ]]; then
    # Storage keys do not use the client secret, so the secret file goes to
    # tmpfs and is deleted at once.
    local sfile="$scratch/$name.secret"
    vng iam create-service-account --name "$name" \
      --description "aboutme vStorage access for $name" \
      --secret-file "$sfile" >/dev/null
    rm -f -- "$sfile"
    id=$(vng iam list-service-accounts --name "$name" |
      jq -r --arg n "$name" '[.Items[]? | select(.Name == $n) | .ID] | .[0] // empty')
    say "service account $name: created" >&2
  else
    say "service account $name: exists" >&2
  fi
  [[ -n $id ]] || die "service account $name not found after create"
  say "$id"
}

ensure_policy() {
  local b=$1 arn=$2 want have
  want=$(policy_for "$arn" "$b")
  have=$(vst get-bucket-policy --bucket "$b" | jq -r '.Policy // empty')
  if [[ -n $have ]] &&
    [[ $(normalize <<<"$have") == "$(normalize <<<"$want")" ]]; then
    say "policy $b: matches"
    return 0
  fi
  vst put-bucket-policy --bucket "$b" --policy "$want" >/dev/null
  say "policy $b: written"
}

attached_keys() {
  vst list-s3-keys |
    jq -r --arg s "$1" '.Items[]?
      | select((.SubUserID // .subUserId // "") == $s)
      | (.UserKeyID // .userKeyId)'
}

ensure_key() {
  local b=$1 sa_id=$2 sub=$3 file dir keys out
  keys=$(attached_keys "$sub")
  if [[ -n $keys && $rotate != "$b" ]]; then
    say "key $b: one is attached already"
    return 0
  fi
  file=$(key_file "$b")
  dir=$(dirname "$file")
  [[ ! -e $file && ! -L $file ]] ||
    die "key file $file exists; refusing to overwrite it"
  mkdir -p -- "$dir"
  chmod 0700 -- "$dir"
  # The vngcloud CLI attaches the key before it writes the file (vngcloud CLI).
  out=$(vst create-s3-key --service-account-id "$sa_id" --secret-file "$file")
  say "key $b: created, written to $file"
  say "key $b: user key id $(jq -r '.UserKeyID // .userKeyId // "unknown"' <<<"$out")"
  if [[ -n $keys ]]; then
    say "older keys on $b to delete once the host uses the new one:"
    say "$keys"
    say "  vngcloud storage delete-s3-key --project-id $project --user-key-id <id> --yes"
  fi
}

for b in "${BUCKETS[@]}"; do
  ensure_bucket "$b"
  sa_id=$(ensure_service_account "$b")
  principal=$(vst ensure-service-account-principal --service-account-id "$sa_id")
  arn=$(jq -r '.PrincipalARN' <<<"$principal")
  sub=$(jq -r '.SubUserID' <<<"$principal")
  [[ $arn != null && -n $arn && $sub != null && -n $sub ]] ||
    die "no principal for $b"
  ensure_policy "$b" "$arn"
  ensure_key "$b" "$sa_id" "$sub"
done
