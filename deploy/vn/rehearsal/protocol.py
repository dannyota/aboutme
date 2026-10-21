"""Fixed rehearsal identity and acceptance arithmetic. No effects on import."""
import hashlib
import ipaddress
import json
import re

BASELINE = '7687166339a0318701b014c41020dd52a8c7fc65'
ZONE = 'HCM03-1C'
FLAVOR = 'flav-530ea5cb-6fac-4264-bcad-9e0e0a5ba3fc'
IMAGE = 'img-34440a82-92fb-40bc-b79c-b1a2b49b93de'
VOLUME_TYPE = 'vtype-e782f8e1-0569-11f0-a0a4-ec2a72332f83'
ENCRYPTION = 'aes-xts-plain64_256'
MAX_PRICE = 1047880
TAG = re.compile(r'v(?:0|[1-9][0-9]{0,2})\.(?:0|[1-9][0-9]{0,2})\.(?:0|[1-9][0-9]{0,2})\Z')
SHA = re.compile(r'[0-9a-f]{64}\Z')


def require(condition):
    if not condition:
        raise ValueError('invalid_rehearsal_state')


def resource_name(run_id):
    require(isinstance(run_id, str) and re.fullmatch('[0-9a-f]{16}', run_id))
    return 'aboutme-cs-rehearsal-' + run_id


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def validate_config(value):
    require(set(value) == {'project_id','storage_project_id','network_id','subnet_id',
                          'zone_id','operator_cidrs','admin_public_key','artifact_run_id'})
    for key in ('project_id','storage_project_id','network_id','subnet_id','zone_id'):
        require(isinstance(value[key], str) and re.fullmatch('[A-Za-z0-9_-]{1,100}', value[key]))
    require(re.fullmatch('[0-9]{1,20}', str(value['artifact_run_id'])))
    require(isinstance(value['operator_cidrs'], list) and 1 <= len(value['operator_cidrs']) <= 4)
    for cidr in value['operator_cidrs']:
        address=ipaddress.ip_network(cidr, strict=True)
        require(address.version == 4 and address.prefixlen == 32 and address.is_global)
    require(re.fullmatch(r'ssh-ed25519 [A-Za-z0-9+/=]+(?: [^\r\n]*)?', value['admin_public_key']))
    return value


def validate_manifest(value):
    name=resource_name(value.get('run_id'))
    require(value['schema'] == 1 and value['protocol'] == 1)
    require(value['name'] == name and value['origin'] == 'https://cs-rehearsal-' + value['run_id'] + '.aboutme.vn')
    require(value['baseline_commit'] == BASELINE and value['baseline_tag'] == 'v0.8.9')
    require(TAG.fullmatch(value['tag']) and re.fullmatch('[0-9a-f]{40}', value['commit']))
    require(value['config'] == validate_config(value['config']))
    require(value['provider'] == {'zone':ZONE,'flavor':FLAVOR,'image':IMAGE,'volume_type':VOLUME_TYPE})
    for release in ('baseline_images','images'):
        require(set(value[release]) == {'server','web','caddy'})
        for image,d in value[release].items():
            require(re.fullmatch('ghcr.io/dannyota/aboutme-' + image + r'@sha256:[0-9a-f]{64}', d))
    for key in ('baseline_bundle_sha256','bundle_sha256','fixtures_sha256','plan_sha256'):
        require(SHA.fullmatch(value[key]))
    require(0 < value['quote'] <= MAX_PRICE)
    require(value['state'] in ('planned','creating','created','running','cleanup','cleaned','unresolved'))
    require(isinstance(value['resources'],dict))
    return value


def validate_plan(plan, run_id, destroying=False):
    name=resource_name(run_id)
    allowed={'vngcloud_vserver_server.main','vngcloud_vserver_volume.data',
             'vngcloud_vserver_volume_attach.data','vngcloud_vserver_secgroup.main',
             'vngcloud_vserver_secgrouprule.http','cloudflare_dns_record.rehearsal'}
    seen=set()
    for change in plan.get('resource_changes',[]):
        address=change['address']
        require(address in allowed or re.fullmatch(r'vngcloud_vserver_secgrouprule\.(https|ssh)\["[0-9.]+/32"\]',address))
        require(not change.get('importing') and not change['change'].get('importing'))
        actions=change['change']['actions']
        require(actions == (['delete'] if destroying else ['create']))
        after=change['change']['before' if destroying else 'after']
        if change['type'] in ('vngcloud_vserver_server','vngcloud_vserver_secgroup'):
            require(after['name'] == name)
        if change['type'] == 'vngcloud_vserver_volume':
            require(after['name'] == name+'-data' and after['size']==20 and after['encryption_type']==ENCRYPTION)
        if change['type'] == 'vngcloud_vserver_server':
            require(after['zone_id']==ZONE and after['flavor_id']==FLAVOR and after['image_id']==IMAGE)
            require(after['root_disk_size']==30 and after['encryption_volume'] is True)
            require(after['root_disk_encryption_type']==ENCRYPTION and after['attach_floating'] is True)
        if change['type'] == 'cloudflare_dns_record':
            require(after['name']=='cs-rehearsal-'+run_id+'.aboutme.vn')
            require(after['type']=='A' and after['ttl']==300 and after['proxied'] is False)
        seen.add(change['type'])
    if destroying:
        require(bool(seen))
    else:
        require({'vngcloud_vserver_server','vngcloud_vserver_volume','vngcloud_vserver_secgroup',
                 'vngcloud_vserver_volume_attach','vngcloud_vserver_secgrouprule','cloudflare_dns_record'} <= seen)


def check_series(rows, interval, minimum):
    require(len(rows)>=minimum)
    streak=0
    for i,row in enumerate(rows):
        require(row['available'] >= 268435456)
        if i:
            previous=rows[i-1]
            require(0 < row['t']-previous['t'] <= interval*2+0.001)
            require(row['some']>=previous['some'] and row['full']>=previous['full'])
        if row['t']-rows[0]['t'] < 1:
            continue
        j=i-1
        while j>0 and row['t']-rows[j]['t'] < 1:
            j-=1
        previous=rows[j]
        elapsed=row['t']-previous['t']
        require(elapsed <= 1+interval*2+0.001)
        elapsed_us=round(elapsed*1000000)
        some=(row['some']-previous['some'])*10>=elapsed_us
        full=(row['full']-previous['full'])*100>=elapsed_us
        # Evaluate one-second endpoints even when collection is 10 Hz.
        if i and int(row['t']-rows[0]['t']) == int(rows[i-1]['t']-rows[0]['t']):
            continue
        streak=streak+1 if some or full else 0
        require(streak<10)
    return {'samples':len(rows),'minimum_available':min(x['available'] for x in rows)}
