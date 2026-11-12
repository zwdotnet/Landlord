"""Generate local WeChat settings without copying the AppSecret into the client."""
import argparse
import json
from pathlib import Path
import re
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parent.parent


def prepare(api_base, production=False):
    parsed = urlsplit(api_base)
    loopback = parsed.hostname in ('localhost', '127.0.0.1', '::1')
    if parsed.scheme not in ('https', 'http') or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError('Use a plain HTTPS API base, or HTTP loopback for local development')
    if parsed.scheme == 'http' and (production or not loopback):
        raise ValueError('HTTP is only allowed on loopback during development')
    if not parsed.path.rstrip('/').endswith('/api'):
        raise ValueError('API base must include the /api suffix')
    values = {}
    for raw in (ROOT / '.env').read_text(encoding='utf-8-sig').splitlines():
        match = re.match(r'\s*(?:export\s+)?([A-Za-z_][A-Za-z_0-9]*)\s*=\s*(.*?)\s*$', raw)
        if match:
            key, value = match.groups()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in ('"', "'"):
                value = value[1:-1]
            values[key] = value
    appid = values.get('WX_APP_ID', '')
    if not re.fullmatch(r'wx[0-9a-fA-F]{16}', appid):
        raise ValueError('WX_APP_ID is missing or invalid in root .env')
    client = ROOT / 'miniprogram'
    project = json.loads((client / 'project.config.example.json').read_text(encoding='utf-8'))
    project['appid'] = appid
    project['setting']['urlCheck'] = production
    (client / 'project.config.json').write_text(json.dumps(project, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    (client / 'config.local.js').write_text('module.exports = ' + json.dumps({'apiBase': api_base.rstrip('/')}, ensure_ascii=True) + '\n', encoding='utf-8')
    # Private config may override urlCheck; preserve all other user settings.
    private_path = client / 'project.private.config.json'
    private = json.loads(private_path.read_text(encoding='utf-8-sig')) if private_path.exists() else {}
    private.setdefault('setting', {})['urlCheck'] = production
    private_path.write_text(json.dumps(private, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    return {'prepared': True, 'api_base': api_base.rstrip('/'), 'url_check': production, 'appsecret_copied': False}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--api-base', default='https://www.zoeng.site/landlord/api')
    parser.add_argument('--production', action='store_true', help='Enable domain validation for release checks')
    args = parser.parse_args()
    print(json.dumps(prepare(args.api_base, args.production)))
