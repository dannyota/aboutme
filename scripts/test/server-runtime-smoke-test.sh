#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd -P)
smoke="$root/scripts/server-runtime-smoke.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/artifacts/server" "$tmp/artifacts/web/.output/server"
printf 'server\n' > "$tmp/artifacts/server/server"
printf 'web\n' > "$tmp/artifacts/web/.output/server/index.mjs"

# A manifest is deterministic, uses relative paths, and includes both trees.
(cd "$tmp/artifacts" && sha256sum server/server web/.output/server/index.mjs) > "$tmp/expected"
bash "$smoke" manifest "$tmp/artifacts" > "$tmp/manifest"
server_pin=$(awk '/^FROM / && / AS runtime$/ {print $2}' "$root/deploy/server.Dockerfile")
web_pin=$(awk '/^FROM / && / AS runtime$/ {print $2}' "$root/deploy/web.Dockerfile")
cmp "$tmp/expected" "$tmp/manifest"
bash "$smoke" manifest "$tmp/artifacts" > "$tmp/second"
cmp "$tmp/manifest" "$tmp/second"
printf 'escaped\n' > "$tmp/artifacts/server/back\\slash"
(cd "$tmp/artifacts" && sha256sum 'server/back\slash' server/server web/.output/server/index.mjs) > "$tmp/expected"
bash "$smoke" manifest "$tmp/artifacts" > "$tmp/escaped"
cmp "$tmp/expected" "$tmp/escaped"
rm "$tmp/artifacts/server/back\\slash"

ln -s server "$tmp/artifacts/server/link"
if bash "$smoke" manifest "$tmp/artifacts" > /dev/null 2>&1; then
  printf '%s\n' 'manifest accepted a symlink' >&2; exit 1
fi
rm "$tmp/artifacts/server/link"
mkfifo "$tmp/artifacts/server/pipe"
if bash "$smoke" manifest "$tmp/artifacts" > /dev/null 2>&1; then
  printf '%s\n' 'manifest accepted a special file' >&2; exit 1
fi
rm "$tmp/artifacts/server/pipe"

pin='example@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
if (cd "$tmp" && bash "$smoke" run --artifacts "$tmp/artifacts" --manifest "$tmp/manifest" --server-base example:latest --web-base "$pin") > /dev/null 2> "$tmp/error"; then
  printf '%s\n' 'run accepted an unpinned base' >&2; exit 1
fi
grep -qx 'base references must be digest-pinned' "$tmp/error"
printf 'changed\n' > "$tmp/artifacts/server/server"
if (cd "$tmp" && bash "$smoke" run --artifacts "$tmp/artifacts" --manifest "$tmp/manifest" --server-base "$pin" --web-base "$pin") > /dev/null 2> "$tmp/error"; then
  printf '%s\n' 'run accepted changed artifacts' >&2; exit 1
fi
grep -qx 'artifact manifest mismatch' "$tmp/error"
printf 'server\n' > "$tmp/artifacts/server/server"
for binary in render-browser-supervisor migrate db-setup; do
  printf 'binary\n' > "$tmp/artifacts/server/$binary"
done
bash "$smoke" manifest "$tmp/artifacts" > "$tmp/manifest"

# Runtime fixtures replace Docker and the clock, never the smoke decisions.
real_python=$(command -v python3)
mkdir "$tmp/bin"
cat > "$tmp/bin/python3" <<PY
#!$real_python
import builtins, os, subprocess, sys, time, types
args = sys.argv[1:]
while args and args[0] in ('-B', '-I'):
    args.pop(0)
