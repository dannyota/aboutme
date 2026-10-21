"""One fixed, synthetic host rehearsal. Cloud writes require its saved create plan."""
import datetime
import json
import os
import pathlib
import re
import secrets
import shutil
import signal
import stat
import sys
import tempfile
import time
from allowance import validate_allowance
from api import Client, Load
from cleanup import cleanup, cleanup_attempt
from common import Runner, Store, safe_write, runtime_directory
from fixed_api import dns_records
from images import bundles, resolve_images, fixture_artifact
from protocol import BASELINE, TAG, MAX_PRICE, ZONE, FLAVOR, IMAGE, VOLUME_TYPE, digest, require, resource_name, validate_config, validate_plan
from resources import Cloud, field
from transport import Remote

REPOSITORY=pathlib.Path(__file__).resolve().parents[3]


def release(runner,tag):
    require(TAG.fullmatch(tag))
    return runner.call(['bash',str(REPOSITORY/'scripts/release-eligibility.sh'),'--deploy-checkout',tag],
                       timeout=180,cwd=REPOSITORY).decode().strip()


def cloud_init(runner,manifest,work,runtime):
    key=runtime/'hostkey'
    runner.call(['ssh-keygen','-q','-t','ed25519','-N','','-C',manifest['name'],'-f',str(key)],timeout=10)
    public=key.with_suffix('.pub').read_text().strip()
    safe_write(work/'known_hosts',(manifest['name']+' '+' '.join(public.split()[:2])+'\n').encode())
    anchor={name:manifest[name] for name in ('run_id','commit','baseline_commit','bundle_sha256','baseline_bundle_sha256')}
    # JSON quoted scalars are valid YAML and preserve private key newlines.
    data={'hostname':manifest['name'],'preserve_hostname':False,'users':[{
        'name':'aboutme-admin','groups':['sudo'],'shell':'/bin/bash','sudo':'ALL=(ALL) NOPASSWD:ALL',
        'lock_passwd':True,'ssh_authorized_keys':[manifest['config']['admin_public_key']]}],
        'disable_root':True,'ssh_pwauth':False,'ssh_deletekeys':True,'ssh_genkeytypes':[],
        'ssh_keys':{'ed25519_public':public,'ed25519_private':key.read_text()},
        'package_update':True,'packages':['podman','nftables','jq','rsync','curl','ca-certificates',
                                         'unattended-upgrades','systemd-zram-generator'],
        'write_files':[
            {'path':'/etc/aboutme-rehearsal.json','permissions':'0600','content':json.dumps(anchor)},
            {'path':'/etc/ssh/sshd_config','permissions':'0644','content':
             'Port 22922\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin no\n'
             'AllowUsers aboutme-admin\nPubkeyAcceptedAlgorithms ssh-ed25519\nUsePAM yes\n'
             'Subsystem sftp /usr/lib/openssh/sftp-server\n'},
            {'path':'/etc/nftables.d/aboutme-rehearsal-egress.nft','permissions':'0600','content':
             'table inet rehearsal_egress { chain output { type filter hook output priority -100; policy accept; '
             'ip daddr != 127.0.0.0/8 tcp dport { 22, 22922, 5432, 465, 587, 25 } reject; } }\n'}],
        'runcmd':[
            ['sh','-c','sshd -t && systemctl daemon-reload && if systemctl is-enabled --quiet ssh.socket; then systemctl stop ssh.service && systemctl restart ssh.socket; else systemctl restart ssh.service; fi'],
            ['nft','-f','/etc/nftables.d/aboutme-rehearsal-egress.nft']]}
    try: return '#cloud-config\n'+json.dumps(data)
    finally: key.unlink(); key.with_suffix('.pub').unlink()


def check_new_names(cloud):
    name=cloud.m['name']
    for service,operation in (('compute','list-servers'),('volume','list-volumes'),('network','list-security-groups')):
        require(not any(field(row,'name','Name') in (name,name+'-data') for row in cloud.pages(service,operation)))
    require(not any(row['name']==cloud.m['origin'][8:] for row in dns_records(cloud.m,cloud.runner)))
    buckets=cloud.storage('list-buckets',read=True)['Items']
    require(not any(field(row,'name','Name') in (name+'-media',name+'-backups') for row in buckets))
    for role in ('media','backups'):
        result=cloud.vng('iam','list-service-accounts','--name',name+'-'+role,'--page','0','--size','100',read=True)
        require(not result['Items'])
    flavors=cloud.vng('compute','list-flavors','--zone-id',ZONE,read=True)['Items']
    found=[row for row in flavors if field(row,'flavorId','FlavorID')==FLAVOR]
    require(len(found)==1 and field(found[0],'cpu','CPU')==2 and field(found[0],'memory','Memory')==4)


