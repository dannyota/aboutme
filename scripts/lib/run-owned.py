"""Portable process owner used by run-owned.sh. No command output is captured."""

import os
import signal
import subprocess
import sys
import time


def say(message):
    print(f"run-owned: {message}", file=sys.stderr, flush=True)


def snapshot():
    # lstart binds each retained PID to its observed process identity.
    output = subprocess.check_output(
        ["ps", "-axo", "pid=,ppid=,pgid=,lstart=,stat="], text=True, timeout=2,
        env={**os.environ, "LC_ALL": "C"},
    )
    rows = []
    for line in output.splitlines():
        if not line.strip():
            continue
        fields = line.split()
        if len(fields) != 9:
            raise ValueError("invalid ps sample")
        numbers = tuple(map(int, fields[:3]))
        if min(numbers) < 0:
            raise ValueError("invalid ps sample")
        started = " ".join(fields[3:8])
        time.strptime(started, "%a %b %d %H:%M:%S %Y")
        rows.append((*numbers, started, fields[8]))
    if not rows:
        raise ValueError("empty ps sample")
    return rows


def members(rows, leader, known, root, group_owned=False):
    # A numeric PID alone never preserves ownership after an observed exit.
    current = {(pid, started) for pid, _, _, started, _ in rows}
    retained = known & current
    anchors = retained | ({root} & current if root else set())
    # The original group stays owned between samples even when every sampled
    # member exits. A new leader identity proves that the PGID was reused.
    if root and any(pid == leader and started != root[1]
                    for pid, _, _, started, _ in rows):
        group_owned = False
    else:
        group_owned = group_owned or any((pid, started) in anchors and group == leader
                                        for pid, _, group, started, _ in rows)
    selected = {pid for pid, started in retained}
    selected.update(pid for pid, _, group, _, _ in rows
                    if group_owned and group == leader)
    if root in current:
        selected.add(leader)
    while True:
        expanded = selected | {pid for pid, parent, _, _, _ in rows if parent in selected}
        if expanded == selected:
            break
        selected = expanded
    return {(pid, started) for pid, _, _, started, state in rows
            if pid in selected and not state.startswith("Z")}


def signal_members(known, sig, observe=None):
    if observe is None:
        observe = snapshot
    for pid, started in sorted(known):
        # Revalidate immediately before each signal, including KILL cleanup.
        rows = observe()
        if not any(other == pid and birth == started and not state.startswith("Z")
                   for other, _, _, birth, state in rows):
            continue
        try:
            os.kill(pid, sig)
        except ProcessLookupError:
            pass


def main(observe=None):
    if observe is None:
        observe = snapshot
    args = sys.argv[1:]
    timeout = None
    try:
        if args[:1] == ["--timeout"]:
            timeout = float(args[1])
            args = args[2:]
            if not 0 < timeout < float("inf"):
                raise ValueError("timeout must be positive")
        if args[:1] == ["--owner"]:
            token = args[1]
            if len(token) != 32 or any(char not in "0123456789abcdef" for char in token):
                raise ValueError("invalid owner token")
            args = args[2:]
        if len(args) < 2 or args[0] != "--":
            raise ValueError("usage: scripts/run-owned.sh [--timeout seconds] -- command [args...]")
        observe()  # Refuse before launch if process ownership is unavailable.
    except (OSError, ValueError, IndexError, subprocess.SubprocessError) as error:
        say(f"refused: cannot inspect process ownership: {error}")
        return 125

    pending = []
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda number, _frame: pending.append(number))
    try:
        child = subprocess.Popen(args[1:], start_new_session=True)
    except OSError as error:
        say(f"cannot start command: {error}")
        return 127
    known = set()
    root = None
    # setsid gives us this group at launch. Only a successful observation can
    # resolve ownership, including when the first post-launch sample fails.
    group_open = True
    start = time.monotonic()
    reason = None

    def sample():
        nonlocal known, root, group_open
        rows = observe()
        if root is None and group_open:
            root = next(((pid, started) for pid, _, _, started, _ in rows
                         if pid == child.pid), None)
        if root and any(pid == child.pid and started != root[1]
                        for pid, _, _, started, _ in rows):
            group_open = False
        known = members(rows, child.pid, known, root, group_owned=group_open)
        # Keep the leader identity after it is reaped. A descendant can fork
        # and exit entirely between observations without ending group ownership.
        if not any(group == child.pid and not state.startswith("Z")
                   for _, _, group, _, state in rows):
            group_open = False

    def send(sig):
        nonlocal reason, group_open
        try:
            sample()
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            say(f"cleanup observation failed: {error}")
            reason = reason or 126
        if group_open:
            try:
                os.killpg(child.pid, sig)
            except ProcessLookupError:
                # ESRCH is kernel evidence that the launched group is empty.
                group_open = False
        try:
            signal_members(known, sig, observe)
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            say(f"cleanup observation failed: {error}")
            reason = reason or 126
        # Popen retains the unreaped child identity even if ps is unavailable.
        if child.poll() is None:
            child.send_signal(sig)

    try:
        while True:
            sample()
            if pending:
                reason = 128 + pending[0]
                break
            if timeout is not None and time.monotonic() - start >= timeout:
                say(f"timeout after {timeout:g} seconds")
                reason = 124
                break
            if child.poll() is not None:
                break
            time.sleep(0.5)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        say(f"ownership inspection failed: {error}")
        reason = 126
    finally:
        # A completed command cannot leave background descendants.
        send(pending[0] if pending and reason == 128 + pending[0] else signal.SIGTERM)
        deadline = time.monotonic() + 5
        while (group_open or known) and time.monotonic() < deadline:
            try:
                sample()
            except (OSError, ValueError, subprocess.SubprocessError):
                reason = reason or 126
            child.poll()
            if not group_open and not known:
                break
            time.sleep(0.1)
        if group_open or known or child.poll() is None:
            say("KILL escalation")
        send(signal.SIGKILL)
        # Keep the lock-owning caller waiting until every observed identity ends.
        while group_open or known:
            try:
                sample()
            except (OSError, ValueError, subprocess.SubprocessError):
                reason = reason or 126
            send(signal.SIGKILL)
            child.poll()
            if group_open or known:
                time.sleep(0.1)
        status = child.wait()
    return reason if reason is not None else (128 - status if status < 0 else status)


if __name__ == "__main__":
    sys.exit(main())
