"""Cleanup never starts a host or recovers a workload credential."""
import os
import time
from common import Runner
from fixed_api import dns_records, delete_floating
from object_cleanup import Objects
from protocol import require
from resources import field


def identity(row):
    for name in ('uuid','UUID','id','ID'):
        if row.get(name): return row[name]
    raise ValueError('missing_resource_identity')


def retire_compute(cloud):
    cloud.destroy()
    resources=cloud.m['resources']
    for service,operation,key in (('compute','list-servers','server_id'),('volume','list-volumes','data_volume_id'),
                                  ('network','list-security-groups','security_group_id')):
        rows=cloud.pages(service,operation)
        require(not any(identity(row)==resources.get(key) for row in rows))
        require(not any(field(row,'name','Name') in (cloud.m['name'],cloud.m['name']+'-data') for row in rows))
    require(resources.get('boot_volume_id') or not resources.get('server_id'))
    require(not any(identity(row)==resources.get('boot_volume_id') for row in cloud.pages('volume','list-volumes')))
    rows=dns_records(cloud.m)
    require(not any(row['id']==resources.get('dns_record_id') or row['name']==cloud.m['origin'][8:] for row in rows))
    ips=cloud.pages('network','list-wanips')
    matches=[row for row in ips if identity(row)==resources.get('floating_ip_id')]
    if matches:
        require(len(matches)==1 and resources.get('floating_ip_id'))
        row=matches[0]
        require(resources['floating_ip'] in (row.get('ipAddress'),row.get('floatingIp')))
        require(not field(row,'serverId','ServerID') and not field(row,'networkInterfaceId','NetworkInterfaceID'))
        cloud.record('delete-floating-ip')
        delete_floating(cloud.m)
        require(not any(identity(row)==resources['floating_ip_id'] for row in cloud.pages('network','list-wanips')))
        cloud.completed()
    if resources.get('server_id'): require(resources.get('floating_ip_id'))
    cloud.m['compute_absent']=True; cloud.store.save(cloud.m)


def accounts(cloud,role):
    name=cloud.m['name']+'-'+role
    rows=[]
    for page in range(50):
        result=cloud.vng('iam','list-service-accounts','--name',name,'--page',str(page),'--size','100',read=True)['Items']
        require(len(result)<=100); rows.extend(result)
        if len(result)<100: break
    else: raise ValueError('account_pagination_bound')
    return [row for row in rows if field(row,'Name','name')==name]


def check_account(cloud,role):
    record=cloud.m['storage'][role]
    found=accounts(cloud,role)
    require(len(found)==1 and identity(found[0])==record['account_id'])


