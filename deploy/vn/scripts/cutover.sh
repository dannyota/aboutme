#!/usr/bin/env bash
# The data move in the cutover window (docs/design/vietnam-production.md,
# "Migration", step 5). The owner runs each step in order from the laptop,
# with AWS credentials and SSH to the Vietnam host. Every value and every row
# stays out of the terminal: steps print counts, versions, and hashes only.
#
#   cutover.sh preflight                 AWS serves maintenance, nothing runs
#   cutover.sh reset-db                  drop the rehearsal database, run db-setup
#   cutover.sh dump-restore              pg_dump from RDS, age-encrypted to a
#                                        one-use host key, pg_restore on the host
#   cutover.sh verify                    goose version, row counts, ordered
#                                        hashes, and the catalog grant test
#   cutover.sh media <dest-key-file>     rclone copy and check --download
#
# Environment: DEPLOY_HOST (Vietnam host), AWS_PROFILE for the owner's AWS
# session, RDS_INSTANCE (default aboutme-prod), BASTION_INSTANCE (the EC2
# instance ID the SSM port forward runs through).
set -euo pipefail

admin=aboutme-admin
host=${DEPLOY_HOST:?set DEPLOY_HOST to the Vietnam host address}
region=ap-southeast-1
rds=${RDS_INSTANCE:-aboutme-prod}
cluster=aboutme-prod
port=25432

say() { printf 'cutover: %s\n' "$*" >&2; }
die() {
  say "$*"
  exit 1
}
usage() {
  sed -n '7,15p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

work=$(mktemp -d "${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR must point at a per-user tmpfs}/aboutme-cutover.XXXXXX")
forward_pid=
op=
tag=
lock_held=0
mutation_active=0
cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n $forward_pid ]]; then
    kill "$forward_pid" 2>/dev/null || true
    wait "$forward_pid" 2>/dev/null || true
    forward_pid=
  fi
  rm -rf "$work"
  if ((lock_held)); then
    if ((mutation_active)); then
      say "a cutover mutation may still be active; the marker and operation lock remain for manual recovery"
    elif host_deploy cutover-fail-closed && host_deploy cutover-check && host_fence release "$op"; then
      lock_held=0
    else
      say "could not prove fail-closed cleanup; the operation lock remains for manual recovery"
      status=1
    fi
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# Runs a command on the host as root; the command text is fixed in this file.
host_root() { ssh -o BatchMode=yes "$admin@$host" "sudo bash -c $(printf '%q' "$1")"; }
host_fence() { ssh -o BatchMode=yes "$admin@$host" sudo /usr/local/lib/aboutme/fence.sh "$@"; }
host_deploy_read() { ssh -o BatchMode=yes "$admin@$host" "sudo /usr/local/lib/aboutme/deploy-host.sh - - $1"; }
host_deploy() { ssh -o BatchMode=yes "$admin@$host" sudo /usr/local/lib/aboutme/deploy-host.sh "$op" "$tag" "$1"; }
host_psql() { # sql, as postgres on the aboutme database
  ssh -o BatchMode=yes "$admin@$host" \
    "sudo /usr/local/lib/aboutme/fence.sh checkpoint '$op' '$tag' >/dev/null && sudo -u postgres psql -X -q -At -v ON_ERROR_STOP=1 -d aboutme" <<<"$1"
}

mutation_start() { # kind
  mutation_active=1
  host_fence mutation-start "$op" "$tag" "$1"
}

mutation_finish() { # kind
  host_fence mutation-clear "$op" "$1"
  mutation_active=0
}

