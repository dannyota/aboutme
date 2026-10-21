#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
host=$repo_root/deploy/vn/host

if ! python3 - "$host" <<'PY' >/dev/null 2>&1
import pathlib
import re
import sys

host = pathlib.Path(sys.argv[1])
script = (host / "crowdsec-vacuum.sh").read_text()
service = (host / "etc/systemd/system/aboutme-crowdsec-vacuum.service").read_text()
timer = (host / "etc/systemd/system/aboutme-crowdsec-vacuum.timer").read_text()
assert "crowdsec-db.sh" in script and "preflight-ready" in script
assert re.search(r"\bflock\b", script)
assert "crowdsec-schema-v1.8.1.tsv" in script
assert re.search(r"VACUUM\s*\(\s*ANALYZE\s+FALSE\s*\)", script, re.I)
without_allowed = re.sub(r"VACUUM\s*\(\s*ANALYZE\s+FALSE\s*\)", "", script, flags=re.I)
assert not re.search(r"\bANALYZE\b|\bFULL\b|\bFREEZE\b|\bCHECKPOINT\b|sqlite|pgbackrest|pg_switch_wal", without_allowed, re.I)
assert "postgresql@18-main.service" in service
assert "Type=oneshot" in service and "TimeoutStartSec=5min" in service
assert "ExecStart=/usr/local/lib/aboutme/crowdsec-vacuum.sh" in service
assert "StandardOutput=null" in service and "StandardError=null" in service
assert "OnFailure=" in service
assert re.search(r"(?m)^OnCalendar=(?:hourly|\*:\d{1,2})$", timer)
assert "Unit=aboutme-crowdsec-vacuum.service" in timer
assert "Persistent=true" in timer
PY
then
  echo "FAIL postgresql-non-analyzing-hourly-vacuum" >&2
  exit 1
fi

echo "CrowdSec vacuum tests passed"
