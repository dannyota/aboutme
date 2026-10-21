"""Fixed operations on a manifest-bound synthetic host. No caller-supplied commands."""
import concurrent.futures
import hashlib
import json
import os
import pathlib
import re
import socket
import subprocess
import sys
import threading
import time
from protocol import BASELINE, digest, require, validate_manifest
from metrics import Collector, capture
from lifecycle import install_hooks, final_record, final_records

ROOT=pathlib.Path('/run/aboutme-rehearsal')
LIB='/usr/local/lib/aboutme/'
DEADLINE=None


def effect(args,timeout=600,input_data=None):
    if DEADLINE is not None:
        timeout=min(timeout,DEADLINE-time.time()); require(timeout>0)
    return subprocess.run(args,input=input_data,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,
                          timeout=timeout,check=True,env={'PATH':'/usr/sbin:/usr/bin:/sbin:/bin','LC_ALL':'C','HOME':'/root'}).stdout


def read_environment(path):
    values={}
    for line in pathlib.Path(path).read_text().splitlines():
        if not line or line.startswith('#'): continue
        key,value=line.split('=',1); require(key not in values); values[key]=value
    return values


def change_environment(path,updates):
    values=read_environment(path); values.update(updates)
    temporary=pathlib.Path(str(path)+'.rehearsal')
    with temporary.open('x') as stream:
        os.chmod(temporary,0o600)
        stream.write(''.join(key+'='+value+'\n' for key,value in values.items()))
    temporary.replace(path)


def context():
    require(os.geteuid()==0)
    raw=sys.stdin.buffer.read(65537); require(len(raw)<=65536)
    value=json.loads(raw); manifest=validate_manifest(value['manifest'])
    anchor=pathlib.Path('/etc/aboutme-rehearsal.json')
    require(anchor.is_file() and not anchor.is_symlink() and anchor.stat().st_uid==0)
    expected={key:manifest[key] for key in ('run_id','commit','baseline_commit','bundle_sha256','baseline_bundle_sha256')}
    require(json.loads(anchor.read_bytes())==expected)
    require(socket.gethostname()==manifest['name'])
    require(digest(ROOT/'baseline.tar')==manifest['baseline_bundle_sha256'])
    require(digest(ROOT/'candidate.tar')==manifest['bundle_sha256'])
    require(time.time()<manifest['deadline'])
    global DEADLINE
    DEADLINE=manifest['deadline']
    return manifest,value.get('options',{})


def psql(sql):
    return effect(['runuser','-u','postgres','--','psql','-XAtq','-v','ON_ERROR_STOP=1','--host=/run/postgresql','--username=postgres','--dbname=aboutme'],
                  60,sql.encode()).decode().strip()


def isolated(manifest):
    require(capture(['mountpoint','-q','/srv/data'])=='')
    host=manifest['origin'].removeprefix('https://')
    flags=read_environment('/etc/aboutme/flags.env')
    require(flags['PUBLIC_ORIGIN']==manifest['origin'])
    for key in ('PROVIDER_LOGIN_ENABLED','PASSKEY_ENROLLMENT_ENABLED','TOTP_ENROLLMENT_ENABLED','SIGN_IN_TO_VIEW_ENABLED'):
        require(flags[key]=='false')
    caddy=read_environment('/etc/aboutme/caddy.env')
    require(caddy['DIRECT_HOST']==host and caddy['DIRECT_WWW_HOST']=='')
    server=read_environment('/etc/aboutme/server.env')
    require(server['MEDIA_BUCKET']==manifest['name']+'-media' and server['ENV']=='prod')
    require(server['DEPLOY_SITE']==manifest['origin'])
    require(server['MEDIA_ENDPOINT']=='https://hcm04.vstorage.vngcloud.vn' and server['MEDIA_REGION']=='HCM04')
    require(server['MEDIA_FORCE_PATH_STYLE']=='true' and server['AUTH_EMAIL_MODE']=='smtp')
    smtp=read_environment('/etc/aboutme/smtp.env')
    require(smtp['SMTP_HOST']=='localhost' and smtp['SMTP_PORT']=='465' and smtp['SMTP_TLS']=='implicit')
    require(smtp['SES_FROM_ADDRESS']=='fixture@'+host)
    repository=pathlib.Path('/etc/pgbackrest/pgbackrest.conf').read_text()
    require('repo1-s3-bucket='+manifest['name']+'-backups\n' in repository)
    require('repo1-path=/rehearsal\n' in repository and 'aboutme-prod' not in repository)


