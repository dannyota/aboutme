#!/usr/bin/env bash
# Versioned durable state and closed wire output for host operations.
set -euo pipefail

# The caller selects the request before saving its state.
declare id

operation_id_valid() { [[ $1 =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]]; }
operation_initialize() {
  operations_dir=/srv/data/host-operations
  mountpoint -q /srv/data || return 1
  for parent in /srv /srv/data; do
    [[ -d $parent && ! -L $parent && $(stat -c '%u' "$parent") == 0 ]] || return 1
    [[ $(stat -c '%a' "$parent") =~ ^[0-7][0145][0145]$ ]] || return 1
  done
  if [[ ! -e $operations_dir && ! -L $operations_dir ]]; then
    install -d -o root -g root -m 0700 "$operations_dir"
    sync /srv/data
  fi
  [[ -d $operations_dir && ! -L $operations_dir && $(stat -c '%u:%a' "$operations_dir") == 0:700 ]] || return 1
  [[ ! -L $operations_dir/.bundle.lock ]] || return 1
  exec {operation_bundle_fd}>>"$operations_dir/.bundle.lock"
  [[ $(stat -c '%u:%a' "$operations_dir/.bundle.lock") == 0:600 ]] || return 1
  flock -s -w 30 "$operation_bundle_fd"
}

operation_state() { # mode id; payload through descriptor 3
  python3 - "$1" "${2:-}" "${operations_dir:-/srv/data/host-operations}" <<'PY'
import datetime, fcntl, json, os, re, secrets, stat, sys, tempfile
mode, request_id, root = sys.argv[1:]
ID = r'[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]'
UUID = r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}'
SLUG = r'[a-z0-9]+(?:-[a-z0-9]+)*'
# Reserved roots use the same contract as the public namespace.
reserved_slugs = {'.well-known','admin','api','app','authorize','forgot-password','guide','healthz','_nuxt','internal-render','llms.txt','login','mcp','oauth','people','print','privacy','readyz','register','reset-password','robots.txt','showcase','sitemap.xml','templates','terms','u','verify','verify-email'}
def slug(x):
    return isinstance(x, str) and 4 <= len(x) <= 30 and re.fullmatch(SLUG, x) and x not in reserved_slugs
def identifier(x):
    return isinstance(x, str) and re.fullmatch(ID, x)
def pairs(items):
    out = {}
    for k, v in items:
        if k in out: raise ValueError('duplicate')
        out[k] = v
    return out
def decode(raw, limit):
    if len(raw) > limit: raise ValueError('size')
    if mode in ('envelope','wire') and (raw.count(b'\n')>1 or b'\n' in raw.rstrip(b'\n')): raise ValueError('lines')
    return json.loads(raw, object_pairs_hook=pairs)
def payload(limit=2097152):
    with os.fdopen(os.dup(3), 'rb') as f: return decode(f.read(limit+1), limit)
def exact(x, keys):
    if not isinstance(x, dict) or set(x) != set(keys.split()): raise ValueError('fields')
def utc(): return datetime.datetime.now(datetime.timezone.utc)
def stamp(t): return t.strftime('%Y-%m-%dT%H:%M:%SZ')
def parse(t):
    if not isinstance(t,str) or not re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ',t): raise ValueError('time')
    return datetime.datetime.strptime(t,'%Y-%m-%dT%H:%M:%SZ').replace(tzinfo=datetime.timezone.utc)
def release(x):
    exact(x, 'caddy_image maintenance_image maintenance_number maintenance_tag release_number release_tag server_image web_image')
    for prefix in ('release','maintenance'):
        tag=x[prefix+'_tag']
        m=re.fullmatch(r'v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})', tag)
        if not m or x[prefix+'_number'] != sum(int(n)*v for n,v in zip(m.groups(),(1000000,1000,1))): raise ValueError('release')
    for name in ('server','web','caddy','maintenance'):
        repo = 'caddy' if name == 'maintenance' else name
        if not re.fullmatch(r'ghcr\.io/dannyota/aboutme-'+repo+r'@sha256:[0-9a-f]{64}',x[name+'_image']): raise ValueError('image')
