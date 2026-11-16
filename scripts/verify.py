"""One verification entry point on Windows and Linux. No credentials or DevTools required."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]


def execute(command, cwd, log, kind='', env=None):
    started = time.monotonic()
    # Keep stderr as bytes inside Python. PS5 only sees our normal stdout.
    with log.open('wb') as stream:
        result = subprocess.run(command, cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT)
    output = log.read_text(encoding='utf-8-sig', errors='replace')
    counts = {}
    if kind == 'go':
        events = []
        for line in output.splitlines():
            try: events.append(json.loads(line))
            except ValueError: continue
        tests = [event for event in events if event.get('Test')]
        counts = {action: sum(e.get('Action') == action for e in tests) for action in ('pass','fail','skip')}
        counts['top_level_pass'] = sum(e.get('Action') == 'pass' and '/' not in e['Test'] for e in tests)
    elif kind == 'node':
        counts = {key: int(re.search(r'^# ' + key + r' (\d+)',output,re.M).group(1))
                  for key in ('tests','pass','fail','skipped') if re.search(r'^# '+key+r' (\d+)',output,re.M)}
    elif kind == 'python':
        match = re.search(r'Ran (\d+) tests? in ', output)
        counts = {'tests': int(match.group(1)) if match else 0, 'fail': 0 if re.search(r'^OK(?: \(|$)',output,re.M) else 1}
    passed = result.returncode == 0
    if kind in ('node','go'): passed = passed and counts.get('pass',0) > 0 and counts.get('fail',1) == 0
    if kind == 'python': passed = passed and counts['tests'] > 0 and counts['fail'] == 0
    return {'command': command, 'cwd': str(cwd), 'exit_code': result.returncode,
            'pass': passed, 'counts': counts, 'seconds': round(time.monotonic()-started,3), 'log': log.name}


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--focused', action='store_true')
    parser.add_argument('--evidence', type=Path)
    args=parser.parse_args()
    stamp=datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S-%f')
    evidence=(args.evidence or ROOT / '.runtime/verification' / stamp).resolve()
    evidence.mkdir(parents=True, exist_ok=False)
    commit=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
    names=subprocess.check_output(['git','ls-files','-z'],cwd=ROOT).decode().strip('\0').split('\0')
    hashes={name:hashlib.sha256((ROOT/name).read_bytes()).hexdigest() for name in names if (ROOT/name).is_file()}
    for base in ('server','scripts','deploy','web/src','miniprogram'):
        for p in (ROOT/base).rglob('*'):
            if p.is_file() and p.suffix in ('.go','.py','.ps1','.js','.cjs','.mjs','.ts','.vue','.json','.wxml','.wxss') and not any(x in p.parts for x in ('node_modules','miniprogram_npm','__pycache__')) and p.name not in ('project.config.json','project.private.config.json','config.local.js'):
                hashes[p.relative_to(ROOT).as_posix()]=hashlib.sha256(p.read_bytes()).hexdigest()
    summary={'commit':commit,'source_sha256':hashes,'focused':args.focused,'pass':False,'release_candidate':False,'steps':[],
             'not_run':['WeChat native/device/platform','Linux image/PDF smoke (separate cloud step)']}
    backend=['go','test','./...','-count=1','-json']
    if args.focused: backend += ['-run','TestSQLite|TestSnapshot|TestExplicitTrusted|TestUpload|TestAttachment|TestContract|TestMeterPhoto|TestRoomPhoto|TestRoomItem']
    node_files=[str(p.relative_to(ROOT)) for directory,pattern in [('web/tests','*.test.mjs'),('scripts/miniprogram-tests','*.test.cjs')] for p in sorted((ROOT/directory).glob(pattern))]
    env=os.environ.copy();env.setdefault('VITE_BASE_PATH','/landlord/')
    npm=shutil.which('npm.cmd' if os.name=='nt' else 'npm') or 'npm'
    steps=[('backend',backend,ROOT/'server','go'),('node',['node','--test','--test-reporter=tap',*node_files],ROOT,'node'),
           ('deploy-python',[sys.executable,'-m','unittest','discover','-s','deploy','-p','test_*.py'],ROOT,'python'),
           ('mini-python',[sys.executable,'-m','unittest','discover','-s','scripts/miniprogram-tests','-p','test_*.py'],ROOT,'python'),
           ('tools-python',[sys.executable,'-m','unittest','discover','-s','scripts','-p','test_*.py'],ROOT,'python'),
           ('mini-syntax',['node',str(ROOT/'scripts/check-miniprogram-syntax.cjs')],ROOT,''),
           ('web-build',[npm,'run','build'],ROOT/'web','')]
    try:
        for name,command,cwd,kind in steps:
            print('Checking '+name, flush=True)
            item=execute(command,cwd,evidence/(name+'.log'),kind,env)
            summary['steps'].append(dict(name=name,**item))
            print(name+': '+('PASS' if item['pass'] else 'FAIL')+' '+json.dumps(item['counts']),flush=True)
            if not item['pass']: break
        summary['pass']=len(summary['steps'])==len(steps) and all(s['pass'] for s in summary['steps'])
        summary['release_candidate']=summary['pass'] and not args.focused
    finally:
        (evidence/'summary.json').write_text(json.dumps(summary,ensure_ascii=True,indent=2)+'\n',encoding='utf-8')
        print('Evidence: '+str(evidence),flush=True)
    return 0 if summary['pass'] else 1


if __name__=='__main__':
    sys.exit(main())
