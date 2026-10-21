"""Pinned SSH to the single manifest-owned host, with fixed remote entry points."""
import json
from contextlib import closing
import math
from metrics import UNITS, CONTAINERS
import os
import selectors
import shlex
import signal
import subprocess
import time
from common import safe_write
from protocol import require

ROOT='/run/aboutme-rehearsal'


def pipe_rows(child,runner,payload):
    require(0<len(payload)<=65536)
    selector=selectors.DefaultSelector(); pending=bytearray(); offset=0
    os.set_blocking(child.stdout.fileno(),False); os.set_blocking(child.stdin.fileno(),False)
    selector.register(child.stdout,selectors.EVENT_READ)
    selector.register(child.stdin,selectors.EVENT_WRITE)
    try:
        while True:
            remaining=runner.bound(3600)-5; require(remaining>0)
            ready=selector.select(timeout=min(5,remaining)); require(ready)
            for key,_ in ready:
                if key.fileobj is child.stdin:
                    try: amount=os.write(child.stdin.fileno(),payload[offset:offset+16384])
                    except BlockingIOError: continue
                    require(amount>0); offset+=amount
                    if offset==len(payload):
                        selector.unregister(child.stdin); child.stdin.close()
                    continue
                try: raw=os.read(child.stdout.fileno(),16384)
                except BlockingIOError: continue
                if not raw:
                    require(not pending and offset==len(payload)); return
                pending.extend(raw)
                while b'\n' in pending:
                    raw,_,rest=pending.partition(b'\n'); pending=bytearray(rest)
                    require(0<len(raw)<=65535)
                    yield bytes(raw)
                require(len(pending)<=65535)
    finally: selector.close()


