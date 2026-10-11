# Vietnam production

## Source map

|Path|Role|
|-|-|
|`prod/`|Production OpenTofu root|
|`host/install.sh`, `host/data-volume.sh`|Host installation and explicit data-volume preparation|
|`host/etc/`, `host/bin/`, `host/env/`|Installed configuration, host jobs, and non-secret environment templates|
|`host/quadlet/`|Container templates and network units|
|`host/etc/systemd/system/`|Host service and timer units, mounts, and service drop-ins|
|`edge/`, `probe/`|Edge controls and host fact checks|
|`scripts/deploy.sh`, `scripts/deploy-host.sh`|Laptop coordinator and host deployment operations|
|`scripts/fence.sh`, `host/workload-lock.sh`|Persistent operation fence and serialized heavy workloads|
|`tests/`|Static deployment, lock, recovery, secret, and edge regressions|

## Boundaries and outputs

`host/install.sh` installs the reviewed host bundle. `scripts/deploy-host.sh` renders `host/quadlet/` per release. `prod/` describes infrastructure; templates and plans do not authorize applying it.

Keep persistent deployment locks and workload locks intact. Failed recovery retains the operation fence for manual recovery; do not introduce timed takeover. Host secret files remain root-only runtime input. Infrastructure state, credentials, and encrypted secret copies stay outside tracked sources; inspect existence and metadata, never secret values.

## Authorities and checks

[Deployment rules](../../instructions/deploy.md), [release rules](../../instructions/releases.md), [production runbook](../../docs/runbooks/production.md), and [Vietnam README](README.md) own deployment and recovery. [Vietnam design](../../docs/design/vietnam-production.md) and [ADR 0027](../../docs/adr/0027-vietnam-hosted-production.md) own host layout and trust boundaries.

[Verification](../../instructions/verification.md) names the hosted VN infrastructure gate. `tests/deploy-safety-test.sh`, `tests/deploy-failure-test.sh`, `tests/workload-lock-test.sh`, and `tests/secrets-regression-test.sh` cover static safety; the remaining `tests/` cover cutover, observer publication, alerts, and CrowdSec. Infrastructure formatting and validation use the hosted gate.