def plan(runner,store,tag):
    require(not store.path.exists() and not store.path.is_symlink())
    configuration=validate_config(store.read_file('operator.json'))
    commit=release(runner,tag); require(re.fullmatch('[0-9a-f]{40}',commit))
    require(runner.call(['git','rev-parse','HEAD'],cwd=REPOSITORY).decode().strip()==commit)
    run_id=secrets.token_hex(8); name=resource_name(run_id)
    work=store.root/run_id; work.mkdir(mode=0o700)
    baseline_hash,candidate_hash=bundles(runner,REPOSITORY,work,commit)
    manifest={'schema':1,'protocol':1,'run_id':run_id,'name':name,'tag':tag,'commit':commit,
              'baseline_tag':'v0.8.9','baseline_commit':BASELINE,'baseline_bundle_sha256':baseline_hash,
              'bundle_sha256':candidate_hash,'config':configuration,'origin':'https://cs-rehearsal-'+run_id+'.aboutme.vn',
              'provider':{'zone':ZONE,'flavor':FLAVOR,'image':IMAGE,'volume_type':VOLUME_TYPE},
              'baseline_images':resolve_images(runner,REPOSITORY,'v0.8.9',BASELINE),
              'images':resolve_images(runner,REPOSITORY,tag,commit),'resources':{},'quote':MAX_PRICE,
              'fixtures_sha256':'0'*64,'plan_sha256':'0'*64,'state':'planned','created_at':None,'deadline':None}
    manifest['fixtures_sha256']=fixture_artifact(runner,manifest,work/'artifact')
    runtime=runtime_directory(runner,manifest)
    store.save(manifest)
    cloud=Cloud(runner,store,manifest,REPOSITORY)
    check_new_names(cloud)
    cloud.initialize()
    cloud.tofu('plan','-input=false','-out='+str(work/'plan'),cloud_init=cloud_init(runner,manifest,work,runtime))
    validate_plan(json.loads(cloud.tofu('show','-json',str(work/'plan'))),run_id)
    output=runner.call(['bash',str(REPOSITORY/'deploy/vn/scripts/quote.sh'),'--plan',str(work/'plan'),
                        '--max-price',str(MAX_PRICE)],cwd=cloud.root,env=cloud.environment(),timeout=180).decode()
    prices=re.findall(r'^total: ([0-9]+(?:\.[0-9]+)?) VND a month',output,re.MULTILINE)
    require(len(prices)==1)
    from decimal import Decimal, ROUND_CEILING
    price=int(Decimal(prices[0]).to_integral_value(rounding=ROUND_CEILING)); require(0<price<=MAX_PRICE)
    manifest['quote']=price; manifest['plan_sha256']=digest(work/'plan'); store.save(manifest)
    print('PASS plan')
    print(json.dumps({'candidate_tag':tag,'candidate_commit':commit,'monthly_quote_vnd':price,'protocol':1},separators=(',',':')))


def allowance(store,manifest):
    value=store.read_file('storage-allowance.json')
    evidence=store.root/('storage-allowance-api.json' if value['source_kind']=='api' else 'storage-allowance.png')
    require(evidence.is_file() and not evidence.is_symlink() and evidence.stat().st_uid==os.getuid())
    require(stat.S_IMODE(evidence.stat().st_mode)==0o600 and evidence.stat().st_size<=8388608)
    return validate_allowance(value,manifest,evidence)


def workloads(cloud,remote,client):
    measurements=cloud.work/'measurements'; measurements.mkdir(mode=0o700)
    summaries=[]
    def pause(seconds):
        time.sleep(cloud.runner.bound(seconds))
        cloud.runner.bound(1)
    for repeat in (1,2):
        for scenario in ('serving','print','job'):
            for name in ('minimal','full','maximum'):
                remote.operation('verify',{})
                if scenario=='job': remote.operation('seed',{'action':'idempotency'})
                boundary=remote.operation('sample',{})
                safe_write(measurements/(str(repeat)+'-'+scenario+'-'+name+'-boundary.json'),json.dumps(boundary).encode())
                with Load(client,name,printing=scenario=='print') as load:
                    summary,_=remote.measure(scenario,measurements/(str(repeat)+'-'+scenario+'-'+name+'.jsonl'))
                    end=time.monotonic(); latency=load.summary(summary.pop('warmup_end'),end)
                if scenario=='job':
                    remote.operation('seed',{'action':'job-check'}); client.verify(name)
                summaries.append({'repeat':repeat,'scenario':scenario,'corpus':name,'resources':summary,'latency':latency})
                print('PASS measured-'+scenario+'-'+name)
                pause(60)
        with Load(client,'full',maintenance=True) as load:
            summary,effects=remote.measure('deploy',measurements/(str(repeat)+'-deploy.jsonl'))
        summary.pop('warmup_end')
        starts=[when for step,when in effects if step=='maintenance-up']
        stops=[when for step,when in effects if step=='app-down']
        require(len(starts)==len(stops)==1 and starts[0]<stops[0])
        require(any(starts[0]<=when<=stops[0] and kind in ('api','public') for when,kind,_ in load.results))
        require(any(kind=='maintenance' for _,kind,_ in load.results))
        require({'db-setup','migrate','app-up','maintenance-down'}<=set(step for step,_ in effects))
        summaries.append({'repeat':repeat,'scenario':'deploy','resources':summary})
        for name in ('minimal','full','maximum'): client.verify(name)
        remote.operation('verify',{})
        pause(60)
    safe_write(cloud.work/'results.json',json.dumps({'protocol':1,'candidate_tag':cloud.m['tag'],
       'candidate_commit':cloud.m['commit'],'images':cloud.m['images'],'fixtures_sha256':cloud.m['fixtures_sha256'],
       'summaries':summaries},separators=(',',':')).encode())