sys.argv = args
source = sys.stdin.read()
scope = {'__name__': '__main__'}
if len(args) > 1 and args[1] == 'run':
    clock = [0.0]
    smoke_time = types.SimpleNamespace(**vars(time))
    smoke_time.monotonic = lambda: clock[0]
    # Accelerated waits must not change the one-second sample schedule.
    fast_wait = os.environ.get('SMOKE_CASE') in ('setup-timeout', 'migrate-timeout', 'db-not-ready', 'web-not-ready', 'readiness-timeout')
    def sleep(seconds):
        clock[0] += seconds * (10 if fast_wait else 1)
    smoke_time.sleep = sleep
    # Subprocess polling must keep its real clock and sleep.
    def fixture_import(name, *args, **kwargs):
        return smoke_time if name == 'time' else builtins.__import__(name, *args, **kwargs)
    scope['__builtins__'] = dict(vars(builtins), __import__=fixture_import)
    original = subprocess.run
    delayed = set()
    def run(command, **kwargs):
        docker_args = command[1:]
        if docker_args[:1] == ['--host']:
            docker_args = docker_args[2:]
        if command[0] == 'docker':
            clock[0] += 0.001
            if os.environ.get('SMOKE_CASE') == 'pull-timeout' and docker_args[0] == 'pull':
                raise subprocess.TimeoutExpired(command, kwargs.get('timeout'))
        if os.environ.get('SMOKE_CASE') == 'success':
            delay = None
            if command[0] == 'git':
                delay = 'preparation'
            elif command[0] == 'docker' and docker_args[0] == 'exec':
                if docker_args[2] == 'timeout':
                    delay = 'measurement'
                elif docker_args[1].endswith('-server') and 'kill -TERM' in docker_args[-1]:
                    delay = 'shutdown'
            elif command[0] == 'docker' and docker_args[0] == 'rm' and docker_args[-1].endswith('-server'):
                delay = 'cleanup'
            if delay is not None and delay not in delayed:
                delayed.add(delay)
                command = [sys.executable, '-B', '-I', '-c',
                           'import os,sys,time; time.sleep(0.1); os.execvp(sys.argv[1], sys.argv[1:])', *command]
        return original(command, **kwargs)
    subprocess.run = run
exec(compile(source, '<smoke>', 'exec'), scope)
PY
cat > "$tmp/bin/docker" <<PY
#!$real_python
import json, os, pathlib, signal, stat, sys

state_path = pathlib.Path(os.environ['SMOKE_STATE'])
state = json.loads(state_path.read_text())
args = sys.argv[1:]
case = os.environ['SMOKE_CASE']
endpoint = None
if args[:1] == ['--host']:
    endpoint, args = args[1], args[2:]
state['calls'].append(args)

def finish(value='', code=0, stage=None):
    signum = None
    if stage is not None and case in ('sigint-'+stage, 'sigterm-'+stage) and 'signal' not in state:
        signum = signal.SIGINT if case.startswith('sigint-') else signal.SIGTERM
        state['signal'] = {'number': signum, 'stage': stage}
    state_path.write_text(json.dumps(state))
    if signum is not None:
        os.kill(os.getppid(), signum)
    if value:
        print(value)
    if code:
        print('PRIVATE_SENTINEL credential body row', file=sys.stderr)
    sys.exit(code)

def role(name):
    return state['containers'][name]['role']

if args[:2] == ['context', 'inspect']:
    assert endpoint is None
    assert args[2:4] == ['--format', '{{json .Endpoints.docker.Host}}']
    selected = args[4:]
    assert selected == (['production'] if case == 'remote-context' else ['local'] if case == 'local-context' else [])
    host = 'ssh://production.invalid' if case == 'remote-context' else (
           'tcp://production.invalid:2376' if case == 'remote-active-context' else 'unix:///var/run/docker.sock')
    finish(json.dumps(host))
state.setdefault('endpoints', []).append({'host': endpoint, 'context': os.environ.get('DOCKER_CONTEXT'),
                                         'inherited_host': os.environ.get('DOCKER_HOST')})
if args[0] == 'pull':
    finish(code=1 if case == 'pull-fail' else 0)
if args[:2] == ['network', 'create']:
    state['network'] = args[-1]
    finish(args[-1], 1 if case == 'network-fail' else 0)
if args[0] in ('network', 'volume') and args[1] == 'inspect':
    finish(json.dumps(state[args[0]].rsplit('-', 1)[0]))
if args[:2] == ['volume', 'create']:
    state['volume'] = args[-1]
    finish(args[-1], 1 if case == 'volume-fail' else 0)
if args[0] == 'create':
    name = args[args.index('--name') + 1]
    service = name.rsplit('-', 1)[1]
    state['containers'][name] = {'role': service, 'started': False, 'go': False, 'exit': None}
    state['work'] = str(pathlib.Path(args[args.index('--env-file') + 1]).parent)
    for mount in args:
        if mount.startswith('type=bind,') and ('target=/artifacts/server,' in mount or 'target=/app/.output,' in mount):
            source = pathlib.Path(mount.split('source=', 1)[1].split(',target=', 1)[0])
            directories = [source, *(path for path in source.rglob('*') if path.is_dir())]
            state.setdefault('artifact_modes', {}).update({str(path): stat.S_IMODE(path.stat().st_mode) for path in directories})
    if service == 'setup' and case == 'db-socket-first':
        state['setup_tcp_ready'] = state.get('tcp_ready', False)
    finish(name)
