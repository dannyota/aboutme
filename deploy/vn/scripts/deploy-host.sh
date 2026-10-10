#!/usr/bin/env bash
# The host half of deploy.sh (docs/design/vietnam-production.md, "Deploy").
# Runs as root on the production host, installed at
# /usr/local/lib/aboutme/deploy-host.sh. deploy.sh calls one step at a time
# over SSH and decides the order and the recovery; this script only does what
# one step names and proves its result.
#
#   deploy-host.sh <op> <tag> <step> [args]
#
# <op> is the operation id fence.sh lock printed, or "-" for read-only steps;
# <tag> is "-" only for release-json.
# Every step that starts an image first reproves the lock and the release
# floor with fence.sh checkpoint (docs/design/passkey-release-fence.md,
# "Serialized production operation"). Steps:
#
#   status                      release and unit state
#   running-release             DEPLOY_RELEASE_NUMBER of the running server
#   release-json                the installed release, shape-checked
#   pull <server> <web> <caddy> pull images by digest; require linux/amd64
#   backup                      pgBackRest incremental annotated with <tag>
#   timers-stop | timers-start  the scheduled job timers
#   fail-closed                 maintenance up; app and jobs stopped
#   cutover-quiesce             stop job timers and wait for active jobs
#   cutover-check               prove maintenance with server and jobs stopped
#   cutover-fail-closed         enforce maintenance with app and jobs stopped
#   maintenance-up <caddy>      render and start maintenance beside Caddy
#   app-down                    stop Caddy and the server; prove both stopped
#   render <server> <web> <caddy>  write release.env and the app units
#   restore-previous            render the previous release again
#   secrets-check               every secret the next render names exists
#   ban-test add|del <ip>       short CrowdSec decision for the outside smoke
#   db-setup | migrate          run the one-shot unit; require success
#   app-up                      web, then server (/readyz), then Caddy
#   maintenance-down            stop maintenance; prove it stopped
#   db-bootstrap                first install: owner role, database, stanza
set -euo pipefail

lib=/usr/local/lib/aboutme
fence=$lib/fence.sh
templates=$lib/quadlet
units=/etc/containers/systemd
state=/var/lib/aboutme
release_json=$state/release.json
maintenance_json=$state/maintenance.json
secrets=/etc/aboutme/secrets
repo=ghcr.io/dannyota/aboutme
jobs=(idempotency-expiry-sweep media-deletion-sweep privacy-retention-sweep media-orphan-sweep)
# shellcheck source=deploy-safety.sh
source "$lib/deploy-safety.sh"
# Floors from docs/design/passkey-release-fence.md and
# docs/design/viewer-analytics/sign-in-to-view.md, "Release and rollback".
fence_epoch=4002
fence_epoch_totp=4007
fence_epoch_signin=6026

say() { printf 'deploy-host: %s\n' "$*" >&2; }
die() {
  say "$*"
  exit 1
}