def run(runner,store,tag):
    manifest=store.load(); require(manifest['state']=='planned' and manifest['tag']==tag)
    require(manifest['config']==validate_config(store.read_file('operator.json')))
    require(release(runner,tag)==manifest['commit'])
    cloud=Cloud(runner,store,manifest,REPOSITORY)
    require(digest(cloud.work/'plan')==manifest['plan_sha256'])
    for name,key in (('baseline','baseline_bundle_sha256'),('candidate','bundle_sha256')):
        require(digest(cloud.work/(name+'.tar'))==manifest[key])
    require(digest(cloud.work/'artifact/fixtures/manifest.json')==manifest['fixtures_sha256'])
    require(digest(cloud.work/'artifact/SHA256SUMS')==manifest['artifact_sha256'])
    for line in (cloud.work/'artifact/SHA256SUMS').read_text().splitlines():
        expected,name=line.split('  ',1)
        require(name in ('render-budget','allowance-adapter','fixtures/minimal.json','fixtures/full.json','fixtures/maximum.json','fixtures/full-photo.png','fixtures/manifest.json'))
        require(digest(cloud.work/'artifact'/name)==expected)
    runtime_directory(runner,manifest)
    check_new_names(cloud); cloud.initialize()
    adapter=cloud.work/'artifact/allowance-adapter'; adapter.chmod(0o700)
    response=runner.call([str(adapter)],json.dumps({'run_id':manifest['run_id'],'project_id':manifest['config']['storage_project_id'],
                           'plan_sha256':manifest['plan_sha256']}).encode(),timeout=35)
    require(re.fullmatch(rb'VALID allowance\nUNUSED [0-9]+\n',response))
    allowance(store,manifest)
    accepted=False
    try:
        cloud.apply()
        cloud.create_storage()
        remote=Remote(cloud); remote.stage()
        remote.operation('bootstrap',{'stage':'install'})
        for role in ('media','backups'):
            file=cloud.make_key(role,'workload')
            remote.import_key(role,file)
        cold=remote.operation('bootstrap',{'stage':'deploy'})
        require(cold.get('cold_start')==cold.get('valid')==1)
        safe_write(cloud.work/'cold-start.json',json.dumps(cold,separators=(',',':')).encode())
        remote.operation('verify',{})
        manifest['state']='running'; store.save(manifest)
        client=Client(manifest,cloud.work/'artifact/fixtures',cloud.runner)
        client.register(remote.operation); client.seed()
        manifest['setup_passed']=True; store.save(manifest); print('PASS setup')
        workloads(cloud,remote,client)
        manifest['measurement_passed']=True; store.save(manifest); accepted=True
    except BaseException:
        manifest['failed']=True; store.save(manifest)
        raise
    finally:
        try: cleanup(cloud)
        except BaseException:
            manifest['state']='unresolved'; manifest['failed']=True; store.save(manifest)
            print('FAIL cleanup',file=sys.stderr)
            raise
    require(accepted and not manifest.get('failed'))
    print('PASS rehearsal')


def main():
    try:
        require(len(sys.argv) in (2,3))
        operation=sys.argv[1]
        require(operation in ('plan','run','cleanup') and len(sys.argv)==(2 if operation=='cleanup' else 3))
        for tool in ('tofu','vngcloud','aws','ssh','ssh-keygen','gh','bash','findmnt'):
            require(shutil.which(tool) is not None)
        runner=Runner(time.time()+1800 if operation=='cleanup' else None)
        require(json.loads(runner.call(['tofu','version','-json']))['terraform_version']=='1.12.6')
        store=Store(os.environ['ABOUTME_REHEARSAL_SCRATCH'],REPOSITORY)
        if operation=='plan': plan(runner,store,sys.argv[2])
        elif operation=='run': run(runner,store,sys.argv[2])
        else:
            cloud=Cloud(runner,store,store.load(),REPOSITORY)
            cleanup_attempt(cloud,explicit=True,initialize=True)
    except BaseException:
        print('FAIL rehearsal-operation',file=sys.stderr)
        return 1
    return 0


if __name__=='__main__':
    signal.signal(signal.SIGTERM,lambda *_:sys.exit(1))
    signal.signal(signal.SIGINT,lambda *_:sys.exit(1))
    sys.exit(main())
