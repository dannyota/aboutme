"""Final hierarchical accounting before systemd removes a fixed unit cgroup."""
import json
import os
import pathlib
import re
import socket
import sys
from common import safe_write
from metrics import capture, counters, numeric_file, CONTAINERS
from protocol import require

ROOT=pathlib.Path('/run/aboutme-rehearsal/accounting')
UNITS={name:name for name in CONTAINERS if name!='aboutme-job-idempotency-expiry-sweep'}
UNITS['aboutme-job@idempotency-expiry-sweep']='aboutme-job-idempotency-expiry-sweep'
EVENTS=('low','high','max','oom','oom_kill')


def snapshot(path):
    identity=path.stat().st_ino
    current=numeric_file(path/'memory.current'); peak=numeric_file(path/'memory.peak')
    events=counters(path/'memory.events')
    require(set(EVENTS)<=events.keys() and peak>=current)
    require(path.stat().st_ino==identity)
    return {'identity':identity,'current':current,'peak':peak,'events':{key:events[key] for key in EVENTS}}


def finish(baseline,final):
    require(final['identity']==baseline['identity'] and final['peak']>=baseline['peak'])
    delta={key:final['events'][key]-baseline['events'][key] for key in EVENTS}
    require(all(value>=0 for value in delta.values()))
    require(final['events']['oom']==final['events']['oom_kill']==0)
    return dict(final,events=delta,present=1,final=1,payload_contained=1)


def read_record(path):
    require(path.is_file() and not path.is_symlink() and path.stat().st_uid==0)
    require(path.stat().st_mode&0o777==0o600 and path.stat().st_size<=4096)
    return json.loads(path.read_bytes())


def final_record(unit,invocation):
    require(unit in UNITS and re.fullmatch('[0-9a-f]{32}',invocation))
    base=read_record(ROOT/(unit+'-'+invocation+'.baseline.json'))
    final=read_record(ROOT/(unit+'-'+invocation+'.final.json'))
    require(base['invocation']==final['invocation']==invocation and base['unit']==final['unit']==unit)
    require(base['group']==final['group'])
    expected=0 if unit=='aboutme-job@idempotency-expiry-sweep' else 1
    require(base['payload_contained']==final['payload_contained']==expected)
    result=finish(base['snapshot'],final['snapshot']); result['payload_contained']=expected
    if expected==0:
        from job_accounting import completed
        result['shared_payload_envelope']=completed(base,final,ROOT)
    return result


def final_for_identity(unit,identity):
    matches=[]
    for path in ROOT.glob(unit+'-*.baseline.json'):
        record=read_record(path)
        if record['snapshot']['identity']==identity:
            require(record['unit']==unit)
            matches.append(record['invocation'])
    require(len(matches)==1)
    row=final_record(unit,matches[0]); require(row['identity']==identity)
    return row


def final_records():
    result={}
    for path in ROOT.glob('*.final.json'):
        match=re.fullmatch(r'(.+)-([0-9a-f]{32})\.final\.json',path.name)
        require(match is not None and match[1] in UNITS)
        result.setdefault(match[1],[]).append(final_record(match[1],match[2]))
    return result


def anchor():
    anchor=pathlib.Path('/etc/aboutme-rehearsal.json')
    require(anchor.is_file() and not anchor.is_symlink() and anchor.stat().st_uid==0)
    run=json.loads(anchor.read_bytes())['run_id']
    require(re.fullmatch('[0-9a-f]{16}',run) and socket.gethostname()=='aboutme-cs-rehearsal-'+run)


def validate_command(unit,command):
    if unit=='aboutme-job@idempotency-expiry-sweep':
        require('path=/usr/local/lib/aboutme/job-run.sh' in command)
        require(re.search(r'argv\[\]=/usr/local/lib/aboutme/job-run.sh idempotency-expiry-sweep(?:[ ;]|$)',command))
        require('--cgroups' not in command and '--cgroup-parent' not in command)
        return
    require(command.count('--cgroups=split')==1 and command.count('--cgroups=')==1)
    require('--cgroup-parent' not in command and re.search(r'--name='+re.escape(UNITS[unit])+r'(?:[ ;]|$)',command))


def hook(operation,unit):
    require(os.geteuid()==0 and operation in ('baseline','final') and unit in UNITS)
    anchor()
    invocation=os.environ['INVOCATION_ID']; require(re.fullmatch('[0-9a-f]{32}',invocation))
    group=capture(['systemctl','show','--property=ControlGroup','--value',unit+'.service']).strip()
    own=pathlib.Path('/proc/self/cgroup').read_text().splitlines()
    require(any(line in ('0::'+group,'0::'+group+'/.control') for line in own))
    require(group.startswith('/system.slice/') and '..' not in group)
    command=capture(['systemctl','show','--property=ExecStart','--value',unit+'.service'])
    validate_command(unit,command)
    job=unit=='aboutme-job@idempotency-expiry-sweep'
    if job:
        from protocol import digest
        installed=pathlib.Path('/usr/local/lib/aboutme/job-run.sh')
        bundles=pathlib.Path('/run/aboutme-rehearsal')
        require(digest(installed) in {digest(bundles/name/'host/job-run.sh') for name in ('baseline','candidate')})
    record={'unit':unit,'invocation':invocation,'group':group,'payload_contained':int(not job),
            'snapshot':snapshot(pathlib.Path('/sys/fs/cgroup'+group))}
    if job:
        from job_accounting import add_envelope
        add_envelope(record)
    path=ROOT/(unit+'-'+invocation+'.'+operation+'.json')
    safe_write(path,json.dumps(record,separators=(',',':')).encode())
    if operation=='final': final_record(unit,invocation)


def install_hooks(effect):
    options=capture(['findmnt','-n','-o','OPTIONS','-T','/sys/fs/cgroup']).strip().split(',')
    require('memory_localevents' not in options)
    ROOT.mkdir(mode=0o700,exist_ok=True)
    require(not ROOT.is_symlink() and ROOT.stat().st_uid==0 and ROOT.stat().st_mode&0o777==0o700)
    script=str(pathlib.Path(__file__).resolve())
    for unit in UNITS:
        directory=pathlib.Path('/run/systemd/system')/(unit+'.service.d')
        directory.mkdir(mode=0o755,exist_ok=True)
        require(not directory.is_symlink() and directory.stat().st_uid==0)
        path=directory/'90-rehearsal-accounting.conf'
        require(not path.exists() and not path.is_symlink())
        safe_write(path,('[Service]\nMemoryAccounting=yes\nExecStartPre=/usr/bin/python3 -Es '+script+' baseline '+unit+
                         '\nExecStopPost=/usr/bin/python3 -Es '+script+' final '+unit+'\n').encode())
    from job_accounting import install_observer
    install_observer()
    effect(['systemctl','daemon-reload'],30)


if __name__=='__main__':
    try:
        require(len(sys.argv)==3); hook(sys.argv[1],sys.argv[2])
    except BaseException: sys.exit(1)
