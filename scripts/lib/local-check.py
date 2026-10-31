"""Hold the shared advisory lock in a parent, never in the checked process."""

import fcntl
import os
import signal
import subprocess
import sys
import time


def main():
    child = None
    pending = []

    def forward(number, _frame):
        pending.append(number)
        if child is not None and child.poll() is None:
            child.send_signal(number)

    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, forward)
    path, seconds, *command = sys.argv[1:]
    deadline = time.monotonic() + float(seconds)
    flags = os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW
    try:
        descriptor = os.open(path, flags, 0o600)
        with os.fdopen(descriptor, "w") as lock:
            while True:
                if pending:
                    return 128 + pending[0]
                try:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
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


if __name__ == "__main__":
    sys.exit(main())
