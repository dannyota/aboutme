# Deploy tasks

## Task 13

### `deploy.sh` with first-deploy and rollback modes

**Files:**

- Create: `deploy/aws/scripts/deploy.sh`
- Create: `deploy/aws/scripts/deploy_test.sh`
- Modify: `Makefile` (integration owner: add `deploy-script-test`)

**Interfaces:**

- Usage: `deploy.sh <tag> [--first-deploy]` or `deploy.sh --rollback <tag>`.
- Consumes the task definition families and container names from Task 10, the
  schedule group from Task 12, and GHCR tags from Task 7.
- Every external command goes through the `aws`, `gh`, `git` and `curl` binaries
  on `PATH`, so the test replaces them with stubs.

- [ ] **Step 1: Write the failing test**

`deploy_test.sh` puts stub commands first on `PATH`. Each stub appends its
arguments to `$CALLS` and prints a canned response from `$STUB_DIR`. The test
runs four scenarios and checks the order of recorded calls:

```bash
#!/usr/bin/env bash
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

make_stubs() {
  mkdir -p "$work/bin"
  for cmd in aws gh git curl; do
    cat > "$work/bin/$cmd" <<'EOF'
#!/usr/bin/env bash
printf '%s %s\n' "$(basename "$0")" "$*" >> "$CALLS"
exec "$STUB_DIR/respond" "$(basename "$0")" "$@"
EOF
    chmod +x "$work/bin/$cmd"
  done
  cp "$here/testdata/respond" "$work/respond"
}

run_case() { # name expected-exit args...
  local name=$1 want=$2; shift 2
  : > "$work/$name.calls"
  set +e
  CALLS="$work/$name.calls" STUB_DIR="$work" STUB_CASE="$name" \
    PATH="$work/bin:$PATH" DEPLOY_SMOKE_TIMEOUT=1 bash "$here/deploy.sh" "$@" >/dev/null 2>&1
  local got=$?
  set -e
  [[ $got -eq $want ]] || { echo "$name: exit $got, want $want" >&2; exit 1; }
}

before() { # file first second
  local a b
  a=$(grep -n -m1 -- "$2" "$1" | cut -d: -f1)
  b=$(grep -n -m1 -- "$3" "$1" | cut -d: -f1)
  [[ -n $a && -n $b && $a -lt $b ]] || { echo "order: '$2' must precede '$3' in $1" >&2; exit 1; }
}

make_stubs

run_case ok 0 v0.1.0
f=$work/ok.calls
before "$f" "rds create-db-snapshot" "ecs update-service --cluster aboutme-prod --service aboutme-prod-app --desired-count 0"
before "$f" "scheduler update-schedule" "ecs update-service --cluster aboutme-prod --service aboutme-prod-app --desired-count 0"
before "$f" "--started-by deploy-migrate" "--service aboutme-prod-app --task-definition"
if grep -q "deploy-db-bootstrap" "$f"; then echo "ok: ran first-deploy steps" >&2; exit 1; fi

run_case first 0 v0.1.0 --first-deploy
f=$work/first.calls
before "$f" "--started-by deploy-db-bootstrap" "--started-by deploy-db-provision"
before "$f" "--started-by deploy-db-provision" "--started-by deploy-db-set-login"
before "$f" "--started-by deploy-db-set-login" "--started-by deploy-migrate"

run_case migrate_fails 1 v0.1.0
f=$work/migrate_fails.calls
grep -q -- "--desired-count 1" "$f" || { echo "migrate_fails: app not restored" >&2; exit 1; }
[[ $(grep -c "scheduler update-schedule" "$f") -eq 8 ]] || { echo "migrate_fails: schedules not re-enabled" >&2; exit 1; }

run_case rollback 0 --rollback v0.0.9
f=$work/rollback.calls
if grep -q "deploy-migrate" "$f"; then echo "rollback: ran migrate" >&2; exit 1; fi

echo "deploy-script-test: ok"
```

`deploy/aws/scripts/testdata/respond` answers by command and `$STUB_CASE`:

```bash
#!/usr/bin/env bash
cmd=$1; shift
args="$*"
case "$cmd $args" in
  "git merge-base"*) exit 0 ;;
  "git rev-list"*) echo 0123456789abcdef ;;
  "gh run list"*) echo success ;;
  "curl "*"/token"*) echo '{"token":"t"}' ;;
  "curl "*"/manifests/"*) printf 'docker-content-digest: sha256:%064d\r\n' 1 ;;
  "curl "*"api.cloudflare.com/client/v4/ips"*) echo '{"result":{"ipv4_cidrs":["192.0.2.0/24"]}}' ;;
  "curl -fsSI https://aboutme.vn/") echo 'strict-transport-security: max-age=31536000' ;;
  "curl "*"https://aboutme.vn/"*) echo 200 ;;
  "curl "*) exit 28 ;;
  "aws "*"ecs describe-task-definition"*)
    echo '{"family":"f","containerDefinitions":[{"name":"caddy","image":"x","environment":[{"name":"CLOUDFLARE_RANGES","value":"192.0.2.0/24"}]}]}' ;;
  "aws "*"ecs describe-services"*) echo 'arn:aws:ecs:ap-southeast-1:1:task-definition/aboutme-prod-app:3' ;;
  "aws "*"ecs register-task-definition"*) echo 'arn:aws:ecs:ap-southeast-1:1:task-definition/new:4' ;;
  "aws "*"ecs run-task"*)
    by=$(sed -E 's/.*--started-by ([^ ]+).*/\1/' <<<"$args")
    echo "arn:aws:ecs:ap-southeast-1:1:task/aboutme-prod/$by" ;;
  "aws "*"ecs describe-tasks"*)
    if [[ $STUB_CASE == migrate_fails && $args == *deploy-migrate* ]]; then echo 1; else echo 0; fi ;;
  "aws "*"scheduler list-schedules"*) printf 'a\nb\nc\nd\n' ;;
  "aws "*"scheduler get-schedule"*) echo '{"Name":"a","GroupName":"g","ScheduleExpression":"rate(1 hour)","FlexibleTimeWindow":{"Mode":"OFF"},"Target":{"Arn":"x","RoleArn":"y"},"State":"ENABLED"}' ;;
  "aws "*"ec2 describe-addresses"*) echo 192.0.2.10 ;;
  *) exit 0 ;;
esac
```

Each one-shot task starts with `--started-by deploy-<family>`. The stub puts
that value in the task ARN, so the later `describe-tasks` call for the migrate
task carries `deploy-migrate` and the `migrate_fails` case can fail it.

- [ ] **Step 2: Run it and confirm it fails**

```sh
bash deploy/aws/scripts/deploy_test.sh
```

Expected: fails because `deploy.sh` does not exist.

- [ ] **Step 3: Write `deploy.sh`**