def empty_activation():
    result=effect([LIB+'crowdsec-privacy-notice.sh','status'],90)
    status=json.loads(result)
    require(status['state']=='activated' and status['expected_count']==0 and status['actual_count']==0)
    receipt=pathlib.Path('/var/lib/aboutme/privacy-notice/activation.json')
    require(receipt.is_file() and not receipt.is_symlink() and receipt.stat().st_mode&0o777==0o444)
    value=json.loads(receipt.read_bytes())
    require(value['expected_count']==value['accepted_count']==value['deleted_count']==0)


def deploy(manifest,baseline=False,first=False,observe=None):
    tag='v0.8.9' if baseline else manifest['tag']
    images=manifest['baseline_images' if baseline else 'images']
    owner=effect([LIB+'fence.sh','lock','deploy',tag],35).decode().strip()
    require(re.fullmatch('[A-Za-z0-9_-]{43}',owner))
    def step(command,*arguments):
        isolated(manifest)
        unit={'db-setup':'aboutme-db-setup','migrate':'aboutme-migrate'}.get(command)
        before=capture(['systemctl','show','--property=InvocationID','--value',unit+'.service']).strip() if unit else None
        result=effect([LIB+'deploy-host.sh',owner,tag,command,*arguments],1800)
        if unit:
            after=capture(['systemctl','show','--property=InvocationID','--value',unit+'.service']).strip()
            require(after and after!=before)
            final_record(unit,after)
        if observe: observe(command)
        return result
    # Failures preserve the existing operation owner. Only teardown follows.
    step('pull',images['server'],images['web'],images['caddy'])
    step('secrets-check'); step('backup'); step('timers-stop')
    step('maintenance-up',images['caddy']); step('app-down')
    step('render',images['server'],images['web'],images['caddy'])
    if first: step('db-setup')
    step('migrate'); step('app-up'); step('maintenance-down'); step('timers-start')
    require(capture(['curl','--fail','--silent','--max-time','10','--output','/dev/null','http://127.0.0.1:8080/readyz'])=='')
    effect([LIB+'fence.sh','release',owner],35)


def install(manifest):
    disks=json.loads(capture(['lsblk','-J','-p','-o','NAME,TYPE,MOUNTPOINTS,SIZE']))['blockdevices']
    candidates=[]
    for disk in disks:
        def mounted(node):
            return any(node.get('mountpoints') or []) or any(mounted(x) for x in node.get('children',[]))
        if disk['type']=='disk' and not mounted(disk) and not disk.get('children'):
            candidates.append(disk['name'])
    require(len(candidates)==1 and re.fullmatch('/dev/[a-z0-9]+',candidates[0]))
    effect(['bash',str(ROOT/'baseline/host/data-volume.sh'),candidates[0],'--format'],120)
    effect(['bash',str(ROOT/'baseline/host/install.sh'),str(ROOT/'baseline')],1800)
    host=manifest['origin'].removeprefix('https://')
    change_environment('/etc/aboutme/flags.env',{'PUBLIC_ORIGIN':manifest['origin'],'PASSWORD_REGISTRATION_ENABLED':'false'})
    change_environment('/etc/aboutme/caddy.env',{'DIRECT_HOST':host,'DIRECT_WWW_HOST':''})
    change_environment('/etc/aboutme/server.env',{'MEDIA_BUCKET':manifest['name']+'-media','DEPLOY_SITE':manifest['origin']})
    change_environment('/etc/aboutme/smtp.env',{'SMTP_HOST':'localhost','SMTP_PORT':'465','SMTP_TLS':'implicit',
                        'SES_FROM_ADDRESS':'fixture@'+host,'SES_FROM_NAME':'CrowdSec rehearsal','ALERT_TO':'fixture@'+host})
    config=pathlib.Path('/etc/pgbackrest/pgbackrest.conf')
    text=config.read_text().replace('repo1-s3-bucket=aboutme-backups','repo1-s3-bucket='+manifest['name']+'-backups')
    text=text.replace('repo1-path=/aboutme-prod','repo1-path=/rehearsal')
    config.write_text(text)
    effect([LIB+'secrets-host.sh','generate'])
    for name in ('smtp-username','smtp-password','google-client-id','google-client-secret',
                 'linkedin-client-id','linkedin-client-secret','totp-key-b'):
        value=effect(['openssl','rand','-hex','24']).strip()
        effect([LIB+'secrets-host.sh','import',name],30,value)
    effect([LIB+'secrets-host.sh','podman-sync'])
    isolated(manifest)
    install_hooks(effect)


