"""Fixed synthetic account and corpus through the public HTTPS APIs."""
import concurrent.futures
import copy
import hashlib
import http.cookiejar
import json
import math
import pathlib
import secrets
import threading
import time
import urllib.error
import urllib.request
import uuid
from protocol import require, digest
from common import Runner
from http_exchange import NoRedirect, exchange
from types import SimpleNamespace


def canonical(document):
    text=json.dumps(document,ensure_ascii=False,separators=(',',':'))
    for char,escaped in (('<',r'\u003c'),('>',r'\u003e'),('&',r'\u0026'),('\u2028',r'\u2028'),('\u2029',r'\u2029')):
        text=text.replace(char,escaped)
    return text.encode()


def bounded_body(value):
    data=canonical(value)
    require(len(data)<=262144)
    return data


class Client:
    def __init__(self,manifest,fixtures,runner):
        self.runner=runner
        self.manifest=manifest
        self.origin=manifest['origin']
        self.fixtures=pathlib.Path(fixtures)
        self.cookies=http.cookiejar.CookieJar()
        self.opener=urllib.request.build_opener(NoRedirect(),urllib.request.HTTPCookieProcessor(self.cookies))
        self.csrf=None; self.last_write=0; self.resumes={}; self.documents={}
        exported=json.loads(self.fixtures.joinpath('manifest.json').read_bytes())
        self.schema=exported['schemaVersion']
        require([x['name'] for x in exported['fixtures']]==['minimal','full','maximum'])
        for row in exported['fixtures']:
            require(row['file']==row['name']+'.json')
            require(digest(self.fixtures/row['file'])==row['sha256'])
            self.documents[row['name']]=json.loads((self.fixtures/row['file']).read_bytes())
        photo=exported['fixtures'][1]['photo']
        require(photo['file']=='full-photo.png' and digest(self.fixtures/'full-photo.png')==photo['sha256'])
        require(len((self.fixtures/'maximum.json').read_bytes())==524288)

    def request(self,method,path,body=None,expected=200,revision=None,content_type='application/json',pdf=False,maintenance=False):
        require(path.startswith('/api/v1/') or path.startswith('/cs-'+self.manifest['run_id']+'-'))
        self.runner.bound(1)
        headers={'Origin':self.origin}
        if method not in ('GET','HEAD'):
            headers['Content-Type']=content_type
            if self.csrf: headers['X-CSRF-Token']=self.csrf
        if '/resumes' in path and not pdf:
            headers['X-Resume-Schema-Version']=str(self.schema)
            if method not in ('GET','HEAD'):
                delay=max(0,0.5-(time.monotonic()-self.last_write))
                if delay: time.sleep(self.runner.bound(delay))
                self.runner.bound(1)
                self.last_write=time.monotonic()
                headers['Idempotency-Key']=str(uuid.uuid4())
                if revision is not None: headers['If-Match']='"r'+str(revision)+'"'
        payload=bounded_body(body) if isinstance(body,dict) else body
        req=urllib.request.Request(self.origin+path,data=payload,headers=headers,method=method)
        started=time.monotonic()
        self.cookies.add_cookie_header(req)
        code,headers,raw=exchange(self.runner,req,16777216 if pdf else 2097152,20 if pdf else 10)
        self.runner.bound(1)
        self.cookies.extract_cookies(SimpleNamespace(info=lambda:headers),req)
        if maintenance and code==503:
            require(b'aboutme:maintenance' in raw)
            return None,time.monotonic()-started
        require(code==expected)
        if pdf:
            require(raw.startswith(b'%PDF-') and raw.rstrip().endswith(b'%%EOF') and len(raw)<=16777216)
            require(headers.get('Cache-Control')=='no-store, no-transform')
        elapsed=time.monotonic()-started
        require(elapsed<=20 if pdf else elapsed<=10)
        return (json.loads(raw)['data'] if raw and headers.get_content_type()=='application/json' else raw),elapsed

    def register(self,remote):
        password=secrets.token_urlsafe(32)
        email='fixture@'+self.origin.removeprefix('https://')
        remote('seed',{'action':'registration-enable'})
        self.request('POST','/api/v1/auth/password/register',
                     {'name':'CrowdSec rehearsal','email':email,'password':password},202)
        remote('seed',{'action':'registration-complete'})
        self.request('POST','/api/v1/auth/password/login',{'email':email,'password':password},204)
        password=None
        me,_=self.request('GET','/api/v1/me')
        self.csrf=me['csrfToken']

    def write(self,name,path,body,method='PATCH',content_type='application/json'):
        resume=self.resumes[name]
        data,_=self.request(method,'/api/v1/resumes/'+resume['id']+path,body,
                            revision=resume['revision'],content_type=content_type)
        self.resumes[name]=data
        return data

    def seed(self):
        for name,document in self.documents.items():
            seed=copy.deepcopy(document); seed['personalDetails'].pop('photo',None)
            if name=='maximum':
                seed['content']={}
                seed['customization']['layout']['sections']={'main':[],'sidebar':[]}
            data,_=self.request('POST','/api/v1/resumes',{'title':'Rehearsal '+name,'lng':'en','document':seed},201)
            self.resumes[name]=data
            if name=='maximum':
                for column in ('main','sidebar'):
                    for index,key in enumerate(document['customization']['layout']['sections'][column]):
                        section=document['content'][key]
                        command={'op':'createSection','key':key,'sectionType':section['sectionType'],
                                 'column':column,'index':index}
                        for field in ('displayName','iconKey'):
                            if field in section: command[field]=section[field]
                        self.write(name,'/structure',{'commands':[command]})
                        for entry in section['entries']:
                            self.write(name,'/entries/'+key,{'entry':entry})
            if name=='full':
                boundary='aboutme-rehearsal-photo'
                body=('--'+boundary+'\r\nContent-Disposition: form-data; name="file"; filename="photo.png"\r\n'
                      'Content-Type: image/png\r\n\r\n').encode()+self.fixtures.joinpath('full-photo.png').read_bytes()+(
                          '\r\n--'+boundary+'--\r\n').encode()
                self.write(name,'/photo',body,'POST','multipart/form-data; boundary='+boundary)
                crop=document['personalDetails'].get('photo',{}).get('crop')
                if crop is not None: self.write(name,'/photo',{'crop':crop})
            slug='cs-'+self.manifest['run_id']+'-'+name
            self.write(name,'/publish',{'slug':slug,'live':True,'downloadEnabled':True,'seoGeoEnabled':False},'POST')
            self.verify(name)

    def verify(self,name):
        resume=self.resumes[name]
        actual,_=self.request('GET','/api/v1/resumes/'+resume['id'])
        expected=copy.deepcopy(self.documents[name]); received=copy.deepcopy(actual['document'])
        if name=='full':
            actual_photo=received['personalDetails'].pop('photo')
            expected_photo=expected['personalDetails'].pop('photo')
            require(actual_photo.get('crop')==expected_photo.get('crop'))
        require(received==expected)
        if name=='maximum': require(len(canonical(received))==524288)
        require(actual['live'] is True and actual['slug']==resume['slug'])

    def request_load(self,name,pdf=False,public=False,maintenance=False):
        resume=self.resumes[name]
        path=('/'+resume['slug']) if public else '/api/v1/resumes/'+resume['id']+('/pdf' if pdf else '')
        data,elapsed=self.request('GET',path,pdf=pdf,maintenance=maintenance)
        return None if data is None else elapsed


