"""Hold the shared advisory lock in a parent, never in the checked process."""

import fcntl
import os
import signal
import subprocess
import sys
import time

SHARED_LOCK = os.path.join(".dev", "locks", "local-check.lock")
PRE_MOVE_LOCK = os.path.join(".git", "aboutme-local-check.lock")


def lock_paths(path):
    """Return the locks to take, in order.

    Branches older than the move to .dev/locks lock <main>/.git/aboutme-local-check.lock.
    Until none remain, the shared lock is taken after that one, so old and new
    callers exclude each other and every caller takes the two in the same order.
    """
    path = os.path.abspath(path)
    if not path.endswith(os.sep + SHARED_LOCK):
        return [path]
    main = path[: -len(os.sep + SHARED_LOCK)]
    if not os.path.isdir(os.path.join(main, ".git")):
        return [path]
    return [os.path.join(main, PRE_MOVE_LOCK), path]


def make_parents(path):
    # makedirs ignores mode for intermediate directories such as a new .dev.
    directory = os.path.dirname(path)
    missing = []
    while not os.path.isdir(directory):
        missing.append(directory)
        directory = os.path.dirname(directory)
    for directory in reversed(missing):
        os.makedirs(directory, mode=0o700, exist_ok=True)


def open_lock(path, pre_move):
    try:
        return os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    except PermissionError:
        if not pre_move:
            raise
        # A Codex sandbox cannot write .git; flock works on a read-only descriptor.
        return os.open(path, os.O_RDONLY | os.O_NOFOLLOW)


def main():
    child = None
    pending = []
    descriptors = []

    def forward(number, _frame):
        pending.append(number)
        if child is not None and child.poll() is None:
            child.send_signal(number)

    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, forward)
    path, seconds, *command = sys.argv[1:]
    deadline = time.monotonic() + float(seconds)
    try:
        paths = lock_paths(path)
        make_parents(paths[-1])
        for lock in paths:
            descriptors.append(open_lock(lock, lock != paths[-1]))
            while True:
                if pending:
                    return 128 + pending[0]
                try:
                    fcntl.flock(descriptors[-1], fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    if time.monotonic() >= deadline:
                        print("another local check holds the shared lock", file=sys.stderr)
                        return 75
                    time.sleep(0.1)
        child = subprocess.Popen(command, close_fds=True)
        for number in pending:
            if child.poll() is None:
                child.send_signal(number)
        status = child.wait()
        return 128 - status if status < 0 else status
    except OSError as error:
        print(f"local-check: cannot acquire lock or launch command: {error}", file=sys.stderr)
        return 75
    finally:
        for descriptor in descriptors:
            os.close(descriptor)


if __name__ == "__main__":
    sys.exit(main())
