"""Observe the unchanged job's default scope and a shared payload envelope."""
import json
import os
import pathlib
import re
import sys
from common import safe_write
from metrics import capture
from protocol import require

JOB='aboutme-job@idempotency-expiry-sweep'
ENVELOPE=pathlib.Path('/sys/fs/cgroup/machine.slice')


def _initialize_parent(effect,unit,path):
    from lifecycle import snapshot
    require(re.fullmatch(r'[a-z]+\.slice',unit) and path.name==unit)
    require(not path.is_symlink())
    previous=path.stat().st_ino if path.exists() else None
    effect(['systemctl','start',unit],10)
    effect(['systemctl','is-active','--quiet',unit],5)
    group=effect(['systemctl','show','--property=ControlGroup','--value',unit],5).decode().strip()
    require(group=='/'+unit and path.is_dir() and not path.is_symlink())
    current=snapshot(path)
    require(previous is None or current['identity']==previous)
    return int(previous is not None)


def initialize_parent(effect):
    from lifecycle import anchor
    require(os.geteuid()==0); anchor()
    return _initialize_parent(effect,'machine.slice',ENVELOPE)


def payload_path(group,identifier):
    require(re.fullmatch('[0-9a-f]{64}',identifier))
    require(group=='/machine.slice/libpod-'+identifier+'.scope')
    return pathlib.Path('/sys/fs/cgroup'+group)


def configuration(state):
    require(re.fullmatch('[0-9a-f]{64}',state['id']))
    path=pathlib.Path(state['bundle'])/'config.json'
    require(path.is_absolute() and path.is_file() and not path.is_symlink() and path.stat().st_uid==0)
    require(path.stat().st_size<=1048576)
    value=json.loads(path.read_bytes())
    return value


def observe(state,config,root,unit=JOB):
    from lifecycle import read_record, snapshot
    if config.get('process',{}).get('args')!=['/usr/local/bin/server','idempotency-expiry-sweep']: return
    identifier=state['id']; pid=state['pid']
    require(type(pid) is int and pid>0 and re.fullmatch('[0-9a-f]{64}',identifier))
    require(config['linux']['cgroupsPath']=='machine.slice:libpod:'+identifier)
    invocation=capture(['systemctl','show','--property=InvocationID','--value',unit+'.service']).strip()
    require(re.fullmatch('[0-9a-f]{32}',invocation))
    baseline=read_record(root/(unit+'-'+invocation+'.baseline.json'))
    require(baseline['unit']==unit and baseline['invocation']==invocation)
    groups=pathlib.Path('/proc/'+str(pid)+'/cgroup').read_text().splitlines()
    matches=[line[3:] for line in groups if line.startswith('0::')]; require(len(matches)==1)
    path=payload_path(matches[0],identifier)
    envelope=snapshot(ENVELOPE)
    require(envelope['identity']==baseline['shared_payload_envelope']['identity'])
    record={'unit':unit,'invocation':invocation,'container_id':identifier,'payload_group':matches[0],
            'payload_identity':path.stat().st_ino,'envelope_identity':envelope['identity']}
    safe_write(root/(unit+'-'+invocation+'.observed.json'),json.dumps(record,separators=(',',':')).encode())


def add_envelope(record):
    from lifecycle import snapshot
    require(capture(['podman','info','--format','{{.Host.CgroupManager}}']).strip()=='systemd')
    record['shared_payload_envelope']=snapshot(ENVELOPE)


def completed(base,final,root):
    from lifecycle import read_record, finish
    observed=read_record(root/(base['unit']+'-'+base['invocation']+'.observed.json'))
    require(observed['unit']==base['unit'] and observed['invocation']==base['invocation'])
    payload_path(observed['payload_group'],observed['container_id'])
    require(observed['envelope_identity']==base['shared_payload_envelope']['identity'])
    result=finish(base['shared_payload_envelope'],final['shared_payload_envelope'])
    result['shared_envelope']=1
    return result


def install_observer():
    directory=pathlib.Path('/usr/share/containers/oci/hooks.d')
    directory.mkdir(mode=0o755,parents=True,exist_ok=True)
    require(not directory.is_symlink() and directory.stat().st_uid==0)
    path=directory/'90-aboutme-rehearsal-job.json'
    require(not path.exists() and not path.is_symlink())
    script=str(pathlib.Path(__file__).resolve())
    value={'version':'1.0.0','hook':{'path':'/usr/bin/timeout','args':[
             '/usr/bin/timeout','--signal=TERM','--kill-after=1s','4s','/usr/bin/python3','-Es',script],
             'timeout':5},'when':{'commands':['^/usr/local/bin/server$']},'stages':['createRuntime']}
    safe_write(path,json.dumps(value,separators=(',',':')).encode())


def main():
    from lifecycle import ROOT, anchor
    require(len(sys.argv)==1 and os.geteuid()==0)
    raw=sys.stdin.buffer.read(65537); require(len(raw)<=65536)
    state=json.loads(raw); config=configuration(state)
    if config.get('process',{}).get('args')!=['/usr/local/bin/server','idempotency-expiry-sweep']: return
    anchor()
    observe(state,config,ROOT)


if __name__=='__main__':
    try: main()
    except BaseException: sys.exit(1)
