#!/usr/bin/env bash
# The quarterly restore drill (deploy/vn/README.md, "Restore drill").
# Run it from the laptop. It orders a temporary
# vServer, installs PostgreSQL 18 and pgBackRest on it from the bundle, restores
# the latest backup and then a point-in-time target, checks each result, prints
# the recovery times, and deletes the server.
#
#   restore-drill.sh --prod-host <ip> --vpc-id <id> \
#     --subnet-id <id> --security-group-id <id> --max-price <vnd a month> \
#     --admin-key-file <ssh-ed25519 public key> \
#     [--target-time <ISO-8601 UTC, e.g. 2026-10-09T03:00:00Z>] \
#     [--resume] [--keep] [--direct]
#
# - --resume adopts an existing server named aboutme-restore-drill, left by an
#   earlier run, instead of refusing; the run then deletes it at the end.
# - --keep leaves the server in place and prints its ID.
# - The server has no public IP (the CLI never attaches one; wiki
#   Compute-Servers.md). By default the script reaches its private address
#   through the production host as an SSH jump host, so the security group must
#   allow SSH from the production host. --direct connects to the private
#   address straight from the laptop, for a VPN.
#
# The drill server is created with an encrypted root disk and no data disk.
# The production copy lands on a separately created encrypted volume. A server
# created with plain disks refuses an encrypted volume. Encryption at server
# create adds one CES line of 30% of the flavor price (wiki Compute-Servers.md,
# "Encrypted disks"). The volume needs `volume create-volume
# --encryption-type-id` (CLI v0.59.0 and later); the script refuses an older CLI.
# The exit trap deletes the server and the volume. Nothing here prints row data,
# a secret, or secrets.conf: secrets.conf travels host to host in one pipe and
# never touches the laptop disk.
set -euo pipefail

