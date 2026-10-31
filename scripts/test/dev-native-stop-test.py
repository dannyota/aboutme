"""Native shutdown preserves watchdog ownership until cleanup finishes."""

import os
from pathlib import Path
import shutil
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


def run_case(root, blocked):
    with tempfile.TemporaryDirectory(prefix="dev-native-stop-") as temporary:
        repository = Path(temporary)
        scripts = repository / "scripts"
        (scripts / "lib").mkdir(parents=True)
        shutil.copyfile(root / "scripts/dev-native.sh", scripts / "dev-native.sh")
        shutil.copyfile(root / "scripts/lib/platform.sh", scripts / "lib/platform.sh")
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
                process = subprocess.Popen([sys.executable, __file__, "watchdog", str(directory)],
                                           start_new_session=True)
                services[name] = (process, directory)
                reaper = threading.Thread(target=process.wait)
                reaper.start()
                reapers.append(reaper)
                wait_for(lambda: (directory / "ready").exists(), f"{name} readiness")
                (state / f"{name}.pid").write_text(str(process.pid))
            down = subprocess.Popen(["bash", str(scripts / "dev-native.sh"), "down"],
                                    cwd=repository, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, text=True,
                                    env={**os.environ, "ABOUTME_DEV_STATE_DIR": ".dev",
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


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "watchdog":
        watchdog(Path(sys.argv[2]))
    else:
        source = Path(__file__).resolve().parents[2]
        run_case(source, blocked=True)
        run_case(source, blocked=False)
        print("dev-native stop tests passed")
