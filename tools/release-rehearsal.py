"""Bounded tests of an existing image on a disposable Linux Docker host.

Only new temporary data, random synthetic credentials, loopback HTTP, and owned
containers are used. Upload only the sanitized evidence directory, never data.
"""
import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
from contextlib import closing, nullcontext
from datetime import datetime, timezone, timedelta
import hashlib
import http.client
import importlib.util
import json
import math
import os
from pathlib import Path
import random
import secrets
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]


def command(*args):
    p = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if p.returncode:
        # Never print subprocess argv/env or response bodies containing tokens.
        raise RuntimeError('Command failed: ' + args[0] + ' exit ' + str(p.returncode))
    return p.stdout.decode().strip()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def database_snapshot(data):
    with closing(sqlite3.connect((data/'db/rental.db').as_uri()+'?mode=ro', uri=True)) as db:
        assert db.execute('PRAGMA quick_check').fetchall() == [('ok',)]
        assert not db.execute('PRAGMA foreign_key_check').fetchall()
        result = {}
        for (table,) in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"):
            if table in ('audit_logs', 'system_configs'):
                continue
            rows = db.execute('SELECT * FROM "'+table.replace('"','""')+'"').fetchall()
            values = sorted(json.dumps(row, default=lambda b: {'blob_sha256': sha(b)}) for row in rows)
            result[table] = {'count': len(rows), 'sha256': sha(json.dumps(values).encode())}
        return result


