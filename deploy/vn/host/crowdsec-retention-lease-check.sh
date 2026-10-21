#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Every check revalidates the current epoch; source-only callers retain their lock.

crowdsec_lease_file() {
  crowdsec_lease_epoch=$(python3 -I - /run/aboutme/crowdsec-retention-ok <<'PY' 2>/dev/null
import os,re,stat,sys,time
try:
    fd=os.open(sys.argv[1],os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    entry=os.fstat(fd)
    assert stat.S_ISREG(entry.st_mode) and entry.st_nlink==1
    assert entry.st_mtime<=time.time()<=entry.st_mtime+360
    assert entry.st_uid==entry.st_gid==0 and stat.S_IMODE(entry.st_mode)==0o600
    raw=os.read(fd,66)
    assert re.fullmatch(rb'[0-9a-f]{64}\n',raw)
    print(raw.decode('ascii'),end='')
except Exception: sys.exit(1)
PY
  )
}
crowdsec_lease_check() {
  # The executed preflight-ready interface and this source-only path use the
  # same complete read-only gate. Internal callers retain the canonical lock.
  # shellcheck source=crowdsec-db.sh
  source /usr/local/lib/aboutme/crowdsec-db.sh
  crowdsec_db_read_lock && crowdsec_lease_file && crowdsec_db_ready "$crowdsec_lease_epoch" || return 1
  local timer normal=yes
  for timer in aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer; do
    systemctl is-enabled --quiet "$timer" && systemctl is-active --quiet "$timer" || normal=no
  done
  if [[ $normal == yes ]]; then return 0; fi
  return 1
}
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  set -euo pipefail
  (($#==0)) || exit 2
  unset crowdsec_db_lock_fd
  crowdsec_lease_check >/dev/null 2>&1
fi