class Load:
    def __init__(self,client,name,printing=False,maintenance=False):
        self.client=client; self.name=name; self.printing=printing; self.maintenance=maintenance
        self.stop=threading.Event(); self.lock=threading.Lock(); self.results=[]; self.errors=[]
        self.started=time.monotonic(); self.workers=[]

    def worker(self,index):
        next_start=self.started+index*0.5; number=0
        while not self.stop.is_set():
            try:
                delay=max(0,next_start-time.monotonic())
                if delay and self.stop.wait(self.client.runner.bound(delay)): break
                self.client.runner.bound(1)
            except Exception:
                with self.lock: self.errors.append(1)
                self.stop.set(); return
            try:
                latency=self.client.request_load(self.name,public=number%2==1,maintenance=self.maintenance)
                with self.lock: self.results.append((time.monotonic(), 'maintenance' if latency is None else ('public' if number%2 else 'api'),latency))
            except Exception:
                with self.lock: self.errors.append(1)
                self.stop.set(); return
            next_start=max(next_start+2,time.monotonic()); number+=1

    def pdf_worker(self):
        next_start=self.started
        while not self.stop.is_set():
            try:
                delay=max(0,next_start-time.monotonic())
                if delay and self.stop.wait(self.client.runner.bound(delay)): break
                self.client.runner.bound(1)
            except Exception:
                with self.lock: self.errors.append(1)
                self.stop.set(); return
            try:
                latency=self.client.request_load(self.name,pdf=True)
                with self.lock: self.results.append((time.monotonic(),'pdf',latency))
            except Exception:
                with self.lock: self.errors.append(1)
                self.stop.set(); return
            next_start=max(next_start+7.5,time.monotonic())

    def runner_join_budget(self):
        runner=self.client.runner
        if runner.deadline is None: return 25
        return max(0,min(25,runner.deadline-time.time(),runner.monotonic_deadline-time.monotonic()))

    def __enter__(self):
        try:
            for index in range(4):
                self.client.runner.bound(1)
                worker=threading.Thread(target=self.worker,args=(index,)); worker.start(); self.workers.append(worker)
            if self.printing:
                self.client.runner.bound(1)
                worker=threading.Thread(target=self.pdf_worker); worker.start(); self.workers.append(worker)
            return self
        except BaseException:
            self.stop.set()
            for worker in self.workers: worker.join(self.runner_join_budget())
            raise

    def __exit__(self,*args):
        self.stop.set()
        for worker in self.workers: worker.join(self.runner_join_budget())
        require(all(not worker.is_alive() for worker in self.workers))
        require(not self.errors)

    def summary(self,start,end):
        require(not self.errors)
        summary={}
        for kind,minimum,budget in (('api',100,0.15),('public',100,0.4),('pdf',12,20)):
            if kind=='pdf' and not self.printing: continue
            values=sorted(latency for timestamp,label,latency in self.results if label==kind and start<=timestamp<=end)
            require(len(values)>=minimum)
            p95=values[math.ceil(len(values)*0.95)-1]
            require(p95<=budget)
            summary[kind]={'count':len(values),'p95_seconds':p95}
        return summary