def envelope(x):
    if not isinstance(x,dict): raise ValueError('object')
    verb=x.get('verb')
    keys={'show':'verb kind slug','apply':'verb requestId confirmation','status':'verb requestId','resume':'verb requestId','verify':'verb requestId','abort':'verb requestId','proof':'verb requestId phase nonce result'}
    if verb not in keys: raise ValueError('verb')
    exact(x,keys[verb])
    if verb == 'show':
        if x['kind'] != 'account-delete' or not slug(x['slug']): raise ValueError('target')
    else:
        if not identifier(x['requestId']): raise ValueError('id')
    if verb == 'apply':
        match=re.fullmatch(r'delete-account:('+ID+'):('+UUID+r'):('+SLUG+')',x['confirmation'])
        if not match or match[1] != x['requestId'] or not slug(match[3]): raise ValueError('confirmation')
    if verb == 'proof':
        if x['phase'] != 'serving' or not identifier(x['nonce']) or x['result'] not in ('pass','fail'): raise ValueError('proof')
    return x
state_keys='schemaVersion requestId kind phase outcome createdAt expiresAt confirmation release action challenge recoveryCopy planningDeadline flagsHash dbSecretId attemptCount attempts intent pendingVerb fenceHeld reserved closureQuiescent evidenceAt proofReceipt'
def state(x):
    exact(x,state_keys)
    if type(x['schemaVersion']) != int or x['schemaVersion'] != 1 or not identifier(x['requestId']) or x['kind'] != 'account-delete': raise ValueError('version')
    if x['phase'] not in ('planning','planned','accepted','serving','closed') or x['outcome'] not in ('pending','complete','refused','unknown','absent_after_unknown','aborted','unverified'): raise ValueError('transition')
    for key in ('createdAt','planningDeadline','evidenceAt'): parse(x[key])
    if x['expiresAt'] is not None: parse(x['expiresAt'])
    release(x['release'])
    if not re.fullmatch(r'[0-9a-f]{64}',x['flagsHash']) or not isinstance(x['dbSecretId'],str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}',x['dbSecretId']): raise ValueError('metadata')
    for key in ('fenceHeld','reserved','closureQuiescent'):
        if type(x[key]) != bool: raise ValueError('boolean')
    if x['intent'] not in ('none','show','delete','status','queue','show-verify','sweep') or x['pendingVerb'] not in ('show','apply','resume','verify','abort','none'): raise ValueError('intent')
    a=x['action']; exact(a,'userId reportedSlug shownSlugs returnedSlugs writeOutcome publicVerification physicalCleanup')
    if a['userId'] is not None and not re.fullmatch(UUID,a['userId']): raise ValueError('uuid')
    if a['reportedSlug'] is not None and not slug(a['reportedSlug']): raise ValueError('slug')
    for key in ('shownSlugs','returnedSlugs'):
        values=a[key]
        if values is None and key == 'returnedSlugs': continue
        if not isinstance(values,list) or len(values)>3 or values != sorted(set(values)) or not all(slug(s) for s in values): raise ValueError('slugs')
    if a['writeOutcome'] not in ('not_started','committed','not_found','mismatch','unknown','present','absent_after_unknown','evidence_expired'): raise ValueError('write')
    if a['publicVerification'] not in ('pending','passed','failed','limited','expired') or a['physicalCleanup'] not in ('pending','complete'): raise ValueError('verification')
    if x['confirmation'] is not None:
        if x['phase'] != 'planned' or x['confirmation'] != 'delete-account:'+x['requestId']+':'+str(a['userId'])+':'+str(a['reportedSlug']) or a['reportedSlug'] not in a['shownSlugs']: raise ValueError('binding')
    if x['recoveryCopy'] is not None: raise ValueError('rotation')
    if type(x['attemptCount'])!=int or not 0<=x['attemptCount']<=999999: raise ValueError('attempt count')
    if not isinstance(x['attempts'],list) or len(x['attempts'])>999999: raise ValueError('attempts')
    for attempt in x['attempts']:
        exact(attempt,'number container applicationName startedAt stopped')
        n=attempt['number']
        if n != x['attemptCount']-len(x['attempts'])+x['attempts'].index(attempt)+1: raise ValueError('sequence')
        if type(n) != int or not 1<=n<=999999 or attempt['container'] != 'aboutme-operation-'+x['requestId']+'-'+str(n) or attempt['applicationName'] != 'hostop:'+x['requestId']+':'+str(n) or type(attempt['stopped']) != bool: raise ValueError('attempt')
        parse(attempt['startedAt'])
    challenge=x['challenge']
    if challenge is not None:
        exact(challenge,'requestId digest phase nonce expiresAt')
        if challenge['requestId'] != x['requestId'] or challenge['digest'] != x['release']['server_image'].split('@')[1] or challenge['phase']!='serving' or not identifier(challenge['nonce']): raise ValueError('challenge')
        parse(challenge['expiresAt'])
    receipt=x['proofReceipt']
    if receipt is not None:
        exact(receipt,'nonce result')
        if not identifier(receipt['nonce']) or receipt['result'] not in ('pass','fail'): raise ValueError('receipt')
    return x