class Remote:
    def __init__(self,cloud):
        self.cloud=cloud; self.m=cloud.m
        self.host='aboutme-admin@'+self.m['resources']['floating_ip']
        self.options=['ssh','-F','/dev/null','-p','22922','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes',
                      '-o','HostKeyAlgorithms=ssh-ed25519','-o','ForwardAgent=no','-o','ConnectTimeout=10',
                      '-o','ServerAliveInterval=10','-o','ServerAliveCountMax=2',
                      '-o','HostKeyAlias='+self.m['name'],'-o','UserKnownHostsFile='+str(cloud.work/'known_hosts'),
                      '-o','GlobalKnownHostsFile=/dev/null']

    def call(self,arguments,input_data=None,timeout=90):
        self.cloud.store.checkpoint(self.m); self.cloud.verify_identity()
        return self.cloud.runner.call([*self.options,self.host,shlex.join(arguments)],input_data,timeout=timeout)

    def stage(self):
        for attempt in range(60):
            self.cloud.store.checkpoint(self.m); self.cloud.verify_identity()
            code,output,error=self.cloud.runner.call([*self.options,self.host,'true'],timeout=15,private_error=True)
            if code==0: break
            require(code==255 and b'Host key verification failed' not in error and b'REMOTE HOST IDENTIFICATION' not in error)
            time.sleep(self.cloud.runner.bound(10))
        else: raise ValueError('ssh_start_bound')
        self.call(['sudo','cloud-init','status','--wait'],timeout=1200)
        anchor=json.loads(self.call(['sudo','cat','/etc/aboutme-rehearsal.json']))
        require(anchor=={key:self.m[key] for key in ('run_id','commit','baseline_commit','bundle_sha256','baseline_bundle_sha256')})
        # These paths and commands are fixed, never supplied by context.
        self.call(['sudo','install','-d','-m','0700',ROOT])
        for name in ('baseline','candidate'):
            self.call(['sudo','tee',ROOT+'/'+name+'.tar'],(self.cloud.work/(name+'.tar')).read_bytes(),180)
            self.call(['sudo','install','-d','-m','0755',ROOT+'/'+name])
            self.call(['sudo','tar','--no-same-owner','-xf',ROOT+'/'+name+'.tar','-C',ROOT+'/'+name],timeout=180)

    def operation(self,operation,options):
        require(operation in ('bootstrap','seed','sample','measure','verify'))
        value={'manifest':self.m,'options':options}
        result=self.call(['sudo','bash',ROOT+'/candidate/probe/crowdsec-rehearsal-host.sh',operation],
                         json.dumps(value,separators=(',',':')).encode(),3600)
        if operation=='sample' or operation=='bootstrap' and options=={'stage':'deploy'}:
            row=json.loads(result); validate_numeric(row); return row
        require(result.decode().strip()=='VALID '+operation)

    def import_key(self,role,file):
        require(role in ('media','backups'))
        self.call(['sudo','/usr/local/lib/aboutme/secrets-host.sh','import-s3',role],file.read_bytes(),30)
        file.unlink()

    def measure(self,scenario,output):
        self.cloud.store.checkpoint(self.m); self.cloud.verify_identity()
        arguments=['sudo','bash',ROOT+'/candidate/probe/crowdsec-rehearsal-host.sh','measure']
        value={'manifest':self.m,'options':{'scenario':scenario}}
        require(self.cloud.runner.bound(6)>5)
        payload=json.dumps(value,separators=(',',':')).encode(); require(len(payload)<=65536)
        descriptor=os.open(output,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        try:
            child=subprocess.Popen([*self.options,self.host,shlex.join(arguments)],stdin=subprocess.PIPE,
                                   stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,start_new_session=True,bufsize=0)
        except BaseException:
            os.close(descriptor); raise
        rows=[]; effects=[]; summary=None; warmup_end=None
        try:
            with os.fdopen(descriptor,'w') as stream, closing(pipe_rows(child,self.cloud.runner,payload)) as frames:
                for raw in frames:
                    row=json.loads(raw)
                    if 'effect' in row:
                        require(set(row)=={'effect','t'} and row['effect'] in ('pull','secrets-check','backup','timers-stop',
                           'maintenance-up','app-down','render','db-setup','migrate','app-up','maintenance-down','timers-start'))
                        require(type(row['t']) in (int,float) and row['t']>=0)
                        effects.append((row['effect'],time.monotonic()))
                    else:
                        validate_numeric(row)
                        if row.get('valid')==1: summary=row
                        else:
                            if warmup_end is None: warmup_end=time.monotonic()-row['t']+30
                            rows.append(row)
                    stream.write(json.dumps(row,separators=(',',':'))+'\n')
                budget=self.cloud.runner.bound(15)-5; require(budget>0)
                require(child.wait(timeout=min(10,budget))==0 and summary is not None)
                require(len([row for row in rows if row['t']>=30])>=120)
                summary['warmup_end']=warmup_end
                return summary,effects
        finally:
            if child.poll() is None:
                os.killpg(child.pid,signal.SIGTERM)
                grace=max(0,min(5,self.cloud.runner.deadline-time.time(),self.cloud.runner.monotonic_deadline-time.monotonic()))
                try: child.wait(timeout=min(4,grace))
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid,signal.SIGKILL)
                    remaining=max(0,min(1,self.cloud.runner.deadline-time.time(),self.cloud.runner.monotonic_deadline-time.monotonic()))
                    child.wait(timeout=remaining)
            for pipe in (child.stdin,child.stdout):
                if not pipe.closed: pipe.close()


def validate_numeric(value):
    if isinstance(value,dict):
        labels={'interval','t','total','available','free','used','some','full','psi','avg10','avg60','avg300',
                'zram_physical','swap_in','swap_out','kernel_oom','tmpfs_used','tmpfs_capacity','connections',
                'backup_count','backup_rss','cgroups','present','identity','current','peak','events','low','high',
                'max','oom','oom_kill','valid','samples','overlap','combined_peak','minimum_available',
                'retired_cgroup_peaks','migration_noop','cold_start','final_lifecycles','final','payload_contained','shared_payload_envelope','shared_envelope'} | set(UNITS) | {
                'container-'+name.removeprefix('aboutme-') for name in CONTAINERS}
        require(set(value)<=labels)
        for item in value.values(): validate_numeric(item)
    elif isinstance(value,list):
        require(len(value)<10000)
        for item in value: validate_numeric(item)
    else:
        require(type(value) in (int,float) and math.isfinite(value) and 0<=value<10**18)