class App:
    def __init__(self, image, data, name, setup):
        self.name, self.token, self.setup = name, '', setup
        data.mkdir(parents=True, exist_ok=True)
        # The runner owns this new synthetic directory; the image runs as 10001.
        command('sudo', 'chown', '-R', '10001:10001', str(data))
        # Synthetic files only: allow the runner to validate the stopped data.
        command('sudo', 'chmod', '-R', 'a+rX', str(data))
        env = os.environ.copy()
        env['LANDLORD_SETUP_KEY'] = setup
        subprocess.run(['docker', 'run', '-d', '--name', name, '--read-only', '--cap-drop=ALL',
                        '--security-opt=no-new-privileges', '--memory=512m', '--memory-swap=768m',
                        '--cpus=1', '--pids-limit=160', '-e', 'GOMEMLIMIT=192MiB', '-e', 'LANDLORD_SETUP_KEY',
                        '--tmpfs', '/tmp:rw,size=256m,mode=1777',
                        '--mount', f'type=bind,source={data},target=/app/data',
                        '-p', '127.0.0.1::8910', image,
                        '-port', '8910', '-data-dir', '/app/data', '-web-dir', '/app/web', '-rate-limit', '0'],
                       env=env, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        address = command('docker', 'port', name, '8910/tcp')
        assert address.startswith('127.0.0.1:')
        self.port = int(address.rsplit(':', 1)[1])
        self.base = 'http://' + address + '/api'
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for _ in range(100):
            try:
                self.api('GET', '/health'); break
            except OSError: time.sleep(.1)
        else: raise RuntimeError('Isolated startup timed out')

    def api(self, method, path, body=None, raw=False, expected=200, auth=True):
        headers = {'Content-Type': 'application/json', 'X-Landlord-Setup': self.setup}
        if self.token and auth: headers['Authorization'] = 'Bearer ' + self.token
        req = urllib.request.Request(self.base+path, None if body is None else json.dumps(body).encode(), headers, method=method)
        try: response = self.opener.open(req, timeout=35)
        except urllib.error.HTTPError as e: response = e
        with response: status, data = response.status, response.read()
        assert status == expected, f'{method} {path.split("?")[0]} HTTP {status}; body suppressed'
        if raw: return data
        value = json.loads(data); assert value.get('code') == 0
        return value.get('data')

    def multipart(self, path, files, fields=None, expected=200):
        boundary = uuid.uuid4().hex
        parts = []
        for key, value in (fields or {}).items():
            parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'.encode())
        for name, content in files:
            parts.extend([f'--{boundary}\r\nContent-Disposition: form-data; name="files"; filename="{name}"\r\nContent-Type: application/octet-stream\r\n\r\n'.encode(), content, b'\r\n'])
        parts.append(f'--{boundary}--\r\n'.encode())
        return boundary, b''.join(parts)

    def upload(self, path, files, fields=None, expected=200):
        boundary, body = self.multipart(path, files, fields)
        request = urllib.request.Request(self.base+path, body, {'Authorization': 'Bearer '+self.token,
                                         'Content-Type': 'multipart/form-data; boundary='+boundary}, method='POST')
        try: response = self.opener.open(request, timeout=35)
        except urllib.error.HTTPError as e: response = e
        with response: status, data = response.status, response.read()
        assert status == expected, f'Upload status {status}; body suppressed'
        return json.loads(data).get('data')

    def stop(self): command('docker', 'stop', '-t', '20', self.name)


def container_sample(name):
    # cgroup v2 accounts the whole container, including file cache and tmpfs.
    script = 'cat /sys/fs/cgroup/memory.current; cat /sys/fs/cgroup/memory.peak; cat /sys/fs/cgroup/pids.current; df -Pk /tmp | tail -1'
    lines = command('docker', 'exec', name, '/bin/sh', '-c', script).splitlines()
    return {'memory_current_bytes': int(lines[0]), 'memory_kernel_peak_bytes': int(lines[1]),
            'pids_current': int(lines[2]), 'tmp_used_bytes': int(lines[3].split()[2])*1024}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--artifact-dir', type=Path, required=True)
    parser.add_argument('--expected-commit', required=True)
    parser.add_argument('--expected-run-id', required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    args = parser.parse_args()
    evidence = args.evidence.resolve(); evidence.mkdir(parents=True, exist_ok=False)
    result = {'pass': False, 'candidate_commit': args.expected_commit, 'production_access': False}
    names, apps = [], []
    monitor_stop, entered, send = threading.Event(), threading.Event(), threading.Event()
    samples, monitor_errors, health_times, intervals = [], [], [], {}
    slow_thread = None
    temp_stop = None
    try:
        spec = importlib.util.spec_from_file_location('release', ROOT/'deploy/release.py')
        release = importlib.util.module_from_spec(spec); spec.loader.exec_module(release)
        artifact = args.artifact_dir.resolve()
        build = json.loads((artifact/'build.json').read_text())
        release.validate_package(artifact, args.expected_commit, build['image'], args.expected_run_id)
        command('git', 'diff', '--exit-code', args.expected_commit, 'HEAD', '--', 'server', 'web', 'miniprogram', 'deploy', 'scripts')
        command('docker', 'load', '--input', str(artifact/'image.tar.gz'))
        image = build['image']
        image_info = json.loads(command('docker', 'image', 'inspect', image))[0]
        assert image_info['Os'] == 'linux' and image_info['Architecture'] == 'amd64'
        assert image_info['RootFS']['Layers'] == build['rootfs_layers']
        # Container-owned files outlive this scope; the disposable runner removes
        # the synthetic filesystem after the job. Only evidence JSON is uploaded.
        with nullcontext(tempfile.mkdtemp(prefix='landlord-isolated-rehearsal-')) as temporary:
            case = Path(temporary); data = case/'source'
            setup, password = secrets.token_urlsafe(24), secrets.token_urlsafe(24)
            name = 'landlord-rehearsal-' + uuid.uuid4().hex[:12]; names.append(name)
            app = App(image, data, name, setup); apps.append(app)
            assert app.api('GET', '/version')['gitCommit'] == args.expected_commit
            app.token = app.api('POST', '/auth/register', {'username':'synthetic-owner','password':password,'phone':''.join(['138','0000','9999'])})['token']
            ledger = app.api('GET','/rental/ledgers/mine')[0]['id']; q=f'?ledger_id={ledger}'
            bills = []
            period = datetime.now(timezone(timedelta(hours=8))).strftime('%Y-%m')
            for i in range(4):
                room = app.api('POST','/rental/rooms'+q,{'room_no':f'CAP-{i}','default_rent':1000,'initial_water':0,'initial_elec':0,'landlord_name':'合成房东'})
                tenant = app.api('POST','/rental/tenants'+q,{'room_id':room['id'],'name':'合成峰值租客','move_in_date':'2025-01-01','lease_end_date':'2027-12-31','water_price':0})
                app.api('POST','/rental/meter-records'+q,{'room_id':room['id'],'period':period,'water':1,'elec':1,'gas':0})
                bills.append(app.api('POST','/rental/bills'+q,{'room_id':room['id'],'period':period,'tenant_ids':[tenant['id']]}))
                if i == 0: first_room, first_tenant = room, tenant
            lease = app.api('POST',f'/rental/tenants/{first_tenant["id"]}/agreements'+q,{'kind':'lease','request_key':'capacity-lease'})
            lease_pdf = app.api('GET',f'/rental/documents/{lease["id"]}/file'+q,raw=True)
            assert lease_pdf.startswith(b'%PDF-')
            png = base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jXioAAAAASUVORK5CYII=')
            attached = app.upload(f'/rental/tenants/{first_tenant["id"]}/attachments'+q,[('synthetic.png',png)])['items'][0]
            photo = app.upload(f'/rental/rooms/{first_room["id"]}/meter-photos'+q,[('meter.png',png)],{'period':period,'meter':'water'})['items'][0]
            upload_path = f'/rental/tenants/{first_tenant["id"]}/contracts'+q
            rng = random.Random(17)
            content = lease_pdf + rng.randbytes((15<<20)-len(lease_pdf))
            boundary, body = app.multipart(upload_path,[('synthetic-a.pdf',content),('synthetic-b.pdf',content)])
            slow_result = []

            def monitor():
                while not monitor_stop.is_set():
                    try: samples.append(container_sample(name))
                    except Exception as error: monitor_errors.append(type(error).__name__)
                    monitor_stop.wait(.15)
            monitor_thread = threading.Thread(target=monitor,daemon=True); monitor_thread.start()

            def slow_upload():
                conn = http.client.HTTPConnection('127.0.0.1',app.port,timeout=40)
                intervals['upload_start'] = time.monotonic()
                try:
                    conn.putrequest('POST','/api'+upload_path)
                    conn.putheader('Authorization','Bearer '+app.token)
                    conn.putheader('Content-Type','multipart/form-data; boundary='+boundary)
                    conn.putheader('Content-Length',str(len(body)));conn.endheaders()
                    conn.send(body[:65536]);entered.set();assert send.wait(10)
                    for offset in range(65536,len(body),65536):
                        conn.send(body[offset:offset+65536]);time.sleep(.01)
                    response=conn.getresponse();raw=response.read();slow_result.append((response.status,json.loads(raw).get('data')))
                except Exception as error: slow_result.append((type(error).__name__,None))
                finally: conn.close(); intervals['upload_end']=time.monotonic()
            slow_thread=threading.Thread(target=slow_upload);slow_thread.start();assert entered.wait(5)
            time.sleep(.15)
            app.upload(upload_path,[('busy.pdf',b'%PDF- synthetic')],expected=503)

            def payment(i):
                start=time.monotonic();intervals[f'payment_{i}_start']=start
                value=app.api('POST',f'/rental/bills/{bills[i]["id"]}/pay'+q,{'amount':(i+1)*1.25,'request_key':f'capacity-pay-{i}'})
                end=time.monotonic();intervals[f'payment_{i}_end']=end
                return {'id':value['last_payment_id'],'seconds':end-start}
            def backup():
                intervals['db_backup_start']=time.monotonic()
                value=app.api('POST','/backups')
                intervals['db_backup_end']=time.monotonic();return value
            with ThreadPoolExecutor(max_workers=5) as pool:
                pay_futures=[pool.submit(payment,i) for i in range(4)]
                backup_future=pool.submit(backup);send.set()
                while not all(f.done() for f in pay_futures+[backup_future]) or slow_thread.is_alive():
                    start=time.monotonic();assert app.api('GET','/health')['status']=='ok'
                    health_times.append(time.monotonic()-start);time.sleep(.05)
                payments=[f.result() for f in pay_futures];online=backup_future.result()
            slow_thread.join(timeout=5)
            assert slow_result and slow_result[0][0]==200, 'Large batch did not complete'
            assert len(slow_result[0][1]['items'])==2
            assert all(intervals['upload_start'] < intervals[f'payment_{i}_end'] and intervals[f'payment_{i}_start'] < intervals['upload_end'] for i in range(4))
            assert intervals['upload_start'] < intervals['db_backup_start'] < intervals['upload_end']
            downloaded=app.api('GET',f'/backups/{online["name"]}/download',raw=True)
            assert downloaded.startswith(b'SQLite format 3\x00')
            paths={f'/rental/attachments/{attached["id"]}/file'+q:png,
                   f'/rental/meter-photos/{photo["id"]}/file'+q:png,
                   f'/rental/documents/{lease["id"]}/file'+q:lease_pdf}
            for row in slow_result[0][1]['items']: paths[f'/rental/contracts/{row["id"]}/file'+q]=content
            with closing(sqlite3.connect((data/'db/rental.db').as_uri()+'?mode=ro',uri=True)) as db:
                assert db.execute('SELECT count(*),SUM(amount) FROM payments').fetchone()==(4,12.5)
                for p in payments:
                    document=db.execute('SELECT receipt_id FROM payments WHERE id=?',(p['id'],)).fetchone()[0]
                    raw=app.api('GET',f'/rental/documents/{document}/file'+q,raw=True)
                    assert raw.startswith(b'%PDF-');paths[f'/rental/documents/{document}/file'+q]=raw
            samples.append(container_sample(name));monitor_stop.set();monitor_thread.join(timeout=5)
            assert not monitor_errors and samples
            events=dict(line.split() for line in command('docker','exec',name,'cat','/sys/fs/cgroup/memory.events').splitlines())
            assert int(events['oom'])==0 and int(events['oom_kill'])==0
            assert not json.loads(command('docker','inspect',name))[0]['State']['OOMKilled']
            app.stop();command('sudo','chmod','-R','a+rX',str(data));before=database_snapshot(data)
            temp_base=case/'maintenance-temp';temp_base.mkdir();archive=case/'complete.zip';dest=case/'recovered'
            temp_stop=threading.Event();temp_peak=[0]
            def temp_monitor():
                while not temp_stop.is_set():
                    total=0
                    for p in temp_base.rglob('*'):
                        try: total+=p.stat().st_blocks*512 if p.is_file() else 0
                        except FileNotFoundError: pass
                    temp_peak[0]=max(temp_peak[0],total);temp_stop.wait(.01)
            temp_thread=threading.Thread(target=temp_monitor,daemon=True);temp_thread.start()
            env=os.environ.copy();env['TMPDIR']=str(temp_base)
            def offline(*args):
                p=subprocess.run([sys.executable,str(ROOT/'scripts/full-backup.py'),*args],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
                if p.returncode:raise RuntimeError('Offline backup/recovery failed; details suppressed')
                return json.loads(p.stdout)
            created=offline('create','--offline','--data-dir',str(data),'--output',str(archive))
            restored=offline('restore',str(archive),'--destination',str(dest))
            temp_stop.set();temp_thread.join(timeout=5)
            assert database_snapshot(dest/'data')==before
            restored_name=name+'-recovered';names.append(restored_name)
            recovered=App(image,dest/'data',restored_name,setup);apps.append(recovered);recovered.token=app.token
            for path,expected in paths.items():
                assert recovered.api('GET',path,raw=True)==expected
                recovered.api('GET',path,raw=True,expected=401,auth=False)
            recovered.stop();assert database_snapshot(dest/'data')==before
            ordered=sorted(health_times)
            result={'pass':True,'observed_at':datetime.now(timezone.utc).isoformat(),'candidate_commit':args.expected_commit,
                    'candidate_run_id':args.expected_run_id,'rehearsal_commit':command('git','rev-parse','HEAD'),
                    'scope':'bounded synthetic concurrency and recovery; not production capacity or RTO',
                    'runtime_limits':{'memory_bytes':512<<20,'memory_plus_swap_bytes':768<<20,'cpu':1,'pids':160,'tmpfs_bytes':256<<20},
                    'workload':{'uploaded_payload_bytes':2*len(content),'concurrent_payments':4,'concurrent_database_backups':1,'second_upload_status':503,
                                'large_upload_status':200,'payment_total':12.5,'actual_intervals_overlap':True},
                    'resources':{'samples':len(samples),'memory_cgroup_kernel_peak_bytes':max(s['memory_kernel_peak_bytes'] for s in samples),
                                 'tmpfs_sampled_peak_bytes':max(s['tmp_used_bytes'] for s in samples),'pids_sampled_peak':max(s['pids_current'] for s in samples),
                                 'oom_events':events,'memory_accounting':'whole cgroup including cache and tmpfs; do not add overlapping measures'},
                    'health':{'samples':len(ordered),'maximum_seconds':max(ordered),'empirical_p95_seconds':ordered[math.ceil(len(ordered)*.95)-1]},
                    'payments_seconds':[p['seconds'] for p in payments],
                    'offline_recovery':{'backup_seconds':created['seconds'],'restore_seconds':restored['seconds'],
                                        'archive_bytes':archive.stat().st_size,'host_maintenance_temp_sampled_peak_allocated_bytes':temp_peak[0],
                                        'host_temp_is_outside_container_tmpfs':True,'business_tables':len(before),'authenticated_downloads_exact':len(paths),'anonymous_status':401},
                    'production_access':False,'deployed':False}
            (evidence/'resource-samples.json').write_text(json.dumps(samples,indent=2))
    finally:
        send.set();monitor_stop.set()
        if temp_stop:temp_stop.set()
        if slow_thread and slow_thread.is_alive():slow_thread.join(timeout=45)
        for name in reversed(names):subprocess.run(['docker','rm','-f',name],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        result['owned_containers_removed']=all(subprocess.run(['docker','inspect',name],stdout=subprocess.PIPE,stderr=subprocess.PIPE).returncode!=0 for name in names)
        result['pass']=result['pass'] and result['owned_containers_removed']
        (evidence/'summary.json').write_text(json.dumps(result,indent=2))
    print(json.dumps(result))
    return 0 if result['pass'] else 1


if __name__=='__main__':sys.exit(main())
