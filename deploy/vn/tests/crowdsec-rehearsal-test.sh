#!/usr/bin/env bash
# Synthetic files exercise the collector without reading live host counters.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
python3 -I - "$here/../rehearsal" <<'PY'
import subprocess
import pathlib, sys, tempfile, unittest, json, email.message
from unittest import mock
sys.path.insert(0,sys.argv[1])
from metrics import parse_memory, parse_pressure, parse_counters, Collector
from protocol import check_series
from api import bounded_body, canonical, Client
from lifecycle import finish, final_record, validate_command
from job_accounting import payload_path, completed
class Counters(unittest.TestCase):
    def test_shared_payload_envelope_is_separate_and_observation_bound(self):
        identifier='a'*64; invocation='b'*32; unit='aboutme-job@idempotency-expiry-sweep'
        observed={'unit':unit,'invocation':invocation,'container_id':identifier,
                  'payload_group':'/machine.slice/libpod-'+identifier+'.scope','envelope_identity':1}
        values={'identity':1,'current':1,'peak':1,'events':dict(low=0,high=0,max=0,oom=0,oom_kill=0)}
        base={'unit':unit,'invocation':invocation,'shared_payload_envelope':values}
        final=dict(base,shared_payload_envelope=dict(values,peak=67108864))
        with mock.patch('lifecycle.read_record',return_value=observed):
            result=completed(base,final,pathlib.Path('/unused'))
            self.assertEqual(result['shared_envelope'],1); self.assertEqual(result['peak'],67108864)
        with mock.patch('lifecycle.read_record',return_value=dict(observed,invocation='c'*32)):
            with self.assertRaises(ValueError): completed(base,final,pathlib.Path('/unused'))
    def test_job_keeps_unchanged_wrapper_and_default_scope(self):
        validate_command('aboutme-job@idempotency-expiry-sweep',
                         '{ path=/usr/local/lib/aboutme/job-run.sh ; argv[]=/usr/local/lib/aboutme/job-run.sh idempotency-expiry-sweep ; }')
        identifier='a'*64
        self.assertEqual(payload_path('/machine.slice/libpod-'+identifier+'.scope',identifier),
                         pathlib.Path('/sys/fs/cgroup/machine.slice/libpod-'+identifier+'.scope'))
        for group in ('/system.slice/other.service','/machine.slice/libpod-'+('b'*64)+'.scope'):
            with self.assertRaises(ValueError): payload_path(group,identifier)
        with self.assertRaises(ValueError):
            validate_command('aboutme-job@idempotency-expiry-sweep','podman run --cgroups=split')
    def test_empty_oom_grep_is_valid_but_journal_failure_is_not(self):
        collector=Collector.__new__(Collector)
        for code,output,error,accepted in ((1,b'',b'',True),(1,b'',b'journal failure',False),(2,b'',b'',False)):
            responses=[subprocess.CompletedProcess([],0,b'',b''),subprocess.CompletedProcess([],code,output,error)]
            with mock.patch('metrics.subprocess.run',side_effect=responses):
                if accepted: self.assertEqual(collector.kernel_oom(),0)
                else:
                    with self.assertRaises(ValueError): collector.kernel_oom()
    def test_final_late_peak_replaces_periodic_last_seen(self):
        base={'identity':1,'current':1,'peak':1,'events':dict(low=0,high=0,max=0,oom=0,oom_kill=0)}
        final=dict(base,current=1,peak=67108864)
        self.assertEqual(finish(base,final)['peak'],67108864)
        with self.assertRaises(ValueError): finish(base,dict(final,identity=2))
        failed=dict(final,events=dict(base['events'],oom_kill=1))
        with self.assertRaises(ValueError): finish(base,failed)
    def test_required_lifecycle_refuses_missing_final_record(self):
        unit='aboutme-db-setup'; invocation='a'*32
        base={'unit':unit,'invocation':invocation,'group':'/system.slice/'+unit+'.service','payload_contained':1,
              'snapshot':{'identity':1,'current':1,'peak':1,'events':dict(low=0,high=0,max=0,oom=0,oom_kill=0)}}
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory)
            root.joinpath(unit+'-'+invocation+'.baseline.json').write_text(json.dumps(base))
            def read(path):
                if not path.is_file(): raise ValueError('missing lifecycle record')
                return json.loads(path.read_text())
            with mock.patch('lifecycle.ROOT',root),mock.patch('lifecycle.read_record',side_effect=read):
                with self.assertRaises(ValueError): final_record(unit,invocation)
                root.joinpath(unit+'-'+invocation+'.final.json').write_text(json.dumps(base))
                self.assertEqual(final_record(unit,invocation)['final'],1)
    def test_short_lifecycle_does_not_require_a_periodic_sample(self):
        base={'identity':1,'current':1,'peak':1,'events':dict(low=0,high=0,max=0,oom=0,oom_kill=0)}
        self.assertEqual(finish(base,dict(base,peak=67108864))['final'],1)
    def test_zram_not_added_to_host_used(self):
        value=parse_memory('MemTotal: 4194304 kB\nMemAvailable: 524288 kB\nMemFree: 262144 kB\n')
        self.assertEqual(value,{'total':4294967296,'available':536870912,'free':268435456,'used':3758096384})
    def test_impossible_and_missing_memory_refused(self):
        for text in ('MemTotal: 100 kB\n','MemTotal: 100 kB\nMemAvailable: 101 kB\nMemFree: 0 kB\n'):
            with self.assertRaises(ValueError): parse_memory(text)
    def test_pressure_requires_both_counters_and_averages(self):
        valid='some avg10=0.25 avg60=0.10 avg300=0.01 total=12345\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=67\n'
        self.assertEqual(parse_pressure(valid)['some']['total'],12345)
        for text in ('some total=1\n',valid.replace('full','some'),valid.replace('total=67','total=-1')):
            with self.assertRaises(ValueError): parse_pressure(text)
    def test_counter_text_is_never_output(self):
        with self.assertRaises(ValueError): parse_counters('oom_kill secret\n')
    def test_body_cap_is_bytes(self):
        with self.assertRaises(ValueError): bounded_body({'text':'é'*131072})
        self.assertEqual(bounded_body({'x':1}),b'{"x":1}')
    def test_json_matches_go_html_escaping(self):
        self.assertEqual(canonical({'x':'<p>&</p>'}),b'{"x":"\\u003cp\\u003e\\u0026\\u003c/p\\u003e"}')
    def test_pressure_rolling_windows_at_ten_hertz(self):
        rows=[{'t':i/10,'available':536870912,'some':i*10000,'full':0} for i in range(111)]
        with self.assertRaises(ValueError): check_series(rows,.1,100)