def retire_bucket(cloud,role):
    record=cloud.m['storage'][role]; bucket=cloud.m['name']+'-'+role
    present=[row for row in cloud.storage('list-buckets',read=True)['Items'] if field(row,'name','Name')==bucket]
    if not present:
        require(record.get('retired') is True or record.get('account_retired') is True or 'account_id' not in record)
        record['retired']=True; cloud.store.save(cloud.m); return
    require(len(present)==1)
    cloud.check_bucket(role)
    found=accounts(cloud,role)
    if 'account_id' not in record and found:
        require(any(row['operation']=='create-account-'+role for row in cloud.m.get('effects',[])) and len(found)==1)
        record['account_id']=identity(found[0]); record['iam_policies']=[]; cloud.store.save(cloud.m)
    if 'account_id' not in record:
        require(not found and field(present[0],'count','ObjectCount')==0)
        cloud.record('delete-empty-bucket-'+role)
        cloud.storage('delete-bucket','--bucket',bucket,'--yes')
        record['retired']=True; cloud.completed(); return
    if not found and record.get('policy_retired') and field(present[0],'count','ObjectCount')==0:
        record['account_retired']=True; cloud.store.save(cloud.m)
    else: check_account(cloud,role)
    if record.get('policy_retired'):
        require(field(present[0],'count','ObjectCount')==0)
    elif 'principal' not in record:
        require(field(present[0],'count','ObjectCount')==0)
    else:
        cloud.check_policy(role)
        require(not cloud.keys_for(record['subuser']))
        file=cloud.make_key(role,'cleanup')
        configuration=cloud.runtime/(role+'.aws-config')
        try:
            client=Objects(Runner(cloud.runner.deadline-120),file,configuration,bucket)
            counts=client.empty()
            record['cleanup_counts']=counts; cloud.store.save(cloud.m)
            cloud.record('delete-policy-'+role)
            cloud.storage('delete-bucket-policy','--bucket',bucket)
            require(cloud.storage('get-bucket-policy','--bucket',bucket,read=True)['Policy'] in ('',None))
            record['policy_retired']=True; cloud.completed()
        finally:
            try: cloud.retire_key(role,'cleanup')
            finally:
                if file.exists(): file.unlink()
                if configuration.exists(): configuration.unlink()
        require(record.get('policy_retired') is True)
    if not record.get('account_retired'):
        attached=[]
        for page in range(50):
            rows=cloud.vng('iam','list-service-account-policies','--service-account-id',record['account_id'],
                           '--page',str(page),'--size','100',read=True)['Items']
            require(len(rows)<=100); attached.extend(rows)
            if len(rows)<100: break
        else: raise ValueError('policy_pagination_bound')
        require(sorted(identity(row) for row in attached)==sorted(record['iam_policies']))
        for identifier in record['iam_policies']:
            cloud.record('detach-owned-policy-'+role)
            cloud.vng('iam','detach-service-account-policy','--service-account-id',record['account_id'],'--policy-id',identifier,'--yes')
            cloud.completed()
        cloud.record('delete-account-'+role)
        cloud.vng('iam','delete-service-account','--service-account-id',record['account_id'],'--yes')
        require(not accounts(cloud,role)); record['account_retired']=True; cloud.completed()
    cloud.record('delete-bucket-'+role)
    cloud.storage('delete-bucket','--bucket',bucket,'--yes')
    require(not any(field(row,'name','Name')==bucket for row in cloud.storage('list-buckets',read=True)['Items']))
    record['retired']=True; cloud.completed()


def _cleanup(cloud,explicit=False):
    deadline=time.time()+1800
    if not explicit and cloud.m.get('deadline') is not None: deadline=min(deadline,cloud.m['deadline'])
    cloud.runner=Runner(deadline)
    was_planned=cloud.m.get('created_at') is None
    cloud.m['state']='cleanup'; cloud.store.save(cloud.m)
    cloud.runtime.mkdir(mode=0o700,exist_ok=True)
    require(not cloud.runtime.is_symlink() and cloud.runtime.stat().st_uid==os.getuid())
    cloud.reconcile_key()
    for role in cloud.m.get('storage',{}):
        record=cloud.m['storage'][role]
        if record.get('workload_key') or record.get('cleanup_key'):
            check_account(cloud,role)
            cloud.retire_key(role,'workload'); cloud.retire_key(role,'cleanup')
    if not was_planned:
        retire_compute(cloud)
    for role in cloud.m.get('storage',{}): retire_bucket(cloud,role)
    require(all(row.get('retired') for row in cloud.m.get('storage',{}).values()))
    for name in ('state','state.backup','plan','destroy-plan'):
        path=cloud.work/name
        if path.exists(): require(not path.is_symlink()); path.unlink()
    for path in cloud.runtime.iterdir():
        require(path.is_file() and not path.is_symlink()); path.unlink()
    cloud.runtime.rmdir()
    cloud.m['state']='cleaned'; cloud.store.finish(cloud.m)
    print('PASS cleanup')


def cleanup(cloud,explicit=False):
    try:
        _cleanup(cloud,explicit)
    finally:
        # Fresh cleanup secrets never survive an uncertain provider outcome.
        for role in ('media','backups'):
            for suffix in ('-cleanup.credentials','.aws-config'):
                path=cloud.runtime/(role+suffix)
                if path.exists() or path.is_symlink():
                    require(path.is_file() and not path.is_symlink() and path.stat().st_uid==os.getuid())
                    path.unlink()
