"""Run a fresh isolated test fixture; never reuse a production database."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / '.runtime' / 'm5-20261112' / ('fixture-' + datetime.datetime.now().strftime('%H%M%S'))
BASE = 'http://127.0.0.1:18940/api'


def api(method, path, payload=None, token=None, expected=200, binary=False):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(BASE + path, None if payload is None else json.dumps(payload).encode(), headers, method=method)
    try:
        response = urllib.request.urlopen(req, timeout=35)
    except urllib.error.HTTPError as exc:
        response = exc
    with response:
        status, raw = response.code, response.read()
    if status != expected:
        raise RuntimeError(f'{method} {path}: {status} (expected {expected})')
    return raw if binary else json.loads(raw)['data']


if __name__ == '__main__':
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', 18940))
    OUT.mkdir(parents=True, exist_ok=False)
    password = secrets.token_urlsafe(20)
    env = os.environ.copy()
    env.pop('LANDLORD_SETUP_KEY', None)
    with (OUT / 'server.log').open('w', encoding='utf-8') as log:
        # The existing backend listens on :port; all fixture clients use loopback.
        # Only generated test identities/data are used. Windows firewall is unchanged.
        proc = subprocess.Popen([str(ROOT / '.runtime/m5-20261112/landlord.exe'), '-port', '18940',
                                 '-data-dir', str(OUT / 'data'), '-web-dir', str(ROOT / 'web/dist'),
                                 '-rate-limit', '0'], cwd=ROOT, env=env,
                                stdout=log, stderr=log, creationflags=subprocess.CREATE_NO_WINDOW)
        try:
            for _ in range(100):
                try:
                    api('GET', '/health')
                    break
                except OSError:
                    if proc.poll() is not None:
                        raise RuntimeError('Isolated backend exited')
                    time.sleep(.15)
            else:
                raise RuntimeError('Isolated backend readiness timeout')

            def register(username, role=None):
                phone = ''.join(map(str, [1, 3, 8, 0, 0, 0, 0, 0, 0, 0, 0]))
                return api('POST', '/auth/register', dict(username=username, password=password, phone=phone, **({'role': role} if role else {})))

            owner = register('mini-owner')['token']
            renter = register('mini-renter', 'tenant')
            second = register('mini-second', 'tenant')
            pending = register('mini-pending', 'tenant')
            room = api('POST', '/rental/rooms', {'room_no': '101', 'community': '青禾里', 'building': '2栋', 'default_rent': 2800, 'landlord_name': '测试房东'}, owner)
            tenant = api('POST', '/rental/tenants', {'room_id': room['id'], 'name': '测试租客', 'move_in_date': '2030-01-01', 'lease_end_date': '2031-12-31', 'pay_cycle': 'monthly', 'pay_day': 5, 'deposit': 0}, owner)
            api('POST', f'/rental/tenants/{tenant["id"]}/account', {'user_id': renter['user']['id']}, owner)
            room2 = api('POST', '/rental/rooms', {'room_no': '202', 'community': '青禾里', 'default_rent': 1900}, owner)
            tenant2 = api('POST', '/rental/tenants', {'room_id': room2['id'], 'name': '另一位租客', 'move_in_date': '2030-01-01'}, owner)
            api('POST', f'/rental/tenants/{tenant2["id"]}/account', {'user_id': second['user']['id']}, owner)
            bills = [api('POST', '/rental/bills', {'room_id': room['id'], 'period': f'2030-{month:02d}'}, owner) for month in range(1, 13)]
            bill = bills[-1]
            agreement = api('POST', f'/rental/tenants/{tenant["id"]}/agreements', {'kind': 'lease', 'request_key': 'mini-fixture-lease'}, owner)
            paid = api('POST', f'/rental/bills/{bill["id"]}/pay', {'amount': 800, 'request_key': 'mini-fixture-paid', 'note': '测试线下实收'}, owner)
            api('POST', f'/rental/payments/{paid["last_payment_id"]}/reverse', {'reason': '测试整笔冲销', 'request_key': 'mini-fixture-reverse'}, owner)
            api('POST', f'/rental/bills/{bill["id"]}/pay', {'amount': 600, 'request_key': 'mini-fixture-corrected', 'note': '测试更正后的实收'}, owner)
            api('GET', f'/rental/portal/bills/{bill["id"]}', token=second['token'], expected=404)
            api('GET', f'/rental/portal/documents/{agreement["id"]}/file', token=second['token'], expected=404)
            pdf = api('GET', f'/rental/portal/documents/{agreement["id"]}/file', token=renter['token'], binary=True)
            if not pdf.startswith(b'%PDF-'):
                raise RuntimeError('Fixture agreement is not a PDF')
            state = {'evidence': str(OUT), 'pid': proc.pid, 'base': BASE, 'password': password,
                     'renter': renter, 'second': second, 'pending': pending,
                     'owner': owner, 'tenant_id': tenant['id'], 'bill_id': bill['id'], 'agreement_id': agreement['id'],
                     'pdf_sha256': hashlib.sha256(pdf).hexdigest()}
            (OUT / 'state.json').write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding='utf-8')
            (ROOT / '.runtime/m5-20261112/fixture-path.txt').write_text(str(OUT), encoding='utf-8')
            print(json.dumps({'fixture_ready': True, 'evidence': str(OUT), 'port': 18940, 'bills': len(bills), 'cross_account_api': 'PASS'}), flush=True)
            while True:
                time.sleep(1)
        finally:
            proc.terminate()
            proc.wait(timeout=15)