def bootstrap(manifest,options):
    require(set(options)=={'stage'} and options['stage'] in ('install','deploy'))
    if options['stage']=='install':
        install(manifest); return
    isolated(manifest)
    effect([LIB+'deploy-host.sh','-','v0.8.9','db-bootstrap'],180)
    effect([LIB+'secrets-host.sh','bouncer-register'])
    deploy(manifest,baseline=True,first=True)
    require(psql('SELECT count(*) FROM users;')=='0')
    for command in ('reconcile-initial','prepare','status','activate'):
        report=json.loads(effect([LIB+'crowdsec-privacy-notice.sh',command],180))
        if command!='reconcile-initial': require(report['expected_count']==0)
    empty_activation()
    deploy(manifest,baseline=True)
    collector=Collector(); rows=[]; stopped=threading.Event(); failures=[]
    def sample_cold_start():
        next_sample=time.monotonic()
        try:
            while not stopped.is_set():
                require(time.time()<manifest['deadline'])
                rows.append(collector.sample())
                next_sample+=0.1
                stopped.wait(max(0,next_sample-time.monotonic()))
        except Exception: failures.append(1)
    worker=threading.Thread(target=sample_cold_start); worker.start()
    try: candidate_cutover(manifest)
    finally:
        stopped.set(); worker.join(15)
    require(not worker.is_alive() and not failures)
    from protocol import check_series
    check_series(rows,0.1,2)
    require(collector.kernel_oom()==collector.kernel_initial)
    verify(manifest,{})
    return {'final_lifecycles':final_records(),'valid':1,'cold_start':1,'samples':len(rows),'combined_peak':max(row['used'] for row in rows),
            'minimum_available':min(row['available'] for row in rows),
            'retired_cgroup_peaks':list(collector.last_groups.values())}


def candidate_cutover(manifest):
    bundle=effect(['mktemp','-d','/run/aboutme/crowdsec-bundle.XXXXXXXX'],10).decode().strip()
    require(re.fullmatch('/run/aboutme/crowdsec-bundle.[A-Za-z0-9]{8}',bundle))
    effect(['chmod','0700',bundle],10)
    archive=bundle+'/archive.tar'
    effect(['cp','--',str(ROOT/'candidate.tar'),archive],30)
    effect(['chmod','0600',archive],10)
    require(digest(pathlib.Path(archive))==manifest['bundle_sha256'])
    effect(['tar','--no-same-owner','-xf',archive,'-C',bundle],60)
    owner=effect(['env','ABOUTME_INSTALL_ONLY=crowdsec-stage','bash',bundle+'/host/install.sh',
                  bundle,manifest['tag'],manifest['bundle_sha256']],1800).decode()
    require(re.fullmatch('[A-Za-z0-9_-]{43}\n',owner)); owner=owner.strip()
    effect([LIB+'fence.sh','mutation-start',owner,manifest['tag'],'reset-db'],35)
    images=manifest['images']
    effect([LIB+'deploy-host.sh',owner,manifest['tag'],'pull',images['server'],images['web'],images['caddy']],1800)
    effect([LIB+'crowdsec-cutover-host.sh','quiesce',owner,manifest['tag'],bundle,manifest['bundle_sha256']],1200)
    effect([LIB+'deploy-host.sh',owner,manifest['tag'],'cutover-render',images['server'],images['web'],images['caddy']],180)
    effect([LIB+'crowdsec-cutover-host.sh','transition',owner,manifest['tag'],bundle,manifest['bundle_sha256']],1200)
    effect([LIB+'fence.sh','cutover-complete',owner,manifest['tag']],180)


