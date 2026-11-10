#!/usr/bin/env python3
"""Run P6 audit fixtures through a temporary Go overlay without editing product source."""
import argparse,json,os,pathlib,platform,subprocess,tempfile
here=pathlib.Path(__file__).resolve().parent;root=here.parents[2]
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--run',default='^TestAcceptanceP6',help='Go test name regex');args=p.parse_args()
if platform.system()!='Linux' or os.geteuid()==0:p.error('These probes target Linux as a non-root user (including a chmod write-failure fixture).')
with tempfile.TemporaryDirectory(prefix='bint-p6-overlay-') as tmp:
 overlay=pathlib.Path(tmp)/'overlay.json'
 overlay.write_text(json.dumps({'Replace':{str(root/'internal/evolve/acceptance_p6_probes_test.go'):str(here/'p6-probes_test.go.txt')}}))
 raise SystemExit(subprocess.run(['go','test','-count=1','-overlay',str(overlay),'./internal/evolve','-run',args.run,'-v'],cwd=root).returncode)
