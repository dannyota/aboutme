#!/usr/bin/env bash
# Lifecycle assertions run only with synthetic provider documents.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [[ ${1:-} == linux-accounting ]]; then
  [[ $# == 1 && ${GITHUB_ACTIONS:-} == true && $(id -u) == 0 ]] || exit 2
  exec python3 -I - "$here/../rehearsal" <<'LINUX'
import json, os, pathlib, subprocess, sys, tempfile, time
sys.path.insert(0,sys.argv[1])
from lifecycle import finish, snapshot
from protocol import require
unit='aboutme-rehearsal-accounting-test.service'
with tempfile.TemporaryDirectory(prefix='aboutme-accounting-') as directory:
    root=pathlib.Path(directory); root.chmod(0o700)
    hook=root/'hook.py'
    hook.write_text("""import json,pathlib,sys
sys.path.insert(0,"""+repr(sys.argv[1])+""")
from lifecycle import snapshot
from protocol import require
root=pathlib.Path(__file__).parent
lines=pathlib.Path('/proc/self/cgroup').read_text().splitlines()
group=[line[3:].removesuffix('/.control') for line in lines if line.startswith('0::')]
require(len(group)==1 and group[0].endswith('/aboutme-rehearsal-accounting-test.service'))
row=snapshot(pathlib.Path('/sys/fs/cgroup'+group[0]))
(root/(sys.argv[1]+'.json')).write_text(json.dumps(row))
""")
    try:
        for late in (False,True):
            for name in ('baseline.json','final.json'): root.joinpath(name).unlink(missing_ok=True)
            program='import time; time.sleep('+('0.75' if late else '0')+'); allocation=bytearray(64*1024*1024)'
            command=['systemd-run','--quiet','--wait','--collect','--unit='+unit,
                     '--property=Type=oneshot','--property=Delegate=yes','--property=MemoryAccounting=yes','--property=MemoryMax=128M',
                     '--property=MemorySwapMax=0','--property=TimeoutStartSec=20',
                     '--property=ExecStartPre=/usr/bin/python3 -I '+str(hook)+' baseline',
                     '--property=ExecStopPost=/usr/bin/python3 -I '+str(hook)+' final',
                     '/usr/bin/python3','-I','-c',program]
            child=subprocess.Popen(command,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            try:
                deadline=time.monotonic()+5
                while not root.joinpath('baseline.json').exists():
                    require(time.monotonic()<deadline); time.sleep(0.01)
                periodic=None
                if late: periodic=snapshot(pathlib.Path('/sys/fs/cgroup/system.slice')/unit)
                require(child.wait(timeout=25)==0)
                baseline=json.loads(root.joinpath('baseline.json').read_text())
                final=json.loads(root.joinpath('final.json').read_text())
                complete=finish(baseline,final)
                require(complete['peak']>=64*1024*1024 and complete['final']==1)
                if late: require(complete['peak']>periodic['peak'])
            finally:
                if child.poll() is None: child.kill(); child.wait(timeout=5)
        # The unchanged job uses default Podman placement, outside its wrapper unit.
        import tarfile
        from common import safe_write
        job_unit='aboutme-rehearsal-job-accounting-test'
        image='localhost/aboutme-rehearsal-accounting:temporary'
        require(subprocess.run(['podman','image','exists',image],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10).returncode==1)
        require(subprocess.run(['podman','container','exists','aboutme-job-idempotency-expiry-sweep'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10).returncode==1)
        job=root/'job'; job.mkdir(mode=0o700)
        source=job/'allocation.go'
        source.write_text('package main\nimport("os";"runtime";"time")\nfunc main(){if len(os.Args)!=2 || os.Args[1]!="idempotency-expiry-sweep" {os.Exit(2)}; time.Sleep(750*time.Millisecond); data:=make([]byte,64*1024*1024); for i:=range data {data[i]=1}; runtime.KeepAlive(data)}\n')
        binary=job/'server'
        environment=dict(os.environ,GOWORK='off',GO111MODULE='off',GOTOOLCHAIN='local',CGO_ENABLED='0')
        subprocess.run(['go','build','-o',str(binary),str(source)],env=environment,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=90)
        archive=job/'image.tar'
        with tarfile.open(archive,'w') as stream: stream.add(binary,arcname='usr/local/bin/server')
        job_hook=job/'hook.py'
        job_hook.write_text("""import json,os,pathlib,subprocess,sys
sys.path.insert(0,"""+repr(sys.argv[1])+""")
from common import safe_write
from lifecycle import snapshot,finish
from job_accounting import add_envelope,observe,configuration,completed
from metrics import capture
from protocol import require
root=pathlib.Path(__file__).parent; unit='aboutme-rehearsal-job-accounting-test'
if sys.argv[1]=='observe':
    raw=sys.stdin.buffer.read(65537); require(len(raw)<=65536)
    state=json.loads(raw); observe(state,configuration(state),root,unit)
else:
    invocation=os.environ['INVOCATION_ID']
    group=capture(['systemctl','show','--property=ControlGroup','--value',unit+'.service']).strip()
    record={'unit':unit,'invocation':invocation,'group':group,'payload_contained':0,
            'snapshot':snapshot(pathlib.Path('/sys/fs/cgroup'+group))}
    add_envelope(record)
    safe_write(root/(unit+'-'+invocation+'.'+sys.argv[1]+'.json'),json.dumps(record).encode())
    if sys.argv[1]=='final':
        base=json.loads((root/(unit+'-'+invocation+'.baseline.json')).read_text())
        result={'wrapper':finish(base['snapshot'],record['snapshot']),
                'shared_payload_envelope':completed(base,record,root)}
        safe_write(root/'result.json',json.dumps(result).encode())
""")
        hook_directory=pathlib.Path('/usr/share/containers/oci/hooks.d'); hook_directory.mkdir(parents=True,exist_ok=True)
        hook_config=hook_directory/'90-aboutme-rehearsal-job-fixture.json'
        require(not hook_config.exists() and not hook_config.is_symlink())
        hook_value={'version':'1.0.0','hook':{'path':'/usr/bin/timeout','args':[
            '/usr/bin/timeout','--signal=TERM','--kill-after=1s','4s','/usr/bin/python3','-I',str(job_hook),'observe'],
            'timeout':5},'when':{'commands':['^/usr/local/bin/server$']},'stages':['createRuntime']}
        try:
            subprocess.run(['podman','import',str(archive),image],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=60)
            safe_write(hook_config,json.dumps(hook_value).encode())
            subprocess.run(['systemctl','start','machine.slice'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
            command=['systemd-run','--quiet','--wait','--collect','--unit='+job_unit,
                     '--property=Type=oneshot','--property=MemoryAccounting=yes','--property=TimeoutStartSec=30',
                     '--property=ExecStartPre=/usr/bin/python3 -I '+str(job_hook)+' baseline',
                     '--property=ExecStopPost=/usr/bin/python3 -I '+str(job_hook)+' final',
                     '/usr/bin/podman','run','--rm','--name','aboutme-job-idempotency-expiry-sweep',
                     '--network','none','--entrypoint','/usr/local/bin/server','--memory','256m',
                     '--log-driver','journald',image,'idempotency-expiry-sweep']
            subprocess.run(command,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=40)
            result=json.loads(job.joinpath('result.json').read_text())
            require(result['shared_payload_envelope']['shared_envelope']==1)
            require(result['shared_payload_envelope']['peak']>=64*1024*1024)
            require(len(list(job.glob('*.observed.json')))==1)
        finally:
            subprocess.run(['systemctl','stop',job_unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
            subprocess.run(['systemctl','reset-failed',job_unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
            if hook_config.exists(): hook_config.unlink()
            subprocess.run(['podman','rm','--force','aboutme-job-idempotency-expiry-sweep'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
            subprocess.run(['podman','rmi',image],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=15)
    finally:
        subprocess.run(['systemctl','stop',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
        subprocess.run(['systemctl','reset-failed',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
print('PASS complete-cgroup-lifecycles')
LINUX
fi
[[ $# == 0 ]] || exit 2
python3 -I - "$here/../rehearsal" <<'PY'
import copy, importlib.util, pathlib, sys, unittest, tempfile, json, os, signal, subprocess, time
from unittest import mock
sys.path.insert(0, sys.argv[1])
from protocol import validate_manifest, validate_plan, resource_name, validate_config, check_series
from allowance import bound
from object_cleanup import Objects
from resources import Cloud
from common import Store, Runner
from cleanup import retire_bucket, cleanup_attempt
import fixed_api
from transport import validate_numeric, pipe_rows
from host import candidate_cutover
class Lifecycle(unittest.TestCase):
    def test_rounded_allowance_is_conservative(self):
        row={'display':'10.0','unit':'GB','precision':1,'rounded':True}
        self.assertEqual(bound(row,True),9900000000)
        self.assertEqual(bound(row,False),10844792423)
        with self.assertRaises(ValueError): bound(dict(row,unit='unknown'),True)
    def test_production_origin_refused(self):
        with self.assertRaises(ValueError):
            validate_manifest({'origin':'https://aboutme.vn'})
    def test_numeric_artifact_rejects_unknown_labels(self):
        validate_numeric({'t':1,'available':536870912})
        for value in ({'address':123},{'t':float('nan')},{'t':float('inf')}):
            with self.assertRaises(ValueError): validate_numeric(value)
    def test_cutover_uses_staging_owner_and_quiesces_before_render(self):
        calls=[]
        def effect(arguments,*args,**kwargs):
            calls.append(arguments)
            if arguments[:2]==['mktemp','-d']: return b'/run/aboutme/crowdsec-bundle.ABC12345\n'
            if 'ABOUTME_INSTALL_ONLY=crowdsec-stage' in arguments: return b'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
            return b''
        manifest={'tag':'v0.8.10','bundle_sha256':'a'*64,'images':dict(server='s',web='w',caddy='c')}
        with mock.patch('host.effect',side_effect=effect), mock.patch('host.digest',return_value='a'*64):
            candidate_cutover(manifest)
        stages=[row for row in calls if 'ABOUTME_INSTALL_ONLY=crowdsec-stage' in row]
        self.assertEqual(stages[0][-2:],['v0.8.10','a'*64])
        self.assertFalse(any(row[0].endswith('fence.sh') and row[1]=='lock' for row in calls))
        self.assertLess(next(i for i,row in enumerate(calls) if 'quiesce' in row),
                        next(i for i,row in enumerate(calls) if 'cutover-render' in row))
        self.assertEqual([row for row in calls if 'transition' in row][0][-2:],
                         ['/run/aboutme/crowdsec-bundle.ABC12345','a'*64])
    def test_run_names_are_exact(self):
        self.assertEqual(resource_name('0123456789abcdef'), 'aboutme-cs-rehearsal-0123456789abcdef')
        for bad in ('prod', '../0123456789abcdef', '0123456789abcdef-other'):
            with self.assertRaises(ValueError): resource_name(bad)
    def test_delete_and_import_plan_refused(self):
        for actions in (['delete'], ['delete','create'], ['update'], ['no-op']):
            with self.assertRaises(ValueError):
                validate_plan({'resource_changes':[{'type':'vngcloud_vserver_server',
                    'address':'vngcloud_vserver_server.main', 'change':{'actions':actions,'after':{}}}]},
                    '0123456789abcdef')
    def test_unowned_cleanup_refused(self):
        with self.assertRaises(ValueError):
            validate_plan({'resource_changes':[{'type':'vngcloud_vserver_network',
                'address':'vngcloud_vserver_network.production','change':{'actions':['create'],
                'after':{'name':'aboutme-prod'}}}]}, '0123456789abcdef')
    def test_headroom_failure_is_not_averaged(self):
        with self.assertRaises(ValueError):
            check_series([{'t':0,'available':536870912,'some':0,'full':0},
                          {'t':1,'available':268435455,'some':0,'full':0}],1,2)
    def test_pressure_and_counter_reset_fail(self):
        rows=[{'t':i,'available':536870912,'some':i*100000,'full':0} for i in range(12)]
        with self.assertRaises(ValueError): check_series(rows,1,12)
        rows[1]['some']=-1
        with self.assertRaises(ValueError): check_series(rows,1,12)
    def test_missing_samples_fail(self):
        with self.assertRaises(ValueError): check_series([],1,120)
    def test_normal_series_passes(self):
        rows=[{'t':i,'available':536870912,'some':i*10,'full':0} for i in range(121)]
        self.assertEqual(check_series(rows,1,120)['samples'],121)
    def test_gap_fails(self):
        rows=[{'t':0,'available':536870912,'some':0,'full':0},
              {'t':3,'available':536870912,'some':0,'full':0}]
        with self.assertRaises(ValueError): check_series(rows,1,2)
class Deadlines(unittest.TestCase):
    def test_expired_deadline_refuses_child_before_launch(self):
        runner=Runner(100)
        with mock.patch('common.time.time',return_value=101),mock.patch('common.subprocess.Popen') as launch:
            with self.assertRaises(ValueError): runner.call(['fixed-child'])
        launch.assert_not_called()
    def test_dns_pages_share_absolute_deadline(self):
        manifest={'config':{'zone_id':'a'*32}}
        runner=Runner(100)
        responses=[{'success':True,'result':{'name':'aboutme.vn'}},
                   {'success':True,'result':[],'result_info':{'page':1,'total_pages':2}}]
        def request(*args,**kwargs):
            self.assertIs(kwargs['runner'],runner)
            if responses: return responses.pop(0)
            with mock.patch('common.time.time',return_value=101): runner.bound(30)
        with mock.patch.dict(os.environ,{'CLOUDFLARE_API_TOKEN':'synthetic'}),mock.patch('fixed_api.request',side_effect=request):
            with self.assertRaises(ValueError): fixed_api.dns_records(manifest,runner)
    def test_cleanup_initialization_uses_attempt_deadline(self):
        cloud=mock.Mock(); cloud.runner=Runner()
        cloud.m={'deadline':None}
        observed=[]
        cloud.initialize.side_effect=lambda: observed.append(cloud.runner.deadline)
        with mock.patch('cleanup._cleanup'),mock.patch('cleanup.runtime_directory'),mock.patch('cleanup.time.time',return_value=100):
            cleanup_attempt(cloud,explicit=True,initialize=True)
        self.assertEqual(observed,[1900])

class MeasurementPipes(unittest.TestCase):
    def child(self,program):
        return subprocess.Popen([sys.executable,'-I','-c',program],stdin=subprocess.PIPE,stdout=subprocess.PIPE,
                                stderr=subprocess.DEVNULL,start_new_session=True,bufsize=0)
    def retire(self,child):
        if child.poll() is None: os.killpg(child.pid,signal.SIGKILL)
        child.wait(timeout=2)
        for stream in (child.stdin,child.stdout):
            if not stream.closed: stream.close()
    def test_partial_row_stall_obeys_absolute_deadline(self):
        child=self.child("import sys,time; sys.stdout.write('{'); sys.stdout.flush(); time.sleep(30)")
        started=time.monotonic()
        try:
            with self.assertRaises(ValueError): list(pipe_rows(child,Runner(time.time()+5.3),b'context'))
            self.assertLess(time.monotonic()-started,2)
        finally: self.retire(child)
    def test_input_pipe_stall_obeys_absolute_deadline(self):
        child=self.child('import time; time.sleep(30)')
        started=time.monotonic()
        try:
            with self.assertRaises(ValueError): list(pipe_rows(child,Runner(time.time()+5.3),b'x'*65536))
            self.assertLess(time.monotonic()-started,2)
        finally: self.retire(child)
    def test_fragmented_and_multiple_rows_are_assembled(self):
        child=self.child("import sys,time; sys.stdin.buffer.read(); sys.stdout.write('{'); sys.stdout.flush(); time.sleep(.02); sys.stdout.write('}\\n{}\\n'); sys.stdout.flush()")
        try: self.assertEqual(list(pipe_rows(child,Runner(time.time()+10),b'context')),[b'{}',b'{}'])
        finally: self.retire(child)
    def test_partial_eof_is_never_an_accepted_row(self):
        child=self.child("import sys; sys.stdin.buffer.read(); sys.stdout.write('{}'); sys.stdout.flush()")
        try:
            with self.assertRaises(ValueError): list(pipe_rows(child,Runner(time.time()+10),b'context'))
        finally: self.retire(child)

class ObjectPages(unittest.TestCase):
    def client(self,responses):
        client=Objects.__new__(Objects); calls=[]
        def request(operation,values):
            calls.append((operation,values)); return responses.pop(0)
        client.request=request
        return client,calls
    def test_multipart_uses_both_markers_and_checks_parts(self):
        client,calls=self.client([
            {'Uploads':[{'Key':'synthetic','UploadId':'upload'}],'IsTruncated':True,'NextKeyMarker':'next','NextUploadIdMarker':'next-upload'},
            {},{'NoSuchUpload':True},{'Uploads':[],'IsTruncated':False}])
        self.assertEqual(client.multipart_pass(),1)
        self.assertEqual(calls[-1],('list-multipart-uploads',{'KeyMarker':'next','UploadIdMarker':'next-upload','MaxUploads':100}))
        self.assertEqual(calls[2][0],'list-parts')
    def test_delete_error_fails_cleanup(self):
        client,_=self.client([{'Contents':[{'Key':'synthetic'}],'IsTruncated':False},{'Errors':[{'Code':'AccessDenied'}]}])
        with self.assertRaises(ValueError): client.objects_pass()
    def test_pagination_cycle_refused(self):
        page={'Contents':[],'IsTruncated':True,'NextContinuationToken':'same'}
        client,_=self.client([page,page])
        with self.assertRaises(ValueError): client.objects_pass()
    def test_parts_pagination_uses_returned_marker(self):
        client,calls=self.client([{'Parts':[{'PartNumber':1}],'IsTruncated':True,'NextPartNumberMarker':1},
            {'Parts':[],'IsTruncated':False},{},{'NoSuchUpload':True}])
        client.verify_parts('synthetic','upload')
        self.assertEqual(calls[1][1],{'Key':'synthetic','UploadId':'upload','MaxParts':100,'PartNumberMarker':1})
    def test_post_create_inventory_failure_attempts_exact_retirement(self):
        from cleanup import cleanup_key
        cloud=mock.Mock(); cloud.m={'storage':{'media':{}}}
        cloud.runtime=pathlib.Path('/nonexistent-private-runtime')
        cloud.make_key.side_effect=ValueError('inventory unavailable')
        with self.assertRaises(ValueError):
            with cleanup_key(cloud,'media'): self.fail('creation failed')
        cloud.reconcile_key.assert_called_once()
        cloud.retire_key.assert_called_once_with('media','cleanup')
    def test_created_key_is_reconciled_and_revoked_after_inventory_failure(self):
        from cleanup import cleanup_key
        with tempfile.TemporaryDirectory() as directory:
            cloud=Cloud.__new__(Cloud); cloud.runtime=pathlib.Path(directory)
            cloud.m={'storage':{'media':{'account_id':'owned-account','subuser':'owned'}}}
            cloud.store=mock.Mock(); state={'created':False,'calls':0}; saved=[]
            cloud.store.save.side_effect=lambda value:saved.append(copy.deepcopy(value))
            def inventory():
                state['calls']+=1
                if state['calls']==2: raise ValueError('post-create inventory failed')
                return [{'UserKeyID':'known-key','SubUserID':'owned'}] if state['created'] else []
            def storage(operation,*args,**kwargs):
                if operation=='create-s3-key':
                    state['created']=True
                    path=pathlib.Path(args[args.index('--secret-file')+1]); path.write_text('synthetic'); path.chmod(0o600)
                    return {'UserKeyID':'known-key'}
                if operation=='delete-s3-key':
                    self.assertEqual(args,('--user-key-id','known-key','--yes'))
                    state['created']=False; return {}
                self.fail('unexpected key operation')
            cloud.inventory=inventory; cloud.storage=storage
            with self.assertRaises(ValueError):
                with cleanup_key(cloud,'media'): self.fail('verification must fail')
            self.assertFalse(state['created'])
            self.assertNotIn('key_attempt',cloud.m)
            self.assertTrue(any(value['storage']['media'].get('cleanup_key')=='known-key' for value in saved))
            self.assertFalse(list(cloud.runtime.iterdir()))
    def test_no_principal_account_can_retire_after_creation_failure(self):
        cloud=mock.Mock(); record={'account_id':'owned'}
        cloud.m={'name':'aboutme-cs-rehearsal-0123456789abcdef','storage':{'media':record}}
        state={'bucket':True,'account':True}
        def storage(operation,*args,**kwargs):
            if operation=='list-buckets': return {'Items':[{'name':cloud.m['name']+'-media','count':0}] if state['bucket'] else []}
            if operation=='delete-bucket': state['bucket']=False; return {}
            self.fail('unexpected storage operation')
        def vng(service,operation,*args,**kwargs):
            if operation=='list-service-accounts': return {'Items':[{'id':'owned','name':cloud.m['name']+'-media'}] if state['account'] else []}
            if operation=='list-service-account-policies': return {'Items':[]}
            if operation=='delete-service-account': state['account']=False; return {}
            self.fail('unexpected IAM operation')
        cloud.storage.side_effect=storage; cloud.vng.side_effect=vng
        retire_bucket(cloud,'media')
        self.assertEqual(record['iam_policies'],[])
        self.assertTrue(record['retired']); self.assertFalse(state['account'])
    def test_wrong_key_attachment_is_never_deleted(self):
        cloud=Cloud.__new__(Cloud)
        cloud.m={'storage':{'media':{'subuser':'owned','workload_key':'synthetic'}}}
        cloud.inventory=lambda:[{'UserKeyID':'synthetic','SubUserID':'other'}]
        cloud.storage=lambda *args,**kwargs:self.fail('unowned delete')
        with self.assertRaises(ValueError): cloud.retire_key('media','workload')
    def test_uncertain_key_cannot_adopt_other_project_key(self):
        cloud=Cloud.__new__(Cloud)
        cloud.m={'storage':{'media':{'subuser':'owned'}},'key_attempt':{'role':'media','purpose':'cleanup','before':[]}}
        cloud.inventory=lambda:[{'UserKeyID':'synthetic','SubUserID':'other'}]
        with self.assertRaises(ValueError): cloud.reconcile_key()
        self.assertIn('key_attempt',cloud.m)
    def test_private_state_refuses_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory); root.chmod(0o700)
            store=Store(root,pathlib.Path('/nonexistent-repository'))
            root.joinpath('elsewhere').write_text('{}')
            root.joinpath('operator.json').symlink_to(root/'elsewhere')
            with self.assertRaises(ValueError): store.read_file('operator.json')
unittest.main(argv=['lifecycle'])
PY
