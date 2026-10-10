#!/usr/bin/env bash
# Creates the three vStorage buckets, each with its own service account, bucket
# policy, and one S3 key (docs/design/vietnam-production.md, "Secrets and host
# identity" and "Infrastructure code and state"). The recipe and the policy
# template are the vngcloud wiki's Storage-Bucket-Policy.md.
#
#   buckets.sh --storage-project-id <id> [--rotate-key <bucket>]
#
# Buckets and service accounts: aboutme-media, aboutme-backups, aboutme-tfstate.
# The policy allows object work on that one bucket and never uses s3:*.
#
# Key files, each created 0600, never overwritten, never printed:
#   media and backups: ${XDG_RUNTIME_DIR}/aboutme-vn/<bucket>-s3-credentials
#                      (tmpfs; secrets.sh moves them to the host)
#   tfstate:           ~/.config/aboutme/vn/tfstate-s3-credentials
#
# Re-running changes nothing that already matches. --rotate-key <bucket> makes a
# new key for that bucket's service account into a key file that must not exist
# yet, then lists the older keys on the same account for the owner to delete
# once the host uses the new one.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=vng-lib.sh
. "$here/vng-lib.sh"

# required: refuse to create a bucket unless the installed CLI can encrypt it.
# off: the owner's explicit override. v0.58.0 cannot set bucket encryption.
BUCKET_ENCRYPTION=required
# Extra create-bucket arguments for the encryption flag, taken from
# `vngcloud storage create-bucket --help` once a CLI release adds one.
# Unconfirmed: that flag's name and value.
BUCKET_ENCRYPTION_ARGS=${BUCKET_ENCRYPTION_ARGS:-}

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

# vst runs a storage command in the vStorage project.
vst() {
  local cmd=$1
  shift
  vng storage "$cmd" --project-id "$project" "$@"
}

# key_file <bucket> prints where that bucket's key file goes.
key_file() {
  if [[ $1 == aboutme-tfstate ]]; then
    say "$HOME/.config/aboutme/vn/tfstate-s3-credentials"
  else
    say "$XDG_RUNTIME_DIR/aboutme-vn/$1-s3-credentials"
  fi
}

# normalize prints a policy document in a form that does not depend on key
# order, array order, or one-element array versus string.
normalize() {
  jq -S -c '
    def arr: if type == "array" then sort else [.] end;
    .Statement |= (map(.Action |= arr | .Resource |= arr
      | if (.Principal | type) == "object"
        then .Principal.AWS |= arr else . end)
      | sort_by(.Sid))'
}

# policy_for <principal-arn> <bucket> prints the wiki template.
policy_for() {
  jq -n -c --arg p "$1" --arg b "$2" '
    {Version: "2012-10-17", Statement: [
      {Sid: "Bucket", Effect: "Allow", Principal: {AWS: [$p]},
       Action: ["s3:ListBucket", "s3:GetBucketLocation",
                "s3:ListBucketMultipartUploads"],
       Resource: ["arn:aws:s3:::" + $b]},
      {Sid: "Objects", Effect: "Allow", Principal: {AWS: [$p]},
       Action: ["s3:GetObject", "s3:PutObject", "s3:DeleteObject",
                "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"],
       Resource: ["arn:aws:s3:::" + $b + "/*"]}]}'
}

# encryption_flag prints the encryption flag the CLI's create-bucket offers, or
# nothing.
encryption_flag() {
  vng_ro storage create-bucket --help 2>&1 |
    grep -o -i -E -e '--[a-z0-9-]*(encrypt|kms|sse)[a-z0-9-]*' | head -n 1 || true
}

existing_buckets=$(vst list-buckets | jq -r '.Items[]? | (.Name // .name)')
missing=()
for b in "${BUCKETS[@]}"; do
  grep -qx -- "$b" <<<"$existing_buckets" || missing+=("$b")
done

# Check encryption support before changing anything.
if ((${#missing[@]})) && [[ $BUCKET_ENCRYPTION == required ]]; then
  flag=$(encryption_flag)
  [[ -n $flag ]] ||
    die "bucket creation waits for the CLI release that adds bucket encryption (create-bucket offers no encryption flag); set BUCKET_ENCRYPTION=off only as the owner's explicit override. Missing: ${missing[*]}"
  [[ -n $BUCKET_ENCRYPTION_ARGS ]] ||
    die "create-bucket now offers $flag; set BUCKET_ENCRYPTION_ARGS to its arguments from --help (Unconfirmed: the value it takes)"
fi

ensure_bucket() {
  local b=$1
  if grep -qx -- "$b" <<<"$existing_buckets"; then
    say "bucket $b: exists"
    return 0
  fi
  local extra=()
  if [[ $BUCKET_ENCRYPTION == required ]]; then
    # shellcheck disable=SC2206 # the arguments are split on purpose
    extra=($BUCKET_ENCRYPTION_ARGS)
  fi
  vst create-bucket --bucket "$b" "${extra[@]}" >/dev/null
  say "bucket $b: created"
}

# ensure_service_account <name> prints the service account ID.
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

# attached_keys <sub-user-id> prints the UserKeyID of every key attached to it.
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
  # The CLI attaches the key to the service account before it writes the file.
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
