"""Isolated HTTP smoke checks; no production data or external services are used."""
import argparse
from contextlib import closing
import json
import os
from pathlib import Path
import re
import secrets
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('port', nargs='?', type=int, default=18900)
    parser.add_argument('--binary', type=Path, help='Use an existing build instead of building Go')
    args = parser.parse_args()
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', args.port))
    with tempfile.TemporaryDirectory(prefix='landlord-smoke-') as temp:
        out = Path(temp)
        binary = args.binary.resolve() if args.binary else out / ('landlord.exe' if os.name == 'nt' else 'landlord')
        if not args.binary:
            subprocess.run(['go', 'build', '-o', str(binary), '.'], cwd=ROOT / 'server', check=True)
        env = os.environ.copy()
        # Explicit paths and a generated setup key isolate this process from local installations.
        for key in ('DB_PATH', 'DATA_DIR', 'PORT'):
            env.pop(key, None)
        setup = secrets.token_urlsafe(24)
        password = secrets.token_urlsafe(24)
        env['LANDLORD_SETUP_KEY'] = setup
        env['RATE_LIMIT'] = '0'
        base = f'http://127.0.0.1:{args.port}/api'
        checks = []
        web = out / 'web'
        web.mkdir()
        (web / 'index.html').write_text('<html>isolated smoke app</html>', encoding='utf-8')

        def check(name, ok):
            if not ok:
                raise AssertionError(name)
            checks.append(name)
            print('PASS ' + name)

        def api(method, path, data=None, token='', expected=200, invite=False, binary=False):
            headers = {'Content-Type': 'application/json'}
            if token:
                headers['Authorization'] = 'Bearer ' + token
            if invite:
                headers['X-Landlord-Setup'] = setup
            request = urllib.request.Request(base + path, None if data is None else json.dumps(data).encode(), headers, method=method)
            try:
                response = urllib.request.urlopen(request, timeout=15)
            except urllib.error.HTTPError as exc:
                response = exc
            with response:
                status, raw = response.status, response.read()
            if status != expected:
                raise AssertionError(f'{method} {path}: HTTP {status}, expected {expected}')
            return raw if binary else json.loads(raw).get('data')

        with (out / 'server.log').open('w', encoding='utf-8') as log:
            proc = subprocess.Popen([str(binary), '-port', str(args.port), '-data-dir', str(out / 'data'),
                                     '-web-dir', str(web), '-rate-limit', '0'],
                                    cwd=ROOT, env=env, stdout=log, stderr=log,
                                    creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
            try:
                for _ in range(100):
                    try:
                        api('GET', '/health')
                        break
                    except OSError:
                        if proc.poll() is not None:
                            raise RuntimeError('Isolated server exited')
                        time.sleep(.1)
                else:
                    raise RuntimeError('Isolated server readiness timeout')

                check('health', True)
                version = api('GET', '/version')
                check('read-only version and process start time', bool(version['version'] and version['startedAt']))
                owner = api('POST', '/auth/register', {'username': 'smoke-admin', 'password': password,
                                                       'phone': '138' + '0000' + '9999'}, invite=True)
                token = owner['token']
                check('isolated admin initialization', owner['user']['role'] == 'admin')
                for method, path in [('POST', '/donate/support'), ('GET', '/version/check'),
                                     ('POST', '/update/apply'), ('GET', '/update/apply/status')]:
                    for name, credential in [('anonymous', ''), ('admin', token)]:
                        api(method, path, token=credential, expected=404, binary=True)
                        check(f'removed endpoint {method} {path} ({name})', True)

                api('GET', '/backups', expected=401, binary=True)
                check('backups require authentication', True)
                backup = api('POST', '/backups', token=token)['name']
                check('backup creation', re.fullmatch(r'backup_\d{8}_\d{6}\.db', backup) is not None)
                listing = api('GET', '/backups', token=token)
                check('backup listing', any(item['name'] == backup for item in listing['items']) and listing['auto_enabled'])
                downloaded = api('GET', f'/backups/{backup}/download', token=token, binary=True)
                snapshot = out / 'download.db'
                snapshot.write_bytes(downloaded)
                with closing(sqlite3.connect(snapshot)) as db:
                    check('downloaded SQLite snapshot integrity', db.execute('PRAGMA integrity_check').fetchone() == ('ok',))
                    check('snapshot contains initialized account', db.execute('SELECT COUNT(*) FROM users').fetchone()[0] == 1)
                api('GET', '/backups/evil.db/download', token=token, expected=400, binary=True)
                check('invalid backup filename rejected', True)
                check('backup deletion', api('DELETE', f'/backups/{backup}', token=token)['ok'])
                check('backup removed from list', all(item['name'] != backup for item in api('GET', '/backups', token=token)['items']))
                print(json.dumps({'passed': len(checks), 'failed': 0}))
            finally:
                proc.terminate()
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=5)
                if os.sys.exc_info()[0]:
                    print((out / 'server.log').read_text(encoding='utf-8', errors='replace')[-4000:])


if __name__ == '__main__':
    main()