if args[0] == 'start':
    state['containers'][args[-1]]['started'] = True
    finish(args[-1], 1 if case == 'start-fail' else 0)
if args[0] == 'inspect':
    name = args[-1]
    if name not in state['containers']:
        finish(code=1)
    item = state['containers'][name]
    running = item['started']
    if case == 'container-exit' and item['role'] == 'server' and item['go']:
        running = False
    owner = 'foreign' if case == 'foreign-owner' and item['role'] == 'server' else name.rsplit('-', 1)[0]
    finish(json.dumps({'owner': owner,
                       'state': {'Running': running, 'OOMKilled': case == 'oom-killed', 'ExitCode': 0,
                                 'Restarting': False, 'Dead': False}}))
if args[0] == 'top':
    processes = 'PID PPID COMMAND\n10 1 docker-init\n11 10 sh\n12 11 sleep'
    if case == 'orphan-process' and role(args[1]) == 'server':
        processes += '\n13 10 chrome'
    finish(processes)
if args[0] == 'exec':
    name = args[1]
    service = role(name)
    item = state['containers'][name]
    command = args[2:]
    script = command[-1]
    if command[:2] == ['sh', '-c'] and 'memory.peak' in script:
        if case == 'missing-cgroup':
            finish(code=1)
        limit = 1073741824 if service == 'postgres' else 536870912 if service == 'server' else 268435456
        if case == 'wrong-memory' and service == 'server':
            limit = 1024
        swap = '1' if case == 'wrong-swap' and service == 'server' else '0'
        cpu = '50000 100000' if case == 'wrong-cpu' and service == 'server' else 'max 100000'
        oom = int(service == 'server' and ((case == 'oom-increase' and state['ready'] > 2)
                  or (case == 'oom-at-exit' and item['exit'] is not None)))
        killed = int(case == 'oom-kill-increase' and service == 'server' and state['ready'] > 2)
        finish('\n'.join([str(limit), swap, cpu, '12345', 'oom '+str(oom), 'oom_kill '+str(killed)]))
    if command[:2] == ['sh', '-c'] and '/control/go' in script:
        item['go'] = True
        if service in ('setup', 'migrate'):
            item['exit'] = 7 if case == service+'-fail' else None if case == service+'-timeout' else 0
            if service == 'setup' and case == 'db-socket-first' and not state['setup_tcp_ready']:
                item['exit'] = 7
        if case == 'server-exit' and service == 'server':
            item['exit'] = 9
        if case == 'server-exit-zero' and service == 'server':
            item['exit'] = 0
        finish(stage='preparation' if service == 'setup' else None)
    if command[:2] == ['sh', '-c'] and 'kill -TERM' in script:
        if case != 'shutdown-timeout' or service != 'server':
            item['exit'] = 5 if case == 'shutdown-fail' else 0
        finish()
    if command[:2] == ['sh', '-c'] and '/control/exit' in script:
        finish('running' if item['exit'] is None else str(item['exit']))
    if command[0] == 'pg_isready':
        if case == 'db-socket-first':
            state['db_probes'] = state.get('db_probes', 0) + 1
            state['tcp_ready'] = state['db_probes'] > 2
            tcp = '-h' in command and command[command.index('-h') + 1] == '127.0.0.1'
            finish(code=1 if tcp and not state['tcp_ready'] else 0)
        finish(code=1 if case == 'db-not-ready' or (case == 'db-sample-fail' and state['ready'] > 2) else 0)
    if command[0] == 'node':
        finish(code=1 if case == 'web-not-ready' or (case == 'web-sample-fail' and state['ready'] > 2) else 0)
    if command[0] == 'timeout':
        assert command[1:3] == ['1', 'bash']
        state['ready'] += 1
        finish(code=1 if case == 'readiness-timeout' or (case == 'sample-fail' and state['ready'] > 2) else 0,
               stage='measurement' if state['ready'] == 3 else None)
    finish(code=1)
if args[0] == 'rm':
    if case == 'cleanup-fail' and role(args[-1]) == 'server':
        finish(code=1)
    state['containers'].pop(args[-1], None)
    finish()
if args[:2] == ['network', 'rm']:
    state.pop('network', None)
    finish()
if args[:2] == ['volume', 'rm']:
    state.pop('volume', None)
    finish()