def verify(manifest,options):
    require(not options); isolated(manifest)
    require(psql('SELECT count(*) FROM users;') in ('0','1'))
    state=json.loads(effect([LIB+'fence.sh','read'],35))
    require(state.get('operation_id') is None)
    require(not pathlib.Path('/var/lib/aboutme/cutover-mutation.json').exists())
    require(not list(pathlib.Path('/run/aboutme/crowdsec-cutover').glob('*.json')))
    release=json.loads(effect([LIB+'deploy-host.sh','-','-','release-json'],35))
    require(release['release_tag']==manifest['tag'])
    for name,image in manifest['images'].items(): require(release[name+'_image']==image)
    for helper in ('crowdsec-db.sh','crowdsec-retention-lease-check.sh'):
        args=[LIB+helper]+(['preflight-ready'] if helper=='crowdsec-db.sh' else [])
        effect(args,90)
    for timer in ('aboutme-crowdsec-retention','aboutme-crowdsec-vacuum','aboutme-watch'):
        effect(['systemctl','is-enabled','--quiet',timer+'.timer'],5)
        effect(['systemctl','is-active','--quiet',timer+'.timer'],5)
    for unit in ('aboutme-server','aboutme-web','aboutme-caddy','crowdsec','crowdsec-firewall-bouncer'):
        effect(['systemctl','is-active','--quiet',unit+'.service'],5)
    require(int(capture(['nproc']))==2 and capture(['uname','-m']).strip()=='x86_64')
    require(read_environment('/etc/os-release')['VERSION_ID'].strip(chr(34))=='24.04')
    memory=pathlib.Path('/proc/meminfo').read_text()
    from metrics import parse_memory
    require(3900000000<=parse_memory(memory)['total']<=4294967296)
    swaps=pathlib.Path('/proc/swaps').read_text().splitlines()[1:]
    require(len(swaps)==1 and swaps[0].split()[0]=='/dev/zram0')
    require(int(pathlib.Path('/sys/block/zram0/disksize').read_text())==536870912)
    for unit,ceiling in (('postgresql@18-main',1073741824),('crowdsec',268435456),('crowdsec-firewall-bouncer',67108864)):
        require(int(capture(['systemctl','show','--property=MemoryMax','--value',unit+'.service']).strip())==ceiling)
    for name,ceiling in (('aboutme-server',536870912),('aboutme-web',268435456),('aboutme-caddy',134217728)):
        require(int(capture(['podman','inspect','--format','{{.HostConfig.Memory}}',name]).strip())==ceiling)
    settings=psql("SELECT current_setting('shared_buffers'),current_setting('work_mem'),current_setting('maintenance_work_mem'),current_setting('max_connections');")
    require(settings=='256MB|4MB|64MB|40')
    empty_activation()