class Requests(unittest.TestCase):
    def test_expired_setup_starts_no_http_request(self):
        from common import Runner
        client=Client.__new__(Client)
        client.manifest={'run_id':'0123456789abcdef'}; client.runner=Runner(100)
        with mock.patch('api.exchange') as request:
            with self.assertRaises(ValueError): client.request('POST','/api/v1/resumes',{})
        request.assert_not_called()

    def test_mutations_are_paced_and_keep_write_guards(self):
        client=Client.__new__(Client)
        client.origin='https://cs-rehearsal-0123456789abcdef.aboutme.vn'
        client.manifest={'run_id':'0123456789abcdef'}
        client.csrf='synthetic'; client.schema=5; client.last_write=100
        clock=[100.0]; calls=[]
        class Response:
            code=200
            headers=email.message.Message()
            headers['Content-Type']='application/json'
            def __enter__(self): return self
            def __exit__(self,*args): pass
            def read(self,_): return b'{"data":{}}'
        class Opener:
            def open(self,request,timeout): calls.append((clock[0],request)); return Response()
        client.opener=Opener()
        from common import Runner
        import http.cookiejar
        client.runner=Runner(); client.cookies=http.cookiejar.CookieJar()
        def sleep(delay): clock[0]+=delay
        def exchange(runner,request,*args,**kwargs):
            calls.append((clock[0],request)); return 200,Response.headers,b'{"data":{}}'
        with mock.patch('api.exchange',side_effect=exchange),mock.patch('api.time.monotonic',side_effect=lambda:clock[0]),mock.patch('api.time.sleep',side_effect=sleep):
            for _ in range(2): client.request('PATCH','/api/v1/resumes/synthetic/structure',{'commands':[]},revision='1')
        self.assertGreaterEqual(calls[1][0]-calls[0][0],0.5)
        self.assertEqual(calls[0][1].get_header('Origin'),client.origin)
        self.assertEqual(calls[0][1].get_header('If-match'),'"r1"')
        self.assertNotEqual(calls[0][1].get_header('Idempotency-key'),calls[1][1].get_header('Idempotency-key'))
unittest.main(argv=['collector'])
PY