finish(code=1)
PY
chmod +x "$tmp/bin/python3" "$tmp/bin/docker"
cat > "$tmp/bin/uname" <<'SH'
#!/usr/bin/env bash
printf '%s\n' Linux
SH
chmod +x "$tmp/bin/uname"

for scenario in success db-socket-first restrictive-umask \
  sigint-preparation sigterm-preparation sigint-measurement sigterm-measurement \
  remote-host remote-ssh-host remote-context remote-active-context local-host local-context \
  base-pin-mismatch pull-fail pull-timeout network-fail volume-fail start-fail setup-fail migrate-fail \
  setup-timeout migrate-timeout missing-cgroup wrong-memory wrong-swap wrong-cpu \
  oom-killed oom-increase oom-kill-increase oom-at-exit container-exit server-exit server-exit-zero \
  db-not-ready web-not-ready readiness-timeout sample-fail db-sample-fail \
  web-sample-fail shutdown-timeout shutdown-fail orphan-process cleanup-fail foreign-owner; do
  case_dir="$tmp/$scenario"
  mkdir "$case_dir"
  printf '{"containers":{},"calls":[],"ready":0}\n' > "$case_dir/state.json"
  selected_server=$server_pin
  [[ $scenario != base-pin-mismatch ]] || selected_server=$pin
  status=0
  (cd "$case_dir"
    unset DOCKER_HOST DOCKER_CONTEXT
    case $scenario in
      restrictive-umask) umask 077 ;;
      remote-host) export DOCKER_HOST=tcp://production.invalid:2376 ;;
      remote-ssh-host) export DOCKER_HOST=ssh://production.invalid ;;
      remote-context) export DOCKER_CONTEXT=production DOCKER_HOST=unix:///tmp/ignored.sock ;;
      local-host) export DOCKER_HOST=unix:///tmp/smoke.sock ;;
      local-context) export DOCKER_CONTEXT=local DOCKER_HOST=ssh://production.invalid ;;
    esac
    PATH="$tmp/bin:$PATH" SMOKE_CASE="$scenario" SMOKE_STATE="$case_dir/state.json" \
    bash "$smoke" run --artifacts "$tmp/artifacts" --manifest "$tmp/manifest" \
    --server-base "$selected_server" --web-base "$web_pin") > "$case_dir/console" 2>&1 || status=$?
  "$real_python" - "$scenario" "$status" "$case_dir" <<'PY'
import hashlib, json, pathlib, signal, sys
scenario, status, directory = sys.argv[1], int(sys.argv[2]), pathlib.Path(sys.argv[3])
evidence_path = directory / 'server-runtime-smoke/evidence.json'
assert evidence_path.stat().st_size <= 16384
evidence = json.loads(evidence_path.read_text())
passed = scenario in ('success', 'db-socket-first', 'restrictive-umask', 'local-host', 'local-context')
assert (status == 0) == passed, (scenario, status, evidence)
assert evidence['result'] == ('passed' if passed else 'failed')
state = json.loads((directory / 'state.json').read_text())
if scenario in ('success', 'shutdown-timeout'):
    assert evidence['samples'] == 30 and state['ready'] == 31, (scenario, evidence)
    assert evidence['preparation_seconds'] < 1, (scenario, evidence)
    assert evidence['cleanup_seconds'] < 1, (scenario, evidence)
    measurement_min, measurement_max = (30, 31) if scenario == 'success' else (39, 41)
    assert measurement_min <= evidence['measurement_seconds'] < measurement_max, (scenario, evidence)
    assert measurement_min <= evidence['elapsed_seconds'] < measurement_max + 1, (scenario, evidence)
reasons = {'pull-fail': 'command_failed', 'pull-timeout': 'command_timeout',
           'base-pin-mismatch': 'base_pin_mismatch',
           'network-fail': 'command_failed', 'volume-fail': 'command_failed',
           'start-fail': 'command_failed', 'setup-fail': 'one_shot_failed', 'migrate-fail': 'one_shot_failed',
           'setup-timeout': 'deadline_exceeded', 'migrate-timeout': 'deadline_exceeded',
           'missing-cgroup': 'command_failed', 'wrong-memory': 'limits_mismatch',
           'wrong-swap': 'limits_mismatch', 'wrong-cpu': 'limits_mismatch', 'oom-killed': 'container_failed',
           'oom-increase': 'oom_increase', 'oom-kill-increase': 'oom_increase', 'oom-at-exit': 'oom_increase',
           'container-exit': 'container_failed', 'server-exit': 'early_exit', 'server-exit-zero': 'early_exit',
           'db-not-ready': 'deadline_exceeded', 'web-not-ready': 'deadline_exceeded',
           'readiness-timeout': 'readiness_timeout', 'sample-fail': 'sample_failed',
           'db-sample-fail': 'sample_failed', 'web-sample-fail': 'sample_failed',
           'shutdown-timeout': 'shutdown_timeout', 'shutdown-fail': 'shutdown_failed',
           'orphan-process': 'shutdown_incomplete',
           'cleanup-fail': 'cleanup_failed', 'foreign-owner': 'cleanup_failed', 'success': 'healthy'}