def safe(path, directory=False):
    s=os.lstat(path)
    if s.st_uid != 0 or stat.S_IMODE(s.st_mode) != (0o700 if directory else 0o600) or not (stat.S_ISDIR(s.st_mode) if directory else stat.S_ISREG(s.st_mode)): raise ValueError('unsafe')
def read(path):
    safe(path)
    with open(path,'rb') as f: return decode(f.read(2097153),2097152)
def atomic(path,x):
    data=(json.dumps(x,separators=(',',':'),sort_keys=True)+'\n').encode()
    if len(data)>2097152: raise ValueError('size')
    fd,tmp=tempfile.mkstemp(prefix='.write-',dir=os.path.dirname(path))
    try:
        with os.fdopen(fd,'wb') as f:
            f.write(data); f.flush(); os.fsync(f.fileno())
        os.replace(tmp,path)
        fd=os.open(os.path.dirname(path),os.O_DIRECTORY)
        try: os.fsync(fd)
        finally: os.close(fd)
    finally:
        if os.path.exists(tmp): os.unlink(tmp)
def usage(states):
    total=0
    for directory in os.scandir(root):
        if directory.name in ('.admission.lock','.bundle.lock'): continue
        if not identifier(directory.name): raise ValueError('unexpected file')
        safe(directory.path,True)
        s=state(read(directory.path+'/state.json'))
        if s['requestId'] != directory.name: raise ValueError('identity')
        size=0
        for entry in os.scandir(directory.path):
            safe(entry.path)
            size+=entry.stat(follow_symlinks=False).st_size
        if size > 2097152 and s['reserved']: raise ValueError('reservation overflow')
        total+=max(2097152,size) if s['reserved'] or not s['closureQuiescent'] else size
        states.append((directory.path,s))
    return total

def expired(x): return utc() >= parse(x['evidenceAt'])+datetime.timedelta(days=180)
def erase(x):
    x['action'].update(userId=None,reportedSlug=None,shownSlugs=[],returnedSlugs=[],writeOutcome='evidence_expired',publicVerification='expired')
    x['confirmation']=None; x['challenge']=None; x['proofReceipt']=None
    return x

def output(x):
    if expired(x): x=erase(x)
    keys='schemaVersion requestId kind phase outcome release expiresAt confirmation action challenge recoveryCopy'.split()
    data=json.dumps({k:x[k] for k in keys},separators=(',',':'))
    if len(data)>16384: raise ValueError('output size')
    print(data)

