#!/usr/bin/env bash
set -euo pipefail

manifest() {
  python3 -B -I - "$1" <<'PY'
import hashlib
import os
import stat
import sys


def fail():
    print('artifact manifest refused', file=sys.stderr)
    sys.exit(1)


def open_dir(name, parent=None):
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    return os.open(name, flags, dir_fd=parent)


def walk(fd, prefix, records):
    for name in os.listdir(fd):
        info = os.stat(name, dir_fd=fd, follow_symlinks=False)
        path = prefix + b'/' + os.fsencode(name)
        if stat.S_ISDIR(info.st_mode):
            child = open_dir(name, fd)
            try:
                actual = os.fstat(child)
                if (info.st_dev, info.st_ino) != (actual.st_dev, actual.st_ino):
                    fail()
                walk(child, path, records)
            finally:
                os.close(child)
        elif stat.S_ISREG(info.st_mode):
            file_fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
            try:
                actual = os.fstat(file_fd)
                if not stat.S_ISREG(actual.st_mode) or (info.st_dev, info.st_ino) != (actual.st_dev, actual.st_ino):
                    fail()
                digest = hashlib.sha256()
                while True:
                    block = os.read(file_fd, 1024 * 1024)
                    if not block:
                        break
                    digest.update(block)
                records.append((path, digest.hexdigest().encode('ascii')))
            finally:
                os.close(file_fd)
        else:
            fail()


try:
    root = open_dir(sys.argv[1])
    records = []
    try:
        server = open_dir('server', root)
        try:
            walk(server, b'server', records)
        finally:
            os.close(server)
        web = open_dir('web', root)
        try:
            output = open_dir('.output', web)
            try:
                walk(output, b'web/.output', records)
            finally:
                os.close(output)
        finally:
            os.close(web)
    finally:
        os.close(root)
    for path, digest in sorted(records):
        escaped = b'\\' in path or b'\n' in path
        if escaped:
            path = path.replace(b'\\', b'\\\\').replace(b'\n', b'\\n')
        sys.stdout.buffer.write((b'\\' if escaped else b'') + digest + b'  ' + path + b'\n')
except (OSError, ValueError):
    fail()
PY
}

case ${1-} in
  manifest)
    [[ $# == 2 ]] || { printf '%s\n' 'usage: server-runtime-smoke.sh manifest DIR' >&2; exit 2; }
    manifest "$2"
    ;;
  run)
    shift
    artifacts= manifest_file= server_base= web_base=
    while [[ $# -gt 0 ]]; do
      [[ $# -ge 2 ]] || { printf '%s\n' 'invalid run arguments' >&2; exit 2; }
      case $1 in
        --artifacts) [[ -z $artifacts ]] || exit 2; artifacts=$2 ;;
        --manifest) [[ -z $manifest_file ]] || exit 2; manifest_file=$2 ;;
        --server-base) [[ -z $server_base ]] || exit 2; server_base=$2 ;;
        --web-base) [[ -z $web_base ]] || exit 2; web_base=$2 ;;
        *) printf '%s\n' 'invalid run arguments' >&2; exit 2 ;;
      esac
      shift 2
    done
    [[ -n $artifacts && -n $manifest_file && -n $server_base && -n $web_base ]] || {
      printf '%s\n' 'invalid run arguments' >&2; exit 2;
    }
    smoke_path=$(cd "$(dirname "$0")" && pwd -P)/$(basename "$0")
    exec python3 -B -I - run "$smoke_path" "$artifacts" "$manifest_file" "$server_base" "$web_base" <<'PY'
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time

_, _, script, artifacts, manifest_file, server_base, web_base = sys.argv
repo = Path(script).resolve().parent.parent
started = time.monotonic()
deadline = started + 360
measurement_started = None
work = None
containers = {}
terminating = set()
network = volume = None
evidence_dir = None
evidence = dict(commit=None, artifact_sha256=None, phase='preparation', result='failed',
                reason='preparation_failed', services={}, samples=0, cleanup='pending',
                elapsed_seconds=0, preparation_seconds=0, measurement_seconds=0, cleanup_seconds=0)


class Refused(Exception):
    pass


def require(condition, reason):
    if not condition:
        raise Refused(reason)