release_number() {
  [[ $1 =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || return 1
  echo $((10#${BASH_REMATCH[1]} * 1000000 + 10#${BASH_REMATCH[2]} * 1000 + 10#${BASH_REMATCH[3]}))
}

image_ok() { # name ref
  [[ $2 =~ ^${repo//./\\.}-$1@sha256:[0-9a-f]{64}$ ]] || die "'$2' is not a $1 image pinned by digest"
}

checkpoint() {
  [[ $op != - ]] || die "this step needs the operation id"
  "$fence" checkpoint "$op" "$tag" >/dev/null || die "fence checkpoint failed; the lock or the floor no longer holds"
}

# Every state file and flags.env is parsed here, by jq, against one strict
# shape, so no two readers can disagree about what a file says.
release_shape='
  def img($n): type == "string" and test("^ghcr\\.io/dannyota/aboutme-" + $n + "@sha256:[0-9a-f]{64}$");
  def tag: type == "string" and test("^v(0|[1-9][0-9]{0,2})\\.(0|[1-9][0-9]{0,2})\\.(0|[1-9][0-9]{0,2})$");
  def num: type == "number" and . >= 0 and . == floor;
  if type == "object"
    and (keys == ["caddy_image", "maintenance_image", "maintenance_number", "maintenance_tag",
                  "release_number", "release_tag", "server_image", "web_image"])
    and (.release_tag | tag) and (.release_number | num)
    and (.server_image | img("server")) and (.web_image | img("web"))
    and (.caddy_image | img("caddy")) and (.maintenance_image | img("caddy"))
    and (.maintenance_tag | tag) and (.maintenance_number | num)
  then . else error("malformed release state") end'

release_read() { # file
  local doc
  [[ -f $1 && ! -L $1 ]] || die "$1 is missing"
  doc=$(jq -ce "$release_shape" "$1" 2>/dev/null) || die "$1 is malformed"
  [[ $(release_number "$(jq -r .release_tag <<<"$doc")") == "$(jq -r .release_number <<<"$doc")" &&
    $(release_number "$(jq -r .maintenance_tag <<<"$doc")") == "$(jq -r .maintenance_number <<<"$doc")" ]] ||
    die "$1 has a release number that does not match its tag"
  printf '%s' "$doc"
}

# flags.env as a JSON object. Each line is blank, a comment, or NAME=value
# with no quotes or spaces, so Podman's env-file reader and this one read the
# same values; anything else, or a repeated name, refuses.
flags_json() {
  jq -Rn '
    [inputs | select(test("^[[:space:]]*(#|$)") | not)
      | if test("^[A-Z][A-Z0-9_]*=[A-Za-z0-9_.,:/@+-]*$") then capture("^(?<key>[^=]+)=(?<value>.*)$")
        else error("malformed line in flags.env") end]
    | if (map(.key) | length) == (map(.key) | unique | length) then from_entries
      else error("a name repeats in flags.env") end' </etc/aboutme/flags.env 2>/dev/null ||
    die "/etc/aboutme/flags.env is malformed"
}

active() { systemctl is-active --quiet "$1"; }

# Waits until a unit is inactive or failed, up to $2 seconds.
wait_stopped() { # unit seconds
  local i
  for ((i = 0; i < $2; i++)); do
    active "$1" || return 0
    sleep 1
  done
  return 1
}

http_ok() { # url seconds
  local i
  for ((i = 0; i < $2; i++)); do
    curl -fsS -o /dev/null -m 5 "$1" 2>/dev/null && return 0
    sleep 1
  done
  return 1
}

# Counts listening sockets on a port owned by a process named caddy.
caddy_listeners() { # port
  ss -Hltnp "sport = :$1" 2>/dev/null | grep -c '"caddy"' || true
}

render_unit() { # template output tag number server web caddy maintenance-image maintenance-tag maintenance-number
  local extra=$secrets_block
  sed -e "s|@RELEASE_TAG@|$3|g" -e "s|@RELEASE_NUMBER@|$4|g" \
    -e "s|@SERVER_IMAGE@|$5|g" -e "s|@WEB_IMAGE@|$6|g" -e "s|@CADDY_IMAGE@|$7|g" \
    -e "s|@MAINTENANCE_IMAGE@|$8|g" -e "s|@MAINTENANCE_TAG@|$9|g" -e "s|@MAINTENANCE_NUMBER@|${10}|g" \
    "$1" | awk -v extra="$extra" '$0 == "@SERVER_SECRETS@" { if (extra != "") print extra; next } { print }' >"$2.tmp"
  ! grep -q '@[A-Z_]*@' "$2.tmp" || die "unrendered placeholder in $2"
  chmod 0644 "$2.tmp"
  mv -f "$2.tmp" "$2"
}

flag() { # name
  jq -r --arg n "$1" '.[$n] // "false"' <<<"$flags"
}

# The secrets the server gets beyond the fixed set in its template: the TOTP
# key ring slots and the login providers named in flags.env.
server_secrets() {
  local active_slot previous_slot providers lines=()
  active_slot=$(jq -r '.TOTP_ACTIVE_KEY_SLOT // ""' <<<"$flags")
  previous_slot=$(jq -r '.TOTP_PREVIOUS_KEY_SLOT // ""' <<<"$flags")
  [[ $active_slot == a || $active_slot == b ]] || die "TOTP_ACTIVE_KEY_SLOT must be a or b"
  lines+=("Secret=totp-key-$active_slot,type=env,target=TOTP_ACTIVE_KEY")
  case $previous_slot in
    "") ;;
    a | b)
      [[ $previous_slot != "$active_slot" ]] || die "TOTP_PREVIOUS_KEY_SLOT equals the active slot"
      lines+=("Secret=totp-key-$previous_slot,type=env,target=TOTP_PREVIOUS_KEY")
      ;;
    *) die "TOTP_PREVIOUS_KEY_SLOT must be a, b, or empty" ;;
  esac
  providers=$(flag PROVIDER_LOGIN_ENABLED)
  if [[ ,$providers, == *,google,* ]]; then
    lines+=("Secret=google-client-id,type=env,target=GOOGLE_CLIENT_ID" "Secret=google-client-secret,type=env,target=GOOGLE_CLIENT_SECRET")
  fi
  if [[ ,$providers, == *,linkedin,* ]]; then
    lines+=("Secret=linkedin-client-id,type=env,target=LINKEDIN_CLIENT_ID" "Secret=linkedin-client-secret,type=env,target=LINKEDIN_CLIENT_SECRET")
  fi
  printf '%s\n' "${lines[@]}"
}

# A flag may be on only once the fence reached its floor
# (docs/design/passkey-release-fence.md, "Deployer order and task evidence").
flags_check() { # candidate
  local minimum
  minimum=$("$fence" read | jq -er .minimum_release) || die "could not read the release fence"
  check_floor() { # flag epoch tag
    if [[ $(flag "$1") == true ]] && ((minimum < $2)); then
      die "flags.env turns $1 on while the release fence is below $3; run deploy.sh --activate first"
    fi
    if [[ $(flag "$1") == true ]] && (($1_candidate < $2)); then
      die "flags.env turns $1 on but the candidate is below $3"
    fi
  }
  # shellcheck disable=SC2034 # read through the indirect name in check_floor
  local PASSKEY_ENROLLMENT_ENABLED_candidate=$1 TOTP_ENROLLMENT_ENABLED_candidate=$1 SIGN_IN_TO_VIEW_ENABLED_candidate=$1
  check_floor PASSKEY_ENROLLMENT_ENABLED "$fence_epoch" v0.4.2
  check_floor TOTP_ENROLLMENT_ENABLED "$fence_epoch_totp" v0.4.7
  check_floor SIGN_IN_TO_VIEW_ENABLED "$fence_epoch_signin" v0.6.26
}

write_release_json() { # tag number server web caddy maintenance-image maintenance-tag maintenance-number
  local tmp=$state/.release.json.tmp
  jq -n --arg t "$1" --argjson n "$2" --arg s "$3" --arg w "$4" --arg c "$5" \
    --arg mi "$6" --arg mt "$7" --argjson mn "$8" \
    '{release_tag: $t, release_number: $n, server_image: $s, web_image: $w, caddy_image: $c,
      maintenance_image: $mi, maintenance_tag: $mt, maintenance_number: $mn}' |
    jq -e "$release_shape" >"$tmp" 2>/dev/null || {
    rm -f "$tmp"
    die "refusing to write a malformed release state"
  }
  chmod 0644 "$tmp"
  sync "$tmp"
  if [[ -f $release_json ]]; then
    cp -f "$release_json" "$state/release.previous.json"
  fi
  mv -f "$tmp" "$release_json"
  sync "$state"
}

render_all() { # tag number server web caddy maintenance-image maintenance-tag maintenance-number
  local t name
  "$fence" check "$2" deploy-host-render >/dev/null || die "$1 ($2) is below the release fence"
  flags=$(flags_json)
  flags_check "$2"
  secrets_block=$(server_secrets)
  install -d -m 0755 "$units"
  install -m 0644 "$templates/aboutme.network" "$units/aboutme.network"
  for t in "$templates"/*.container.in; do
    name=$(basename "$t" .in)
    render_unit "$t" "$units/$name" "$@"
  done
  write_release_json "$@"
  systemctl daemon-reload
}

step_status() {
  local u
  if [[ -f $release_json ]]; then release_read "$release_json" | jq .; else echo "no release installed"; fi
  for u in aboutme-caddy aboutme-server aboutme-web aboutme-maintenance; do
    printf '%s %s\n' "$u" "$(systemctl is-active "$u" || true)"
  done
}

step_running_release() {
  active aboutme-server || die "aboutme-server is not running"
  podman container inspect aboutme-server |
    jq -er '[.[0].Config.Env[] | select(startswith("DEPLOY_RELEASE_NUMBER=")) | ltrimstr("DEPLOY_RELEASE_NUMBER=")]
      | if length == 1 and (.[0] | test("^[0-9]+$")) then .[0] else error("no release number") end' ||
    die "the running server has no release number"
}

step_release_json() {
  [[ $op == - ]] || checkpoint
  release_read "$release_json"
  echo
}

step_pull() { # server web caddy
  local name ref arch
  for name in server web caddy; do
    ref=$1
    shift
    image_ok "$name" "$ref"
    podman pull --quiet --arch amd64 "$ref" >/dev/null || die "could not pull $ref"
    arch=$(podman image inspect --format '{{.Os}}/{{.Architecture}}' "$ref")
    [[ $arch == linux/amd64 ]] || die "$ref resolved to $arch, not linux/amd64"
    say "pulled $ref"
  done
}

step_backup() {
  runuser -u postgres -- pgbackrest --stanza=aboutme --type=incr \
    --annotation="aboutme-release=$tag" backup || die "the release backup failed"
  say "backup for $tag done"
}

step_timers() { # stop|start
  local j
  if [[ $1 == stop ]]; then
    aboutme_stop_jobs 600 || die "job timers or active jobs did not stop"
    say "job timers stopped and no job runs"
  else
    checkpoint
    for j in "${jobs[@]}"; do systemctl enable --now "aboutme-job-$j.timer" >/dev/null; done
    say "job timers started"
  fi
}

step_fail_closed() {
  aboutme_fail_closed 600 || die "could not enforce the fail-closed state"
  say "maintenance up; app and job timers stopped"
}

step_cutover_quiesce() {
  checkpoint
  aboutme_stop_jobs 600 || die "job timers or active jobs did not stop"
  aboutme_assert_cutover_quiescent || die "the host is not quiescent for cutover"
  say "cutover host is quiescent"
}

step_cutover_check() {
  checkpoint
  aboutme_assert_cutover_quiescent || die "the host is not quiescent for cutover"
  say "cutover host is quiescent"
}

step_cutover_fail_closed() {
  checkpoint
  aboutme_fail_closed 600 || die "could not enforce the fail-closed cutover state"
  say "cutover host is fail closed"
}

# Maintenance is the Caddy image only: a static 503 page with no
# authentication or database logic, so starting it needs no checkpoint
# (docs/design/passkey-release-fence.md, "Serialized production operation").
# It is rendered on its own so the app units keep the running release until
# the app is stopped.
step_maintenance_up() { # maintenance-image
  local image=$1 mtag mnumber
  image_ok caddy "$image"
  mtag=$tag
  mnumber=$(release_number "$tag")
  secrets_block=""
  install -d -m 0755 "$units"
  render_unit "$templates/aboutme-maintenance.container.in" "$units/aboutme-maintenance.container" \
    "$mtag" "$mnumber" "-" "-" "-" "$image" "$mtag" "$mnumber"
  jq -n --arg i "$image" --arg t "$mtag" --argjson n "$mnumber" '{image: $i, tag: $t, number: $n}' >"$maintenance_json"
  systemctl daemon-reload
  systemctl start aboutme-maintenance
  sleep 3
  active aboutme-maintenance || die "aboutme-maintenance did not stay up"
  # Both Caddy processes now share 443 with SO_REUSEPORT, or only maintenance
  # holds it when the app Caddy is already down.
  (($(caddy_listeners 443) >= 1)) || die "nothing listens on 443 after maintenance started"
  say "maintenance up"
}

step_app_down() {
  # On a first deploy neither unit exists yet; the checks below still hold.
  systemctl stop aboutme-caddy aboutme-server 2>/dev/null || true
  wait_stopped aboutme-caddy 60 || die "aboutme-caddy did not stop"
  wait_stopped aboutme-server 60 || die "aboutme-server did not stop"
  ! curl -fsS -o /dev/null -m 3 http://127.0.0.1:8080/healthz 2>/dev/null || die "something still answers on 127.0.0.1:8080"
  say "app down"
}

step_render() { # server web caddy
  local number mimage mtag mnumber
  number=$(release_number "$tag") || die "'$tag' is not a release tag"
  image_ok server "$1"
  image_ok web "$2"
  image_ok caddy "$3"
  # The maintenance unit keeps whatever maintenance-up rendered and recorded.
  mimage=$3 mtag=$tag
  if [[ -f $maintenance_json ]]; then
    mimage=$(jq -er '.image | strings' "$maintenance_json") || die "$maintenance_json is malformed"
    mtag=$(jq -er '.tag | strings' "$maintenance_json") || die "$maintenance_json is malformed"
    image_ok caddy "$mimage"
  fi
  mnumber=$(release_number "$mtag") || die "the recorded maintenance release has a malformed tag"
  checkpoint
  render_all "$tag" "$number" "$1" "$2" "$3" "$mimage" "$mtag" "$mnumber"
  say "rendered $tag"
}

# Failed-deploy restoration: render the release that ran before this one.
# It must still meet the fence, so an image below the floor never returns.
step_restore_previous() {
  local p cur
  p=$(release_read "$state/release.previous.json")
  cur=$(release_read "$release_json")
  checkpoint
  # shellcheck disable=SC2046 # the values are shape-checked tags, numbers, and digests
  render_all $(jq -r '.release_tag, .release_number, .server_image, .web_image, .caddy_image' <<<"$p") \
    $(jq -r '.maintenance_image, .maintenance_tag, .maintenance_number' <<<"$cur")
  say "rendered the previous release $(jq -r .release_tag <<<"$p")"
}

# Runs before the site changes: every secret the templates and flags.env will
# name must already exist as a Podman secret, or the app would fail after the
# migration (docs/design/single-host-production.md, "Release and deploy").
step_secrets_check() {
  local name missing=0
  flags=$(flags_json)
  while read -r name; do
    [[ $name =~ ^[a-z0-9-]+$ ]] || die "malformed secret name in a template"
    if ! podman secret exists "$name"; then
      say "missing Podman secret $name"
      missing=1
    fi
  done < <({
    cat "$templates"/*.container.in
    server_secrets
  } | sed -n 's/^Secret=\([^,]*\),.*/\1/p' | sort -u)
  ((!missing)) || die "create the missing secrets with secrets.sh, then rerun"
  say "every referenced secret exists"
}

# The outside smoke's CrowdSec check: a short manual decision for the
# operator's own address, which the Caddy bouncer must answer with 403
# (docs/design/vietnam-production.md, "Deploy", step 10). The firewall
# bouncer acts only on SSH scenarios, so SSH stays open.
step_ban_test() { # add|del ip
  [[ $2 =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || die "'$2' is not an IPv4 address"
  if [[ $1 == add ]]; then
    cscli decisions add --ip "$2" --duration 3m --type ban --reason aboutme-deploy-smoke >/dev/null
  else
    cscli decisions delete --ip "$2" >/dev/null
  fi
}

step_oneshot() { # unit
  local result
  checkpoint
  systemctl start "$1.service" || true
  result=$(systemctl show -p Result --value "$1.service")
  [[ $result == success ]] || die "$1 finished with result '$result'"
  say "$1 done"
}

step_app_up() {
  checkpoint
  systemctl restart aboutme-web
  http_ok http://127.0.0.1:3000/ 120 || die "aboutme-web did not answer on 127.0.0.1:3000"
  checkpoint
  systemctl start aboutme-server
  # From the host, not only from outside, so the answer does not depend on
  # which Caddy takes the connection (docs/design/vietnam-production.md,
  # "Deploy", step 8).
  http_ok http://127.0.0.1:8080/readyz 180 || die "aboutme-server did not become ready"
  systemctl start aboutme-caddy
  sleep 3
  active aboutme-caddy || die "aboutme-caddy did not stay up"
  (($(caddy_listeners 443) >= 1)) || die "no Caddy listens on 443"
  say "app up"
}

step_maintenance_down() {
  systemctl stop aboutme-maintenance
  wait_stopped aboutme-maintenance 60 || die "aboutme-maintenance did not stop"
  ! podman container exists aboutme-maintenance || die "the maintenance container still exists"
  say "maintenance down"
}

# First install only, as postgres over peer authentication: the database
# owner role aboutme (CREATEROLE, like the RDS master) and the database, then
# the pgBackRest stanza (docs/design/vietnam-production.md, "PostgreSQL").
# The password goes to psql on standard input and never on a command line;
# log_min_error_statement is raised for the session so a failed statement
# cannot log it.
step_db_bootstrap() {
  [[ -s $secrets/db-admin-password ]] || die "run secrets.sh first: $secrets/db-admin-password is missing"
  {
    printf '%s\n' "SET log_min_error_statement = panic;" "SET log_statement = 'none';"
    printf '\\set pw %s\n' "'$(cat "$secrets/db-admin-password")'"
    cat <<'SQL'
SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'aboutme') AS create_role \gset
\if :create_role
CREATE ROLE aboutme LOGIN CREATEROLE PASSWORD :'pw';
\else
ALTER ROLE aboutme LOGIN CREATEROLE PASSWORD :'pw';
\endif
SELECT NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'aboutme') AS create_db \gset
\if :create_db
CREATE DATABASE aboutme OWNER aboutme;
\endif
SQL
  } | runuser -u postgres -- psql -X -q -v ON_ERROR_STOP=1 -d postgres >/dev/null ||
    die "could not create the aboutme role and database"
  if runuser -u postgres -- pgbackrest --stanza=aboutme info --output=json | jq -e '.[0].status.code == 0' >/dev/null 2>&1; then
    say "pgBackRest stanza exists"
  else
    runuser -u postgres -- pgbackrest --stanza=aboutme stanza-create || die "pgBackRest stanza-create failed"
  fi
  runuser -u postgres -- pgbackrest --stanza=aboutme check || die "pgBackRest check failed"
  say "database bootstrap done"
}

[[ $(id -u) == 0 ]] || die "run as root on the host"
(($# >= 3)) || die "usage: deploy-host.sh <op|-> <tag> <step> [args]"
op=$1 tag=$2 step=$3
shift 3
[[ $op == - || $op =~ ^[A-Za-z0-9_-]{43}$ ]] || die "malformed operation id"
[[ $op == - && $tag == - && $step == release-json ]] || release_number "$tag" >/dev/null ||
  die "'$tag' is not a strict vMAJOR.MINOR.PATCH tag"
secrets_block=""
flags="{}"

case "$step:$#" in
  status:0) step_status ;;
  running-release:0) step_running_release ;;
  release-json:0) step_release_json ;;
  pull:3) step_pull "$@" ;;
  backup:0) step_backup ;;
  timers-stop:0) step_timers stop ;;
  timers-start:0) step_timers start ;;
  fail-closed:0) step_fail_closed ;;
  cutover-quiesce:0) step_cutover_quiesce ;;
  cutover-check:0) step_cutover_check ;;
  cutover-fail-closed:0) step_cutover_fail_closed ;;
  maintenance-up:1) step_maintenance_up "$1" ;;
  ban-test:2) step_ban_test "$@" ;;
  app-down:0) step_app_down ;;
  render:3) step_render "$@" ;;
  restore-previous:0) step_restore_previous ;;
  secrets-check:0) step_secrets_check ;;
  db-setup:0) step_oneshot aboutme-db-setup ;;
  migrate:0) step_oneshot aboutme-migrate ;;
  app-up:0) step_app_up ;;
  maintenance-down:0) step_maintenance_down ;;
  db-bootstrap:0) step_db_bootstrap ;;
  *) die "unknown step or wrong arguments: $step" ;;
esac