# Host entry points precede every laptop import and cloud operation.
notice_host_invalid() { printf 'invalid_state\n' >&2; exit 1; }
notice_postgres() {
  setpriv --reuid="$(id -u postgres)" --regid="$(id -g postgres)" --init-groups "$@" 3<&- 4>&-
}
notice_sql() {
  notice_postgres psql -X -q -At -F '|' -v ON_ERROR_STOP=1 -v tbl=resumes -h /run/postgresql -d aboutme -f -
}
notice_database_checks() {
  local out table count extra rows=0
  out=$(notice_sql <<<'SELECT max(version_id) FROM goose_db_version;') || return 1
  [[ $out =~ ^(0|[1-9][0-9]{0,17})$ ]] || return 1
  printf 'PASS goose version: %s\n' "$out"
  out=$(notice_sql <<'SQL'
SELECT table_name, (xpath('/row/c/text()',
 query_to_xml(format('select count(*) as c from %I.%I', table_schema, table_name),
 false, true, '')))[1]::text FROM information_schema.tables
 WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY 1;
SQL
  ) || return 1
  [[ -n $out && ${#out} -le 32768 ]] || return 1
  while IFS='|' read -r table count extra; do
    case $table in
      goose_db_version|users|identities|oauth_transactions|sessions|idempotency_records|resumes|slug_tombstones|idempotency_usage|media_deletion_jobs|public_state|password_credentials|password_registrations|password_reset_tokens|auth_email_jobs|oauth_clients|oauth_authorization_codes|oauth_grants|oauth_tokens|lifecycle_audit_events|privacy_sweep_state|second_factor_policies|webauthn_credentials|second_factor_recovery_codes|pending_authentications|webauthn_ceremonies|authentication_security_events|totp_credentials|totp_enrollments|resume_preview_cards|resume_view_days|resume_share_signal_days|resume_showcase|privacy_notice_campaigns|privacy_notice_recipients) ;;
      *) return 1 ;;
    esac
    [[ -z $extra && $count =~ ^(0|[1-9][0-9]{0,18})$ ]] || return 1
    printf 'COUNT %s %s\n' "$table" "$count"; rows=$((rows + 1))
  done <<<"$out"
  printf 'PASS row counts: %s tables\n' "$rows"
  out=$(notice_sql <<'SQL'
SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 CROSS JOIN LATERAL aclexplode(c.relacl) a WHERE n.nspname='public'
 AND c.relkind IN ('r','p','v','m','S','f') AND a.grantee=0;
SQL
  ) || return 1
  [[ $out == 0 ]] || return 1
  printf 'PASS no PUBLIC grant on a public table\n'
  out=$(notice_sql <<<"SELECT has_schema_privilege('aboutme_app', 'public', 'CREATE');") || return 1
  [[ $out == f ]] || return 1
  printf 'PASS aboutme_app has no CREATE on schema public\n'
  out=$(notice_sql <<'SQL'
SELECT string_agg(rolname, ',' ORDER BY rolname) FROM pg_roles
 WHERE rolname !~ '^pg_' AND rolname <> 'postgres';
SQL
  ) || return 1
  [[ $out == aboutme,aboutme_app,aboutme_migrator ]] || return 1
  printf 'PASS role list\n'
  out=$(notice_sql <<<'SET ROLE aboutme_app; SELECT count(*) FROM :"tbl";') || return 1
  [[ $out =~ ^(0|[1-9][0-9]{0,18})$ ]] || return 1
  printf 'PASS store smoke as aboutme_app on resumes: %s\n' "$out"
}
notice_host_child() {
  (($# == 0)) || return 1
  [[ $(stat -Lc %F /proc/self/fd/3) == fifo && $(stat -Lc %F /proc/self/fd/4) == fifo ]] || return 1
  local context state start elapsed recovery_started=0 descriptor inherited=0
  context=$(notice_wire context <&3) || return 1
  exec 3<&-
  notice_operation=$(jq -r .operation_id <<<"$context")
  notice_admission=$(jq -r .admission_tag <<<"$context")
  notice_identity=$(jq -c .mount_identity <<<"$context")
  notice_mode=$(jq -r .mode <<<"$context"); notice_target=$(jq -r '.target // empty' <<<"$context")
  for descriptor in /proc/$$/fd/*; do
    if [[ $(readlink "$descriptor") == /run/aboutme/workload.lock ]]; then inherited=1; fi
  done
  ((inherited)) || return 1
  notice_restore_pre_restore 4>&- || return 1
  start=$SECONDS
  notice_restore_denial 4>&- && systemctl stop postgresql@18-main.service 4>&- || return 1
  local restore_args=(--stanza=aboutme --delta --archive-mode=off)
  if [[ $notice_mode == time ]]; then
    restore_args+=(--type=time "--target=$notice_target" --target-action=promote)
  fi
  notice_restore_denial 4>&- && notice_postgres pgbackrest "${restore_args[@]}" restore || return 1
  notice_restore_denial 4>&- && systemctl start postgresql@18-main.service 4>&- || return 1
  while ((SECONDS - start <= 3600)); do
    state=$(notice_postgres psql -X -q -At -h /run/postgresql -d postgres -c 'select pg_is_in_recovery()') || return 1
    case $state in f) recovery_started=1; break ;; t) sleep 1 4>&- ;; *) return 1 ;; esac
  done
  ((recovery_started)) || return 1
  elapsed=$((SECONDS - start))
  notice_database_checks 4>&- || return 1
  state=$(systemctl show --property=ActiveState --property=Job postgresql@18-main.service 4>&-) || return 1
  grep -qxF Job=0 <<<"$state" || return 1
  ! pgrep -f '[/]pgbackrest.*restore' 4>&- >/dev/null || return 1
  notice_restore_postcheck 4>&- || return 1
  printf 'RECOVERY %s\n' "$elapsed"
  jq -cn --arg id "$notice_operation" --arg epoch "$notice_epoch" '{version:1,operation_id:$id,epoch:$epoch}' >&4 || return 1
  exec 4>&-
}
notice_host_parent() {
  local mode=$1 target=${2:-} context completion report_fd
  notice_app_absent && notice_restore_begin || return 1
  context=$(jq -cn --arg id "$notice_operation" --arg tag "$notice_admission" --arg mode "$mode" \
    --arg target "$target" --argjson identity "$notice_identity" \
    '{version:1,operation_id:$id,admission_tag:$tag,mode:$mode,target:(if $mode=="latest" then null else $target end),mount_identity:$identity}') || return 1
  exec {report_fd}>&1
  completion=$(python3 -I - 5<<<"$context" 6>&"$report_fd" <<'PY'
import fcntl,os,subprocess,sys
try:
    data=os.fdopen(5,'rb').read(1025); assert len(data)<=1024
    def pipe():
        r,w=os.pipe()
        high=[fcntl.fcntl(fd,fcntl.F_DUPFD_CLOEXEC,10) for fd in (r,w)]
        os.close(r); os.close(w); return high
    cr,cw=pipe(); rr,rw=pipe()
    os.dup2(cr,3,inheritable=True); os.dup2(rw,4,inheritable=True)
    child=subprocess.Popen('/usr/local/lib/aboutme/workload-lock.sh 300 /usr/local/lib/aboutme/restore-drill.sh _host-pass'.split(),
        pass_fds=(3,4),stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
    os.close(3); os.close(4); os.close(cr); os.close(rw)
    assert os.write(cw,data)==len(data); os.close(cw)
    reports,_=child.communicate()
    completion=os.fdopen(rr,'rb').read(258)
    assert child.returncode==0 and 0<len(completion)<=256
    assert completion.endswith(b'\n') and b'\n' not in completion[:-1] and b'\r' not in completion and b'\0' not in completion
    assert len(reports)<=65536
    for line in reports.decode('ascii').splitlines():
        assert line.startswith(('PASS goose version: ','COUNT ','PASS row counts: ','PASS no PUBLIC grant on a public table','PASS aboutme_app has no CREATE on schema public','PASS role list','PASS store smoke as aboutme_app on resumes: ','RECOVERY '))
    os.write(6,reports); os.write(1,completion)
except Exception:
    sys.exit(1)
PY
  ) || { exec {report_fd}>&-; return 1; }
  exec {report_fd}>&-
  completion=$(notice_wire completion <<<"$completion") || return 1
  [[ $(jq -r .operation_id <<<"$completion") == "$notice_operation" ]] || return 1
  notice_epoch=$(jq -r .epoch <<<"$completion")
  # shellcheck disable=SC2034 # Consumed by the sourced exact-owner finish.
  notice_completed=yes notice_helper_joined=yes
  notice_restore_finish
}
notice_host_entry() {
  [[ $(id -u) == 0 ]] || return 1
  notice_overrides_absent || return 1
  trap 'exit 1' HUP TERM INT
  case "$#:${1-}:${2-}" in
    2:host-pass:latest) notice_host_parent latest ;;
    3:host-pass:time)
      python3 -I -c 'import datetime,re,sys; s=sys.argv[1]; assert re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z",s); datetime.datetime.strptime(s,"%Y-%m-%dT%H:%M:%SZ")' "$3" || return 1
      notice_host_parent time "$3" ;;
    1:_host-pass:) notice_host_child "${@:2}" ;;
    *) return 1 ;;
  esac
}
case ${1-} in
  host-pass|_host-pass|pre-restore|postcheck|finish|resume-owned-operation)
    # shellcheck source=../host/crowdsec-privacy-notice-host.sh
    source /usr/local/lib/aboutme/crowdsec-privacy-notice-host.sh 2>/dev/null || notice_host_invalid
    if ! notice_host_entry "$@" 2>/dev/null; then notice_host_invalid; fi
    exit 0 ;;
esac


here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=ssh-lib.sh
source "$here/ssh-lib.sh"
# shellcheck source=vng-lib.sh
. "$here/vng-lib.sh"

SERVER_NAME=aboutme-restore-drill
ADMIN_USER=aboutme-admin
# Sizes and IDs match quote.sh, so the drill is priced like production. Keep
# the defaults in sync with it. The encryption type is the one switch for the
# drill disks' encryption, as disk_encryption_type is for production.
: "${ZONE_ID:=HCM03-1C}"
: "${FLAVOR_ID:=flav-530ea5cb-6fac-4264-bcad-9e0e0a5ba3fc}"
: "${IMAGE_ID:=img-34440a82-92fb-40bc-b79c-b1a2b49b93de}"
: "${VOLUME_TYPE_ID:=vtype-e782f8e1-0569-11f0-a0a4-ec2a72332f83}"
: "${ROOT_DISK_GB:=30}"
: "${DATA_DISK_GB:=20}"
: "${DISK_ENCRYPTION_TYPE:=aes-xts-plain64_256}"
VOLUME_NAME=${SERVER_NAME}-data

usage() {
  cat >&2 <<'USAGE'
usage: restore-drill.sh --prod-host <ip> --vpc-id <id>
  --subnet-id <id> --security-group-id <id> --max-price <vnd a month>
  --admin-key-file <ssh-ed25519 public key>
  [--target-time <ISO-8601 UTC>] [--resume] [--keep] [--direct]
USAGE
  exit 2
}

prod_host='' vpc_id='' subnet_id='' sg_id='' max_price=''
admin_key_file='' target_time='' resume=0 keep=0 direct=0
while (($#)); do
  case $1 in
    --resume | --keep | --direct)
      case $1 in
        --resume) resume=1 ;;
        --keep) keep=1 ;;
        --direct) direct=1 ;;
      esac
      shift
      ;;
    --prod-host | --vpc-id | --subnet-id | --security-group-id | \
      --max-price | --admin-key-file | --target-time)
      (($# >= 2)) || usage
      case $1 in
        --prod-host) prod_host=$2 ;;
        --vpc-id) vpc_id=$2 ;;
        --subnet-id) subnet_id=$2 ;;
        --security-group-id) sg_id=$2 ;;
        --max-price) max_price=$2 ;;
        --admin-key-file) admin_key_file=$2 ;;
        --target-time) target_time=$2 ;;
      esac
      shift 2
      ;;
    *) usage ;;
  esac
done
for v in prod_host vpc_id subnet_id sg_id max_price admin_key_file; do
  [[ -n ${!v} ]] || usage
done
[[ $max_price =~ ^[0-9]+$ ]] || usage

need_tools
for t in ssh tar ssh-keygen date mktemp; do
  command -v "$t" >/dev/null 2>&1 || die "missing tool on PATH: $t"
done
[[ -d ${XDG_RUNTIME_DIR:-} ]] || die "XDG_RUNTIME_DIR must name a tmpfs directory"

# The key must be one ssh-ed25519 public key, as the production admin key is
# (design "Host", access).
[[ -f $admin_key_file ]] || die "--admin-key-file: no such file"
admin_key=$(<"$admin_key_file")
[[ $admin_key == ssh-ed25519\ * && $admin_key != *$'\n'* ]] ||
  die "--admin-key-file must hold one ssh-ed25519 public key line"
ssh-keygen -l -f "$admin_key_file" >/dev/null 2>&1 ||
  die "--admin-key-file is not a valid public key"

# pgBackRest takes the target as 'YYYY-MM-DD HH:MM:SS+00'.
if [[ -z $target_time ]]; then
  target_time=$(date -u -d '24 hours ago' '+%Y-%m-%dT%H:%M:%SZ')
fi
iso='^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}(:[0-9]{2})?Z$'
[[ $target_time =~ $iso ]] || die "--target-time must be ISO-8601 UTC ending in Z"
pg_target=$(date -u -d "$target_time" '+%Y-%m-%d %H:%M:%S+00') ||
  die "--target-time is not a valid time"

umask 077
run=$(mktemp -d "$XDG_RUNTIME_DIR/restore-drill.XXXXXX")
server_id='' create_attempted=0 drill_ip='' volume_id='' volume_attempted=0
failures=0
declare -a result_lines=() count_lines=()

# ---- Cleanup -------------------------------------------------------------

# Runs on every exit. A server whose create may have reached the API is found
# by name when no ID came back, so a half-finished order is not left billing.
cleanup() {
  local rc=$?
  trap - EXIT
  set +e
  ssh -p "$SSH_PORT" -S "$run/drill.sock" -O exit x >/dev/null 2>&1
  ssh -p "$SSH_PORT" -S "$run/prod.sock" -O exit x >/dev/null 2>&1
  rm -rf "$run"
  if [[ -z $server_id && $create_attempted == 1 ]]; then
    server_id=$(find_server_id)
  fi
  if [[ -z $volume_id && $volume_attempted == 1 ]]; then
    volume_id=$(find_volume_id)
  fi
  if [[ -z $server_id && -z $volume_id ]]; then
    exit "$rc"
  fi
  if ((keep)); then
    say "kept drill server $server_id; delete it with: $VNG_BIN compute delete-server --server-id $server_id --delete-volumes --yes"
  else
    if [[ -n $server_id ]]; then
      if vng compute delete-server --server-id "$server_id" --delete-volumes --yes >/dev/null; then
        say "deleted drill server $server_id"
      else
        say "ERROR: could not delete drill server $server_id; delete it by hand" >&2
        ((rc)) || rc=1
      fi
    fi
    # The volume may have gone with the server; delete-volume reads first.
    if [[ -n $volume_id ]] && vng_ro volume get-volume --volume-id "$volume_id" >/dev/null 2>&1; then
      if ! vng volume delete-volume --volume-id "$volume_id" --yes >/dev/null; then
        say "ERROR: could not delete drill volume $volume_id; it holds production data, delete it by hand" >&2
        ((rc)) || rc=1
      fi
    fi
  fi
  exit "$rc"
}
trap cleanup EXIT

# ---- vngcloud ------------------------------------------------------------

# find_server_id prints the ID of the server named $SERVER_NAME, or nothing.
# The list's JSON shape is not pinned, so any object with that exact Name and
# a UUID counts.
find_server_id() {
  vng_ro compute list-servers --size 100 2>/dev/null | jq -r --arg n "$SERVER_NAME" '
    [.. | objects | select((.Name // .name) == $n) | (.UUID // .uuid // .ID // .id)
     | select(. != null)] | .[0] // empty'
}

find_volume_id() {
  vng_ro volume list-volumes --name "$VOLUME_NAME" 2>/dev/null | jq -r --arg n "$VOLUME_NAME" '
    [.. | objects | select((.Name // .name) == $n) | (.UUID // .uuid // .ID // .id)
     | select(. != null)] | .[0] // empty'
}

order_server() {
  local quoted vquoted file
  vng_ro volume create-volume --help 2>&1 | grep -q -- '--encryption-type-id' ||
    die "this vngcloud CLI cannot create an encrypted volume; the drill needs v0.59.0 or later"
  quoted=$(vng_ro compute quote-create-server \
    --zone-id "$ZONE_ID" --flavor-id "$FLAVOR_ID" --image-id "$IMAGE_ID" \
    --root-disk-size "$ROOT_DISK_GB" --root-disk-type-id "$VOLUME_TYPE_ID" \
    --root-disk-encryption-type-id "$DISK_ENCRYPTION_TYPE" |
    jq -r '.OptimumPrice // empty')
  vquoted=$(vng_ro volume quote-create-volume --zone-id "$ZONE_ID" --size "$DATA_DISK_GB" \
    --volume-type-id "$VOLUME_TYPE_ID" --encryption-type-id "$DISK_ENCRYPTION_TYPE" |
    jq -r '.OptimumPrice // empty')
  require_price "drill server with a ${ROOT_DISK_GB} GB encrypted root disk" "$quoted" "$max_price"
  require_price "drill ${DATA_DISK_GB} GB encrypted volume" "$vquoted" "$max_price"
  require_price "drill total" "$(awk -v a="$quoted" -v b="$vquoted" 'BEGIN { printf "%d", a + b }')" "$max_price"

  # First boot sets SSH access and a host key made for this run;
  # host/install.sh owns the other host files (design "Host").
  # The host key's public half is pinned before the first SSH (pin_host_key),
  # so no connection trusts an unknown key. Its private half reaches the
  # server only through the user data, lives in this run's tmpfs directory,
  # and dies with the server.
  new_host_key
  file=$run/user-data.yaml
  {
    cat <<CLOUD
#cloud-config
users:
  - name: $ADMIN_USER
    groups: [sudo]
    shell: /bin/bash
    sudo: "ALL=(ALL) NOPASSWD:ALL"
    lock_passwd: true
    ssh_authorized_keys:
      - $admin_key
disable_root: true
ssh_pwauth: false
ssh_deletekeys: true
ssh_genkeytypes: []
ssh_keys:
  ed25519_public: $(cat "$run/hostkey.pub")
  ed25519_private: |
CLOUD
    sed 's/^/    /' "$run/hostkey"
    cat <<CLOUD
bootcmd:
  # Print GreenNode's own sshd_config to the serial console once, before
  # write_files replaces it. It listens on port 234 and ignored the drop-in.
  - [sh, -c, "[ -e /root/sshd_config.image ] || { cp -p /etc/ssh/sshd_config /root/sshd_config.image; { echo '== image sshd_config'; grep -v -E '^[[:space:]]*(#|$)' /etc/ssh/sshd_config; ls -l /etc/ssh/sshd_config.d; echo '== end'; } > /dev/ttyS0 2>&1; }"]

write_files:
  - path: /etc/ssh/sshd_config
    permissions: "0644"
    content: |
      # Ubuntu's defaults with the drop-in directory; GreenNode's image file
      # does not apply the drop-in below.
      Include /etc/ssh/sshd_config.d/*.conf
      KbdInteractiveAuthentication no
      UsePAM yes
      PrintMotd no
      AcceptEnv LANG LC_*
      Subsystem sftp /usr/lib/openssh/sftp-server
  - path: /etc/ssh/sshd_config.d/90-aboutme.conf
    permissions: "0644"
    content: |
      Port $SSH_PORT
      PasswordAuthentication no
      KbdInteractiveAuthentication no
      PermitRootLogin no
      AllowUsers $ADMIN_USER
      PubkeyAcceptedAlgorithms ssh-ed25519
runcmd:
  # GreenNode's Ubuntu 24.04 image starts ssh.service directly; stock Ubuntu
  # uses ssh.socket. Apply the new port either way, after sshd -t passes.
  - [sh, -c, "sshd -t && systemctl daemon-reload && if systemctl is-enabled --quiet ssh.socket; then systemctl stop ssh.service && systemctl restart ssh.socket; else systemctl restart ssh.service; fi"]
  # What listens, and any guest firewall, goes to the serial console, which
  # the console-log API reads when SSH does not answer. No secret is printed.
  - [sh, -c, "{ echo '== aboutme ssh check'; sshd -T | grep -iE '^(port|listenaddress|addressfamily|allowusers|pubkeyauthentication|authorizedkeysfile|pubkeyacceptedalgorithms|permitrootlogin|passwordauthentication) '; ss -tlnp; systemctl --no-pager --full status ssh.service ssh.socket ssh-keygen.service; journalctl -b --no-pager -u ssh -u ssh.socket -u ssh-keygen | tail -n 60; nft list ruleset | head -n 60; iptables -S | head -n 40; ufw status; echo '== end'; } > /dev/ttyS0 2>&1"]
CLOUD
  } >"$file"
  rm -f "$run/hostkey"

  create_attempted=1
  say "creating $SERVER_NAME (waits for ACTIVE)"
  # No --ssh-key-id: GreenNode refuses user data on a create that also sets an
  # SSH key, and the admin key arrives through the cloud-config above.
  server_id=$(vng compute create-server --name "$SERVER_NAME" \
    --zone-id "$ZONE_ID" --flavor-id "$FLAVOR_ID" --image-id "$IMAGE_ID" \
    --vpc-id "$vpc_id" --subnet-id "$subnet_id" --security-group-id "$sg_id" \
    --root-disk-size "$ROOT_DISK_GB" --root-disk-type-id "$VOLUME_TYPE_ID" \
    --root-disk-encryption-type-id "$DISK_ENCRYPTION_TYPE" \
    --max-price "$max_price" --user-data-file "$file" |
    jq -r '(.Server // .) | (.UUID // .uuid // empty)')
  rm -f "$file"
  [[ -n $server_id ]] || server_id=$(find_server_id)
  [[ -n $server_id ]] || die "create-server returned no server ID; look for $SERVER_NAME by hand"

  volume_attempted=1
  say "creating the encrypted volume $VOLUME_NAME"
  volume_id=$(vng volume create-volume --name "$VOLUME_NAME" --zone-id "$ZONE_ID" \
    --size "$DATA_DISK_GB" --volume-type-id "$VOLUME_TYPE_ID" \
    --encryption-type-id "$DISK_ENCRYPTION_TYPE" --max-price "$max_price" |
    jq -r '(.Volume // .) | (.UUID // .uuid // .ID // .id // empty)')
  [[ -n $volume_id ]] || volume_id=$(find_volume_id)
  [[ -n $volume_id ]] || die "create-volume returned no volume ID; look for $VOLUME_NAME by hand"
  vng volume attach-volume --volume-id "$volume_id" --server-id "$server_id" >/dev/null ||
    die "could not attach $VOLUME_NAME to the drill server"
}

server_private_ip() {
  vng_ro compute get-server --server-id "$server_id" | jq -r '
    (.Server // .) | .InternalInterfaces[0] | (.FixedIP // .fixedIp // empty)'
}

# ---- SSH -----------------------------------------------------------------

# One multiplexed connection per host.
# The drill server's host key is the one this run generated and handed to
# cloud-init, pinned under a fixed alias in a per-run known_hosts file, with
# strict checking: a server that presents any other key is refused.
prod_opts=(-p "$SSH_PORT" -o ControlMaster=auto -o "ControlPath=$run/prod.sock" -o ControlPersist=15m
  -o ConnectTimeout=15)
drill_opts=(-p "$SSH_PORT" -o ControlMaster=auto -o "ControlPath=$run/drill.sock" -o ControlPersist=15m
  -o ConnectTimeout=15 -o StrictHostKeyChecking=yes -o "HostKeyAlias=$SERVER_NAME" -o LogLevel=ERROR
  -o "UserKnownHostsFile=$run/known_hosts" -o ServerAliveInterval=30)

# The public half is also kept in the state directory, so --resume can pin the
# same key; it is not secret.
hostkey_state=${XDG_STATE_HOME:-$HOME/.local/state}/aboutme/restore-drill-hostkey.pub
new_host_key() {
  ssh-keygen -q -t ed25519 -N '' -C "$SERVER_NAME" -f "$run/hostkey"
  mkdir -p "$(dirname "$hostkey_state")"
  cp -f "$run/hostkey.pub" "$hostkey_state"
}
pin_host_key() {
  [[ -f $hostkey_state ]] || die "no pinned host key for $SERVER_NAME at $hostkey_state"
  printf '%s %s\n' "$SERVER_NAME" "$(cut -d' ' -f1,2 "$hostkey_state")" >"$run/known_hosts"
}

# prsh runs a quoted command on the production host. rsh runs one on the drill.
# shellcheck disable=SC2029 # the command is quoted for the remote shell on purpose
prsh() { ssh "${prod_opts[@]}" "$ADMIN_USER@$prod_host" "$(printf '%q ' "$@")"; }
# shellcheck disable=SC2029 # as above
rsh() { ssh "${drill_opts[@]}" "$ADMIN_USER@$drill_ip" "$(printf '%q ' "$@")"; }

connect_drill() {
  pin_host_key
  drill_ip=$(server_private_ip)
  [[ -n $drill_ip ]] || die "could not read the drill server's private IP"
  if ((!direct)); then
    drill_opts+=(-o "ProxyCommand=ssh -p $SSH_PORT -o ControlPath=$run/prod.sock -W %h:%p $ADMIN_USER@$prod_host")
  fi
  prsh true || die "cannot reach the production host $prod_host over SSH"
  say "waiting for SSH on the drill server $drill_ip"
  for _ in $(seq 1 60); do
    if rsh true 2>/dev/null; then
      rsh sudo cloud-init status --wait >/dev/null 2>&1 || true
      return 0
    fi
    sleep 10
  done
  die "the drill server did not accept SSH within 10 minutes"
}

# ---- Install and restore -------------------------------------------------

# Copies only host/ and the two scripts install.sh installs, never deploy/vn/prod
# (it may hold ignored state and variable files).
install_drill_host() {
  local bundle=/tmp/aboutme-bundle
  say "copying the bundle and installing PostgreSQL and pgBackRest"
  rsh sudo rm -rf "$bundle"
  tar -C "$here/.." -c host scripts/fence.sh scripts/deploy-host.sh scripts/deploy-safety.sh \
    scripts/crowdsec-privacy-notice.sh scripts/restore-drill.sh |
    rsh bash -c 'mkdir -p /tmp/aboutme-bundle && tar -C /tmp/aboutme-bundle -x'
  # The data disk is the one disk that is neither the root disk nor zram.
  # shellcheck disable=SC2016 # runs on the drill host
  data_dev=$(rsh bash -c 'root=$(lsblk -no PKNAME "$(findmnt -no SOURCE /)" | head -n1)
    lsblk -dnpo NAME,TYPE | awk -v r="/dev/$root" "\$2==\"disk\" && \$1!=r && \$1!~/zram/ {print \$1}"')
  [[ $data_dev =~ ^/dev/[a-z0-9]+$ ]] ||
    die "expected exactly one data disk on the drill server, found: ${data_dev:-none}"
  rsh sudo bash "$bundle/host/data-volume.sh" "$data_dev" --format
  # install.sh needs an ABOUTME_INSTALL_ONLY=postgres switch that stops after
  # PostgreSQL and pgBackRest exist and starts no timer or other agent.
  rsh sudo env ABOUTME_INSTALL_ONLY=postgres bash "$bundle/host/install.sh" "$bundle"
  # Nothing on this server may archive WAL or back up to the production
  # repository: stop the fresh cluster and any backup timer before the
  # repository credentials arrive.
  rsh sudo systemctl stop postgresql@18-main.service
  rsh sudo bash -c 'systemctl stop "aboutme-backup*.timer" 2>/dev/null || true'
}

# One pipe from host to host. The file never reaches the laptop disk or the
# terminal.
copy_secrets() {
  say "copying pgBackRest secrets.conf from the production host"
  ssh "${prod_opts[@]}" "$ADMIN_USER@$prod_host" \
    'sudo cat /etc/pgbackrest/conf.d/secrets.conf' |
    ssh "${drill_opts[@]}" "$ADMIN_USER@$drill_ip" \
      'sudo sh -c "umask 077; install -o root -g postgres -m 0440 /dev/stdin /etc/pgbackrest/conf.d/secrets.conf"'
  rsh sudo test -s /etc/pgbackrest/conf.d/secrets.conf ||
    die "secrets.conf on the drill host is empty"
}

# One confirmed SSH result covers replacement, recovery, and every locked check.
restore_run() {
  local mode=$1 out label=latest
  local call=(sudo /usr/local/lib/aboutme/restore-drill.sh host-pass "$mode")
  if [[ $mode == time ]]; then call+=("$target_time"); label=point-in-time; fi
  if ! out=$(rsh "${call[@]}"); then
    printf 'FAIL %s notice restore fence\n' "$label"
    die "invalid_state"
  fi
  restore_seconds=$(sed -n 's/^RECOVERY \([0-9][0-9]*\)$/\1/p' <<<"$out")
  [[ $restore_seconds =~ ^(0|[1-9][0-9]{0,3})$ && $restore_seconds -le 3600 ]] || die "invalid_state"
  printf '%s\n' "$out"
  if [[ $mode == latest ]]; then printf 'PASS latest notice restore fence\n'
  else printf 'PASS point-in-time notice restore fence\n'; fi
  result_lines+=("PASS $label notice restore fence")
}

# ---- Main ----------------------------------------------------------------

existing=$(find_server_id)
if [[ -n $existing ]]; then
  ((resume)) || die "a server named $SERVER_NAME already exists ($existing); delete it or pass --resume"
  server_id=$existing
  volume_id=$(find_volume_id)
  [[ -n $volume_id ]] || die "resuming needs the encrypted volume $VOLUME_NAME, which does not exist"
  say "resuming on existing server $server_id"
else
  order_server
fi

connect_drill
install_drill_host
copy_secrets

restore_run latest
latest_seconds=$restore_seconds
say "latest restore took ${latest_seconds}s"

restore_run time
time_seconds=$restore_seconds
say "point-in-time restore to $pg_target took ${time_seconds}s"

# ---- Summary -------------------------------------------------------------

printf 'notice reconciliation: not performed\n'
summary=$(
  printf 'restore drill %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  printf 'latest backup recovery: %ss\n' "$latest_seconds"
  printf 'point-in-time recovery to %s: %ss\n' "$pg_target" "$time_seconds"
  printf 'checks (%s failed):\n' "$failures"
  printf '  %s\n' "${result_lines[@]}"
  printf 'row counts per table in public (counts only):\n'
  printf '%s\n' "${count_lines[@]}"
)
state_dir=${XDG_STATE_HOME:-$HOME/.local/state}/aboutme
mkdir -p "$state_dir"
out_file=$state_dir/restore-drill-$(date -u '+%Y-%m-%d').txt
printf '%s\n' "$summary" >"$out_file"
say ""
printf '%s\n' "$summary"
say "summary written to $out_file"
((failures == 0)) || die "$failures check(s) failed"