def command(args, timeout=10, check=True):
    remaining = min(timeout, deadline - time.monotonic())
    require(remaining > 0, 'deadline_exceeded')
    with tempfile.TemporaryFile() as output:
        try:
            result = subprocess.run(args, stdout=output, stderr=subprocess.DEVNULL, timeout=remaining)
        except subprocess.TimeoutExpired:
            if not check:
                return 124, ''
            raise Refused('command_timeout') from None
        output.seek(0)
        data = output.read(32769)
    require(len(data) <= 32768, 'inspection_failed')
    require(not check or result.returncode == 0, 'command_failed')
    return result.returncode, data.decode('utf-8', errors='strict').strip()


def docker(*args, **kwargs):
    return command(['docker', *args], **kwargs)


def private_directory(path):
    try:
        path.mkdir(mode=0o700)
    except FileExistsError:
        pass
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    info = os.fstat(fd)
    require(info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o700, 'evidence_refused')
    return fd


def evidence_file_safe():
    require(evidence_dir is not None, 'evidence_refused')
    try:
        previous = os.stat('evidence.json', dir_fd=evidence_dir, follow_symlinks=False)
        require(stat.S_ISREG(previous.st_mode) and previous.st_nlink == 1 and
                previous.st_uid == os.getuid() and stat.S_IMODE(previous.st_mode) == 0o600,
                'evidence_refused')
    except FileNotFoundError:
        pass


def emit():
    evidence_file_safe()
    data = (json.dumps(evidence, sort_keys=True, separators=(',', ':')) + '\n').encode()
    require(len(data) <= 16384, 'evidence_refused')
    name = secrets.token_hex(16)
    fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=evidence_dir)
    with os.fdopen(fd, 'wb') as output:
        output.write(data)
    os.replace(name, 'evidence.json', src_dir_fd=evidence_dir, dst_dir_fd=evidence_dir)


def env_file(name, values):
    path = work / (name + '.env')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as output:
        output.write(''.join(key + '=' + value + '\n' for key, value in values.items()))
    return str(path)


