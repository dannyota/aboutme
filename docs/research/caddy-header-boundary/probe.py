"""Probe the pinned Caddy header boundary using synthetic raw HTTP requests."""
from pathlib import Path
import json
import socket
import subprocess
import time

ROOT = Path(__file__).resolve().parent
EVIDENCE = ROOT.parents[2] / '.dev/phase-10/caddy-header-boundary'
EVIDENCE.mkdir(parents=True, exist_ok=True)
CASES = [
    ('current-ipv4', ['fixture-current'], ['192.0.2.10'], 200),
    ('next-ipv6', ['fixture-next'], ['2001:db8::1'], 200),
    ('missing-secret', [], ['192.0.2.10'], 403),
    ('wrong-secret', ['wrong'], ['192.0.2.10'], 403),
    ('duplicate-secret-same', ['fixture-current', 'fixture-current'], ['192.0.2.10'], 403),
    ('duplicate-secret-first', ['fixture-current', 'wrong'], ['192.0.2.10'], 403),
    ('duplicate-secret-last', ['wrong', 'fixture-current'], ['192.0.2.10'], 403),
    ('duplicate-secret-empty', ['fixture-current', ''], ['192.0.2.10'], 403),
    ('comma-secret', ['fixture-current,fixture-next'], ['192.0.2.10'], 403),
    ('missing-ip', ['fixture-current'], [], 403),
    ('empty-ip', ['fixture-current'], [''], 403),
    ('duplicate-ip-same', ['fixture-current'], ['192.0.2.10', '192.0.2.10'], 403),
    ('duplicate-ip-other', ['fixture-current'], ['192.0.2.10', '198.51.100.7'], 403),
    ('duplicate-ip-empty', ['fixture-current'], ['192.0.2.10', ''], 403),
    ('comma-ip', ['fixture-current'], ['192.0.2.10,198.51.100.7'], 403),
    ('internal-space-ip', ['fixture-current'], ['192.0. 2.10'], 403),
    ('invalid-v4', ['fixture-current'], ['999.0.2.10'], 403),
    ('invalid-v6', ['fixture-current'], ['2001:db8:::1'], 403),
    ('ip-port', ['fixture-current'], ['192.0.2.10:443'], 403),
    ('ip-zone', ['fixture-current'], ['fe80::1%lo'], 403),
    ('placeholder-input', ['fixture-current'], ['{env.SYNTHETIC_PROBE_VALUE}'], 403),
]

def request(secrets, ips):
    headers = ['GET / HTTP/1.1', 'Host: 127.0.0.1', 'Connection: close',
               'X-Real-IP: 203.0.113.5', 'X-Forwarded-For: 203.0.113.6']
    headers += ['X-Origin-Secret: ' + value for value in secrets]
    headers += ['X-Aboutme-Client-IP: ' + value for value in ips]
    with socket.create_connection(('127.0.0.1', 20961), timeout=2) as conn:
        conn.sendall(('\r\n'.join(headers) + '\r\n\r\n').encode('ascii'))
        response = b''
        while chunk := conn.recv(4096):
            response += chunk
    status = int(response.split(b' ', 2)[1])
    return status, response.partition(b'\r\n\r\n')[2].decode('ascii')

version = subprocess.check_output(['caddy', 'version'], text=True).strip()
assert version.startswith('v2.11.4 '), version
report = {'version': version, 'cases': {}}
for mode in ('red', 'green'):
    with (EVIDENCE / (mode + '.log')).open('w') as log:
        process = subprocess.Popen(['caddy', 'run', '--config', str(ROOT / (mode + '.Caddyfile')), '--adapter', 'caddyfile'], stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 5
            while True:
                assert process.poll() is None, 'Caddy exited during startup'
                try:
                    request([], [])
                    break
                except ConnectionRefusedError:
                    assert time.monotonic() < deadline, 'Caddy startup timeout'
                    time.sleep(0.02)
            results = []
            for name, secrets, ips, expected in CASES:
                status, body = request(secrets, ips)
                success = status == expected and (expected != 200 or body == ips[0])
                results.append({'case': name, 'expected': expected, 'actual': status, 'passed': success})
            report['cases'][mode] = results
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=2)
(EVIDENCE / 'results.json').write_text(json.dumps(report, indent=2) + '\n')
for mode, results in report['cases'].items():
    failures = [r['case'] for r in results if not r['passed']]
    print(mode, 'passed', len(results) - len(failures), '/', len(results), 'failures:', ', '.join(failures))
assert any(not r['passed'] for r in report['cases']['red']), 'Baseline unexpectedly rejects all hostile requests'
assert all(r['passed'] for r in report['cases']['green']), 'Native expression failed a required case'
