#!/usr/bin/env bash
# Every start revalidates the current database epoch and the retention lease.
set -euo pipefail

lease=${1:-/run/aboutme/crowdsec-retention-ok}
if ((EUID == 0)) && (($# != 0)); then exit 2; fi
for timer in aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer; do
  systemctl is-enabled --quiet "$timer"
  systemctl is-active --quiet "$timer"
done
[[ -f $lease && ! -L $lease ]]
epoch=$(python3 -I - "$lease" <<'PY' 2>/dev/null
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
) || exit 1
printf '%s\n' "$epoch" | /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready \
  >/dev/null 2>&1
