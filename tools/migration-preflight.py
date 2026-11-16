"""Read-only intake of an explicitly approved, stopped, desensitized data copy.

No network, migration, backup, restore, checkpoint, or anonymization is performed.
SQLite immutable mode requires a closed, checkpointed copy without sidecars.
"""
import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import sqlite3
import stat


REFERENCES = (
    ('contracts', 'file_path', 'contracts', False, False),
    ('tenant_attachments', 'file_path', 'attachments', False, False),
    ('room_photos', 'file_path', 'attachments', False, False),
    ('meter_photos', 'file_path', 'attachments', False, False),
    ('room_item_photos', 'file_path', 'attachments', False, False),
    ('tenant_applications', 'id_photo', 'attachments', False, True),
    ('room_applications', 'photos', 'attachments', True, True),
    ('tenant_change_requests', 'new_photos', 'attachments', True, True),
)


class Rejected(ValueError):
    pass


def reject_link(path):
    info = path.lstat()
    if stat.S_ISLNK(info.st_mode) or getattr(info, 'st_file_attributes', 0) & 0x400:
        raise Rejected('LINK_OR_REPARSE_POINT')


def inventory(root):
    files = {}
    def inaccessible(error):
        raise error
    for current, dirs, names in os.walk(root, followlinks=False, onerror=inaccessible):
        for name in dirs + names:
            reject_link(Path(current) / name)
        for name in names:
            path = Path(current) / name
            if not stat.S_ISREG(path.stat().st_mode):
                raise Rejected('NON_REGULAR_FILE')
            if name in ('rental.db-wal', 'rental.db-shm', 'rental.db-journal'):
                raise Rejected('SQLITE_SIDECAR_PRESENT')
            if name.casefold().startswith('.env') or name.casefold() == 'bootstrap.env' or path.suffix.lower() in ('.pem', '.p12', '.pfx', '.key'):
                raise Rejected('CONFIGURATION_OR_KEY_PRESENT')
            with path.open('rb') as stream:
                digest = hashlib.file_digest(stream, 'sha256').hexdigest()
            files[path.relative_to(root).as_posix()] = [path.stat().st_size, digest]
    return files


def inspect(data_dir, *, offline=False, approved_copy=False):
    if not offline or not approved_copy:
        raise Rejected('APPROVED_OFFLINE_COPY_REQUIRED')
    root = Path(data_dir).absolute()
    for ancestor in (root, *root.parents):
        reject_link(ancestor)
    if not root.is_dir():
        raise Rejected('DATA_DIRECTORY_REQUIRED')
    before = inventory(root)
    if 'db/rental.db' not in before:
        raise Rejected('DATABASE_MISSING')
    database = root / 'db/rental.db'
    with closing(sqlite3.connect(database.as_uri() + '?mode=ro&immutable=1', uri=True)) as db:
        db.execute('PRAGMA query_only=ON')
        db.execute('PRAGMA trusted_schema=OFF')
        if db.execute('PRAGMA quick_check').fetchall() != [('ok',)]:
            raise Rejected('DATABASE_INTEGRITY_FAILED')
        schema = db.execute("SELECT name, sql FROM sqlite_master WHERE type='table' ORDER BY name").fetchall()
        tables = {row[0] for row in schema}
        if not {'users', 'system_configs'} <= tables:
            raise Rejected('NOT_LANDLORD_DATABASE')
        declared = 0
        row_counts = {}
        for table in sorted(tables):
            quoted = '"' + table.replace('"', '""') + '"'
            declared += len(db.execute('PRAGMA foreign_key_list(' + quoted + ')').fetchall())
            row_counts[table] = db.execute('SELECT count(*) FROM ' + quoted).fetchone()[0]
        if db.execute('PRAGMA foreign_key_check').fetchone() is not None:
            raise Rejected('DECLARED_FOREIGN_KEY_VIOLATION')
        references = 0
        for table, column, directory, multiple, optional in REFERENCES:
            if table not in tables:
                continue
            for (raw,) in db.execute(f'SELECT {column} FROM {table}'):
                if optional and raw in (None, ''):
                    continue
                try:
                    paths = json.loads(raw) if multiple else [raw]
                except (TypeError, ValueError):
                    raise Rejected('INVALID_UPLOAD_REFERENCE') from None
                if multiple and optional and paths is None:
                    continue
                if not isinstance(paths, list):
                    raise Rejected('INVALID_UPLOAD_REFERENCE')
                for name in paths:
                    if not isinstance(name, str) or not name:
                        raise Rejected('INVALID_UPLOAD_REFERENCE')
                    rel = PurePosixPath(name)
                    if rel.is_absolute() or '..' in rel.parts or '\\' in name or ':' in name or rel.as_posix() != name:
                        raise Rejected('INVALID_UPLOAD_REFERENCE')
                    if (PurePosixPath(directory) / rel).as_posix() not in before:
                        raise Rejected('UPLOAD_MISSING')
                    references += 1
    if before != inventory(root):
        raise Rejected('SOURCE_CHANGED')
    return {
        'status': 'pass', 'scope': 'offline-copy-intake-only',
        'desensitization': 'operator-attestation-not-automatically-verified',
        'file_count': len(before), 'file_bytes': sum(value[0] for value in before.values()),
        'inventory_sha256': hashlib.sha256(json.dumps(before, sort_keys=True).encode()).hexdigest(),
        'schema_sha256': hashlib.sha256(json.dumps(schema).encode()).hexdigest(),
        'table_rows': row_counts, 'declared_foreign_keys': declared,
        'upload_references_checked': references,
        'warnings': ['NO_DECLARED_FOREIGN_KEYS_BUSINESS_LINKS_UNVERIFIED'] if not declared else [],
        'migration_verified': False, 'restore_verified': False,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--data-dir', type=Path, required=True)
    parser.add_argument('--offline', action='store_true')
    parser.add_argument('--approved-copy', action='store_true')
    parser.add_argument('--output', type=Path, help='New local JSON file outside the data directory; parent must exist')
    args = parser.parse_args()
    try:
        if args.output:
            root, output = args.data_dir.resolve(), args.output.resolve()
            if output == root or root in output.parents:
                raise Rejected('OUTPUT_INSIDE_SOURCE')
            if args.output.exists() or args.output.is_symlink():
                raise Rejected('OUTPUT_EXISTS')
        result = inspect(args.data_dir, offline=args.offline, approved_copy=args.approved_copy)
    except Rejected as exc:
        result = {'status': 'fail', 'code': str(exc)}
    except (OSError, sqlite3.Error, ValueError):
        # Exception text may contain filenames, SQL values, or other identifiers.
        result = {'status': 'fail', 'code': 'INPUT_IO_OR_SCHEMA_ERROR'}
    body = json.dumps(result, ensure_ascii=True, indent=2)
    if args.output and result.get('code') not in ('OUTPUT_EXISTS', 'OUTPUT_INSIDE_SOURCE'):
        try:
            fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'w', encoding='utf-8') as stream:
                stream.write(body + '\n')
        except OSError:
            print(json.dumps({'status': 'fail', 'code': 'OUTPUT_UNAVAILABLE'}))
            return 1
    print(body)
    return 0 if result['status'] == 'pass' else 1


if __name__ == '__main__':
    raise SystemExit(main())
