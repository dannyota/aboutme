#!/usr/bin/env bash
# Lifecycle assertions run only with synthetic provider documents.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
python3 -I - "$here/../rehearsal" <<'PY'
import copy, importlib.util, pathlib, sys, unittest, tempfile, json, os
from unittest import mock
sys.path.insert(0, sys.argv[1])
from protocol import validate_manifest, validate_plan, resource_name, validate_config, check_series
from allowance import bound
from object_cleanup import Objects
from resources import Cloud
from common import Store
from transport import validate_numeric
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