try:
    if mode == 'envelope':
        print(json.dumps(envelope(payload(4096)),separators=(',',':'))); sys.exit(0)
    if mode == 'wire':
        x=payload(16384)
        exact(x,'schemaVersion requestId kind phase outcome release expiresAt confirmation action challenge recoveryCopy')
        defaults=dict(createdAt=stamp(utc()),planningDeadline=stamp(utc()),evidenceAt=stamp(utc()),flagsHash='0'*64,dbSecretId='wire',attemptCount=0,attempts=[],intent='none',pendingVerb='none',fenceHeld=False,reserved=False,closureQuiescent=True,proofReceipt=None)
        state(dict(defaults,**x))
        print(json.dumps(x,separators=(',',':'))); sys.exit(0)
    safe(root,True)
    if mode not in ('new','save','prune','read','load'): raise ValueError('mode')
    if mode not in ('new','prune') and not identifier(request_id): raise ValueError('id')
    if mode == 'read':
        path=root+'/'+request_id; safe(path,True)
        x=state(read(path+'/state.json'))
        if x['requestId'] != request_id: raise ValueError('identity')
        output(x); sys.exit(0)
    if mode == 'load':
        path=root+'/'+request_id; safe(path,True)
        x=state(read(path+'/state.json'))
        if x['requestId'] != request_id: raise ValueError('identity')
        if expired(x): x=erase(x)
        print(json.dumps(x,separators=(',',':'))); sys.exit(0)
    lockpath=root+'/.admission.lock'
    fd=os.open(lockpath,os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600)
    safe(lockpath); fcntl.flock(fd,fcntl.LOCK_EX)
    records=[]; total=usage(records)
    if mode == 'prune':
        for path,x in records:
            request_lock=os.open(path+'/.request.lock',os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600)
            safe(path+'/.request.lock')
            try: fcntl.flock(request_lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
            except BlockingIOError:
                os.close(request_lock); continue
            # Reload only after acquiring the request lock. Admission is held,
            # so writers cannot race accounting or replacements.
            x=state(read(path+'/state.json'))
            if expired(x):
                x=erase(x)
                atomic(path+'/state.json',x)
                for entry in os.scandir(path):
                    if entry.name == 'output' or entry.name.startswith('.write-'):
                        os.unlink(entry.path)
            for entry in os.scandir(path):
                if not entry.name.startswith('audit-'): continue
                event=read(entry.path)
                if utc()>=parse(event['at'])+datetime.timedelta(days=180): os.unlink(entry.path)
            # Nonpersonal attempt metadata expires after 30 days only when stopped.
            if x['closureQuiescent'] and not x['reserved'] and x['attempts'] and all(a['stopped'] and utc()>=parse(a['startedAt'])+datetime.timedelta(days=30) for a in x['attempts']):
                x['attempts']=[]; atomic(path+'/state.json',x)
            directory_fd=os.open(path,os.O_DIRECTORY)
            try: os.fsync(directory_fd)
            finally: os.close(directory_fd)
            os.close(request_lock)
        sys.exit(0)
    x=state(payload())
    if mode == 'new':
        if total+2097152>67108864: raise ValueError('capacity')
        request_id=x['requestId']; path=root+'/'+request_id
        os.mkdir(path,0o700); atomic(path+'/state.json',x)
        root_fd=os.open(root,os.O_DIRECTORY)
        try: os.fsync(root_fd)
        finally: os.close(root_fd)
    else:
        path=root+'/'+request_id; safe(path,True)
        old=state(read(path+'/state.json'))
        if expired(old): x=erase(x)
        if x['requestId'] != request_id: raise ValueError('identity')
        actual=sum(e.stat(follow_symlinks=False).st_size for e in os.scandir(path))
        charge=max(2097152,actual) if old['reserved'] or not old['closureQuiescent'] else actual
        if x['reserved'] and total-charge+2097152>67108864: raise ValueError('capacity')
        if not x['reserved'] and not x['closureQuiescent']: raise ValueError('uncertain closure')
        # Audit intent is durable before the state replacement. A failed audit
        # write prevents dispatch, and a failed result write leaves intent unknown.
        event={'at':stamp(utc()),'requestId':request_id,'kind':x['kind'],'phase':x['phase'],'outcome':x['outcome'],'action':x['action']}
        growth=len(json.dumps(event).encode())+2*len(json.dumps(x).encode())+4096
        if actual+growth>2097152 or total-charge+max(actual+growth,2097152 if x['reserved'] else 0)>67108864: raise ValueError('capacity')
        atomic(path+'/audit-'+secrets.token_hex(16)+'.json',event)
        atomic(path+'/state.json',x)
    print(json.dumps(x,separators=(',',':')))
except (ValueError,TypeError,KeyError,OSError,OverflowError,RecursionError):
    print('protocol_failed',file=sys.stderr); sys.exit(1)
PY
}

operation_envelope() { operation_state envelope '' 3<<<"$1"; }
operation_load() { state=$(operation_state load "$1"); }
operation_save() { state=$(operation_state save "$id" 3<<<"$state"); }
operation_output() { operation_state read "$1"; }
