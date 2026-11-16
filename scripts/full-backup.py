"""Offline, verified Landlord backups. Never overwrite an existing restore target."""
import argparse
from contextlib import closing
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import sqlite3
import stat
import tempfile
import time
import zipfile

FORMAT = 'landlord-offline-backup-v1'
MAX_BYTES = 32 << 30


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def regular_files(root):
    result = {}
    for current, dirs, files in os.walk(root, followlinks=False):
        for name in dirs + files:
            path = Path(current) / name
            if path.is_symlink() or getattr(path, 'is_junction', lambda: False)():
                raise ValueError('Symlinks/junctions are not supported')
        dirs[:] = [d for d in dirs if not (Path(current) == root and d == 'backups')]
        for name in files:
            path = Path(current) / name
            if not stat.S_ISREG(path.stat().st_mode):
                raise ValueError('Non-regular data file')
            result[path.relative_to(root).as_posix()] = sha(path)
    return result


def check_database(path):
    with closing(sqlite3.connect(path.resolve().as_uri() + '?mode=ro', uri=True)) as db:
        if db.execute('PRAGMA quick_check').fetchall() != [('ok',)]:
            raise ValueError('Database integrity check failed')
        tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        if not {'users', 'system_configs'} <= tables:
            raise ValueError('Not a Landlord database')
        # A checksum manifest alone cannot detect uploads already missing before
        # backup. Check references in both the source and the unpacked snapshot.
        data_dir = path.parent.parent.resolve()
        references = [
            ('contracts', 'file_path', 'contracts', False, False),
            ('tenant_attachments', 'file_path', 'attachments', False, False),
            ('room_photos', 'file_path', 'attachments', False, False),
            ('meter_photos', 'file_path', 'attachments', False, False),
            ('room_item_photos', 'file_path', 'attachments', False, False),
            ('tenant_applications', 'id_photo', 'attachments', False, True),
            ('room_applications', 'photos', 'attachments', True, True),
            ('tenant_change_requests', 'new_photos', 'attachments', True, True),
        ]
        for table, column, directory, multiple, optional in references:
            if table not in tables:  # Older schemas may not have this feature.
                continue
            for row_id, raw in db.execute(f'SELECT id, {column} FROM {table}'):
                error = f'Missing or invalid upload reference: {table} row {row_id}'
                if optional and raw in (None, ''):
                    continue
                try:
                    paths = json.loads(raw) if multiple else [raw]
                except (TypeError, ValueError):
                    raise ValueError(error) from None
                if multiple and paths is None and optional:
                    continue
                if not isinstance(paths, list):
                    raise ValueError(error)
                base = data_dir / directory
                for name in paths:
                    if not isinstance(name, str) or not name:
                        raise ValueError(error)
                    rel = PurePosixPath(name)
                    if rel.is_absolute() or '..' in rel.parts or '\\' in name or ':' in name or rel.as_posix() != name:
                        raise ValueError(error)
                    target = base / rel
                    if not target.resolve().is_relative_to(base.resolve()) or not target.is_file() or target.is_symlink():
                        raise ValueError(error)


def create(data_dir, output, config_files=(), offline=False):
    started = time.monotonic()
    if not offline:
        raise ValueError('Stop this instance and pass --offline; live file/DB consistency is not supported')
    data_dir, output = Path(data_dir).resolve(), Path(output).resolve()
    if output == data_dir or data_dir in output.parents:
        raise ValueError('Backup output must be outside the source data directory')
    source_db = data_dir / 'db/rental.db'
    check_database(source_db)
    before = regular_files(data_dir)
    config_before = {str(Path(p).resolve()): sha(Path(p)) for p in config_files}
    with tempfile.TemporaryDirectory(prefix='landlord-backup-') as temporary:
        stage = Path(temporary)
        target_db = stage / 'data/db/rental.db'
        target_db.parent.mkdir(parents=True)
        with closing(sqlite3.connect(source_db.as_uri() + '?mode=ro', uri=True)) as src, closing(sqlite3.connect(target_db)) as dst:
            src.backup(dst)
        for name in before:
            if name in ('db/rental.db', 'db/rental.db-wal', 'db/rental.db-shm'):
                continue
            destination = stage / 'data' / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(data_dir / name, destination)
        seen = set()
        for filename in config_before:
            path = Path(filename)
            if path.name.casefold() in seen:
                raise ValueError('Configuration filenames must be unique')
            seen.add(path.name.casefold())
            destination = stage / 'config' / path.name
            destination.parent.mkdir(exist_ok=True)
            shutil.copyfile(path, destination)
        if before != regular_files(data_dir) or any(sha(Path(p)) != h for p,h in config_before.items()):
            raise ValueError('Source changed during backup; stop all writers and retry')
        files = {p.relative_to(stage).as_posix(): {'sha256': sha(p), 'size': p.stat().st_size}
                 for p in stage.rglob('*') if p.is_file()}
        manifest = {'format': FORMAT, 'created_at': datetime.now(timezone.utc).isoformat(),
                    'offline_required': True, 'database': 'data/db/rental.db', 'files': files,
                    'excluded': ['data/backups', 'SQLite WAL/SHM replaced by consistent DB snapshot'],
                    'external_configuration_included': sorted(seen)}
        output.parent.mkdir(parents=True, exist_ok=True)
        # Exclusive create preserves older archives. The caller controls retention.
        fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        try:
            with os.fdopen(fd, 'wb') as stream, zipfile.ZipFile(stream, 'w', zipfile.ZIP_DEFLATED) as archive:
                for name in sorted(files):
                    archive.write(stage / name, name)
                archive.writestr('manifest.json', json.dumps(manifest, ensure_ascii=False, indent=2))
            verify(output)
        except BaseException:
            output.unlink(missing_ok=True)
            raise
    return {'archive': str(output), 'files': len(files), 'seconds': round(time.monotonic()-started,3)}


