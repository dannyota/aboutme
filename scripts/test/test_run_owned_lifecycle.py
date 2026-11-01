"""Deterministic watchdog lifecycles with real process groups and sample hooks."""

import importlib.util
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
from unittest.mock import patch


def wait_for(predicate, description):
    deadline = time.monotonic() + 30
    while not predicate():
        if time.monotonic() >= deadline:
            raise AssertionError(f"deadline waiting for {description}")
        time.sleep(0.02)


def load(path):
    spec = importlib.util.spec_from_file_location("watchdog", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def command(case, directory):
    # Each command has fresh files and waits for the observer to release it.
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    if case in ("escalation", "interrupt"):
        (directory / "pid").write_text(str(os.getpid()))
        (directory / "ready").touch()
        wait_for(lambda: (directory / "release").exists(), "release")
        signal.pause()
        return
    child = os.fork()
    if child:
        (directory / "leader").write_text(str(os.getpid()))
        wait_for(lambda: (directory / "ready").exists(), "descendant readiness")
        wait_for(lambda: (directory / "release").exists(), "leader release")
        return
    if case == "escaped":
        os.setsid()
    (directory / "pid").write_text(str(os.getpid()))
    (directory / "ready").touch()
    if case == "replacement":
        wait_for(lambda: (directory / "replace").exists(), "replacement release")
        replacement = os.fork()
        if replacement:
            os._exit(0)
        (directory / "replacement").write_text(str(os.getpid()))
        (directory / "replacement-ready").touch()
    signal.pause()


def driver(path, case, directory):
    watchdog = load(path)
    original_snapshot = watchdog.snapshot
    original_popen = subprocess.Popen
    samples = 0
    launched = None
    first_poll = True
    group_signaled = False
    original_killpg = os.killpg

    def launch(*args, **kwargs):
        nonlocal launched
        if not kwargs.get("start_new_session"):
            return original_popen(*args, **kwargs)
        launched = original_popen(*args, **kwargs)
        (directory / "group").write_text(str(launched.pid))
        original_poll = launched.poll

        def poll():
            nonlocal first_poll
            if first_poll and case in ("replacement", "parent-exits", "escaped"):
                first_poll = False
                (directory / "release").touch()
                launched.wait(timeout=30)
                (directory / "reaped").touch()
            return original_poll()

        launched.poll = poll
        return launched

    def signal_group(group, sig):
        nonlocal group_signaled
        original_killpg(group, sig)
        group_signaled = True

    def observe():
        nonlocal samples
        samples += 1
        if samples == 2:
            wait_for(lambda: (directory / "ready").exists(), "command readiness")
        if case == "sampling-failure" and samples >= 2 and not group_signaled:
            # Sampling stays unavailable until the whole group is signaled;
            # no post-launch identities can populate the fallback member set.
            raise ValueError("forced first post-launch sample failure")
        if case == "replacement" and (directory / "reaped").exists():
            (directory / "replace").touch()
            wait_for(lambda: (directory / "replacement-ready").exists(), "replacement readiness")
            old = int((directory / "pid").read_text())
            wait_for(lambda: not any(pid == old and not state.startswith("Z")
                                    for pid, _, _, _, state in original_snapshot()),
                     "original descendant exit")
        rows = original_snapshot()
        if samples == 2:
            (directory / "sampled").touch()
        return rows

    sys.argv = [path, "--", sys.executable, "-B", __file__, "command", case, str(directory)]
    # The hook changes observation only. Launch, group signaling and teardown
    # use real OS processes. Popen's poll hook reaps the leader before replacement.
    with patch.object(watchdog.subprocess, "Popen", side_effect=launch), \
            patch.object(watchdog.os, "killpg", side_effect=signal_group):
        return watchdog.main(observe=observe)


def run_cases(path):
    for case in ("replacement", "sampling-failure", "escalation", "interrupt", "parent-exits", "escaped"):
        with tempfile.TemporaryDirectory(prefix=f"run-owned-{case}-") as temporary:
            directory = Path(temporary)
            environment = os.environ.copy()
            runner = subprocess.Popen([sys.executable, "-B", __file__, "driver", path, case, temporary],
                                      env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                      text=True)
            completed = False
            try:
                if case in ("escalation", "interrupt"):
                    wait_for(lambda: (directory / "sampled").exists(), "first sample")
                    (directory / "release").touch()
                    runner.send_signal(signal.SIGINT if case == "interrupt" else signal.SIGTERM)
                output, errors = runner.communicate(timeout=60)
                expected = {"sampling-failure": 126, "escalation": 143, "interrupt": 130}.get(case, 0)
                assert runner.returncode == expected, (case, runner.returncode, output, errors)
                if case in ("escalation", "interrupt"):
                    assert "KILL escalation" in errors, errors
                if case == "replacement":
                    assert (directory / "replacement").exists(), "replacement was never exercised"
                rows = load(path).snapshot()
                for name in ("leader", "pid", "replacement"):
                    if (directory / name).exists():
                        pid = int((directory / name).read_text())
                        assert not any(other == pid and not state.startswith("Z")
                                       for other, _, _, _, state in rows), (case, name, pid)
                completed = True
            finally:
                if runner.poll() is None:
                    runner.kill()
                # Successful cases proved emptiness. Failed cases still own
                # their launched session and must clean it before joining pipes.
                if not completed and (directory / "group").exists():
                    try:
                        os.killpg(int((directory / "group").read_text()), signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                runner.communicate(timeout=10)


if __name__ == "__main__":
    mode = sys.argv[1] if len(sys.argv) > 1 else str(Path(__file__).resolve().parents[1] / "lib/run-owned.py")
    if mode == "command":
        command(sys.argv[2], Path(sys.argv[3]))
    elif mode == "driver":
        sys.exit(driver(sys.argv[2], sys.argv[3], Path(sys.argv[4])))
    else:
        run_cases(mode)
