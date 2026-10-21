"""Exact released images and hosted fixture binding."""
import json
import pathlib
import re
import urllib.request
from protocol import BASELINE, SHA, digest, require
from common import safe_write


def registry(path,token=None):
    headers={'Accept':'application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json'}
    if token: headers['Authorization']='Bearer '+token
    with urllib.request.urlopen(urllib.request.Request('https://ghcr.io/'+path,headers=headers),timeout=30) as response:
        value=response.read(2097153); require(len(value)<=2097152)
        return json.loads(value)


def resolve_images(runner,repository,tag,commit):
    images={}
    for name in ('server','web','caddy'):
        scope='dannyota/aboutme-'+name
        token=registry('token?scope=repository:'+scope+':pull&service=ghcr.io')['token']
        index=registry('v2/'+scope+'/manifests/'+tag,token)
        candidates=[item['digest'] for item in index['manifests'] if
                    item.get('platform',{}).get('os')=='linux' and item['platform'].get('architecture')=='amd64']
        require(len(candidates)==1 and re.fullmatch('sha256:[0-9a-f]{64}',candidates[0]))
        runner.call(['bash','-c','source "$1"; provenance_verify "$2" "$3" "$4" "$5"',
                     'bash',str(repository/'deploy/aws/scripts/provenance.sh'),name,candidates[0],tag,commit],timeout=180)
        images[name]='ghcr.io/'+scope+'@'+candidates[0]
    return images


def fixture_artifact(runner,manifest,output):
    run_id=str(manifest['config']['artifact_run_id'])
    run=json.loads(runner.call(['gh','run','view',run_id,'--repo','dannyota/aboutme','--json',
                               'headSha,event,conclusion,status,workflowName']))
    require(run=={'headSha':manifest['commit'],'event':'push','conclusion':'success','status':'completed','workflowName':'CI'})
    output.mkdir(mode=0o700)
    runner.call(['gh','run','download',run_id,'--repo','dannyota/aboutme','--name','crowdsec-rehearsal-fixtures',
                 '--dir',str(output)],timeout=180)
    allowed={'source-commit.txt','SHA256SUMS','render-budget','allowance-adapter','fixtures',
             'fixtures/minimal.json','fixtures/full.json','fixtures/maximum.json','fixtures/full-photo.png','fixtures/manifest.json'}
    for path in output.rglob('*'):
        require(not path.is_symlink() and str(path.relative_to(output)) in allowed)
    require(output.joinpath('source-commit.txt').read_text().strip()==manifest['commit'])
    hashes={}
    for line in output.joinpath('SHA256SUMS').read_text().splitlines():
        sha,name=line.split('  ',1)
        require(SHA.fullmatch(sha) and name in allowed and name not in hashes)
        require(digest(output/name)==sha); hashes[name]=sha
    require(set(hashes)=={'render-budget','allowance-adapter','fixtures/manifest.json','fixtures/minimal.json','fixtures/full.json',
                          'fixtures/maximum.json','fixtures/full-photo.png'})
    manifest['artifact_sha256']=digest(output/'SHA256SUMS')
    return digest(output/'fixtures/manifest.json')


def bundles(runner,repository,output,commit):
    require(runner.call(['git','rev-parse','v0.8.9^{commit}'],cwd=repository).decode().strip()==BASELINE)
    for name,revision in (('baseline',BASELINE),('candidate',commit)):
        data=runner.call(['git','archive','--format=tar',revision+':deploy/vn'],cwd=repository)
        safe_write(output/(name+'.tar'),data)
    return digest(output/'baseline.tar'),digest(output/'candidate.tar')
