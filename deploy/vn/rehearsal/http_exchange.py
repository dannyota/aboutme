"""Private, deadline-owned HTTPS worker for the fixed rehearsal routes."""
import base64
import email.message
import json
import pathlib
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from protocol import require


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,req,fp,code,msg,headers,newurl):
        raise ValueError('unexpected_redirect')


def exchange(runner,request,maximum,timeout):
    value={'url':request.full_url,'method':request.get_method(),'headers':dict(request.header_items()),
           'body':None if request.data is None else base64.b64encode(request.data).decode(),
           'maximum':maximum,'timeout':timeout}
    response=json.loads(runner.call([sys.executable,'-Es',str(pathlib.Path(__file__).resolve())],
                       json.dumps(value).encode(),timeout=timeout))
    headers=email.message.Message()
    for name,value in response['headers']: headers[name]=value
    raw=base64.b64decode(response['body'],validate=True)
    require(len(raw)<=maximum)
    return response['status'],headers,raw


def worker():
    raw=sys.stdin.buffer.read(1048577); require(len(raw)<=1048576)
    value=json.loads(raw); url=urllib.parse.urlsplit(value['url'])
    require(url.scheme=='https' and url.port in (None,443) and url.username is None and not url.fragment)
    method=value['method']; require(method in ('GET','POST','PATCH','DELETE'))
    if re.fullmatch(r'cs-rehearsal-[0-9a-f]{16}\.aboutme\.vn',url.hostname or ''):
        run=url.hostname.split('.')[0].removeprefix('cs-rehearsal-')
        require(not url.query and (url.path.startswith('/api/v1/') or url.path.startswith('/cs-'+run+'-')))
    elif url.hostname=='api.cloudflare.com':
        require(method=='GET' and re.fullmatch(r'/client/v4/zones/[0-9a-f]{32}(?:/dns_records)?',url.path))
        require(not url.query or re.fullmatch(r'per_page=100&page=[0-9]{1,3}',url.query))
    elif url.hostname=='iamapis.vngcloud.vn':
        require(method=='POST' and url.path=='/accounts-api/v2/auth/token' and not url.query)
    else:
        require(url.hostname=='hcm-3.api.vngcloud.vn' and method=='DELETE' and not url.query)
        require(re.fullmatch(r'/vserver/vserver-gateway/v2/[A-Za-z0-9_-]{1,100}/wanIps/[A-Za-z0-9_-]{1,100}',url.path))
    require(type(value['maximum']) is int and 0<value['maximum']<=16777216)
    require(type(value['timeout']) in (float,int) and 0<value['timeout']<=30)
    body=None if value['body'] is None else base64.b64decode(value['body'],validate=True)
    request=urllib.request.Request(value['url'],headers=value['headers'],data=body,method=method)
    try: response=urllib.request.build_opener(NoRedirect()).open(request,timeout=value['timeout'])
    except urllib.error.HTTPError as error: response=error
    with response:
        raw=response.read(value['maximum']+1); require(len(raw)<=value['maximum'])
        headers=[(key,item) for key,item in response.headers.items() if key.lower() in ('content-type','cache-control','set-cookie')]
        print(json.dumps({'status':response.code,'headers':headers,'body':base64.b64encode(raw).decode()},separators=(',',':')))


if __name__=='__main__':
    try: worker()
    except BaseException: sys.exit(1)
