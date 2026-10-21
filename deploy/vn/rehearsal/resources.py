"""Fixed create and retirement operations on manifest-owned cloud resources."""
import json
import os
import pathlib
import re
import time
from protocol import require, validate_plan, resource_name, ZONE, FLAVOR, IMAGE
from common import safe_write


def field(value,*names):
    for name in names:
        if name in value: return value[name]
    raise ValueError('missing_provider_field')


def policy_for(principal,bucket):
    return {'Version':'2012-10-17','Statement':[
        {'Sid':'Bucket','Effect':'Allow','Principal':{'AWS':[principal]},
         'Action':['s3:ListBucket','s3:GetBucketLocation','s3:ListBucketMultipartUploads'],
         'Resource':['arn:aws:s3:::'+bucket]},
        {'Sid':'Objects','Effect':'Allow','Principal':{'AWS':[principal]},
         'Action':['s3:GetObject','s3:PutObject','s3:DeleteObject','s3:AbortMultipartUpload','s3:ListMultipartUploadParts'],
         'Resource':['arn:aws:s3:::'+bucket+'/*']}]}


def normalized(value):
    if isinstance(value,dict): return {k:normalized(v) for k,v in value.items()}
    if isinstance(value,list): return sorted((normalized(v) for v in value),key=lambda x:json.dumps(x,sort_keys=True))
    return value


