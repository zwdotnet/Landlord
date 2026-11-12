"""Validate and publish an existing cloud artifact; never build on the target host.

Default mode verifies the package and prints the concrete release plan. --apply
stages the exact frontend/image, backs up the stopped app, switches and verifies.
Run this once over SSH instead of issuing a separate SSH call for every check.
"""
import argparse
from contextlib import closing
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import sqlite3
import subprocess
import tarfile
import time
import urllib.request


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def validate_package(directory, commit, image, run_id):
    directory = Path(directory)
    if not re.fullmatch(r'[0-9a-f]{40}', commit) or not re.fullmatch(r'landlord:[A-Za-z0-9_.-]+', image):
        raise ValueError('Invalid commit or image')
    expected = {'build.json', 'image.tar.gz', 'release.tar.gz'}
    sums = {}
    for line in (directory / 'SHA256SUMS').read_text(encoding='utf-8').splitlines():
        digest, name = line.split('  ', 1)
        if name not in expected or name in sums or not re.fullmatch('[0-9a-f]{64}', digest):
            raise ValueError('Unexpected checksum entry')
        if sha(directory / name) != digest:
            raise ValueError('Checksum mismatch: ' + name)
        sums[name] = digest
    if set(sums) != expected:
        raise ValueError('Incomplete checksums')
    build = json.loads((directory / 'build.json').read_text(encoding='utf-8'))
    if (build['commit'], build['image'], str(build['run_id']), build['platform'], build['base_path']) != (commit, image, str(run_id), 'linux/amd64', '/landlord/'):
        raise ValueError('Artifact provenance differs from requested release')
    with tarfile.open(directory / 'release.tar.gz') as archive:
        members = archive.getmembers()
        names = set()
        for member in members:
            path = PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or '\\' in member.name or member.name in names or not (member.isfile() or member.isdir()):
                raise ValueError('Unsafe or duplicate archive member')
            if not path.parts or path.parts[0] not in {'web', 'web-manifest.json', 'compose.yml', 'build.json', 'source.tar.gz'}:
                raise ValueError('Unexpected release file')
            names.add(member.name)
        if json.load(archive.extractfile('build.json')) != build:
            raise ValueError('Inner provenance differs')
        manifest = json.load(archive.extractfile('web-manifest.json'))
        web_files = {m.name for m in members if m.isfile() and m.name.startswith('web/')}
        if not manifest or set(manifest) != web_files:
            raise ValueError('Frontend manifest differs from archive')
        for name, digest in manifest.items():
            if hashlib.sha256(archive.extractfile(name).read()).hexdigest() != digest:
                raise ValueError('Frontend hash mismatch')
        compose = archive.extractfile('compose.yml').read().decode()
        if compose.count('    image: ' + image + '\n') != 1 or re.search(r'^\s*build:', compose, re.M):
            raise ValueError('Compose must reference the exact prebuilt image')
    return build, sums, manifest


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--artifact-dir', required=True, type=Path)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--image', required=True)
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--root-dir', type=Path, default=Path('/opt/landlord'))
    parser.add_argument('--data-dir', type=Path, default=Path('/var/lib/landlord/data'))
    parser.add_argument('--env-file', type=Path, default=Path('/var/lib/landlord/bootstrap.env'))
    parser.add_argument('--backup-root', type=Path, default=Path('/var/backups/landlord'))
    parser.add_argument('--require-migration-proof', action='store_true')
    parser.add_argument('--migration-proof', type=Path)
    args = parser.parse_args(argv)
    timings = {}; started = time.monotonic()
    build, checksums, manifest = validate_package(args.artifact_dir, args.commit, args.image, args.run_id)
    timings['package_validation_seconds'] = round(time.monotonic() - started, 2)
    release = args.root_dir / 'releases' / ('run-' + str(build['run_id']) + '-' + args.commit[:12])
    plan = {'commit': args.commit, 'image': args.image, 'run_id': args.run_id, 'release': str(release), 'web_files': len(manifest), 'artifact_checksums': checksums, 'apply': args.apply, 'timings': timings}
    if args.require_migration_proof or args.migration_proof:
        if not args.migration_proof:
            raise ValueError('Schema changes require a completed migration rehearsal')
        proof = json.loads(args.migration_proof.read_text(encoding='utf-8'))
        if proof.get('commit') != args.commit or not proof.get('pass') or not proof.get('old_version_startup_compatible'):
            raise ValueError('Migration proof must match this commit and verify rollback compatibility')
        plan['migration_proof_sha256'] = sha(args.migration_proof)
    if not args.apply:
        print(json.dumps(plan, indent=2)); return plan
    if os.name != 'posix':
        raise ValueError('Publishing runs on the Linux deployment host')
    os.umask(0o077)
    def run(*command): return subprocess.check_output(command, text=True).strip()
    def inspect(): return json.loads(run('docker', 'inspect', 'landlord'))[0]
    def up(path):
        subprocess.run(['docker', 'compose', '-p', 'landlord', '-f', str(path / 'compose.yml'), 'up', '-d', '--no-build', '--pull', 'never'], check=True)
        # Poll the app directly; Docker's 30-second health cadence adds avoidable delay.
        end = time.monotonic() + 90
        while time.monotonic() < end:
            try:
                with urllib.request.urlopen('http://127.0.0.1:18930/api/health', timeout=2) as response:
                    health = json.load(response)
                    if health.get('code') == 0 and health.get('data', {}).get('status') == 'ok': return
            except (OSError, ValueError): time.sleep(0.3)
        raise RuntimeError('Application health timeout')
    # Keep a lock across staging and switch so two publishers cannot race.
    import fcntl
    args.root_dir.mkdir(parents=True, exist_ok=True)
    with (args.root_dir / '.publish.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        previous = (args.root_dir / 'current').resolve(strict=True)
        if previous.parent != (args.root_dir / 'releases').resolve(): raise ValueError('Current release is outside release root')
        before = inspect()
        env_sha = sha(args.env_file)
        caddy = Path('/etc/caddy/Caddyfile'); caddy_sha = sha(caddy)
        if release.exists(): raise ValueError('Release directory already exists; inspect its receipt before retrying')
        release.mkdir(parents=True)
        with tarfile.open(args.artifact_dir / 'release.tar.gz') as archive: archive.extractall(release, filter='data')
        compose = (release / 'compose.yml').read_text(encoding='utf-8')
        if str(args.data_dir) + ':/app/data' not in compose or str(args.env_file) not in compose:
            raise ValueError('Compose data/env mounts differ from requested backup paths')
        for path in [release / 'web', *(release / 'web').rglob('*')]: path.chmod(0o755 if path.is_dir() else 0o644)
        if not subprocess.run(['docker', 'image', 'inspect', args.image], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
            subprocess.run(['docker', 'load', '-i', str(args.artifact_dir / 'image.tar.gz')], check=True)
        candidate = json.loads(run('docker', 'image', 'inspect', args.image))[0]
        if candidate['RootFS']['Layers'] != build['rootfs_layers'] or candidate['Config']['Labels']['org.opencontainers.image.revision'] != args.commit:
            raise ValueError('Loaded image does not match artifact')
        subprocess.run(['docker', 'compose', '-p', 'landlord', '-f', str(release / 'compose.yml'), 'config', '-q'], check=True)
        backup = args.backup_root / ('pre-run-' + str(build['run_id']) + '-' + datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S'))
        backup.mkdir(parents=True)
        switched = False
        cutoff = time.monotonic()
        try:
            subprocess.run(['docker', 'stop', '--time', '20', 'landlord'], check=True, stdout=subprocess.DEVNULL)
            def ignore(parent, names):
                return [n for n in names if n in ('rental.db', 'rental.db-wal', 'rental.db-shm')] if Path(parent) == args.data_dir / 'db' else []
            shutil.copytree(args.data_dir, backup / 'data', ignore=ignore)
            with closing(sqlite3.connect('file:' + str(args.data_dir / 'db/rental.db') + '?mode=ro', uri=True)) as source, closing(sqlite3.connect(backup / 'data/db/rental.db')) as dest:
                source.backup(dest)
                if dest.execute('pragma quick_check').fetchone()[0] != 'ok': raise ValueError('Backup integrity failed')
            shutil.copy2(args.env_file, backup / 'bootstrap.env'); shutil.copy2(previous / 'compose.yml', backup / 'previous-compose.yml'); shutil.copy2(caddy, backup / 'Caddyfile')
            up(release)
            current = inspect()
            if current['Image'] != candidate['Id'] or current['State']['OOMKilled']: raise ValueError('Unexpected running image/OOM')
            for key in ('Memory', 'MemorySwap', 'NanoCpus', 'PidsLimit', 'ReadonlyRootfs', 'PortBindings'):
                if current['HostConfig'][key] != before['HostConfig'][key]: raise ValueError('Changed runtime limit or binding: ' + key)
            with urllib.request.urlopen('http://127.0.0.1:18930/', timeout=10) as response:
                if response.read() != (release / 'web/index.html').read_bytes().replace(b'__FNOS_GATEWAY_FLAG__', b'false'): raise ValueError('Frontend mismatch')
            with closing(sqlite3.connect('file:' + str(args.data_dir / 'db/rental.db') + '?mode=ro', uri=True)) as db:
                if db.execute('pragma quick_check').fetchone()[0] != 'ok': raise ValueError('Database integrity failed')
                with closing(sqlite3.connect('file:' + str(backup / 'data/db/rental.db') + '?mode=ro', uri=True)) as old:
                    for table in ('users', 'rooms', 'tenants', 'bills', 'bill_items', 'payments', 'issued_documents', 'contracts', 'tenant_attachments', 'lease_renewals', 'orgs', 'user_orgs'):
                        exists = old.execute("select count(*) from sqlite_master where type='table' and name=?", (table,)).fetchone()[0]
                        if not exists: continue
                        cols = [r[1] for r in old.execute('pragma table_info(' + table + ')')]
                        select = 'select ' + ','.join('"' + c + '"' for c in cols) + ' from ' + table
                        rows = old.execute(select).fetchall()
                        # Existing IDs/fields are preserved; later legitimate inserts may exist.
                        if 'id' in cols:
                            index = cols.index('id'); ids = {r[index] for r in rows}
                            actual = [r for r in db.execute(select).fetchall() if r[index] in ids]
                        else: actual = db.execute(select).fetchall()
                        if sorted(rows, key=repr) != sorted(actual, key=repr): raise ValueError('Existing business values changed: ' + table)
            for old_file in (backup / 'data').rglob('*'):
                if not old_file.is_file(): continue
                relative = old_file.relative_to(backup / 'data')
                if relative.parts[0] in ('db', 'logs'): continue
                if not (args.data_dir / relative).is_file() or sha(old_file) != sha(args.data_dir / relative): raise ValueError('Existing uploaded file changed')
            if sha(args.env_file) != env_sha or sha(caddy) != caddy_sha: raise ValueError('Protected configuration drifted')
            temp = args.root_dir / 'current.next'; temp.symlink_to(release, target_is_directory=True); os.replace(temp, args.root_dir / 'current')
            switched = True
        finally:
            if not switched:
                up(previous)  # Application only; never overwrite later business with an old database.
        plan.update(published=True, previous_release=str(previous), backup=str(backup), source_database_restored=False)
        timings['switch_and_backup_seconds'] = round(time.monotonic() - cutoff, 2)
        timings['total_seconds'] = round(time.monotonic() - started, 2)
        (release / 'published.json').write_text(json.dumps(plan, indent=2) + '\n')
        print(json.dumps(plan, indent=2)); return plan


if __name__ == '__main__': main()
