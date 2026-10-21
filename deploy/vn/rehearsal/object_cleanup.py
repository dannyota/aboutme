"""Bounded S3 cleanup using one fresh, locally held attached key."""
import json
import os
import pathlib
import re
from common import safe_write
from protocol import require


class Objects:
    def __init__(self,runner,credentials,configuration,bucket):
        self.runner=runner; self.credentials=credentials; self.configuration=configuration; self.bucket=bucket
        require(re.fullmatch(r'aboutme-cs-rehearsal-[0-9a-f]{16}-(media|backups)',bucket))
        require(credentials.is_file() and not credentials.is_symlink())
        require(credentials.stat().st_mode&0o777==0o600 and credentials.stat().st_uid==os.getuid())
        safe_write(configuration,b'[default]\nregion = HCM04\ns3 =\n    addressing_style = path\n')

    def request(self,operation,values):
        require(operation in ('list-multipart-uploads','abort-multipart-upload','list-objects-v2','delete-objects','list-parts'))
        require('Bucket' not in values)
        values=dict(values,Bucket=self.bucket)
        environment={'PATH':os.environ['PATH'],'HOME':str(self.credentials.parent),'LC_ALL':'C',
                     'AWS_CONFIG_FILE':str(self.configuration),'AWS_SHARED_CREDENTIALS_FILE':str(self.credentials),
                     'AWS_PROFILE':'default','AWS_DEFAULT_REGION':'HCM04','AWS_EC2_METADATA_DISABLED':'true',
                     'AWS_MAX_ATTEMPTS':'1','AWS_PAGER':''}
        code,output,error=self.runner.call(['aws','--endpoint-url','https://hcm04.vstorage.vngcloud.vn','--region','HCM04',
                                 '--no-cli-pager','--output','json','--no-paginate','--cli-connect-timeout','5',
                                 '--cli-read-timeout','20','s3api',operation,'--cli-input-json','file:///dev/stdin'],
                                input_data=json.dumps(values).encode(),timeout=30,env=environment,private_error=True)
        if code:
            expected={'list-parts':'ListParts','abort-multipart-upload':'AbortMultipartUpload'}.get(operation)
            require(expected is not None and not output and re.fullmatch(
                rb'\s*An error occurred \(NoSuchUpload\) when calling the '+expected.encode()+rb' operation: [^\r\n]*\s*',error))
            return {'NoSuchUpload':True}
        return json.loads(output or b'{}')

    def multipart_pass(self):
        markers={}; seen=set(); count=0
        for _ in range(10000):
            page=self.request('list-multipart-uploads',dict(markers,MaxUploads=100))
            uploads=page.get('Uploads',[])
            require(isinstance(uploads,list) and len(uploads)<=100 and isinstance(page.get('IsTruncated'),bool))
            for upload in uploads:
                require(isinstance(upload['Key'],str) and isinstance(upload['UploadId'],str))
                self.request('abort-multipart-upload',{'Key':upload['Key'],'UploadId':upload['UploadId']})
                self.verify_parts(upload['Key'],upload['UploadId'])
                count+=1
            if not page['IsTruncated']: return count
            key=page['NextKeyMarker']; upload=page.get('NextUploadIdMarker','')
            require(isinstance(key,str) and key and isinstance(upload,str) and (key,upload) not in seen)
            require(len(key.encode())<=8192 and len(upload.encode())<=8192)
            seen.add((key,upload)); markers={'KeyMarker':key,'UploadIdMarker':upload}
        raise ValueError('pagination_bound')

    def verify_parts(self,key,upload):
        for _ in range(10000):
            marker=None; seen=set(); count=0
            for _ in range(10000):
                values={'Key':key,'UploadId':upload,'MaxParts':100}
                if marker is not None: values['PartNumberMarker']=marker
                page=self.request('list-parts',values)
                if page.get('NoSuchUpload') is True: return
                parts=page.get('Parts',[])
                require(isinstance(parts,list) and len(parts)<=100 and isinstance(page.get('IsTruncated'),bool))
                count+=len(parts)
                if not page['IsTruncated']: break
                marker=page['NextPartNumberMarker']
                require(type(marker) is int and marker>0 and marker not in seen)
                seen.add(marker)
            else: raise ValueError('part_pagination_bound')
            if count==0: return
            self.request('abort-multipart-upload',{'Key':key,'UploadId':upload})
        raise ValueError('abort_bound')

    def objects_pass(self):
        token=None; seen=set(); count=0
        for _ in range(10000):
            values={'MaxKeys':100}
            if token: values['ContinuationToken']=token
            page=self.request('list-objects-v2',values)
            rows=page.get('Contents',[])
            require(isinstance(rows,list) and len(rows)<=100 and isinstance(page.get('IsTruncated'),bool))
            if rows:
                result=self.request('delete-objects',{'Delete':{'Objects':[{'Key':row['Key']} for row in rows],'Quiet':True}})
                require(not result.get('Errors'))
                count+=len(rows)
            if not page['IsTruncated']: return count
            token=page['NextContinuationToken']
            require(isinstance(token,str) and token and len(token.encode())<=8192 and token not in seen)
            seen.add(token)
        raise ValueError('pagination_bound')

    def empty(self):
        totals={'multipart_aborted':0,'objects_deleted':0}
        for _ in range(10000):
            aborted=self.multipart_pass(); deleted=self.objects_pass()
            totals['multipart_aborted']+=aborted; totals['objects_deleted']+=deleted
            if aborted==deleted==0: return totals
        raise ValueError('cleanup_bound')
