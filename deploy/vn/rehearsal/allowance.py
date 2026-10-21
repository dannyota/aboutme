"""Conservative integer transcription of the provider console allowance."""
import datetime
import decimal
import re
import time
from protocol import SHA, digest, require


UNITS={'B':(1,1),'KB':(1000,1024),'KiB':(1024,1024),
       'MB':(1000000,1048576),'MiB':(1048576,1048576),
       'GB':(1000000000,1073741824),'GiB':(1073741824,1073741824)}


def bound(row,lower):
    require(set(row)=={'display','unit','precision','rounded'})
    require(isinstance(row['display'],str) and re.fullmatch(r'[0-9]{1,12}(\.[0-9]{1,32})?',row['display']))
    require(row['unit'] in UNITS and type(row['precision']) is int and 0<=row['precision']<=32)
    require(type(row['rounded']) is bool)
    places=len(row['display'].split('.')[1]) if '.' in row['display'] else 0
    require(places==row['precision'])
    with decimal.localcontext() as context:
        context.prec=80
        value=decimal.Decimal(row['display'])
        if row['rounded']:
            quantum=decimal.Decimal(10)**(-row['precision'])
            value=value-quantum if lower else value+quantum
        require(value>=0)
        multiplier=UNITS[row['unit']][0 if lower else 1]
        return int((value*multiplier).to_integral_value(rounding=decimal.ROUND_FLOOR if lower else decimal.ROUND_CEILING))



def validate_allowance(value,manifest,evidence,now=None):
    require(set(value)=={'version','run_id','project_id','region','observed_at','plan_sha256','quota','usage',
                         'quota_lower_bytes','usage_upper_bytes','evidence_sha256','limits','source_kind'})
    require(value['source_kind'] in ('api','console'))
    require(value['version']==1 and value['run_id']==manifest['run_id'])
    require(value['project_id']==manifest['config']['storage_project_id'] and value['region']=='HCM04')
    require(value['plan_sha256']==manifest['plan_sha256'])
    require(re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z',value['observed_at']))
    observed=datetime.datetime.strptime(value['observed_at'],'%Y-%m-%dT%H:%M:%SZ').replace(tzinfo=datetime.timezone.utc).timestamp()
    age=(time.time() if now is None else now)-observed
    require(0<=age<=300)
    require(SHA.fullmatch(value['evidence_sha256']) and digest(evidence)==value['evidence_sha256'])
    require(value['limits']==['provider_usage_refresh_lag_undocumented','multipart_accounting_undocumented'])
    if value['source_kind']=='api':
        import json
        filtered=json.loads(evidence.read_bytes())
        require(set(filtered)=={'project_id','region','quota_lexeme','usage_lexeme','observed_at','units'})
        require(filtered['project_id']==value['project_id'] and filtered['region']=='HCM04' and filtered['units']=='GB')
        require(filtered['observed_at']==value['observed_at'])
        require(decimal.Decimal(filtered['quota_lexeme'])==decimal.Decimal(value['quota']['display']))
        require(decimal.Decimal(filtered['usage_lexeme'])==decimal.Decimal(value['usage']['display']))
        require(value['quota']['rounded'] is False and value['usage']['rounded'] is False)
    quota=bound(value['quota'],True); usage=bound(value['usage'],False)
    require(type(value['quota_lower_bytes']) is int and type(value['usage_upper_bytes']) is int)
    require(value['quota_lower_bytes']==quota and value['usage_upper_bytes']==usage)
    require(quota-usage>=2147483648)
    return quota-usage
