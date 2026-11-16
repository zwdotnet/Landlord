import importlib.util
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

SCRIPT = Path(__file__).with_name('migration-preflight.py')
spec = importlib.util.spec_from_file_location('preflight', SCRIPT)
preflight = importlib.util.module_from_spec(spec)
spec.loader.exec_module(preflight)


class IntakeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.data = self.root / 'data'
        (self.data / 'db').mkdir(parents=True)
        (self.data / 'attachments').mkdir()
        (self.data / 'attachments/synthetic.pdf').write_bytes(b'%PDF- synthetic')
        with closing(sqlite3.connect(self.data / 'db/rental.db')) as db, db:
            db.executescript("CREATE TABLE users(id INTEGER PRIMARY KEY, secret TEXT);"
                             "INSERT INTO users VALUES(1, 'SYNTHETIC_DO_NOT_PRINT');"
                             "CREATE TABLE system_configs(id INTEGER PRIMARY KEY);"
                             "CREATE TABLE tenant_attachments(id INTEGER PRIMARY KEY, file_path TEXT);"
                             "INSERT INTO tenant_attachments VALUES(1, 'synthetic.pdf');"
                             "CREATE TABLE room_applications(id INTEGER PRIMARY KEY, photos TEXT);"
                             "INSERT INTO room_applications VALUES(1, '[\"synthetic.pdf\"]');")

    def inspect(self):
        return preflight.inspect(self.data, offline=True, approved_copy=True)

    def sql(self, sql, args=()):
        with closing(sqlite3.connect(self.data / 'db/rental.db')) as db, db:
            db.execute(sql, args)

    def test_closed_copy_unchanged_and_no_identifiers_emitted(self):
        before = preflight.inventory(self.data)
        result = self.inspect()
        self.assertEqual(result['status'], 'pass')
        self.assertEqual(result['upload_references_checked'], 2)
        self.assertEqual(result['declared_foreign_keys'], 0)
        self.assertTrue(result['warnings'])
        self.assertFalse(result['migration_verified'])
        self.assertNotIn('SYNTHETIC_DO_NOT_PRINT', json.dumps(result))
        self.assertNotIn('synthetic.pdf', json.dumps(result))
        self.assertEqual(before, preflight.inventory(self.data))

    def test_requires_explicit_offline_copy(self):
        for flags in ({}, {'offline': True}, {'approved_copy': True}):
            with self.assertRaisesRegex(preflight.Rejected, 'APPROVED_OFFLINE_COPY_REQUIRED'):
                preflight.inspect(self.data, **flags)

    def test_sidecars_refused_without_opening_database(self):
        for suffix in ('-wal', '-shm', '-journal'):
            path = self.data / ('db/rental.db' + suffix)
            path.write_bytes(b'')
            with self.assertRaisesRegex(preflight.Rejected, 'SQLITE_SIDECAR_PRESENT'):
                self.inspect()
            path.unlink()

    def test_missing_upload_and_unsafe_references(self):
        for value, code in [('missing.pdf', 'UPLOAD_MISSING'), ('../escape', 'INVALID_UPLOAD_REFERENCE'),
                            ('C:/outside', 'INVALID_UPLOAD_REFERENCE'), ('bad\\path', 'INVALID_UPLOAD_REFERENCE')]:
            self.sql('UPDATE tenant_attachments SET file_path=?', (value,))
            with self.assertRaisesRegex(preflight.Rejected, code):
                self.inspect()

    def test_malformed_pending_photo_list(self):
        self.sql('UPDATE room_applications SET photos=?', ('[42]',))
        with self.assertRaisesRegex(preflight.Rejected, 'INVALID_UPLOAD_REFERENCE'):
            self.inspect()

    def test_declared_foreign_key_violation(self):
        self.sql('CREATE TABLE child(user_id INTEGER REFERENCES users(id))')
        self.sql('INSERT INTO child VALUES(999)')
        with self.assertRaisesRegex(preflight.Rejected, 'DECLARED_FOREIGN_KEY_VIOLATION'):
            self.inspect()

    def test_configuration_rejected(self):
        (self.data / 'bootstrap.env').write_text('SYNTHETIC=value')
        with self.assertRaisesRegex(preflight.Rejected, 'CONFIGURATION_OR_KEY_PRESENT'):
            self.inspect()

    def test_source_change_rejected(self):
        original = preflight.inventory
        calls = 0
        def inventory(path):
            nonlocal calls
            calls += 1
            if calls == 2:
                (path / 'attachments/new').write_bytes(b'new')
            return original(path)
        with mock.patch.object(preflight, 'inventory', inventory):
            with self.assertRaisesRegex(preflight.Rejected, 'SOURCE_CHANGED'):
                self.inspect()

    def test_output_preserves_existing_and_source_directory(self):
        output = self.root / 'existing.json'
        output.write_text('keep')
        for path, code in [(output, 'OUTPUT_EXISTS'), (self.data / 'report.json', 'OUTPUT_INSIDE_SOURCE')]:
            result = subprocess.run([sys.executable, str(SCRIPT), '--data-dir', str(self.data),
                                     '--offline', '--approved-copy', '--output', str(path)],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(json.loads(result.stdout)['code'], code)
        self.assertEqual(output.read_text(), 'keep')
        self.assertFalse((self.data / 'report.json').exists())

    def test_database_error_does_not_print_raw_exception(self):
        (self.data / 'db/rental.db').write_bytes(b'SYNTHETIC_DO_NOT_PRINT')
        result = subprocess.run([sys.executable, str(SCRIPT), '--data-dir', str(self.data), '--offline', '--approved-copy'],
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(json.loads(result.stdout)['code'], 'INPUT_IO_OR_SCHEMA_ERROR')
        self.assertNotIn(str(self.data), result.stdout + result.stderr)

    def test_new_output_written_outside_source(self):
        before = preflight.inventory(self.data)
        output = self.root / 'intake.json'
        result = subprocess.run([sys.executable, str(SCRIPT), '--data-dir', str(self.data),
                                 '--offline', '--approved-copy', '--output', str(output)],
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(output.read_text()), json.loads(result.stdout))
        self.assertEqual(before, preflight.inventory(self.data))

    def test_unreadable_directory_not_silently_skipped(self):
        def inaccessible_walk(*args, **kwargs):
            kwargs['onerror'](PermissionError('SYNTHETIC_DO_NOT_PRINT'))
        with mock.patch.object(preflight.os, 'walk', inaccessible_walk):
            with self.assertRaises(PermissionError):
                self.inspect()


if __name__ == '__main__':
    unittest.main()
