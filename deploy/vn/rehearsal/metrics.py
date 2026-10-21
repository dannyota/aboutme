"""Bounded simultaneous host measurements with no raw process or journal output."""
import json
import os
import pathlib
import re
import subprocess
import time
from protocol import require, check_series

UNITS=('aboutme-server','aboutme-web','aboutme-caddy','aboutme-maintenance',
       'postgresql@18-main','crowdsec','crowdsec-firewall-bouncer','aboutme-migrate',
       'aboutme-db-setup','aboutme-job@idempotency-expiry-sweep',
       'aboutme-crowdsec-retention','aboutme-crowdsec-retention-startup','aboutme-crowdsec-vacuum')
CONTAINERS=('aboutme-server','aboutme-web','aboutme-caddy','aboutme-maintenance',
            'aboutme-migrate','aboutme-db-setup','aboutme-job-idempotency-expiry-sweep')


def numeric_file(path):
    value=pathlib.Path(path).read_text().strip()
    require(re.fullmatch('[0-9]{1,18}',value))
    return int(value)


def parse_counters(text):
    values={}
    for line in text.splitlines():
        fields=line.split()
        require(len(fields)>=2 and fields[1].isdigit())
        key=fields[0].rstrip(':'); require(key not in values)
        values[key]=int(fields[1])
    return values


def counters(path):
    return parse_counters(pathlib.Path(path).read_text())


def parse_memory(text):
    values=parse_counters(text)
    require({'MemTotal','MemAvailable','MemFree'}<=set(values))
    total=values['MemTotal']*1024; available=values['MemAvailable']*1024
    require(total>0 and total>=available>=0 and values['MemFree']*1024<=total)
    return {'total':total,'available':available,'free':values['MemFree']*1024,'used':total-available}


def parse_pressure(text):
    result={}
    for line in text.splitlines():
        fields=line.split(); require(fields[0] in ('some','full') and fields[0] not in result)
        values=dict(x.split('=') for x in fields[1:])
        require(set(values)=={'avg10','avg60','avg300','total'})
        require(values['total'].isdigit())
        result[fields[0]]={k:float(v) if k.startswith('avg') else int(v) for k,v in values.items()}
        require(all(value>=0 for value in result[fields[0]].values()))
    require(set(result)=={'some','full'})
    return result


def pressure():
    return parse_pressure(pathlib.Path('/proc/pressure/memory').read_text())


def capture(args, timeout=3):
    completed=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=timeout,check=True,
                             env={'PATH':'/usr/sbin:/usr/bin:/sbin:/bin','LC_ALL':'C','HOME':'/root'})
    require(len(completed.stdout)<65536)
    return completed.stdout.decode()