def snapshot(source, target, parent=None):
    fd = os.open(source, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
    try:
        target.mkdir(mode=0o755)
        for name in os.listdir(fd):
            info = os.stat(name, dir_fd=fd, follow_symlinks=False)
            if stat.S_ISDIR(info.st_mode):
                snapshot(name, target / name, fd)
            else:
                require(stat.S_ISREG(info.st_mode), 'artifact_manifest_refused')
                child = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
                with os.fdopen(child, 'rb') as reader:
                    actual = os.fstat(reader.fileno())
                    require(stat.S_ISREG(actual.st_mode) and
                            (info.st_dev, info.st_ino) == (actual.st_dev, actual.st_ino),
                            'artifact_manifest_refused')
                    with (target / name).open('xb') as output:
                        shutil.copyfileobj(reader, output)
                (target / name).chmod(0o644)
    finally:
        os.close(fd)


# Keep the cgroup alive after a child exits so final counters remain readable.
wrapper = '''set -eu
while [ ! -f /control/go ]; do sleep 0.05; done
set +e
"$@" &
child=$!
printf '%s\\n' "$child" > /control/pid
wait "$child"
code=$?
printf '%s\\n' "$code" > /control/exit.tmp
mv /control/exit.tmp /control/exit
while :; do sleep 1; done
'''
metrics = '''set -eu
test -f /sys/fs/cgroup/cgroup.controllers
cat /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory.swap.max /sys/fs/cgroup/cpu.max /sys/fs/cgroup/memory.peak
cat /sys/fs/cgroup/memory.events
'''
child_state = 'if [ -f /control/exit ]; then cat /control/exit; else printf "running\\n"; fi'


def owned_state(role):
    template = '{"state":{{json .State}},"owner":{{json (index .Config.Labels "aboutme-runtime")}}}'
    _, raw = docker('inspect', '--format', template, containers[role])
    record = json.loads(raw)
    require(record['owner'] == prefix, 'ownership_failed')
    return record['state']


def inspect(role):
    state = owned_state(role)
    require(type(state['Running']) is bool and type(state['OOMKilled']) is bool and
            type(state['ExitCode']) is int, 'inspection_failed')
    evidence['services'][role]['container_exit_code'] = state['ExitCode']
    evidence['services'][role]['oom_killed'] = state['OOMKilled']
    require(state['Running'] and not state['OOMKilled'] and not state['Restarting'] and
            not state['Dead'], 'container_failed')


def capture(role):
    inspect(role)
    _, raw = docker('exec', containers[role], 'sh', '-c', metrics)
    lines = raw.splitlines()
    require(len(lines) >= 6, 'inspection_failed')
    memory, swap, cpu, peak = lines[:4]
    require(memory.isdecimal() and swap.isdecimal() and peak.isdecimal() and
            re.fullmatch(r'(max|[0-9]+) [1-9][0-9]*', cpu), 'inspection_failed')
    counters = dict(line.split() for line in lines[4:])
    require(all(counters.get(key, '').isdecimal() for key in ('oom', 'oom_kill')), 'inspection_failed')
    service = evidence['services'][role]
    service['observed'] = dict(memory_max=int(memory), swap_max=int(swap), cpu_max=cpu.split()[0])
    service['peak_bytes'] = max(service['peak_bytes'], int(peak))
    current = {key: int(counters[key]) for key in ('oom', 'oom_kill')}
    if service['oom_before'] is None:
        service['oom_before'] = current
    service['oom_after'] = current
    require(int(memory) == service['configured']['memory_max'] and int(swap) == 0 and
            cpu.split()[0] == 'max', 'limits_mismatch')
    require(current == service['oom_before'] and not any(current.values()), 'oom_increase')


def exit_code(role):
    _, raw = docker('exec', containers[role], 'sh', '-c', child_state)
    require(raw == 'running' or re.fullmatch(r'[0-9]{1,3}', raw), 'inspection_failed')
    code = None if raw == 'running' else int(raw)
    require(code is None or code <= 255, 'inspection_failed')
    evidence['services'][role]['exit_code'] = code
    return code


def health():
    for role in containers:
        capture(role)
        code = exit_code(role)
        require(code is None if role not in ('setup', 'migrate') else code == 0, 'early_exit')


def start(role, image, memory, values, executable, extra=(), user='1000:1000'):
    name = prefix + '-' + role
    containers[role] = name
    size = {'256m': 268435456, '512m': 536870912, '1g': 1073741824}[memory]
    evidence['services'][role] = dict(configured=dict(memory_max=size, swap_max=0, cpu_max='max'),
                                    observed=None, peak_bytes=0, oom_before=None, oom_after=None,
                                    exit_code=None, container_exit_code=None, oom_killed=None)
    networking = ('--network', 'container:' + containers['web']) if role == 'server' else (
                  '--network', network, '--network-alias', role)
    docker('create', '--name', name, *networking,
           '--label', 'aboutme-runtime=' + prefix, '--memory', memory, '--memory-swap', memory,
           '--restart', 'no', '--log-driver', 'none', '--init', '--user', user,
           '--tmpfs', '/control:rw,nosuid,nodev,noexec,mode=1777,size=1m',
           '--env-file', env_file(role, values), *extra, '--entrypoint', 'sh', image,
           '-c', wrapper, 'sh', *executable)
    docker('start', name)
    capture(role)
    docker('exec', name, 'sh', '-c', 'touch /control/go')


def pause(seconds=1):
    require(time.monotonic() + seconds < deadline, 'deadline_exceeded')
    time.sleep(seconds)


def db_ready():
    code, _ = docker('exec', containers['postgres'], 'pg_isready', '-U', 'smoke', '-d', 'smoke', '-t', '1',
                     timeout=2, check=False)
    return code == 0


def web_ready():
    probe = ("const http=require('node:http');const r=http.get('http://127.0.0.1:3000/',"
             "s=>{s.resume();process.exit(s.statusCode===200?0:1)});"
             "r.setTimeout(1000,()=>{r.destroy();process.exit(1)});r.on('error',()=>process.exit(1));")
    code, _ = docker('exec', containers['web'], 'node', '-e', probe, timeout=2, check=False)
    return code == 0


def ready():
    # Read only the status line and suppress the response body.
    probe = ('exec 3<>/dev/tcp/127.0.0.1/8080; '
             'printf "GET /readyz HTTP/1.0\\r\\nHost: localhost\\r\\n\\r\\n" >&3; '
             'IFS= read -r line <&3; [[ $line == HTTP/1.[01]\\ 200\\ * ]]')
    code, _ = docker('exec', containers['server'], 'timeout', '1', 'bash', '-c', probe, timeout=2, check=False)
    return code == 0


def wait_ready(probe):
    while True:
        health()
        if probe():
            health()
            return
        pause()


def one_shot(role, values, binary):
    start(role, server_base, '256m', values, ['/artifacts/server/' + binary], server_mount)
    while True:
        capture(role)
        code = exit_code(role)
        if code is not None:
            require(code == 0, 'one_shot_failed')
            drained(role)
            docker('rm', '--force', containers[role])
            del containers[role]
            return
        capture('postgres')
        require(exit_code('postgres') is None, 'early_exit')
        pause()


def drained(role):
    _, raw = docker('top', containers[role], '-eo', 'pid,ppid,comm')
    rows = [line.split() for line in raw.splitlines()[1:]]
    require(2 <= len(rows) <= 3 and all(len(row) == 3 and row[0].isdecimal() and row[1].isdecimal()
                                      for row in rows), 'shutdown_incomplete')
    processes = {int(pid): (int(parent), name) for pid, parent, name in rows}
    roots = [pid for pid, (parent, _) in processes.items() if parent not in processes]
    require(len(roots) == 1 and processes[roots[0]][1] in ('docker-init', 'tini'), 'shutdown_incomplete')
    shells = [pid for pid, (parent, name) in processes.items() if parent == roots[0] and name == 'sh']
    require(len(shells) == 1, 'shutdown_incomplete')
    require(all(pid in (roots[0], shells[0]) or (parent == shells[0] and name == 'sleep')
                for pid, (parent, name) in processes.items()), 'shutdown_incomplete')


def stop(role):
    global deadline
    previous = deadline
    deadline = min(deadline, time.monotonic() + 10)
    terminating.add(role)
    try:
        require(exit_code(role) is None, 'early_exit')
        docker('exec', containers[role], 'sh', '-c', 'kill -TERM "$(cat /control/pid)"')
        while True:
            capture(role)
            code = exit_code(role)
            if code is not None:
                require(code == 0, 'shutdown_failed')
                drained(role)
                return
            pause(0.1)
    except Refused as error:
        if str(error) in ('deadline_exceeded', 'command_timeout'):
            raise Refused('shutdown_timeout') from None
        raise
    finally:
        deadline = previous


def cancelled(signum, frame):
    raise Refused('cancelled')


def expired(signum, frame):
    raise Refused('deadline_exceeded')


signal.signal(signal.SIGTERM, cancelled)
signal.signal(signal.SIGINT, cancelled)
signal.signal(signal.SIGALRM, expired)
signal.setitimer(signal.ITIMER_REAL, 360)


def verify_manifest(path, expected):
    with tempfile.TemporaryFile() as actual:
        result = subprocess.run(['bash', script, 'manifest', str(path)], stdout=actual,
                                stderr=subprocess.DEVNULL, timeout=max(0.01, deadline-time.monotonic()))
        actual.seek(0)
        require(result.returncode == 0, 'artifact_manifest_refused')
        require(actual.read() == expected, 'artifact_manifest_mismatch')


def cleanup_owned():
    clean = True
    for role in reversed(list(containers)):
        try:
            capture(role)
            if exit_code(role) is None and role not in terminating:
                stop(role)
        except Exception:
            evidence['result'] = 'failed'
            if evidence['reason'] == 'healthy':
                evidence['reason'] = 'inspection_failed'
        try:
            owned_state(role)
            docker('rm', '--force', containers[role], timeout=5)
        except Exception:
            clean = False
    for kind, name in (('volume', volume), ('network', network)):
        if name is not None:
            try:
                _, owner = docker(kind, 'inspect', '--format', '{{json (index .Labels "aboutme-runtime")}}', name)
                require(json.loads(owner) == prefix, 'ownership_failed')
                docker(kind, 'rm', name, timeout=5)
            except Exception:
                clean = False
    if work is not None:
        require(time.monotonic() < deadline, 'deadline_exceeded')
        try:
            shutil.rmtree(work)
        except OSError:
            clean = False
    return clean


try:
    evidence_dir = private_directory(Path.cwd() / 'server-runtime-smoke')
    evidence_file_safe()
    _, commit = command(['git', '-C', str(repo), 'rev-parse', 'HEAD'])
    require(re.fullmatch('[0-9a-f]{40}', commit), 'commit_refused')
    evidence['commit'] = commit
    for base in (server_base, web_base):
        require(re.fullmatch(r'[^\s@]+@sha256:[0-9a-f]{64}', base) and not base.startswith('-'),
                'base_references_must_be_digest_pinned')
    fd = os.open(manifest_file, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        require(stat.S_ISREG(os.fstat(source.fileno()).st_mode), 'artifact_manifest_refused')
        expected = source.read()
    evidence['artifact_sha256'] = hashlib.sha256(expected).hexdigest()
    verify_manifest(artifacts, expected)
    _, platform = command(['uname', '-s'])
    require(platform == 'Linux', 'linux_required')
    for component, base in (('server', server_base), ('web', web_base)):
        pins = re.findall(r'^FROM (\S+) AS runtime$', (repo / ('deploy/' + component + '.Dockerfile')).read_text(), re.M)
        require(pins == [base], 'base_pin_mismatch')
    # The checked copy is the only tree mounted into containers.
    work = Path(tempfile.mkdtemp(prefix='aboutme-runtime-'))
    snapshot(Path(artifacts) / 'server', work / 'server')
    (work / 'web').mkdir()
    snapshot(Path(artifacts) / 'web/.output', work / 'web/.output')
    verify_manifest(work, expected)
    for binary in ('server', 'render-browser-supervisor', 'db-setup', 'migrate'):
        require((work / 'server' / binary).is_file(), 'artifact_incomplete')
        (work / 'server' / binary).chmod(0o755)
    require((work / 'web/.output/server/index.mjs').is_file(), 'artifact_incomplete')
    postgres = re.findall(r'^    image: (docker.io/library/postgres:[^\s]+@sha256:[0-9a-f]{64})$',
                          (repo / 'deploy/compose.yml').read_text(), re.M)
    require(len(postgres) == 1, 'postgres_pin_refused')
    for image in dict.fromkeys((postgres[0], server_base, web_base)):
        docker('pull', image, timeout=deadline-time.monotonic())
    prefix = 'aboutme-runtime-' + secrets.token_hex(16)
    network = prefix + '-network'
    docker('network', 'create', '--internal', '--label', 'aboutme-runtime=' + prefix, network)
    volume = prefix + '-postgres'
    docker('volume', 'create', '--label', 'aboutme-runtime=' + prefix, volume)
    password, migrator, app = (secrets.token_hex(32) for _ in range(3))
    common = dict(TZ='UTC', LANG='C.UTF-8')
    server_mount = ('--mount', 'type=bind,source=' + str(work / 'server') + ',target=/artifacts/server,readonly')
    start('postgres', postgres[0], '1g', dict(common, POSTGRES_USER='smoke', POSTGRES_DB='smoke',
          POSTGRES_PASSWORD=password), ['docker-entrypoint.sh', 'postgres'],
          ('--mount', 'type=volume,source=' + volume + ',target=/var/lib/postgresql'), user='0:0')
    wait_ready(db_ready)
    dsn = 'postgres://smoke@postgres:5432/smoke?sslmode=disable'
    one_shot('setup', dict(common, DATABASE_URL=dsn, PGPASSWORD=password,
                          MIGRATOR_PASSWORD=migrator, APP_PASSWORD=app), 'db-setup')
    one_shot('migrate', dict(common, DATABASE_URL=dsn.replace('smoke@', 'aboutme_migrator@'),
                            PGPASSWORD=migrator), 'migrate')
    start('web', web_base, '256m', dict(common, HOST='0.0.0.0', PORT='3000', NODE_ENV='production',
                                      NUXT_PRINT_ORIGIN='http://127.0.0.1:8081'),
          ['node', '/app/.output/server/index.mjs'],
          ('--workdir', '/app', '--mount', 'type=bind,source=' + str(work / 'web/.output') + ',target=/app/.output,readonly'))
    wait_ready(web_ready)
    health()
    require(db_ready() and web_ready(), 'preparation_health_failed')
    (work / 'media').mkdir(mode=0o777)
    (work / 'media').chmod(0o777)
    def key():
        return base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip('=')
    values = dict(common, ENV='prod', PORT='8080', LISTEN_HOST='127.0.0.1',
                  DATABASE_URL=dsn.replace('smoke@', 'aboutme_app@'), PGPASSWORD=app,
                  PUBLIC_ORIGIN='https://smoke.invalid', PUBLIC_RENDER_ORIGIN='http://127.0.0.1:3000',
                  TRUSTED_PROXY_CIDRS='127.0.0.1/32', MEDIA_BACKEND='fs', MEDIA_FS_DIR='/media',
                  PRINT_LISTEN_ADDR='127.0.0.1:8081', CHROMIUM_PATH='/ms-playwright/chromium-1248/chrome-linux64/chrome',
                  APP_BUILD_DIGEST='sha256:' + evidence['artifact_sha256'],
                  PUBLIC_RENDERER_BUILD_DIGEST='sha256:' + evidence['artifact_sha256'],
                  TOTP_ACTIVE_KEY=key(), VIEW_PASS_KEY=key(), PASSWORD_RATE_HMAC_KEY=key(),
                  AUTH_EMAIL_ACTIVE_KEY_ID='smoke', AUTH_EMAIL_ACTIVE_KEY=key(), AUTH_EMAIL_MODE='smtp',
                  SES_FROM_ADDRESS='smoke@smoke.invalid', SES_FROM_NAME='Smoke',
                  SMTP_HOST='mail.invalid', SMTP_PORT='465', SMTP_TLS='implicit',
                  SMTP_USERNAME='smoke', SMTP_PASSWORD=secrets.token_hex(32))
    evidence['preparation_seconds'] = round(time.monotonic()-started, 3)
    require(time.monotonic() < deadline, 'deadline_exceeded')
    measurement_started = time.monotonic()
    deadline = measurement_started + 120
    signal.setitimer(signal.ITIMER_REAL, 120)
    evidence['phase'] = 'measurement'
    start('server', server_base, '512m', values, ['/artifacts/server/server'],
          (*server_mount, '--cap-add', 'SYS_ADMIN', '--shm-size', '128m',
           '--mount', 'type=bind,source=' + str(work / 'media') + ',target=/media'))
    ready_deadline = measurement_started + 60
    while True:
        health()
        if ready():
            require(time.monotonic() <= ready_deadline, 'readiness_timeout')
            break
        require(time.monotonic() < ready_deadline, 'readiness_timeout')
        pause()
    next_sample = time.monotonic()
    for _ in range(30):
        next_sample += 1
        time.sleep(max(0, next_sample-time.monotonic()))
        require(time.monotonic() < next_sample + 1, 'sample_deadline_exceeded')
        health()
        require(ready() and web_ready() and db_ready(), 'sample_failed')
        health()
        evidence['samples'] += 1
    health()
    stop('server')
    for role in ('web', 'postgres'):
        capture(role)
        require(exit_code(role) is None, 'early_exit')
    evidence['result'] = 'passed'
    evidence['reason'] = 'healthy'
except Refused as error:
    evidence['reason'] = str(error)
except Exception:
    evidence['reason'] = 'preparation_failed' if measurement_started is None else 'inspection_failed'
finally:
    signal.setitimer(signal.ITIMER_REAL, 0)
    finished = time.monotonic()
    if measurement_started is None:
        evidence['preparation_seconds'] = round(finished-started, 3)
    else:
        evidence['measurement_seconds'] = round(finished-measurement_started, 3)
    deadline = finished + 30
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    signal.setitimer(signal.ITIMER_REAL, 30)
    try:
        clean = cleanup_owned()
    except Exception:
        clean = False
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
    evidence['cleanup_seconds'] = round(time.monotonic()-finished, 3)
    clean = clean and time.monotonic() <= deadline
    evidence['cleanup'] = 'passed' if clean else 'failed'
    if not clean:
        evidence['result'], evidence['reason'] = 'failed', 'cleanup_failed'
    evidence['elapsed_seconds'] = round(time.monotonic()-started, 3)
    try:
        emit()
    except Exception:
        evidence['result'], evidence['reason'] = 'failed', 'evidence_refused'
    if evidence_dir is not None:
        os.close(evidence_dir)
messages = {'base_references_must_be_digest_pinned': 'base references must be digest-pinned',
            'artifact_manifest_mismatch': 'artifact manifest mismatch',
            'linux_required': 'server runtime smoke requires Linux'}
print(messages.get(evidence['reason'], 'server runtime smoke ' + evidence['result']), file=sys.stderr)
sys.exit(0 if evidence['result'] == 'passed' else 1)
PY
    ;;
  *)
    printf '%s\n' 'usage: server-runtime-smoke.sh manifest DIR | run --artifacts DIR --manifest FILE --server-base REF --web-base REF' >&2
    exit 2
    ;;
esac