def seed(manifest,options):
    require(set(options)=={'action'})
    action=options['action']; require(action in ('registration-enable','registration-complete','idempotency','job-check'))
    verify(manifest,{})
    email='fixture@'+manifest['origin'].removeprefix('https://')
    if action=='registration-enable':
        require(psql('SELECT count(*) FROM users;')=='0')
        change_environment('/etc/aboutme/flags.env',{'PASSWORD_REGISTRATION_ENABLED':'true'})
        deploy(manifest); return
    if action=='registration-complete':
        require(psql('SELECT count(*) FROM users;')=='0')
        sql="""BEGIN;
DO $body$ DECLARE registration password_registrations; account uuid; amount integer;
BEGIN
  IF (SELECT count(*) FROM users)<>0 OR (SELECT count(*) FROM password_registrations)<>1 THEN RAISE EXCEPTION 'invalid'; END IF;
  SELECT * INTO STRICT registration FROM password_registrations WHERE email='%s' AND name='CrowdSec rehearsal' AND expires_at>now() FOR UPDATE;
  INSERT INTO users(email,name) VALUES(registration.email,registration.name) RETURNING id INTO account;
  INSERT INTO password_credentials(user_id,encoded_hash) VALUES(account,registration.encoded_hash);
  DELETE FROM auth_email_jobs WHERE registration_id=registration.id AND kind='verify';
  GET DIAGNOSTICS amount=ROW_COUNT; IF amount<>1 THEN RAISE EXCEPTION 'invalid'; END IF;
  DELETE FROM password_registrations WHERE id=registration.id;
  GET DIAGNOSTICS amount=ROW_COUNT; IF amount<>1 THEN RAISE EXCEPTION 'invalid'; END IF;
END $body$;
COMMIT;""" % email
        psql(sql)
        change_environment('/etc/aboutme/flags.env',{'PASSWORD_REGISTRATION_ENABLED':'false'})
        deploy(manifest); return
    require(psql("SELECT count(*) FROM users WHERE email='%s';" % email)=='1')
    require(psql('SELECT count(*) FROM users;')=='1')
    if action=='job-check':
        require(psql("SELECT count(*) FROM idempotency_records WHERE route='rehearsal-expiry';")=='0'); return
    psql("""BEGIN;
DO $body$ DECLARE account uuid;
BEGIN
  SELECT id INTO STRICT account FROM users WHERE email='%s';
  IF EXISTS(SELECT 1 FROM idempotency_records WHERE route='rehearsal-expiry') THEN RAISE EXCEPTION 'invalid'; END IF;
  INSERT INTO idempotency_records(user_id,route,idempotency_key,request_hash,response_status,response_body,created_at,expires_at)
  SELECT account,'rehearsal-expiry',md5('%s:'||n)::uuid,decode(repeat('00',32),'hex'),200,'{}',now()-interval '48 hours',now()-interval '24 hours' FROM generate_series(1,10000) n;
  INSERT INTO idempotency_usage(user_id,retained_records,stored_bytes)
  SELECT account,count(*),sum(octet_length(response_body::text)+octet_length(response_headers::text)) FROM idempotency_records WHERE user_id=account
  ON CONFLICT(user_id) DO UPDATE SET retained_records=EXCLUDED.retained_records,stored_bytes=EXCLUDED.stored_bytes;
END $body$;
COMMIT;""" % (email,manifest['run_id']))
    require(psql("SELECT count(*) FROM idempotency_records WHERE route='rehearsal-expiry' AND expires_at<now();")=='10000')


