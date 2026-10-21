"""Private scratch state and bounded child ownership for one rehearsal."""
import fcntl
import json
import os
import pathlib
import signal
import stat
import subprocess
import time
from protocol import require, validate_manifest


class Runner:
    def __init__(self,deadline=None):
        self.set_deadline(deadline)

    def set_deadline(self,deadline):
        self.deadline=deadline
        self.monotonic_deadline=None if deadline is None else time.monotonic()+max(0,deadline-time.time())

    def bound(self,seconds):
        remaining=seconds if self.deadline is None else min(seconds,self.deadline-time.time(),self.monotonic_deadline-time.monotonic())
        require(remaining>0)
        return remaining

    def call(self,arguments,input_data=None,timeout=90,cwd=None,env=None,private_error=False):
        child=subprocess.Popen(arguments,stdin=subprocess.PIPE if input_data is not None else subprocess.DEVNULL,
                               stdout=subprocess.PIPE,stderr=subprocess.PIPE if private_error else subprocess.DEVNULL,cwd=cwd,env=env,start_new_session=True)
        try:
            output,error=child.communicate(input_data,timeout=self.bound(timeout))
            require(len(output)<=16777216 and (error is None or len(error)<=65536))
            if private_error: return child.returncode,output,error
            require(child.returncode==0)
            return output
        except BaseException:
            if child.poll() is None:
                os.killpg(child.pid,signal.SIGTERM)
                try: child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid,signal.SIGKILL); child.wait()
            raise


class Store:
    def __init__(self,root,repository):
        self.root=pathlib.Path(root)
        require(self.root.is_absolute() and str(self.root)==str(self.root.resolve()))
        require(self.root!=repository and repository not in self.root.parents)
        require(self.root.is_dir() and self.root.stat().st_uid==os.getuid())
        require(stat.S_IMODE(self.root.stat().st_mode)==0o700)
        descriptor=os.open(self.root/'rehearsal.lock',os.O_RDWR|os.O_CREAT|os.O_NOFOLLOW,0o600)
        self.lock=os.fdopen(descriptor,'r+')
        fcntl.flock(self.lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        self.path=self.root/'active.json'

    def read_file(self,name):
        path=self.root/name
        require(path.is_file() and not path.is_symlink() and path.stat().st_uid==os.getuid())
        require(stat.S_IMODE(path.stat().st_mode)==0o600 and path.stat().st_nlink==1)
        require(path.stat().st_size<=65536)
        return json.loads(path.read_bytes())

    def load(self):
        return validate_manifest(self.read_file('active.json'))

    def save(self,value):
        validate_manifest(value)
        temporary=self.root/'active.new'
        descriptor=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        with os.fdopen(descriptor,'w') as stream:
            json.dump(value,stream,separators=(',',':')); stream.flush(); os.fsync(stream.fileno())
        os.replace(temporary,self.path)
        descriptor=os.open(self.root,os.O_RDONLY|os.O_DIRECTORY)
        try: os.fsync(descriptor)
        finally: os.close(descriptor)

    def checkpoint(self,value):
        require(self.load()==value)
        require(value['deadline'] is None or time.time()<value['deadline'])

    def finish(self,value):
        require(value['state']=='cleaned')
        self.save(value)
        os.replace(self.path,self.root/(value['run_id']+'.cleaned.json'))


def safe_write(path,data):
    descriptor=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    with os.fdopen(descriptor,'wb') as stream:
        stream.write(data)
