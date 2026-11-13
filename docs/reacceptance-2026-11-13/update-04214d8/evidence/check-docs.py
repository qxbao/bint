#!/usr/bin/env python3
"""Check supplemental report links, source paths, preservation, and scope."""
import glob, hashlib, json, os, pathlib, re, subprocess
ROOT=pathlib.Path(__file__).resolve().parents[4]
HERE=pathlib.Path(__file__).resolve().parents[1]
EVIDENCE=HERE/'evidence'
results=[]
def add(name,ok,detail): results.append({'name':name,'ok':bool(ok),'detail':detail})
def sha(p):
 h=hashlib.sha256()
 with open(p,'rb') as f:
  for block in iter(lambda:f.read(1024*1024),b''): h.update(block)
 return h.hexdigest()
base=json.loads((EVIDENCE/'baseline.json').read_text())
old={x['path']:x.get('sha256') for x in base['report_file_hashes'] if x.get('sha256')}
changed=[]; missing=[]
for rel,digest in old.items():
 p=ROOT/rel
 if not p.exists(): missing.append(rel)
 elif sha(p)!=digest: changed.append(rel)
add('old_report_hashes',not missing and set(changed)=={'docs/reacceptance-2026-11-13/README.md'},f'missing={missing}; changed={changed}')
config=[]
for x in base['config_direct_hashes']:
 p=pathlib.Path(x['path'])
 if not p.is_absolute(): p=ROOT/p
 same=(not p.exists() and x.get('unavailable')=='FileNotFoundError') or (p.exists() and sha(p)==x.get('sha256'))
 config.append({'path':x['path'],'same':same})
add('config_hashes',all(x['same'] for x in config),str(config))
index=ROOT/base['git_index_path']
add('index_hash',index.exists() and sha(index)==base['git_index_direct_hash']['sha256'],str(base['git_index_direct_hash']))
env={**os.environ,'GIT_OPTIONAL_LOCKS':'0'}
tracked=subprocess.run(['git','diff','--name-only','HEAD'],cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env)
untracked=subprocess.run(['git','ls-files','--others','--exclude-standard'],cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env)
paths=sorted(set(tracked.stdout.splitlines()))
allowed=('docs/reacceptance-2026-11-13/update-04214d8/','docs/reacceptance-2026-11-13/README.md','docs/README.md')
preexisting=['docs/status.md']
outside=[p for p in paths if not p.startswith(allowed) and p not in preexisting]
add('scope',tracked.returncode==0 and untracked.returncode==0 and not outside,f'outside={outside}; tracked_changes={paths}; preserved_preexisting={preexisting if any(p in paths for p in preexisting) else []}; untracked_inventory_not_used_as_delta={len(untracked.stdout.splitlines())}')
link_re=re.compile(r'\[[^\]]*\]\(([^)]+)\)')
docs=[ROOT/'docs/README.md',ROOT/'docs/reacceptance-2026-11-13/README.md']+sorted(HERE.glob('*.md'))
broken=[]
for md in docs:
 for target in link_re.findall(md.read_text()):
  if re.match(r'^(https?://|mailto:|#)',target): continue
  part=target.split('#',1)[0]
  if part and not (md.parent/part).resolve().exists(): broken.append(f'{md.relative_to(ROOT)} -> {target}')
add('local_links',not broken,str(broken))
matrix=(HERE/'matrix.md').read_text()
refs=sorted(set(re.findall(r'`((?:internal|docs)/[^`]+)`',matrix)))
missing_refs=[]
for ref in refs:
 ref=ref.split(':',1)[0]
 if not (ROOT/ref).exists() and not glob.glob(str(ROOT/ref)):
  missing_refs.append(ref)
add('matrix_source_paths',not missing_refs,f'count={len(refs)} missing={missing_refs}')
tasks=(HERE/'tasks.md').read_text().splitlines()
header=tasks[4] if len(tasks)>4 else ''
columns=['ID','Stage','Tiêu chí','Command / protocol','Dependency','Trạng thái','Evidence / hoãn']
add('task_columns',all(x.lower() in header.lower() for x in columns),header)
add('required_outputs',all((HERE/n).exists() for n in ['README.md','tasks.md','matrix.md','deferred.md']) and (HERE/'evidence').is_dir(),'required report files present')
add('README_finding','TestREADMEDocumentsTUIGlobalKeys' in (HERE/'deferred.md').read_text() and 'C-README' in matrix,'README regression mapped')
add('staticcheck_deferred','"exit_code": 127' in (EVIDENCE/'C-SC-DISCOVERY.json').read_text(),'missing binary stays deferred')
failed=[x for x in results if not x['ok']]
result={'cwd':str(ROOT),'checks':results,'passed':len(results)-len(failed),'failed':len(failed)}
(EVIDENCE/'document-check.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(result,ensure_ascii=False,indent=2))
raise SystemExit(bool(failed))