def measure(manifest,options):
    require(set(options)=={'scenario'} and options['scenario'] in ('serving','print','job','deploy'))
    verify(manifest,{})
    scenario=options['scenario']
    if scenario in ('serving','print'):
        Collector().collect(150,1); return
    # No scheduled heavy job may race this fixed measured overlap.
    for timer in ('media-deletion-sweep','media-orphan-sweep','idempotency-expiry-sweep','privacy-retention-sweep'):
        effect(['systemctl','stop','aboutme-job-'+timer+'.timer'],10)
    collector=Collector(); failures=[]; done=threading.Event(); completed=[None]; migration_noop=[0]
    def operation():
        try:
            time.sleep(30)
            if scenario=='deploy':
                migration_before=psql('SELECT count(*),COALESCE(max(version_id),0) FROM goose_db_version;')
                deploy(manifest,first=True,observe=lambda step: print(json.dumps({'effect':step,'t':round(time.monotonic()-collector.started,6)},separators=(',',':')),flush=True))
                migration_noop[0]=int(psql('SELECT count(*),COALESCE(max(version_id),0) FROM goose_db_version;')==migration_before)
            else:
                backup=subprocess.Popen(['runuser','-u','postgres','--','pgbackrest','--stanza=aboutme','--type=full','backup'],
                                        stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
                try:
                    before=capture(['systemctl','show','--property=InvocationID','--value','aboutme-job@idempotency-expiry-sweep.service']).strip()
                    effect(['systemctl','start','aboutme-job@idempotency-expiry-sweep.service'],1800)
                    after=capture(['systemctl','show','--property=InvocationID','--value','aboutme-job@idempotency-expiry-sweep.service']).strip()
                    require(after and after!=before)
                    final_record('aboutme-job@idempotency-expiry-sweep',after)
                    require(backup.wait(timeout=min(1800,manifest['deadline']-time.time()))==0)
                    require(capture(['systemctl','show','--property=Result','--value','aboutme-job@idempotency-expiry-sweep.service']).strip()=='success')
                finally:
                    if backup.poll() is None: backup.terminate(); backup.wait(timeout=10)
        except Exception:
            failures.append(1)
        finally:
            completed[0]=time.monotonic(); done.set()
    worker=threading.Thread(target=operation); worker.start()
    rows=[]; overlap=False; deadline=time.monotonic()+1800; next_sample=time.monotonic()
    try:
        while time.monotonic()<deadline:
            require(time.time()<manifest['deadline'])
            elapsed=time.monotonic()-collector.started
            interval=0.1 if elapsed>=25 and (not done.is_set() or time.monotonic()<completed[0]+10) else 1
            time.sleep(max(0,next_sample-time.monotonic()))
            row=collector.sample(); row['interval']=interval; rows.append(row)
            groups=row['cgroups']; serving=groups['container-server'].get('present') and groups['container-caddy'].get('present')
            overlap=overlap or bool(serving and (groups['container-maintenance'].get('present') if scenario=='deploy' else
                                  groups['container-job-idempotency-expiry-sweep'].get('present') and row['backup_count']>0))
            print(json.dumps(row,separators=(',',':')),flush=True)
            require(not failures)
            if done.is_set() and row['t']>=max(150,completed[0]-collector.started+10): break
            next_sample+=interval
        require(done.is_set() and not failures and overlap)
        from protocol import check_series
        check_series(rows,1,120)
        for before,after in zip(rows,rows[1:]): require(after['t']-before['t']<=2*before['interval']+0.001)
        require(collector.kernel_oom()==collector.kernel_initial)
        print(json.dumps({'final_lifecycles':final_records(),'valid':1,'samples':len(rows),'overlap':1,'combined_peak':max(x['used'] for x in rows),
                          'minimum_available':min(x['available'] for x in rows),'migration_noop':migration_noop[0]},separators=(',',':')),flush=True)
    finally:
        worker.join(1810)
    require(not worker.is_alive())
    if scenario=='job':
        for timer in ('media-deletion-sweep','media-orphan-sweep','idempotency-expiry-sweep','privacy-retention-sweep'):
            effect(['systemctl','start','aboutme-job-'+timer+'.timer'],10)


def main():
    try:
        require(len(sys.argv)==2 and sys.argv[1] in ('bootstrap','seed','sample','measure','verify'))
        manifest,options=context()
        if sys.argv[1]=='sample':
            require(not options); print(json.dumps(Collector().sample(),separators=(',',':')))
        else:
            result={'bootstrap':bootstrap,'seed':seed,'measure':measure,'verify':verify}[sys.argv[1]](manifest,options)
            if result is not None: print(json.dumps(result,separators=(',',':')))
            elif sys.argv[1]!='measure': print('VALID '+sys.argv[1])
    except Exception:
        print('INVALID host-operation',file=sys.stderr)
        sys.exit(1)


if __name__=='__main__': main()
