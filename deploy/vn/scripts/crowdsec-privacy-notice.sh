#!/usr/bin/env bash
# Fixed root-only owner exchange for the accepted privacy notice.
set -euo pipefail
# shellcheck source=../host/crowdsec-privacy-notice-host.sh
source /usr/local/lib/aboutme/crowdsec-privacy-notice-host.sh 2>/dev/null || { printf 'invalid_state\n' >&2; exit 1; }
readonly publication_date=2026-10-20
readonly content_sha256=bb128b39501f09db59915f3f75759572a49349ec78fe17ad48b6ba40d500ffdc
notice_manifest() {
  local line
  while IFS= read -r line; do LC_ALL=C printf '%s\n' "$line"; done <<'MANIFEST'
aboutme-crowdsec-privacy-content-v1
campaign_id=crowdsec-postgres-paths-v1
publication_date=2026-10-20
canonical_link=https://aboutme.vn/privacy
public_en=Attack protection uses your IP address, request time, method, response status, broad route class, and a fixed probe category. Only for unrecognized routes returning 400, 404, or 405, attack alert samples may include the exact escaped target path, without the query, up to 512 bytes. We do not record target paths for known routes, including account, API, and valid public resume routes. Invalid or oversized targets use a fixed category and no path. Queries, headers, and bodies are never included. The request feed stays in RAM; CrowdSec stores attack alert samples and bans, not a full access log. Live records are kept for at most 24 hours. Encrypted database backups in Vietnam may retain earlier records for about five weeks after deletion, under the backup policy below. Backups are used only for disaster recovery. Restored CrowdSec data is discarded before attack protection resumes. None of this data is shared with the CrowdSec community.
public_vi=Hệ thống chống tấn công dùng địa chỉ IP, thời điểm yêu cầu, phương thức, mã phản hồi, nhóm tuyến truy cập và nhóm dấu hiệu thăm dò cố định. Chỉ với tuyến không được nhận diện trả về mã 400, 404 hoặc 405, mẫu cảnh báo tấn công mới có thể chứa nguyên đường dẫn đích đã mã hóa ký tự, bỏ phần truy vấn, tối đa 512 byte. Chúng tôi không ghi đường dẫn đích của các tuyến đã biết, gồm tuyến tài khoản, API và CV công khai hợp lệ. Đích không hợp lệ hoặc quá dài chỉ được gán nhóm cố định, không ghi đường dẫn. Tham số truy vấn, tiêu đề và nội dung yêu cầu không được ghi. Dữ liệu yêu cầu nằm trong RAM; CrowdSec lưu mẫu cảnh báo tấn công và lệnh chặn, không lưu nhật ký đầy đủ mọi lượt truy cập. Bản ghi đang hoạt động được giữ tối đa 24 giờ. Bản sao lưu cơ sở dữ liệu được mã hóa tại Việt Nam có thể giữ bản ghi cũ trong khoảng năm tuần sau khi xóa, theo chính sách lưu bản sao bên dưới. Bản sao lưu chỉ dùng để khôi phục sau sự cố. Dữ liệu CrowdSec được khôi phục sẽ bị loại bỏ trước khi hệ thống chống tấn công hoạt động trở lại. Không dữ liệu nào được chia sẻ với cộng đồng CrowdSec.
email_subject=Cập nhật quyền riêng tư aboutme.vn / aboutme.vn privacy update
email_button=Đọc chính sách quyền riêng tư · Read the privacy policy
email_heading_vi=Thông báo trước về thay đổi chống tấn công
email_heading_en=Advance notice of an attack-protection change
email_preheader=Chính sách quyền riêng tư đã được cập nhật. The privacy policy has been updated.
email_body_vi=Chúng tôi đã cập nhật chính sách quyền riêng tư của aboutme.vn. Trước khi áp dụng thay đổi, chúng tôi gửi email này để thông báo: mẫu cảnh báo tấn công có thể chứa đường dẫn đích của tuyến không được nhận diện trả về mã 400, 404 hoặc 405, tối đa 512 byte và không có phần truy vấn. Đường dẫn của tuyến đã biết, gồm tài khoản, API và CV công khai hợp lệ, không được ghi. Dữ liệu CrowdSec sẽ được lưu trong PostgreSQL tại Việt Nam; bản ghi đang hoạt động được giữ tối đa 24 giờ. Bản sao lưu được mã hóa tại Việt Nam có thể giữ bản ghi cũ trong khoảng năm tuần sau khi xóa. Vui lòng đọc chính sách đã cập nhật để biết đầy đủ phạm vi thu thập và cách lưu giữ. Bạn không cần thực hiện thao tác nào. Bạn có thể trả lời email này nếu có câu hỏi.
email_body_en=We have updated the aboutme.vn privacy policy. Before this change takes effect, we are emailing you to explain it: attack alert samples may include target paths for unrecognized routes returning 400, 404, or 405, up to 512 bytes and without the query. Paths for known routes, including account, API, and valid public resume routes, are not recorded. CrowdSec data will be stored in PostgreSQL in Vietnam; live records are kept for at most 24 hours. Encrypted backups in Vietnam may retain earlier records for about five weeks after deletion. Please read the updated policy for the full collection and retention rules. You do not need to take any action. You can reply to this email with questions.
MANIFEST
}
notice_now() { date -u '+%Y-%m-%dT%H:%M:%S.%6NZ'; }
notice_release_number() {
  [[ $1 =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || return 1
  printf '%s\n' "$((10#${BASH_REMATCH[1]} * 1000000 + 10#${BASH_REMATCH[2]} * 1000 + 10#${BASH_REMATCH[3]}))"
}
notice_release_read() {
  local number unit image
  notice_recheck_mount && notice_safe_file /var/lib/aboutme/release.json 644 || return 1
  notice_release=$(jq -ce '
    def image($n): type=="string" and test("^ghcr\\.io/dannyota/aboutme-"+$n+"@sha256:[0-9a-f]{64}$");
    select(keys==["caddy_image","maintenance_image","maintenance_number","maintenance_tag","release_number","release_tag","server_image","web_image"])
    | select((.server_image|image("server")) and (.web_image|image("web")) and (.caddy_image|image("caddy")) and (.maintenance_image|image("caddy")))' /var/lib/aboutme/release.json) || return 1
  notice_tag=$(jq -er .release_tag <<<"$notice_release")
  number=$(notice_release_number "$notice_tag") || return 1
  [[ $number == "$(jq -r .release_number <<<"$notice_release")" && \
    $(notice_release_number "$(jq -r .maintenance_tag <<<"$notice_release")") == "$(jq -r .maintenance_number <<<"$notice_release")" ]] || return 1
  for unit in server web; do
    notice_safe_file "/etc/containers/systemd/aboutme-$unit.container" 644 || return 1
    image=$(jq -r --arg name "${unit}_image" '.[$name]' <<<"$notice_release")
    [[ $(grep -c '^Image=' "/etc/containers/systemd/aboutme-$unit.container") == 1 ]] || return 1
    grep -qxF "Image=$image" "/etc/containers/systemd/aboutme-$unit.container" || return 1
  done
  grep -qxF "Environment=DEPLOY_RELEASE_TAG=$notice_tag" /etc/containers/systemd/aboutme-server.container || return 1
  [[ $(notice_manifest | sha256sum | cut -d' ' -f1) == "$content_sha256" ]] || return 1
  notice_manifest | grep -xF "publication_date=$publication_date" >/dev/null
}
notice_owner_receipts() {
  local name model parsed cutoff expected
  for name in send activation; do
    if ! notice_absent "/var/lib/aboutme/privacy-notice/$name.json"; then
      notice_safe_file "/var/lib/aboutme/privacy-notice/$name.json" || return 1
      model=$name
      parsed=$(notice_wire "$model" 0 "$(notice_now)" <"/var/lib/aboutme/privacy-notice/$name.json") || return 1
      [[ $(jq -r .epoch <<<"$parsed") == "$notice_epoch" ]] || return 1
      if [[ -v notice_report ]]; then
        cutoff=$(jq -r .cutoff_at <<<"$notice_report"); expected=$(jq -r .expected_count <<<"$notice_report")
        [[ $(jq -r .cutoff_at <<<"$parsed") == "$cutoff" && $(jq -r .expected_count <<<"$parsed") == "$expected" ]] || return 1
        if [[ $name == send ]]; then
          [[ $(jq -r .state <<<"$notice_report") == sealed && ( $(jq -r .issued_at <<<"$parsed") > "$(jq -r .sealed_at <<<"$notice_report")" ||
             $(jq -r .issued_at <<<"$parsed") == "$(jq -r .sealed_at <<<"$notice_report")" ) ]] || return 1
        else
          [[ $(jq -r .state <<<"$notice_report") == activated && \
            $(jq -r .accepted_count <<<"$parsed") == "$(jq -r .counts.accepted <<<"$notice_report")" && \
            $(jq -r .deleted_count <<<"$parsed") == "$(jq -r .counts.deleted <<<"$notice_report")" && \
            $(jq -r .activated_at <<<"$parsed") == "$(jq -r .activated_at <<<"$notice_report")" ]] || return 1
        fi
      fi
    fi
  done
}
notice_command() {
  local action=$1 raw result=0 image
  image=$(jq -r .server_image <<<"$notice_release")
  local args=(run --rm --network=none --memory=256m --read-only --cap-drop=ALL --security-opt=no-new-privileges
    --entrypoint /usr/bin/env --volume /run/postgresql:/run/postgresql
    --volume /var/lib/aboutme/privacy-notice:/var/lib/aboutme/privacy-notice:ro
    --env 'DATABASE_URL=postgres://aboutme_app@localhost/aboutme?host=/run/postgresql'
    --secret 'db-app-password,type=env,target=PGPASSWORD')
  if [[ $action == enqueue ]]; then
    args+=(--secret 'auth-email-active-key-id,type=env,target=AUTH_EMAIL_ACTIVE_KEY_ID'
      --secret 'auth-email-active-key,type=env,target=AUTH_EMAIL_ACTIVE_KEY')
  fi
  notice_command_started=yes
  notice_cleanup_safe=no
  raw=$(podman "${args[@]}" "$image" server crowdsec-privacy-notice "$action" 2>/dev/null) || result=$?
  case $result in 0|1|2) ;; *) return 1 ;; esac
  [[ ${#raw} -le 4095 ]] || return 1
  notice_report=$(notice_wire report "$result" "$(notice_now)" <<<"$raw") || return 1
  notice_command_exit=$result
}
notice_initial_authority() {
  notice_recheck_mount && notice_authority_check &&
    cmp -s /var/lib/aboutme/privacy-notice/epoch /var/lib/aboutme/privacy-notice/initial-epoch || return 1
  local name
  for name in send.json activation.json capture-in-progress; do
    notice_absent "/var/lib/aboutme/privacy-notice/$name" || return 1
  done
  if ! notice_absent /var/lib/aboutme/privacy-notice/restore-reconciliation; then
    notice_safe_file /var/lib/aboutme/privacy-notice/restore-reconciliation &&
      [[ ! -s /var/lib/aboutme/privacy-notice/restore-reconciliation ]] || return 1
  fi
}
notice_initial_quiescent() {
  aboutme_assert_cutover_quiescent || return 1
  local unit jobs units rc=0
  # Five-field SQLite protection may continue; changed ingestion has a separate activation gate.
  for unit in aboutme-migrate aboutme-db-setup; do
    notice_unit_stopped "$unit" || return 1
  done
  # shellcheck disable=SC2154 # notice_quiesce sources this array from deploy-safety.sh before this check.
  for unit in "${aboutme_jobs[@]}"; do
    notice_unit_stopped "aboutme-job-$unit.timer" && notice_unit_stopped "aboutme-job@$unit.service" || return 1
  done
  jobs=$(systemctl list-jobs --no-legend --no-pager) || return 1
  [[ ! $jobs =~ (restore|recovery|aboutme-migrate|aboutme-db-setup|aboutme-job) ]] || return 1
  units=$(systemctl list-units --all --state=active,activating,deactivating,reloading --plain --no-legend --no-pager) || return 1
  awk '$1 ~ /(restore|recovery|aboutme-migrate|aboutme-db-setup|aboutme-job)/ { exit 1 }' <<<"$units" || return 1
  pgrep -f '([/]pgbackrest.*restore|[r]estore-drill.sh|[p]ostgres:.*recovery)' >/dev/null || rc=$?
  [[ $rc == 1 ]]
}
notice_initial_bound() {
  local state
  notice_initial_authority && notice_release_read || return 1
  [[ $notice_release == "$notice_initial_release" && $(cat /var/lib/aboutme/privacy-notice/epoch) == "$notice_initial_epoch" ]] || return 1
  state=$(notice_fence read 2>/dev/null) || return 1
  [[ $(jq -r '.notice_authority.state' <<<"$state") == adopted &&
    $(jq -r '.operation_id' <<<"$state") == "$notice_initial_operation" &&
    $(jq -r '.operation_kind' <<<"$state") == cutover ]] || return 1
  [[ $(jq -c '.notice_authority' <<<"$state") == "$notice_initial_evidence" &&
    $(jq -c '{minimum_release,minimum_tag}' <<<"$state") == "$notice_initial_floor" ]] || return 1
  notice_safe_file /var/lib/aboutme/cutover-mutation.json 600 &&
    jq -e --arg id "$notice_initial_operation" 'keys==["mutation_kind","operation_id","started_at","state"] and
      .operation_id==$id and .mutation_kind=="quiesce" and .state=="active"' /var/lib/aboutme/cutover-mutation.json >/dev/null &&
    notice_initial_quiescent
}
notice_initial_command() {
  local raw image
  image=$(jq -r .server_image <<<"$notice_initial_release") || return 1
  # Preserve the kernel locks in every child, and reject surviving descendants.
  raw=$(python3 -I - "$image" <<'PY'
import ctypes,os,selectors,signal,subprocess,sys,time
child=None
try:
    assert ctypes.CDLL(None,use_errno=True).prctl(36,1,0,0,0)==0
    child=subprocess.Popen(['podman','run','--rm','--network=none','--memory=256m','--read-only','--cap-drop=ALL',
        '--security-opt=no-new-privileges','--entrypoint','/usr/bin/env','--volume','/run/postgresql:/run/postgresql',
        '--volume','/var/lib/aboutme/privacy-notice:/var/lib/aboutme/privacy-notice:ro',
        '--env','DATABASE_URL=postgres://aboutme_app@localhost/aboutme?host=/run/postgresql',
        '--secret','db-app-password,type=env,target=PGPASSWORD',sys.argv[1],
        'server','crowdsec-privacy-notice','reconcile-initial'],
        stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE,close_fds=False,start_new_session=True)
    deadline=time.monotonic()+300; selector=selectors.DefaultSelector(); data=bytearray()
    selector.register(child.stdout,selectors.EVENT_READ); selector.register(child.stderr,selectors.EVENT_READ)
    while selector.get_map():
        remaining=deadline-time.monotonic(); assert remaining>0
        for key,_ in selector.select(remaining):
            block=os.read(key.fileobj.fileno(),4097)
            if not block: selector.unregister(key.fileobj); continue
            assert key.fileobj is child.stdout
            data.extend(block); assert len(data)<=4096
    assert child.wait(timeout=max(0.001,deadline-time.monotonic()))==0
    while True:
        try:
            pid,status=os.waitpid(-1,os.WNOHANG)
            assert pid>0 and os.WIFEXITED(status) and os.WEXITSTATUS(status)==0
        except ChildProcessError: break
    assert data.endswith(b'\n') and data.count(b'\n')==1 and b'\r' not in data and b'\0' not in data
    os.write(1,data)
except Exception:
    if child is not None:
        try: os.killpg(child.pid,signal.SIGKILL)
        except ProcessLookupError: pass
        try: child.wait(timeout=5)
        except subprocess.TimeoutExpired: pass
    sys.exit(1)
PY
  ) || return 1
  notice_initial_report=$(notice_wire initial 0 <<<"$raw")
}
notice_reconcile_initial() {
  local state dependency
  for dependency in crowdsec-privacy-notice-host.sh fence.sh deploy-safety.sh workload-lock.sh; do
    notice_safe_file "/usr/local/lib/aboutme/$dependency" 755 || return 1
  done
  notice_identity=$(notice_mount_identity) || return 1
  exec {notice_initial_bundle_fd}>>/srv/data/host-operations/.bundle.lock || return 1
  flock -x -w 30 "$notice_initial_bundle_fd" || return 1
  notice_initial_authority && notice_release_read || return 1
  notice_initial_release=$notice_release
  notice_initial_epoch=$(cat /var/lib/aboutme/privacy-notice/epoch) || return 1
  state=$(notice_fence read 2>/dev/null) || return 1
  [[ $(jq -r '.notice_authority.state' <<<"$state") == adopted && $(jq -r '.operation_id // empty' <<<"$state") == '' ]] || return 1
  notice_initial_evidence=$(jq -c '.notice_authority' <<<"$state")
  notice_initial_floor=$(jq -c '{minimum_release,minimum_tag}' <<<"$state")
  notice_initial_operation=$(notice_fence lock cutover "$notice_tag" 2>/dev/null) || return 1
  [[ $notice_initial_operation =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]] || return 1
  exec {notice_initial_fd}>>/run/aboutme/workload.lock || return 1
  flock -x -w 300 "$notice_initial_fd" || return 1
  notice_initial_authority && notice_release_read && [[ $notice_release == "$notice_initial_release" ]] || return 1
  notice_fence checkpoint "$notice_initial_operation" "$notice_tag" >/dev/null 2>&1 || return 1
  notice_quiesce >/dev/null && notice_initial_quiescent || return 1
  notice_fence mutation-start "$notice_initial_operation" "$notice_tag" quiesce >/dev/null 2>&1 || return 1
  notice_initial_bound && notice_initial_command && notice_initial_bound || return 1
  notice_revoke restore-reconciliation && notice_initial_bound &&
    notice_absent /var/lib/aboutme/privacy-notice/restore-reconciliation || return 1
  notice_fence mutation-clear "$notice_initial_operation" quiesce >/dev/null 2>&1 || return 1
  exec {notice_initial_fd}>&-
  notice_fence release "$notice_initial_operation" >/dev/null 2>&1 || return 1
  printf '%s\n' "$notice_initial_report"
}
notice_owner_run() {
  [[ $(id -u) == 0 && $# == 1 ]] && notice_overrides_absent || return 1
  local action=$1 snapshot receipt previous
  case $action in prepare|enqueue|status|activate|authorize-send) ;; reconcile-initial) notice_reconcile_initial; return ;; adopt) /usr/local/lib/aboutme/crowdsec-privacy-notice-host.sh adopt; return ;; *) return 1 ;; esac
  # shellcheck disable=SC2034 # Used by the sourced canonical mount recheck.
  notice_identity=$(notice_mount_identity) || return 1
  notice_authority_check && notice_absent /var/lib/aboutme/privacy-notice/restore-reconciliation || return 1
  notice_epoch=$(cat /var/lib/aboutme/privacy-notice/epoch)
  notice_release_read || return 1
  snapshot=$notice_release
  notice_owner_receipts || return 1
  notice_owner_operation=$(notice_fence lock deploy "$notice_tag" 2>/dev/null) || return 1
  exec {notice_owner_fd}>>/run/aboutme/workload.lock || return 1
  flock -x -w 300 "$notice_owner_fd" || return 1
  notice_recheck_mount && notice_authority_check || return 1
  notice_fence checkpoint "$notice_owner_operation" "$notice_tag" >/dev/null 2>&1 || return 1
  if [[ $action == prepare ]]; then
    if notice_absent /var/lib/aboutme/privacy-notice/capture-in-progress; then notice_replace capture-in-progress </dev/null || return 1; fi
    notice_cleanup_safe=yes
    notice_quiesce || return 1
  elif [[ $action == activate ]]; then
    notice_cleanup_safe=yes
    notice_quiesce || return 1
    notice_cleanup_safe=no
    notice_revoke send.json || return 1
  fi
  previous=$notice_epoch
  if [[ $action == authorize-send ]]; then notice_command status || return 1
  else notice_command "$action" || return 1; fi
  notice_release_read || return 1
  [[ $notice_release == "$snapshot" && $(cat /var/lib/aboutme/privacy-notice/epoch) == "$previous" ]] || return 1
  notice_owner_receipts || return 1
  case $action in
    authorize-send)
      [[ $(jq -r .state <<<"$notice_report") == sealed && $notice_command_exit != 1 ]] || return 1
      receipt=$(jq -c --arg epoch "$notice_epoch" --arg now "$(notice_now)" \
        '{schema_version,campaign_id,content_sha256,cutoff_at,expected_count,epoch:$epoch,issued_at:$now}' <<<"$notice_report") || return 1
      printf '%s\n' "$receipt" | notice_wire send 0 "$(notice_now)" >/dev/null || return 1
      notice_absent /var/lib/aboutme/privacy-notice/restore-reconciliation && printf '%s\n' "$receipt" | notice_replace send.json || return 1
      notice_command_exit=0 ;;
    activate)
      if [[ $notice_command_exit == 0 ]]; then
        [[ $(jq -r .state <<<"$notice_report") == activated ]] || return 1
        notice_fence raise "$notice_owner_operation" "$notice_tag" >/dev/null 2>&1 || return 1
        receipt=$(jq -c --arg epoch "$notice_epoch" \
          '{schema_version,campaign_id,content_sha256,cutoff_at,expected_count,epoch:$epoch,accepted_count:.counts.accepted,deleted_count:.counts.deleted,activated_at}' <<<"$notice_report") || return 1
        printf '%s\n' "$receipt" | notice_wire activation 0 "$(notice_now)" >/dev/null || return 1
        notice_absent /var/lib/aboutme/privacy-notice/restore-reconciliation && printf '%s\n' "$receipt" | notice_replace activation.json || return 1
      fi ;;
    prepare)
      if [[ $notice_command_exit == 0 ]]; then
        [[ $(jq -r .state <<<"$notice_report") == sealed ]] || return 1
        notice_revoke capture-in-progress || return 1
      fi ;;
  esac
  exec {notice_owner_fd}>&-
  notice_fence release "$notice_owner_operation" >/dev/null 2>&1 || return 1
  unset notice_owner_operation
  if [[ $action != authorize-send ]]; then printf '%s\n' "$notice_report"; fi
}
notice_command_exit=0
notice_command_started=no notice_cleanup_safe=no
trap 'notice_invalid; exit 1' HUP TERM INT
if ! notice_owner_run "$@" 2>/dev/null; then
  # Only a pre-effect quiescence failure may release this returned operation.
  # Once an app call starts, failure requires explicit inspection/recovery.
  if [[ -v notice_owner_operation && $notice_command_started == no && $notice_cleanup_safe == yes ]]; then
    if [[ -v notice_owner_fd ]]; then exec {notice_owner_fd}>&-; fi
    notice_fence release "$notice_owner_operation" >/dev/null 2>&1 || true
  fi
  notice_invalid; exit 1
fi
exit "$notice_command_exit"
