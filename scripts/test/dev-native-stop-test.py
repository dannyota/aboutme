"""Native shutdown preserves watchdog ownership until cleanup finishes."""

import os
from pathlib import Path
import shutil
import json
import secrets
import shlex
import signal
import subprocess
import sys
import tempfile
import threading
import time


def wait_for(predicate, description):
    deadline = time.monotonic() + 30
    while not predicate():
        if time.monotonic() >= deadline:
            raise AssertionError(f"deadline waiting for {description}")
        time.sleep(0.02)


def watchdog(directory):
    def term(_signal, _frame):
        (directory / "term").touch()

    signal.signal(signal.SIGTERM, term)
    # A separate session models a descendant only the watchdog can clean up.
    child = subprocess.Popen([sys.executable, "-c", "import signal; signal.pause()"],
                             start_new_session=True)
    try:
        (directory / "child.pid").write_text(str(child.pid))
        (directory / "ready").touch()
        wait_for(lambda: (directory / "release").exists(), "cleanup release")
        child.terminate()
        child.wait(timeout=10)
        (directory / "cleaned").touch()
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=10)


def prepare(root, repository):
    scripts = repository / "scripts"
    (scripts / "lib").mkdir(parents=True)
    source = (root / "scripts/dev-native.sh").read_text()
    (scripts / "dev-native.sh").write_text(source.rsplit('main "$@"', 1)[0])
    shutil.copyfile(root / "scripts/lib/platform.sh", scripts / "lib/platform.sh")
    (scripts / "lib/run-owned.py").write_text(
        "import runpy, sys\n"
        f"module = runpy.run_path({str(Path(__file__).resolve())!r})\n"
        "module['watchdog'](module['Path'](sys.argv[-1]))\n")
    return scripts


def invoke(repository, command, *args, **kwargs):
    return subprocess.run(["bash", "-c", 'source "$1"; shift; "$@"', "native-test",
                           str(repository / "scripts/dev-native.sh"), command, *map(str, args)],
                          cwd=repository, capture_output=True, text=True, timeout=30, **kwargs)


def run_case(root, blocked, wrapped=False):
    with tempfile.TemporaryDirectory(prefix="dev-native-stop-") as temporary:
        repository = Path(temporary).resolve()
        scripts = prepare(root, repository)
        environment = os.environ.copy()
        if wrapped:
            # Model a framework launcher that replaces its executable spelling.
            binary = repository / "bin"
            binary.mkdir()
            launcher = binary / "python3"
            launcher.write_text(f'#!/bin/sh\nexec {shlex.quote(shutil.which("python3"))} "$@"\n')
            launcher.chmod(0o700)
            environment["PATH"] = f"{binary}{os.pathsep}{environment['PATH']}"
        state = repository / ".dev"
        state.mkdir()
        services = {}
        reapers = []
        down = None
        try:
            # A blocked first service must not prevent later services stopping.
            for name in ("caddy", "web"):
                directory = state / name
                directory.mkdir()
                token = secrets.token_hex(16)
                process = subprocess.Popen([shutil.which("python3", path=environment["PATH"]), "-B", str(scripts / "lib/run-owned.py"),
                                            "--owner", token, "--", str(directory)],
                                           start_new_session=True, env=environment)
                services[name] = (process, directory)
                reaper = threading.Thread(target=process.wait)
                reaper.start()
                reapers.append(reaper)
                wait_for(lambda: (directory / "ready").exists(), f"{name} readiness")
                (state / f"{name}.pid").write_text(str(process.pid))
                (state / f"{name}.pid").chmod(0o600)
                recorded = invoke(repository, "native_identity", "write", name, process.pid, token,
                                  env=environment)
                assert recorded.returncode == 0, recorded.stderr
            identity = state / "caddy.identity"
            original = identity.read_text()
            for field in ("root", "name", "pid", "token", "started", "command_sha256"):
                record = json.loads(original)
                record[field] = 1 if field == "pid" else "invalid"
                identity.write_text(json.dumps(record))
                result = invoke(repository, "stop_service", "caddy", env=environment)
                assert result.returncode != 0 and identity.exists(), field
                assert not (state / "caddy/term").exists(), field
                identity.write_text(original)
            assert invoke(repository, "service_pid", "caddy", env=environment).returncode == 0
            down = subprocess.Popen(["bash", "-c", 'source "$1"; cmd_down', "native-test", str(scripts / "dev-native.sh")],
                                    cwd=repository, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, text=True,
                                    env={**environment, "ABOUTME_DEV_STATE_DIR": ".dev",
                                         "ABOUTME_DEV_MEDIA_DIR": ".dev/media",
                                         "ABOUTME_DEV_AUTO_SEED": "0"})
            caddy, directory = services["caddy"]
            wait_for(lambda: (directory / "term").exists(), "caddy TERM")
            if not blocked:
                (directory / "release").touch()
            web, web_directory = services["web"]
            wait_for(lambda: (web_directory / "term").exists(), "web TERM")
            web_directory.joinpath("release").touch()
            output, errors = down.communicate(timeout=30)
            assert (state / "web/cleaned").exists(), (output, errors)
            assert web.returncode == 0, (output, errors)
            assert not (state / "web.pid").exists(), (output, errors)
            assert "stopped web" in output, (output, errors)
            if blocked:
                assert down.returncode != 0, (output, errors)
                assert caddy.returncode is None, (output, errors)
                assert (state / "caddy.pid").read_text() == str(caddy.pid)
                assert not (directory / "cleaned").exists()
                assert "stopped caddy" not in output, output
                assert "native dev stack is down" not in output, output
                assert "caddy" in errors and "watchdog" in errors and "deadline" in errors, errors
                # Releasing cleanup after the caller fails proves ownership survived.
                (directory / "release").touch()
                reapers[0].join(timeout=10)
                assert not reapers[0].is_alive(), "watchdog did not finish cleanup"
                assert caddy.returncode == 0 and (directory / "cleaned").exists()
            else:
                assert down.returncode == 0, (output, errors)
                assert caddy.returncode == 0 and (directory / "cleaned").exists()
                assert not (state / "caddy.pid").exists()
                assert "stopped caddy" in output and "native dev stack is down" in output
        finally:
            for process, directory in services.values():
                (directory / "release").touch()
            for reaper in reapers:
                reaper.join(timeout=30)
            for process, directory in services.values():
                if process.returncode != 0 and (directory / "child.pid").exists():
                    try:
                        os.kill(int((directory / "child.pid").read_text()), signal.SIGTERM)
                    except ProcessLookupError:
                        pass
            if down is not None and down.poll() is None:
                down.terminate()
                down.communicate(timeout=10)