class Cloud:
    def __init__(self,runner,store,manifest,repository):
        self.runner=runner; self.store=store; self.m=manifest; self.repository=repository
        self.root=repository/'deploy/vn/rehearsal'
        self.work=store.root/manifest['run_id']
        self.runtime=pathlib.Path(os.environ['XDG_RUNTIME_DIR'])/manifest['name']

    def vng(self,service,operation,*args,read=False):
        project=self.m['config']['storage_project_id' if service=='storage' else 'project_id']
        command=['vngcloud']
        if os.environ.get('VNG_PROFILE'): command+=['--profile',os.environ['VNG_PROFILE']]
        command+=['--region','hcm-3','--output','json','--project-id',project]
        if read: command+=['--read-only']
        command+=[service,operation,*args]
        return json.loads(self.runner.call(command,timeout=180) or b'{}')

    def pages(self,service,operation,*arguments):
        rows=[]; previous=None
        for page in range(1,51):
            result=self.vng(service,operation,*arguments,'--page',str(page),'--size','100',read=True)
            require(result['Page']==page and 0<=result['TotalPage']<=50)
            require(len(result['Items'])<=100 and result['TotalItem']>=0)
            if previous is not None: require(result['TotalItem']==previous)
            previous=result['TotalItem']; rows.extend(result['Items'])
            if page>=result['TotalPage']:
                require(len(rows)==result['TotalItem']); return rows
        raise ValueError('provider_pagination_bound')

    def storage(self,operation,*args,read=False):
        return self.vng('storage',operation,*args,read=read)

    def record(self,operation):
        self.m.setdefault('effects',[]).append({'operation':operation,'started_at':int(time.time()),'result':'pending'})
        self.m['intended_effect']=operation; self.store.save(self.m)

    def completed(self):
        if self.m.get('intended_effect') and self.m.get('effects'):
            self.m['effects'][-1]['result']='confirmed'
        self.m.pop('intended_effect',None); self.store.save(self.m)

    def environment(self,cloud_init=''):
        environment={key:value for key,value in os.environ.items() if not key.startswith(('TF_CLI_ARGS','TF_LOG','TF_VAR_'))}
        environment['TF_VAR_state_passphrase']=os.environ['TF_VAR_state_passphrase']
        environment.update({'TF_DATA_DIR':str(self.work/'tofu-data'),
          'TF_VAR_project_id':self.m['config']['project_id'],'TF_VAR_network_id':self.m['config']['network_id'],
          'TF_VAR_subnet_id':self.m['config']['subnet_id'],'TF_VAR_zone_id':self.m['config']['zone_id'],
          'TF_VAR_operator_cidrs':json.dumps(self.m['config']['operator_cidrs']),
          'TF_VAR_run_id':self.m['run_id'],'TF_VAR_cloud_init':cloud_init,'TF_IN_AUTOMATION':'1'})
        environment['VNG_BIN']='vngcloud'
        return environment

    def tofu(self,*arguments,cloud_init=''):
        return self.runner.call(['tofu','-chdir='+str(self.root),*arguments],timeout=1800,env=self.environment(cloud_init))

    def initialize(self):
        self.tofu('init','-input=false','-backend-config=path='+str(self.work/'state'))

    def inventory(self):
        return self.storage('list-s3-keys',read=True)['Items']

    def keys_for(self,subuser):
        return [row for row in self.inventory() if field(row,'SubUserID','subUserId')==subuser]

    def check_bucket(self,role):
        bucket=self.m['name']+'-'+role
        row=self.storage('get-bucket','--bucket',bucket,read=True)
        require(field(row,'name','Name')==bucket and field(row,'isPublic','IsPublic') is False)
        version=self.storage('get-bucket-versioning','--bucket',bucket,read=True)
        require(version['Enabled'] is False and version['Status']=='Off')
        require(self.storage('get-bucket-encryption','--bucket',bucket,read=True)['Enabled'] is True)
        return row

    def check_policy(self,role):
        record=self.m['storage'][role]; bucket=self.m['name']+'-'+role
        actual=self.storage('get-bucket-policy','--bucket',bucket,read=True)['Policy']
        if isinstance(actual,str): actual=json.loads(actual)
        require(normalized(actual)==normalized(policy_for(record['principal'],bucket)))

    def make_key(self,role,purpose):
        record=self.m['storage'][role]; require('key_attempt' not in self.m)
        before=self.inventory()
        file=self.runtime/(role+'-'+purpose+'.credentials')
        require(not file.exists() and not file.is_symlink())
        self.m['key_attempt']={'role':role,'purpose':purpose,'before':[field(x,'UserKeyID','userKeyId') for x in before]}
        self.record('create-'+purpose+'-key-'+role)
        try:
            result=self.storage('create-s3-key','--service-account-id',record['account_id'],'--secret-file',str(file))
        except BaseException:
            if file.exists() or file.is_symlink():
                require(not file.is_symlink() and file.stat().st_uid==os.getuid()); file.unlink()
            raise
        identifier=field(result,'UserKeyID','userKeyId')
        record[purpose+'_key']=identifier; self.store.save(self.m)
        rows=[x for x in self.keys_for(record['subuser']) if field(x,'UserKeyID','userKeyId')==identifier]
        require(len(rows)==1 and file.is_file() and file.stat().st_mode&0o777==0o600)
        record[purpose+'_key']=identifier
        self.m.pop('key_attempt'); self.completed()
        return file

    def reconcile_key(self):
        attempt=self.m.get('key_attempt')
        if not attempt: return
        record=self.m['storage'][attempt['role']]
        new=[row for row in self.inventory() if field(row,'UserKeyID','userKeyId') not in attempt['before']]
        require(len(new)<=1)
        if new:
            require(field(new[0],'SubUserID','subUserId')==record['subuser'])
            record[attempt['purpose']+'_key']=field(new[0],'UserKeyID','userKeyId')
        self.m.pop('key_attempt'); self.completed()

    def retire_key(self,role,purpose):
        record=self.m['storage'][role]; identifier=record.get(purpose+'_key')
        if not identifier:
            file=self.runtime/(role+'-'+purpose+'.credentials')
            if file.exists() or file.is_symlink():
                require(not file.is_symlink() and file.stat().st_uid==os.getuid()); file.unlink()
            return
        matches=[row for row in self.inventory() if field(row,'UserKeyID','userKeyId')==identifier]
        if matches:
            require(len(matches)==1 and field(matches[0],'SubUserID','subUserId')==record['subuser'])
            self.record('delete-'+purpose+'-key-'+role)
            self.storage('delete-s3-key','--user-key-id',identifier,'--yes')
            require(all(field(row,'UserKeyID','userKeyId')!=identifier for row in self.inventory()))
        record.pop(purpose+'_key',None); self.completed()
        file=self.runtime/(role+'-'+purpose+'.credentials')
        if file.exists() or file.is_symlink():
            require(not file.is_symlink() and file.stat().st_uid==os.getuid()); file.unlink()

    def create_storage(self):
        self.m.setdefault('storage',{})
        for role in ('media','backups'):
            bucket=self.m['name']+'-'+role
            existing=self.storage('list-buckets',read=True)['Items']
            require(not any(field(row,'name','Name')==bucket for row in existing))
            self.m['storage'][role]={'bucket':bucket}
            self.record('create-bucket-'+role)
            self.storage('create-bucket','--bucket',bucket,'--encryption')
            self.check_bucket(role); self.completed()
            require(not self.vng('iam','list-service-accounts','--name',bucket,read=True)['Items'])
            secret=self.runtime/(role+'.account-secret')
            self.record('create-account-'+role)
            try:
                self.vng('iam','create-service-account','--name',bucket,'--description',bucket,'--secret-file',str(secret))
            finally:
                if secret.exists(): secret.unlink()
            accounts=self.vng('iam','list-service-accounts','--name',bucket,read=True)['Items']
            require(len(accounts)==1 and field(accounts[0],'Name','name')==bucket)
            record=self.m['storage'][role]; record['account_id']=field(accounts[0],'ID','id'); record['iam_policies']=[]
            self.completed()
            self.record('resolve-principal-'+role)
            principal=self.storage('ensure-service-account-principal','--service-account-id',record['account_id'])
            record['principal']=principal['PrincipalARN']; record['subuser']=principal['SubUserID']
            record['iam_policies']=[]; self.completed()
            self.record('put-policy-'+role)
            self.storage('put-bucket-policy','--bucket',bucket,'--policy',json.dumps(policy_for(record['principal'],bucket)))
            self.check_policy(role); self.completed()

    def verify_identity(self):
        identifier=self.m['resources']['server_id']
        raw=self.vng('compute','get-server','--server-id',identifier,read=True)
        row=raw.get('Server',raw)
        require(field(row,'name','Name')==self.m['name'])
        require(field(row,'uuid','UUID','id','ID')==identifier)
        interfaces=field(row,'internalInterfaces','InternalInterfaces')
        addresses=[field(item,'floatingIp','FloatingIP') for item in interfaces]
        require(self.m['resources']['floating_ip'] in addresses)
        flavor=field(row,'flavor','Flavor'); image=field(row,'image','Image')
        require(field(flavor,'flavorId','FlavorID')==FLAVOR and field(image,'id','ID')==IMAGE)
        require(field(flavor,'cpu','CPU')==2 and field(flavor,'memory','Memory')==4)

    def apply(self):
        self.store.checkpoint(self.m)
        self.m['created_at']=int(time.time()); self.m['deadline']=self.m['created_at']+14400
        self.m['state']='creating'; self.runner.set_deadline(self.m['deadline']); self.record('apply-infrastructure')
        self.tofu('apply','-input=false',str(self.work/'plan'))
        outputs=json.loads(self.tofu('output','-json'))
        self.m['resources']={name:row['value'] for name,row in outputs.items()}
        self.m['state']='created'; self.completed()
        self.verify_identity()
        self.record_floating()

    def record_floating(self):
        raw=self.vng('compute','get-server','--server-id',self.m['resources']['server_id'],read=True)
        row=raw.get('Server',raw)
        require(field(row,'name','Name')==self.m['name'])
        boot=field(row,'bootVolumeId','BootVolumeID')
        require(isinstance(boot,str) and re.fullmatch('[A-Za-z0-9_-]{1,100}',boot))
        recorded=self.m['resources'].get('boot_volume_id'); require(recorded is None or recorded==boot)
        self.m['resources']['boot_volume_id']=boot
        interfaces=field(row,'internalInterfaces','InternalInterfaces')
        matches=[item for item in interfaces if field(item,'floatingIp','FloatingIP')==self.m['resources']['floating_ip']]
        require(len(matches)==1 and field(matches[0],'floatingIpId','FloatingIPID'))
        self.m['resources']['floating_ip_id']=field(matches[0],'floatingIpId','FloatingIPID')
        self.store.save(self.m)

    def state_resources(self):
        if not (self.work/'state').exists(): return []
        state=json.loads(self.tofu('show','-json'))
        return state.get('values',{}).get('root_module',{}).get('resources',[])

    def destroy(self):
        # A partial apply can leave resources in state. Never import a replacement.
        resources=self.state_resources()
        if not resources:
            require(self.m.get('created_at') is None or self.m.get('compute_absent') is True or
                    any(row['operation']=='destroy-infrastructure' for row in self.m.get('effects',[])))
            return
        by_address={row['address']:row for row in resources}
        for row in resources:
            name=row['values'].get('name')
            if name: require(name in (self.m['name'],self.m['name']+'-data','cs-rehearsal-'+self.m['run_id']+'.aboutme.vn'))
        if self.m['resources']:
            for address,key in (('vngcloud_vserver_server.main','server_id'),('vngcloud_vserver_volume.data','data_volume_id'),
                                ('cloudflare_dns_record.rehearsal','dns_record_id'),('vngcloud_vserver_secgroup.main','security_group_id')):
                if address in by_address: require(by_address[address]['values']['id']==self.m['resources'][key])
        if 'vngcloud_vserver_server.main' in by_address:
            server=by_address['vngcloud_vserver_server.main']['values']
            interfaces=server['internal_interfaces']; require(len(interfaces)>=1)
            self.m['resources'].setdefault('server_id',server['id'])
            self.m['resources'].setdefault('floating_ip',interfaces[0]['floating_ip'])
            if not self.m['resources'].get('floating_ip_id'): self.record_floating()
        self.record('plan-destroy')
        self.tofu('plan','-destroy','-input=false','-out='+str(self.work/'destroy-plan'))
        plan=json.loads(self.tofu('show','-json',str(self.work/'destroy-plan')))
        validate_plan(plan,self.m['run_id'],destroying=True)
        self.record('destroy-infrastructure')
        self.tofu('apply','-input=false',str(self.work/'destroy-plan'))
        require(not self.state_resources())
        self.completed()