def validated_manifest(archive):
    entries = archive.infolist()
    names = [entry.filename for entry in entries]
    if len(set(n.casefold() for n in names)) != len(names):
        raise ValueError('Duplicate archive paths')
    if sum(entry.file_size for entry in entries) > MAX_BYTES:
        raise ValueError('Archive exceeds supported size')
    for entry in entries:
        path = PurePosixPath(entry.filename)
        if path.is_absolute() or '..' in path.parts or '\\' in entry.filename or ':' in entry.filename or not path.parts or path.as_posix() != entry.filename or any(part.rstrip(' .') != part for part in path.parts):
            raise ValueError('Unsafe archive path')
        if stat.S_ISLNK(entry.external_attr >> 16) or entry.is_dir():
            raise ValueError('Only regular file entries are supported')
    info = archive.getinfo('manifest.json')
    if info.file_size > 8 << 20:
        raise ValueError('Manifest too large')
    manifest = json.loads(archive.read('manifest.json'))
    if manifest.get('format') != FORMAT or manifest.get('database') != 'data/db/rental.db':
        raise ValueError('Unsupported backup format')
    if set(names) != set(manifest['files']) | {'manifest.json'} or 'data/db/rental.db' not in manifest['files']:
        raise ValueError('Archive/manifest contents differ')
    if any(not name.startswith(('data/', 'config/')) for name in manifest['files']):
        raise ValueError('Unexpected backup entry')
    return manifest


def unpack_verified(archive_path, stage):
    with zipfile.ZipFile(archive_path) as archive:
        manifest = validated_manifest(archive)
        for name, expected in manifest['files'].items():
            target = stage / name
            target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            h, size = hashlib.sha256(), 0
            with archive.open(name) as src, target.open('xb') as dst:
                os.chmod(target, 0o600)
                while chunk := src.read(1 << 20):
                    h.update(chunk); size += len(chunk); dst.write(chunk)
                    if size > expected['size']:
                        raise ValueError('File size mismatch')
            if size != expected['size'] or h.hexdigest() != expected['sha256']:
                raise ValueError('Backup checksum mismatch')
        check_database(stage / manifest['database'])
    return manifest


def verify(archive):
    with tempfile.TemporaryDirectory(prefix='landlord-verify-') as temporary:
        manifest = unpack_verified(archive, Path(temporary))
    return {'verified': True, 'files': len(manifest['files'])}


def restore(archive, destination):
    started = time.monotonic()
    destination = Path(destination).resolve()
    if destination.exists():
        raise ValueError('Restore requires a new destination; existing data is never overwritten')
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.landlord-restore-', dir=destination.parent) as temporary:
        stage = Path(temporary) / 'verified'
        stage.mkdir(mode=0o700)
        manifest = unpack_verified(archive, stage)
        # A new wrapper contains data/ and config/. Configure the instance to use data/.
        os.rename(stage, destination)
    return {'restored': str(destination), 'files': len(manifest['files']), 'seconds': round(time.monotonic()-started,3)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='action', required=True)
    backup = commands.add_parser('create')
    backup.add_argument('--data-dir', required=True)
    backup.add_argument('--output', required=True)
    backup.add_argument('--config-file', action='append', default=[])
    backup.add_argument('--offline', action='store_true')
    validate = commands.add_parser('verify'); validate.add_argument('archive')
    recover = commands.add_parser('restore'); recover.add_argument('archive'); recover.add_argument('--destination', required=True)
    args = parser.parse_args()
    if args.action == 'create':
        result = create(args.data_dir, args.output, args.config_file, args.offline)
    elif args.action == 'verify': result = verify(args.archive)
    else: result = restore(args.archive, args.destination)
    print(json.dumps(result))


if __name__ == '__main__':
    main()