reasons.update({name: 'healthy' for name in ('db-socket-first', 'restrictive-umask', 'local-host', 'local-context')})
reasons.update({name: 'local_engine_required' for name in ('remote-host', 'remote-ssh-host', 'remote-context', 'remote-active-context')})
reasons.update({name: 'cancelled' for name in ('sigint-preparation', 'sigterm-preparation', 'sigint-measurement', 'sigterm-measurement')})
assert evidence['reason'] == reasons[scenario], (scenario, evidence)
assert set(evidence) == {'commit', 'artifact_sha256', 'phase', 'result', 'reason', 'services',
                         'samples', 'cleanup', 'elapsed_seconds', 'preparation_seconds',
                         'measurement_seconds', 'cleanup_seconds'}
assert 'PRIVATE_SENTINEL' not in evidence_path.read_text() + (directory / 'console').read_text()
assert evidence['artifact_sha256'] == hashlib.sha256((directory.parent / 'manifest').read_bytes()).hexdigest()
for item in evidence['services'].values():
    assert set(item) == {'configured', 'observed', 'peak_bytes', 'oom_before', 'oom_after',
                         'exit_code', 'container_exit_code', 'oom_killed'}
assert evidence['cleanup'] == ('failed' if scenario in ('cleanup-fail', 'foreign-owner') else 'passed')
if scenario not in ('cleanup-fail', 'foreign-owner'):
    assert not state['containers'] and 'network' not in state and 'volume' not in state
    if 'work' in state:
        assert not pathlib.Path(state['work']).exists()
if scenario.startswith(('sigint-', 'sigterm-')):
    stage = scenario.split('-', 1)[1]
    assert state['signal'] == {'number': signal.SIGINT if scenario.startswith('sigint-') else signal.SIGTERM, 'stage': stage}
    assert evidence['phase'] == stage and evidence['cleanup'] == 'passed'
    assert evidence['samples'] == (0 if stage == 'preparation' else 1)
    assert set(evidence['services']) == ({'postgres', 'setup'} if stage == 'preparation' else {'postgres', 'setup', 'migrate', 'web', 'server'})
if scenario == 'db-socket-first':
    assert state['setup_tcp_ready']
    probes = [call for call in state['calls'] if call[0] == 'exec' and call[2] == 'pg_isready']
    assert all(call[call.index('-h') + 1] == '127.0.0.1' for call in probes)
if scenario == 'restrictive-umask':
    assert len(state['artifact_modes']) >= 3
    assert set(state['artifact_modes'].values()) == {0o755}, state['artifact_modes']
context_calls = [call for call in state['calls'] if call[:2] == ['context', 'inspect']]
assert len(context_calls) <= 1
if scenario.startswith('remote-'):
    assert state['calls'] == context_calls and not state.get('endpoints') and 'work' not in state
    assert len(context_calls) == (1 if scenario in ('remote-context', 'remote-active-context') else 0)
for endpoint in state.get('endpoints', []):
    assert endpoint == {'host': 'unix:///tmp/smoke.sock' if scenario == 'local-host' else 'unix:///var/run/docker.sock',
                        'context': None, 'inherited_host': None}, (scenario, endpoint)
for call in state['calls']:
    assert 'prune' not in call and '--privileged' not in call and '--cpus' not in call
    if call[0] == 'rm':
        assert call[-1].startswith('aboutme-runtime-')
    if call[0] == 'create':
        assert '--memory' in call and '--memory-swap' in call and '--log-driver' in call
        assert call[call.index('--memory')+1] == call[call.index('--memory-swap')+1]
        assert not any('docker.sock' in arg for arg in call)
if scenario == 'foreign-owner':
    assert not any(call[0] == 'rm' and call[-1].endswith('-server') for call in state['calls'])
