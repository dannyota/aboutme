#!/usr/bin/env bash
# Synthetic files exercise the collector without reading live host counters.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
python3 -I - "$here/../rehearsal" <<'PY'
import pathlib, sys, tempfile, unittest, json, email.message
from unittest import mock
sys.path.insert(0,sys.argv[1])
from metrics import parse_memory, parse_pressure, parse_counters
from protocol import check_series
from api import bounded_body, canonical, Client
class Counters(unittest.TestCase):
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
        def sleep(delay): clock[0]+=delay
        with mock.patch('api.time.monotonic',side_effect=lambda:clock[0]),mock.patch('api.time.sleep',side_effect=sleep):
            for _ in range(2): client.request('PATCH','/api/v1/resumes/synthetic/structure',{'commands':[]},revision='1')
        self.assertGreaterEqual(calls[1][0]-calls[0][0],0.5)
        self.assertEqual(calls[0][1].get_header('Origin'),client.origin)
        self.assertEqual(calls[0][1].get_header('If-match'),'"r1"')
        self.assertNotEqual(calls[0][1].get_header('Idempotency-key'),calls[1][1].get_header('Idempotency-key'))
unittest.main(argv=['collector'])
PY