def foreign_cases(root):
    with tempfile.TemporaryDirectory(prefix="dev-native-foreign-") as temporary:
        repository = Path(temporary).resolve()
        scripts = prepare(root, repository)
        state = repository / ".dev"
        state.mkdir()
        foreign = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)"],
                                   start_new_session=True)
        try:
            pidfile = state / "caddy.pid"
            pidfile.write_text(str(foreign.pid))
            pidfile.chmod(0o600)
            for mode in ("missing", "forged", "symlink", "mode"):
                identity = state / "caddy.identity"
                if mode != "missing":
                    identity.write_text(json.dumps({"version": 1, "root": str(repository),
                                                  "name": "caddy", "pid": foreign.pid,
                                                  "token": secrets.token_hex(16)}))
                    identity.chmod(0o600)
                if mode == "symlink":
                    target = state / "foreign-record"
                    identity.rename(target)
                    identity.symlink_to(target)
                elif mode == "mode":
                    identity.chmod(0o644)
                result = invoke(repository, "cmd_down")
                assert result.returncode != 0, (mode, result.stdout, result.stderr)
                assert foreign.poll() is None, f"foreign group leader killed: {mode}"
                assert pidfile.exists(), f"rejected state deleted: {mode}"
                identity.unlink(missing_ok=True)
            # A command carrying the marker as data cannot register ownership.
            token = secrets.token_hex(16)
            impersonator = subprocess.Popen(
                [shutil.which("python3"), "-c", "import time; time.sleep(60)",
                 str(scripts / "lib/run-owned.py"), "--owner", token, "--", "foreign"],
                start_new_session=True)
            try:
                pidfile.write_text(str(impersonator.pid))
                result = invoke(repository, "native_identity", "write", "caddy", impersonator.pid, token)
                assert result.returncode != 0, "argv marker accepted outside the owner command"
                assert impersonator.poll() is None
            finally:
                impersonator.terminate()
                impersonator.wait(timeout=10)
            # Malformed PIDs and linked PID files also preserve state.
            for value in ("0", "1", "-1", "garbage"):
                pidfile.write_text(value)
                result = invoke(repository, "cmd_down")
                assert result.returncode != 0 and pidfile.exists(), value
                assert foreign.poll() is None
            pidfile.write_text(str(foreign.pid))
            pidfile.rename(state / "saved.pid")
            pidfile.symlink_to(state / "saved.pid")
            result = invoke(repository, "cmd_down")
            assert result.returncode != 0 and pidfile.is_symlink()
            assert foreign.poll() is None
        finally:
            foreign.terminate()
            foreign.wait(timeout=10)


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "watchdog":
        watchdog(Path(sys.argv[2]))
    else:
        source = Path(__file__).resolve().parents[2]
        foreign_cases(source)
        run_case(source, blocked=True)
        run_case(source, blocked=False)
        run_case(source, blocked=False, wrapped=True)
        print("dev-native stop tests passed")