if scenario in ('wrong-memory', 'wrong-swap', 'wrong-cpu'):
    assert not any(call[0] == 'exec' and call[1].endswith('-server') and '/control/go' in call[-1]
                   for call in state['calls'])
if scenario == 'success':
    assert evidence['services']['server']['observed']['memory_max'] == 536870912
    assert evidence['services']['server']['observed']['swap_max'] == 0
    assert evidence['services']['server']['observed']['cpu_max'] == 'max'
    assert all(item['peak_bytes'] == 12345 for item in evidence['services'].values())
    assert all(item['exit_code'] == 0 for item in evidence['services'].values())
    creates = [call for call in state['calls'] if call[0] == 'create']
    assert [call[call.index('--name')+1].rsplit('-', 1)[1] for call in creates] == ['postgres','setup','migrate','web','server']
    assert '--internal' in next(call for call in state['calls'] if call[:2] == ['network','create'])
    server = creates[-1]
    assert server[server.index('--memory')+1] == '512m'
    assert server[server.index('--user')+1] == '1000:1000' and '--cap-add' in server
    assert server[server.index('--network')+1].endswith('-web')
    assert server[server.index('--network')+1].startswith('container:')
    assert '/artifacts/server/server' in server
    assert any('readonly' in arg and '/artifacts/server' in arg for arg in server)
    assert evidence['artifact_sha256'] and len(evidence['commit']) == 40
    for role, limit in {'postgres': 1073741824, 'setup': 268435456, 'migrate': 268435456,
                        'web': 268435456, 'server': 536870912}.items():
        assert evidence['services'][role]['configured']['memory_max'] == limit
    server_create = state['calls'].index(creates[-1])
    for role in ('setup', 'migrate'):
        assert any(index < server_create and call[0] == 'rm' and call[-1].endswith('-'+role)
                   for index, call in enumerate(state['calls']))
PY
done

mkdir "$tmp/nonlinux"
printf '#!/usr/bin/env bash\nprintf "Darwin\\n"\n' > "$tmp/bin/uname"
if (cd "$tmp/nonlinux" && PATH="$tmp/bin:$PATH" bash "$smoke" run \
  --artifacts "$tmp/artifacts" --manifest "$tmp/manifest" --server-base "$server_pin" --web-base "$web_pin") \
  > "$tmp/nonlinux/console" 2>&1; then
  printf '%s\n' 'runtime accepted a non-Linux host' >&2; exit 1
fi
grep -qx 'server runtime smoke requires Linux' "$tmp/nonlinux/console"
printf '#!/usr/bin/env bash\nprintf "Linux\\n"\n' > "$tmp/bin/uname"

for unsafe in directory-link evidence-link evidence-hardlink directory-mode; do
  unsafe_dir="$tmp/$unsafe"
  mkdir "$unsafe_dir" "$unsafe_dir/target"
  printf 'preserve\n' > "$unsafe_dir/target/file"
  if [[ $unsafe == directory-link ]]; then
    ln -s "$unsafe_dir/target" "$unsafe_dir/server-runtime-smoke"
  else
    mkdir -m 700 "$unsafe_dir/server-runtime-smoke"
    case $unsafe in
      evidence-link) ln -s "$unsafe_dir/target/file" "$unsafe_dir/server-runtime-smoke/evidence.json" ;;
      evidence-hardlink) ln "$unsafe_dir/target/file" "$unsafe_dir/server-runtime-smoke/evidence.json" ;;
      directory-mode) chmod 777 "$unsafe_dir/server-runtime-smoke" ;;
    esac
  fi
  printf '{"containers":{},"calls":[],"ready":0}\n' > "$unsafe_dir/state.json"
  if (cd "$unsafe_dir" && PATH="$tmp/bin:$PATH" SMOKE_CASE=success SMOKE_STATE="$unsafe_dir/state.json" bash "$smoke" run \
    --artifacts "$tmp/artifacts" --manifest "$tmp/manifest" --server-base "$server_pin" --web-base "$web_pin") \
    > "$unsafe_dir/console" 2>&1; then
    printf '%s\n' 'runtime accepted unsafe evidence' >&2; exit 1
  fi
  grep -qx preserve "$unsafe_dir/target/file"
  "$real_python" - "$unsafe_dir/state.json" <<'PY'
import json, pathlib, sys
assert not json.loads(pathlib.Path(sys.argv[1]).read_text())['calls']
PY
done
printf '%s\n' 'server runtime smoke fixture passed'
