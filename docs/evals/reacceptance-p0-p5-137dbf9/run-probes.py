#!/usr/bin/env python3
"""Run acceptance probes via a temporary Go overlay; leave repository source unchanged."""
import argparse,json,pathlib,subprocess,tempfile
here=pathlib.Path(__file__).resolve().parent
root=here.parents[2]
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--run',default='^TestReaccept',help='Go test name regex')
a=p.parse_args()
with tempfile.TemporaryDirectory(prefix='bint-reaccept-overlay-') as tmp:
 overlay=pathlib.Path(tmp)/'overlay.json'
 overlay.write_text(json.dumps({'Replace':{
  str(root/'internal/workflow/reaccept_stale_probe_test.go'):str(here/'stale-reclamation_test.go.txt'),
  str(root/'internal/workflow/reaccept_readonly_knowledge_test.go'):str(here/'readonly-verified_test.go.txt')
 }}))
 raise SystemExit(subprocess.run(['go','test','-count=1','-overlay',str(overlay),'./internal/workflow','-run',a.run,'-v'],cwd=root).returncode)