```bash
#!/usr/bin/env bash
# Deploys tagged images to aboutme-prod. See docs/runbooks/production.md.
set -euo pipefail
region=ap-southeast-1
cluster=aboutme-prod
group=aboutme-prod-jobs
repo=dannyota/aboutme
families=(app web migrate jobs db-bootstrap db-provision db-set-login)
first=0 rollback=0 tag=""
case "${1:-}" in
  --rollback) rollback=1; tag=${2:?tag required} ;;
  "") echo "usage: deploy.sh <tag> [--first-deploy] | --rollback <tag>" >&2; exit 2 ;;
  *) tag=$1; [[ "${2:-}" == --first-deploy ]] && first=1 ;;
esac
AWS=(aws --region "$region")
say() { printf 'deploy: %s\n' "$*" >&2; }

digest() { # image name -> sha256:...
  local token
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-$1:pull" | jq -r .token)
  curl -fsSI -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json' \
    "https://ghcr.io/v2/$repo-$1/manifests/$tag" \
    | tr -d '\r' | awk -F': ' 'tolower($1)=="docker-content-digest"{print $2}'
}

# 1. Candidate checks.
commit=$(git rev-list -n1 "$tag")
git merge-base --is-ancestor "$commit" origin/main || { say "$tag is not on main"; exit 1; }
[[ $(gh run list --workflow ci.yml --commit "$commit" --json conclusion -q '.[0].conclusion') == success ]] \
  || { say "CI is not green for $tag"; exit 1; }
declare -A image
for name in server web caddy; do
  d=$(digest "$name")
  [[ $d == sha256:* ]] || { say "no $name image for $tag"; exit 1; }
  image[$name]="ghcr.io/$repo-$name@$d"
done

current_def() {
  "${AWS[@]}" ecs describe-task-definition --task-definition "aboutme-prod-$1" --query taskDefinition --output json
}
live=$(curl -fsS https://api.cloudflare.com/client/v4/ips | jq -r '.result.ipv4_cidrs | sort | join(" ")')
deployed=$(current_def app | jq -r '.containerDefinitions[] | select(.name=="caddy") | .environment[] | select(.name=="CLOUDFLARE_RANGES") | .value | split(" ") | sort | join(" ")')
[[ $live == "$deployed" ]] || { say "Cloudflare ranges changed; run tofu apply first"; exit 1; }

# 2. Snapshot.
if [[ $rollback -eq 0 ]]; then
  snap="aboutme-prod-${tag//./-}-$(date -u +%Y%m%d%H%M)"
  "${AWS[@]}" rds create-db-snapshot --db-instance-identifier aboutme-prod --db-snapshot-identifier "$snap" >/dev/null
  "${AWS[@]}" rds wait db-snapshot-available --db-snapshot-identifier "$snap"
  say "snapshot $snap"
fi

# 3. Register revisions.
declare -A revision
for family in "${families[@]}"; do
  revision[$family]=$(current_def "$family" | jq \
    --arg server "${image[server]}" --arg web "${image[web]}" --arg caddy "${image[caddy]}" '
    .containerDefinitions |= map(
      .image = (if .name == "caddy" then $caddy elif .name == "web" then $web else $server end)
      | .environment = ((.environment // []) | map(
          if .name == "APP_BUILD_DIGEST" then .value = $server
          elif .name == "PUBLIC_RENDERER_BUILD_DIGEST" then .value = $web
          else . end)))
    | {family, taskRoleArn, executionRoleArn, networkMode, containerDefinitions,
       requiresCompatibilities, volumes}
    | with_entries(select(.value != null))' \
    | "${AWS[@]}" ecs register-task-definition --cli-input-json file:///dev/stdin \
        --query taskDefinition.taskDefinitionArn --output text)
done

# 4. Stop jobs and the app.
set_schedules() { # ENABLED|DISABLED
  local name
  for name in $("${AWS[@]}" scheduler list-schedules --group-name "$group" --query 'Schedules[].Name' --output text); do
    "${AWS[@]}" scheduler get-schedule --group-name "$group" --name "$name" --output json \
      | jq --arg s "$1" '{Name, GroupName, ScheduleExpression, ScheduleExpressionTimezone,
          FlexibleTimeWindow, Target, State: $s} | with_entries(select(.value != null))' \
      | "${AWS[@]}" scheduler update-schedule --cli-input-json file:///dev/stdin >/dev/null
  done
}
previous_app=$("${AWS[@]}" ecs describe-services --cluster "$cluster" --services aboutme-prod-app \
  --query 'services[0].taskDefinition' --output text)
set_schedules DISABLED
restore() {
  trap - ERR
  say "restoring the previous app revision and job schedules"
  "${AWS[@]}" ecs update-service --cluster "$cluster" --service aboutme-prod-app \
    --task-definition "$previous_app" --desired-count 1 >/dev/null || true
  set_schedules ENABLED || true
}
trap 'restore' ERR
"${AWS[@]}" ecs update-service --cluster "$cluster" --service aboutme-prod-app --desired-count 0 >/dev/null
"${AWS[@]}" ecs wait services-stable --cluster "$cluster" --services aboutme-prod-app
say "site down"

run_once() { # family
  local task code
  task=$("${AWS[@]}" ecs run-task --cluster "$cluster" --launch-type EC2 \
    --task-definition "${revision[$1]}" --started-by "deploy-$1" --query 'tasks[0].taskArn' --output text)
  "${AWS[@]}" ecs wait tasks-stopped --cluster "$cluster" --tasks "$task"
  code=$("${AWS[@]}" ecs describe-tasks --cluster "$cluster" --tasks "$task" \
    --query 'tasks[0].containers[0].exitCode' --output text)
  [[ $code == 0 ]] || { say "$1 exited $code"; return 1; }
}

# 5. Database steps.
if [[ $first -eq 1 ]]; then
  run_once db-bootstrap
  run_once db-provision
  run_once db-set-login
fi
[[ $rollback -eq 1 ]] || run_once migrate

# 6. Start the new release.
"${AWS[@]}" ecs update-service --cluster "$cluster" --service aboutme-prod-web \
  --task-definition "${revision[web]}" --force-new-deployment >/dev/null
"${AWS[@]}" ecs wait services-stable --cluster "$cluster" --services aboutme-prod-web
"${AWS[@]}" ecs update-service --cluster "$cluster" --service aboutme-prod-app \
  --task-definition "${revision[app]}" --desired-count 1 >/dev/null
"${AWS[@]}" ecs wait services-stable --cluster "$cluster" --services aboutme-prod-app
trap - ERR
set_schedules ENABLED
say "site up"

# 7. Smoke.
for path in /healthz /readyz; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "https://aboutme.vn$path")
  [[ $code == 200 ]] || { say "smoke $path returned $code"; exit 1; }
done
curl -fsSI https://aboutme.vn/ | grep -qi '^strict-transport-security:' || { say "HSTS header missing"; exit 1; }
ip=$("${AWS[@]}" ec2 describe-addresses --filters Name=tag:Project,Values=aboutme --query 'Addresses[0].PublicIp' --output text)
if curl -sk -m "${DEPLOY_SMOKE_TIMEOUT:-5}" -o /dev/null "https://$ip/"; then
  say "the origin answered a direct request"; exit 1
fi
say "deployed $tag"
```

- [ ] **Step 4: Run the test and lint**