class Collector:
    def __init__(self):
        self.started=time.monotonic()
        self.wall=time.time()
        self.events={}
        self.last_groups={}
        self.vm=counters('/proc/vmstat')
        self.kernel_initial=self.kernel_oom()
        require(self.kernel_initial==0 and self.vm['oom_kill']==0)
        self.group_paths={}
        self.container_paths={}
        for unit in UNITS:
            group=capture(['systemctl','show','--property=ControlGroup','--value',unit+'.service']).strip()
            if group:
                require(group.startswith('/system.slice/') and '..' not in group)
                self.group_paths[unit]=pathlib.Path('/sys/fs/cgroup'+group)
            else:
                parent='/sys/fs/cgroup/system.slice/'
                if '@' in unit:
                    parent+=unit.split('@')[0]+'.slice/'
                self.group_paths[unit]=pathlib.Path(parent+unit+'.service')

    def kernel_oom(self):
        result=capture(['journalctl','--boot','--kernel','--quiet','--no-pager','--output=cat',
                        '--grep=Out of memory:|Killed process [0-9]+|oom-kill:'],10)
        return sum(bool(re.search(r'Out of memory:|Killed process [0-9]+|oom-kill:',line))
                   for line in result.splitlines())

    def containers(self):
        names=capture(['podman','ps','--format','{{.Names}}']).splitlines()
        names=[name for name in names if name in CONTAINERS]
        paths={}
        if names:
            raw=capture(['podman','inspect','--format','{{.Name}} {{.State.Pid}}',*names])
            for line in raw.splitlines():
                name,pid=line.split(); name=name.removeprefix('/')
                require(name in names and pid.isdigit() and int(pid)>0)
                records=pathlib.Path('/proc/'+pid+'/cgroup').read_text().splitlines()
                matches=[record.removeprefix('0::') for record in records if record.startswith('0::')]
                require(len(matches)==1 and matches[0].startswith('/') and '..' not in matches[0])
                paths['container-'+name.removeprefix('aboutme-')]=pathlib.Path('/sys/fs/cgroup'+matches[0])
        self.container_paths=paths

    def cgroups(self):
        self.containers()
        result={}
        for name in CONTAINERS:
            result['container-'+name.removeprefix('aboutme-')]={'present':0}
        for unit,path in dict(self.group_paths,**self.container_paths).items():
            if not path.exists():
                result[unit]={'present':0}
                continue
            inode=path.stat().st_ino
            current=numeric_file(path/'memory.current'); peak=numeric_file(path/'memory.peak')
            values=counters(path/'memory.events')
            require({'low','high','max','oom','oom_kill'}<=set(values))
            key=(unit,inode)
            base=self.events.setdefault(key,values)
            delta={k:values[k]-base[k] for k in ('low','high','max','oom','oom_kill')}
            require(all(v>=0 for v in delta.values()))
            require(values['oom']==0 and values['oom_kill']==0)
            row={'present':1,'identity':inode,'current':current,'peak':peak,'events':delta}
            result[unit]=row
            self.last_groups[key]=row
        return result

    def sample(self):
        mem=parse_memory(pathlib.Path('/proc/meminfo').read_text()); vm=counters('/proc/vmstat'); psi=pressure()
        total=mem['total']; available=mem['available']
        require(total>available>=268435456)
        require(vm['oom_kill']==self.vm['oom_kill'])
        require(vm['pswpin']>=self.vm['pswpin'] and vm['pswpout']>=self.vm['pswpout'])
        zram=0
        for path in pathlib.Path('/sys/block').glob('zram*/mm_stat'):
            values=path.read_text().split(); require(len(values)>=3)
            zram+=int(values[2])
        require(capture(['findmnt','-n','-o','FSTYPE','-T','/run/aboutme/caddy-log']).strip()=='tmpfs')
        fs=os.statvfs('/run/aboutme/caddy-log')
        capacity=fs.f_blocks*fs.f_frsize; used=(fs.f_blocks-fs.f_bfree)*fs.f_frsize
        require(capacity==67108864 and used<capacity and fs.f_bavail>0)
        connections=int(capture(['runuser','-u','postgres','--','psql','-XAtq','-v','ON_ERROR_STOP=1',
                                 '--host=/run/postgresql','--username=postgres','--dbname=postgres','--command','SELECT count(*) FROM pg_stat_activity;']).strip())
        require(0<connections<=40)
        backup_count=backup_rss=0
        for proc in pathlib.Path('/proc').iterdir():
            if not proc.name.isdigit(): continue
            try:
                if proc.joinpath('comm').read_text().strip()!='pgbackrest': continue
                backup_rss+=int(proc.joinpath('statm').read_text().split()[1])*os.sysconf('SC_PAGE_SIZE')
                backup_count+=1
            except FileNotFoundError:
                continue
        now=time.monotonic()
        require(abs((time.time()-self.wall)-(now-self.started))<1)
        return {'t':round(now-self.started,6),'total':total,'available':available,'free':mem['free'],
                'used':total-available,'some':psi['some']['total'],'full':psi['full']['total'],'psi':psi,
                'zram_physical':zram,'swap_in':vm['pswpin']-self.vm['pswpin'],
                'swap_out':vm['pswpout']-self.vm['pswpout'],'kernel_oom':0,
                'tmpfs_used':used,'tmpfs_capacity':capacity,'connections':connections,
                'backup_count':backup_count,'backup_rss':backup_rss,'cgroups':self.cgroups()}

    def collect(self,duration,interval):
        require(interval in (0.1,1) and 0<duration<=1800)
        rows=[]; deadline=time.monotonic()+duration; next_sample=time.monotonic()
        while True:
            time.sleep(max(0,next_sample-time.monotonic()))
            row=self.sample(); rows.append(row)
            print(json.dumps(row,separators=(',',':')),flush=True)
            if time.monotonic()>=deadline: break
            next_sample+=interval
        check_series(rows,interval,max(2,int(duration/interval)))
        require(self.kernel_oom()==self.kernel_initial)
        print(json.dumps({'valid':1,'samples':len(rows),'combined_peak':max(x['used'] for x in rows),
                          'minimum_available':min(x['available'] for x in rows),
                          'retired_cgroup_peaks':[value for value in self.last_groups.values()]},
                         separators=(',',':')),flush=True)
