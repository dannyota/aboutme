"""Exact provider endpoints needed to verify DNS and retire one owned WAN IP."""
import base64
import json
import os
import re
import urllib.request
from protocol import require
from api import NoRedirect


def request(method,url,headers=None,body=None):
    opener=urllib.request.build_opener(NoRedirect())
    with opener.open(urllib.request.Request(url,headers=headers or {},data=body,method=method),timeout=30) as response:
        raw=response.read(2097153); require(len(raw)<=2097152 and response.code in (200,202,204))
        return json.loads(raw) if raw else {}


def dns_records(manifest):
    zone=manifest['config']['zone_id']; require(re.fullmatch('[0-9a-f]{32}',zone))
    headers={'Authorization':'Bearer '+os.environ['CLOUDFLARE_API_TOKEN']}
    root='https://api.cloudflare.com/client/v4/zones/'+zone
    response=request('GET',root,headers)
    require(response['success'] is True and response['result']['name']=='aboutme.vn')
    rows=[]
    for page in range(1,101):
        response=request('GET',root+'/dns_records?per_page=100&page='+str(page),headers)
        require(response['success'] is True and len(response['result'])<=100)
        require(response['result_info']['page']==page and response['result_info']['total_pages']<=100)
        rows.extend(response['result'])
        if page>=response['result_info']['total_pages']: return rows
    raise ValueError('dns_pagination_bound')


def delete_floating(manifest):
    resource=manifest['resources']
    project=manifest['config']['project_id']; identifier=resource['floating_ip_id']
    require(re.fullmatch('[A-Za-z0-9_-]{1,100}',project) and re.fullmatch('[A-Za-z0-9_-]{1,100}',identifier))
    credentials=(os.environ['CLIENT_ID']+':'+os.environ['CLIENT_SECRET']).encode()
    headers={'Authorization':'Basic '+base64.b64encode(credentials).decode(),'Content-Type':'application/json','Accept':'application/json'}
    token=request('POST','https://iamapis.vngcloud.vn/accounts-api/v2/auth/token',headers,b'{"grant_type":"client_credentials"}')
    require(token['token_type'].lower()=='bearer' and isinstance(token['access_token'],str) and token['access_token'])
    request('DELETE','https://hcm-3.api.vngcloud.vn/vserver/vserver-gateway/v2/'+project+'/wanIps/'+identifier,
            {'Authorization':'Bearer '+token['access_token'],'Accept':'application/json'})