```sh
bash deploy/aws/scripts/deploy_test.sh
shellcheck deploy/aws/scripts/*.sh deploy/caddy/production/*.sh
```

Expected: `deploy-script-test: ok` and no shellcheck findings. Ask the
integration owner to add `deploy-script-test` to the `Makefile`.

## Task 14

### Phase review, candidate gates and baseline marker

**Files:**

- Create: `apps/server/migrations/.uat-baseline`
- Modify: `docs/plans/phase-10/exit-criteria.md`

**Owner:** integration owner. A fresh reviewer who authored none of Tasks 2–13
reads the integrated diff.

- [ ] **Step 1: Close the single-replica plan**

Run the verification section of
[single-replica direction](../replica/single-replica-direction.md) and record
its results there.

- [ ] **Step 2: Fresh review**

The reviewer confirms by name: Go trusts only loopback; Caddy trusts
`CF-Connecting-IP` only from Cloudflare ranges; the origin requires the
origin-pull certificate; no secret value is in Git, state, images or logs; each
IAM policy grants only what its task uses; migrations complete before the app
starts and a failed migration restores the previous app; provisioning keeps the
owner check; `db-set-login` never sends plaintext. Findings go back to their
author, and the same reviewer confirms each fix.

- [ ] **Step 3: Candidate gates**

Run one at a time at the unchanged candidate commit:

```sh
make ci
make scan
make caddy-prod-test deploy-script-test
```

When full `make ci` does not fit one foreground run, run its targets in
foreground chunks, and run `make dev-https-down` before the web tests.

- [ ] **Step 4: Baseline marker and exit criteria**

```sh
printf '00023\n' > apps/server/migrations/.uat-baseline
```

In `exit-criteria.md`, move the rate candidate selection item to a new "After
launch" section. Its fix is a later forward migration with
`CREATE OR REPLACE FUNCTION`, so it no longer has to land before the baseline.
Commit both, push `main`, and create the first tag:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

Expected: the `release-images` workflow publishes three images. The owner makes
the three packages public.

## Task 15

### First production deploy

**Owner:** integration owner with the owner present.

- [ ] **Step 1: Point the task definitions at the first images**

Set `image_server`, `image_web` and `image_caddy` in `prod.tfvars` to the
`v0.1.0` digests, then `tofu plan` and `tofu apply`. Later releases change
images only through `deploy.sh`.

- [ ] **Step 2: Deploy**

```sh
bash deploy/aws/scripts/deploy.sh v0.1.0 --first-deploy
```

Expected: `snapshot ...`, `site down`, `site up`, `deployed v0.1.0`.

- [ ] **Step 3: Confirm by hand**

- `https://aboutme.vn/` loads the landing page over Cloudflare.
- `https://www.aboutme.vn/` redirects to the apex.
- A request to the Elastic IP times out.
- The server log in CloudWatch shows the client IP of a test request, not a
  Cloudflare address.

## Task 16

### Production checks, runbook, traceability and closure

**Files:**

- Create: `docs/runbooks/production.md`
- Modify: `docs/runbooks/README.md`
- Modify: `docs/plans/traceability/ac-inf.md`, `ac-ops.md`
- Modify: `docs/architecture.md`
- Modify: `docs/plans/phase-10/exit-criteria.md`

- [ ] **Step 1: Owner product checks**

The owner works through registration (with a verified SES address while in
sandbox), sign-in, editing, publish and unpublish, PDF and image exports,
realtime refresh, MCP connection, account export and account deletion. Record
each as PASS or FAIL in `.dev/phase-10/production-checks.txt`, with no personal
data.

- [ ] **Step 2: Alarm and job checks**

Trigger each alarm once (for example with
`aws cloudwatch set-alarm-state --state-value ALARM`) and confirm the email.
Confirm one run of each scheduled job exits 0 in CloudWatch Logs.

- [ ] **Step 3: Write the runbook**

`docs/runbooks/production.md` covers, briefly and with exact commands: deploy,
rollback and its limit, first-deploy steps, restore from a snapshot, secret
rotation for each SSM parameter, Cloudflare range refresh, RDS CA bundle
refresh, and where alarms and logs are. Link it from the runbooks index.

Confirm the privacy notice states that Cloudflare decrypts traffic to deliver
the site, as the single-host design requires. If the notice lacks it, add the
sentence in the same change.

- [ ] **Step 4: Traceability**

Remap every `AC-INF` and `AC-OPS` row to its production evidence or mark it
deferred with the reason. No row is proven by configuration alone.

- [ ] **Step 5: Architecture and closure**

Update `docs/architecture.md` to describe the running production system and
remove the "not built yet" gap. Tick the exit criteria. Before the public
announcement, the owner completes the restore drill, SES production access and
the reviews listed in the exit criteria. Then delete `docs/plans/phase-10/`
under the lean-docs rule.