# The RDS side through an SSM port forward, as aboutme_migrator, whose
# password goes from SSM into this process's environment only.
rds_open() {
  local endpoint
  [[ -n ${BASTION_INSTANCE:-} ]] || die "set BASTION_INSTANCE to the EC2 instance the port forward runs through"
  endpoint=$(aws rds describe-db-instances --region "$region" --db-instance-identifier "$rds" \
    --query 'DBInstances[0].Endpoint.Address' --output text)
  aws ssm start-session --region "$region" --target "$BASTION_INSTANCE" \
    --document-name AWS-StartPortForwardingSessionToRemoteHost \
    --parameters "{\"host\":[\"$endpoint\"],\"portNumber\":[\"5432\"],\"localPortNumber\":[\"$port\"]}" \
    >"$work/forward.log" 2>&1 &
  forward_pid=$!
  for _ in $(seq 30); do
    (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null && break
    sleep 1
  done
  PGPASSWORD=$(aws ssm get-parameter --region "$region" --name /aboutme/prod/db/migrator-password \
    --with-decryption --query Parameter.Value --output text)
  export PGPASSWORD PGSSLMODE=require
  rds_url="postgres://aboutme_migrator@127.0.0.1:$port/aboutme"
}
rds_psql() { psql -X -q -At -v ON_ERROR_STOP=1 "$rds_url" <<<"$1"; }

assert_aws_quiescent() {
  local app maint enabled
  app=$(aws ecs describe-services --region "$region" --cluster "$cluster" --services aboutme-prod-app \
    --query 'services[0].[desiredCount,runningCount]' --output text)
  maint=$(aws ecs describe-services --region "$region" --cluster "$cluster" --services aboutme-prod-maintenance \
    --query 'services[0].runningCount' --output text)
  enabled=$(aws scheduler list-schedules --region "$region" --group-name aboutme-prod-jobs --state ENABLED \
    --query 'length(Schedules)' --output text)
  [[ $app =~ ^0[[:space:]]+0$ ]] || die "the AWS app still runs ($app); put AWS in maintenance first"
  [[ $maint == 1 ]] || die "AWS maintenance is not running"
  [[ $enabled == 0 ]] || die "$enabled AWS job schedules are still enabled"
}

assert_cutover_quiescent() {
  assert_aws_quiescent
  host_deploy cutover-check || die "the Vietnam host is no longer quiescent"
}

# AWS must already serve maintenance with the app stopped and schedules off,
# so no write lands in RDS after the dump (design "Migration", step 5.1).
cmd_preflight() {
  assert_aws_quiescent
  mutation_start quiesce
  host_deploy cutover-quiesce || die "the Vietnam host must serve maintenance with its server and jobs stopped"
  assert_cutover_quiescent
  mutation_finish quiesce
  say "preflight: AWS and the Vietnam host are both in maintenance"
}

# The rehearsal database held fictional data only; the cutover restores into
# a fresh one that db-setup prepared (roles, grants, logins).
cmd_reset_db() {
  local answer
  assert_cutover_quiescent
  read -r -p "type the Vietnam host address to drop its aboutme database: " answer
  [[ $answer == "$host" ]] || die "not confirmed"
  mutation_start reset-db
  host_root "set -euo pipefail
    /usr/local/lib/aboutme/fence.sh checkpoint '$op' '$tag' >/dev/null
    systemctl is-active --quiet aboutme-server && exit 1
    sudo -u postgres dropdb --if-exists aboutme
    sudo -u postgres createdb -O aboutme aboutme
    /usr/local/lib/aboutme/workload-lock.sh 3600 systemctl start aboutme-db-setup.service
    [ \"\$(systemctl show -p Result --value aboutme-db-setup.service)\" = success ]" ||
    die "the database reset or db-setup failed"
  assert_cutover_quiescent
  mutation_finish reset-db
  say "reset the aboutme database and ran db-setup"
}

# The dump never touches the laptop's disk in plain text: pg_dump streams into
# age, encrypted to a one-use key the host generated, and the host decrypts
# straight into pg_restore. The host key is deleted at the end either way.
cmd_dump_restore() {
  local recipient tables
  assert_cutover_quiescent
  tables=$(host_psql "SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'goose_db_version'")
  [[ $tables == 0 ]] || die "the host database already has $tables tables; run reset-db"
  mutation_start dump-restore
  recipient=$(host_root "set -euo pipefail
    /usr/local/lib/aboutme/fence.sh checkpoint '$op' '$tag' >/dev/null
    umask 077; k=/etc/aboutme/secrets/cutover-age.key
    [ -e \"\$k\" ] || age-keygen -o \"\$k\" 2>/dev/null
    age-keygen -y \"\$k\"")
  [[ $recipient =~ ^age1[0-9a-z]{58}$ ]] || die "the host returned no age recipient"
  rds_open
  pg_dump -Fc "$rds_url" | age -r "$recipient" >"$work/dump.age"
  say "dumped $(stat -c %s "$work/dump.age") encrypted bytes"
  ssh -o BatchMode=yes "$admin@$host" \
    "sudo bash -c \"set -euo pipefail; /usr/local/lib/aboutme/fence.sh checkpoint '$op' '$tag' >/dev/null; umask 077; cat > /srv/data/cutover-dump.age\"" \
    <"$work/dump.age"
  rm -f "$work/dump.age"
  # As postgres into the database db-setup prepared, keeping owners and
  # grants (design "Migration", step 5.2). Unconfirmed until the rehearsal:
  # that the RDS dump names no RDS-only role in an owner or a grant.
  host_root "set -euo pipefail
    /usr/local/lib/aboutme/fence.sh checkpoint '$op' '$tag' >/dev/null
    trap \"rm -f /srv/data/cutover-dump.age /etc/aboutme/secrets/cutover-age.key\" EXIT
    age -d -i /etc/aboutme/secrets/cutover-age.key /srv/data/cutover-dump.age |
      sudo -u postgres pg_restore --exit-on-error --single-transaction -d aboutme" ||
    die "pg_restore failed; the host database is unchanged inside its single transaction"
  assert_cutover_quiescent
  mutation_finish dump-restore
  say "restored the dump on the host and deleted the dump and its key"
}

# Counts and ordered hashes per table on both sides, then the catalog grant
# test on the host (design "Migration", step 5.3).
cmd_verify() {
  local sql_tables sql_goose rds_out host_out
  assert_cutover_quiescent
  rds_open
  sql_goose="SELECT max(version_id) FROM goose_db_version WHERE is_applied"
  sql_tables="SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY 1"
  [[ $(rds_psql "$sql_goose") == $(host_psql "$sql_goose") ]] || die "goose versions differ"
  # Fixed session settings, so both sides print every value the same way.
  local per_table="SET TimeZone = 'UTC'; SET DateStyle = 'ISO, YMD'; SET IntervalStyle = 'postgres';
    SET extra_float_digits = 1; SET bytea_output = 'hex';" t
  while read -r t; do
    [[ $t =~ ^[a-z0-9_]+$ ]] || die "unexpected table name"
    per_table+="SELECT '$t', count(*), md5(coalesce(string_agg(x::text, E'\\n' ORDER BY x::text), '')) FROM public.$t x;"
  done < <(rds_psql "$sql_tables")
  rds_out=$(rds_psql "$per_table")
  host_out=$(host_psql "$per_table")
  printf '%s\n' "$rds_out" | awk -F'|' '{ printf "%-40s %s\n", $1, $2 }' >&2
  [[ $rds_out == "$host_out" ]] || die "row counts or ordered hashes differ between RDS and the host"
  host_psql "SELECT count(*) FROM information_schema.role_table_grants
      WHERE table_schema = 'public' AND grantee = 'PUBLIC'" | grep -qx 0 ||
    die "a public table grants to PUBLIC"
  host_psql "SELECT has_schema_privilege('aboutme_app', 'public', 'CREATE')" | grep -qx f ||
    die "aboutme_app can create in schema public"
  assert_cutover_quiescent
  say "verify: goose version, $(wc -l <<<"$rds_out") tables, counts, hashes, and grants match"
}

# Media from S3 to vStorage, then a byte-for-byte check. The destination key
# is a temporary second key on the media service account
# (buckets.sh --rotate-key aboutme-media); delete it after this step.
cmd_media() { # dest-key-file
  local keyfile=$1 src_bucket
  assert_cutover_quiescent
  [[ -f $keyfile ]] || die "$keyfile is missing"
  src_bucket=${SOURCE_MEDIA_BUCKET:?set SOURCE_MEDIA_BUCKET to the AWS media bucket name}
  export RCLONE_CONFIG_SRC_TYPE=s3 RCLONE_CONFIG_SRC_PROVIDER=AWS RCLONE_CONFIG_SRC_ENV_AUTH=true \
    RCLONE_CONFIG_SRC_REGION=$region
  # The destination reads only the key file's default profile; the source
  # uses the owner's AWS session.
  export RCLONE_CONFIG_DST_TYPE=s3 RCLONE_CONFIG_DST_PROVIDER=Other \
    RCLONE_CONFIG_DST_ENDPOINT=https://hcm04.vstorage.vngcloud.vn RCLONE_CONFIG_DST_REGION=HCM04 \
    RCLONE_CONFIG_DST_FORCE_PATH_STYLE=true RCLONE_CONFIG_DST_ENV_AUTH=true \
    RCLONE_CONFIG_DST_SHARED_CREDENTIALS_FILE=$keyfile RCLONE_CONFIG_DST_PROFILE=default
  mutation_start media
  host_fence checkpoint "$op" "$tag"
  rclone copy --checksum "src:$src_bucket" dst:aboutme-media
  rclone check --download "src:$src_bucket" dst:aboutme-media
  assert_cutover_quiescent
  mutation_finish media
  say "media copied and checked byte for byte; delete the temporary media key now"
}

case "${1-}:$#" in
  preflight:1 | reset-db:1 | dump-restore:1 | verify:1 | media:2) ;;
  *) usage ;;
esac
release=$(host_deploy_read release-json)
tag=$(jq -er '.release_tag | select(type == "string" and test("^v(0|[1-9][0-9]{0,2})\\.(0|[1-9][0-9]{0,2})\\.(0|[1-9][0-9]{0,2})$"))' \
  <<<"$release") || die "the host returned no valid installed release tag"
op=$(host_fence lock cutover "$tag") || exit 1
[[ $op =~ ^[A-Za-z0-9_-]{43}$ ]] || die "the host returned a malformed operation id"
lock_held=1
locked_tag=$(host_deploy release-json | jq -er '.release_tag') || die "the installed release could not be reproved under the lock"
[[ $locked_tag == "$tag" ]] || die "the installed release changed while the cutover lock was acquired"

case "${1-}:$#" in
  preflight:1) cmd_preflight ;;
  reset-db:1) cmd_reset_db ;;
  dump-restore:1) cmd_dump_restore ;;
  verify:1) cmd_verify ;;
  media:2) cmd_media "$2" ;;
esac
