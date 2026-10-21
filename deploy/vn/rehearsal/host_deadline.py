"""Own the complete fixed host operation and every descendant until its deadline."""
import json
import os
import subprocess
import pathlib
import signal
import sys
import time
from common import Runner
from protocol import require


def main():
    require(len(sys.argv)==2 and sys.argv[1] in ('bootstrap','seed','sample','measure','verify'))
    raw=sys.stdin.buffer.read(65537); require(len(raw)<=65536)
    context=json.loads(raw); deadline=context['manifest']['deadline']
    require(type(deadline) in (int,float) and 0<deadline-time.time()<=14400)
    runner=Runner(deadline)
    budget=runner.bound(14400)-5; require(budget>0)
    child=subprocess.Popen([sys.executable,'-Es',str(pathlib.Path(__file__).with_name('host.py')),sys.argv[1]],
                           stdin=subprocess.PIPE,stderr=subprocess.DEVNULL,start_new_session=True)
    try:
        child.communicate(raw,timeout=runner.bound(budget))
        require(child.returncode==0)
    finally:
        if child.poll() is None:
            os.killpg(child.pid,signal.SIGTERM)
            try: child.wait(timeout=max(0,min(5,runner.deadline-time.time(),runner.monotonic_deadline-time.monotonic())))
            except subprocess.TimeoutExpired:
                os.killpg(child.pid,signal.SIGKILL); child.wait()
        try: os.killpg(child.pid,signal.SIGKILL)
        except ProcessLookupError: pass



if __name__=='__main__':
    signal.signal(signal.SIGTERM,lambda *_:sys.exit(1))
    signal.signal(signal.SIGINT,lambda *_:sys.exit(1))
    try: main()
    except BaseException: sys.exit(1)
