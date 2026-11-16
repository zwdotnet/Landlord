import importlib.util
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest import mock
import zipfile

spec=importlib.util.spec_from_file_location('full_backup',Path(__file__).with_name('full-backup.py'))
backup=importlib.util.module_from_spec(spec);spec.loader.exec_module(backup)


class FullBackupTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name);self.data=self.root/'data';(self.data/'db').mkdir(parents=True)
        with closing(sqlite3.connect(self.data/'db/rental.db')) as db, db:
            db.executescript('CREATE TABLE users(id INTEGER PRIMARY KEY); INSERT INTO users VALUES(1); CREATE TABLE system_configs(id INTEGER PRIMARY KEY); CREATE TABLE issued_documents(id INTEGER PRIMARY KEY,pdf BLOB);')
            db.execute('INSERT INTO issued_documents VALUES(1,?)',(b'%PDF- synthetic receipt',))
            for table,directory,filename in [('contracts','contracts','contract.pdf'),('tenant_attachments','attachments','id.png'),('meter_photos','attachments','meter.png')]:
                db.execute(f'CREATE TABLE {table}(id INTEGER PRIMARY KEY,file_path TEXT)')
                db.execute(f'INSERT INTO {table} VALUES(1,?)',('ledger-1/'+filename,))
                path=self.data/directory/'ledger-1'/filename;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(b'%PDF-' if filename.endswith('.pdf') else b'\x89PNG\r\n\x1a\n')
        self.config=self.root/'synthetic-bootstrap.env';self.config.write_text('SYNTHETIC=value\n')
        self.archive=self.root/'backup.zip'

    def create(self):
        return backup.create(self.data,self.archive,[self.config],offline=True)

    def test_blank_directory_roundtrip_and_receipt(self):
        self.create();dest=self.root/'restored';result=backup.restore(self.archive,dest)
        self.assertTrue(result['files']>=5)
        for directory in ('contracts','attachments'):
            for path in (self.data/directory).rglob('*'):
                if path.is_file():self.assertEqual(backup.sha(path),backup.sha(dest/'data'/path.relative_to(self.data)))
        with closing(sqlite3.connect(dest/'data/db/rental.db')) as db:
            self.assertEqual(db.execute('SELECT count(*) FROM users').fetchone()[0],1)
            self.assertEqual(db.execute('SELECT pdf FROM issued_documents').fetchone()[0],b'%PDF- synthetic receipt')
        self.assertEqual((dest/'config'/self.config.name).read_bytes(),self.config.read_bytes())

    def test_requires_offline_acknowledgment(self):
        with self.assertRaisesRegex(ValueError,'Stop this instance'):backup.create(self.data,self.archive)

    def test_missing_existing_upload_rejected_before_create(self):
        (self.data/'contracts/ledger-1/contract.pdf').unlink()
        with self.assertRaisesRegex(ValueError,'contracts row 1'): self.create()
        self.assertFalse(self.archive.exists())

    def test_missing_reference_rejected_even_with_consistent_manifest(self):
        self.create()
        with zipfile.ZipFile(self.archive) as src: entries={n:src.read(n) for n in src.namelist()}
        missing='data/attachments/ledger-1/id.png'
        del entries[missing]
        manifest=json.loads(entries['manifest.json']); del manifest['files'][missing]
        entries['manifest.json']=json.dumps(manifest).encode()
        bad=self.root/'missing.zip'
        with zipfile.ZipFile(bad,'w') as dst:
            for name,body in entries.items(): dst.writestr(name,body)
        with self.assertRaisesRegex(ValueError,'tenant_attachments row 1'): backup.verify(bad)
        with self.assertRaisesRegex(ValueError,'tenant_attachments row 1'): backup.restore(bad,self.root/'missing-target')
        self.assertFalse((self.root/'missing-target').exists())

    def test_pending_photo_lists_and_unsafe_references(self):
        photo='ledger-1/id.png'
        for table,column,multiple in [('room_photos','file_path',False),('room_item_photos','file_path',False),
                                     ('tenant_applications','id_photo',False),('room_applications','photos',True),
                                     ('tenant_change_requests','new_photos',True)]:
            with self.subTest(table=table):
                with closing(sqlite3.connect(self.data/'db/rental.db')) as db, db:
                    db.execute(f'CREATE TABLE {table}(id INTEGER PRIMARY KEY,{column} TEXT)')
                    db.execute(f'INSERT INTO {table} VALUES(1,?)',(json.dumps([photo]) if multiple else photo,))
                backup.check_database(self.data/'db/rental.db')
                for invalid in ('ledger-1/missing.png','../outside.png','C:/outside.png','/absolute.png','ledger-1\\id.png'):
                    with closing(sqlite3.connect(self.data/'db/rental.db')) as db, db:
                        db.execute(f'UPDATE {table} SET {column}=?',(json.dumps([invalid]) if multiple else invalid,))
                    with self.assertRaisesRegex(ValueError,table+' row 1'): backup.check_database(self.data/'db/rental.db')
                if multiple:
                    for empty in ('[]','null','',None):
                        with closing(sqlite3.connect(self.data/'db/rental.db')) as db, db:
                            db.execute(f'UPDATE {table} SET {column}=?',(empty,))
                        backup.check_database(self.data/'db/rental.db')
                    for invalid in ('{}','invalid','[42]'):
                        with closing(sqlite3.connect(self.data/'db/rental.db')) as db, db:
                            db.execute(f'UPDATE {table} SET {column}=?',(invalid,))
                        with self.assertRaises(ValueError): backup.check_database(self.data/'db/rental.db')
                with closing(sqlite3.connect(self.data/'db/rental.db')) as db, db: db.execute(f'DROP TABLE {table}')

    def test_preserves_existing_destination(self):
        self.create();dest=self.root/'existing';dest.mkdir();(dest/'keep').write_text('later business')
        with self.assertRaises(ValueError):backup.restore(self.archive,dest)
        self.assertEqual((dest/'keep').read_text(),'later business')

    def test_preserves_older_backup(self):
        self.create();previous=backup.sha(self.archive)
        with self.assertRaises(FileExistsError):self.create()
        self.assertEqual(backup.sha(self.archive),previous)

    def test_tampered_file_and_traversal_rejected(self):
        self.create()
        with zipfile.ZipFile(self.archive) as src:entries={n:src.read(n) for n in src.namelist()}
        for name in ('data/attachments/ledger-1/id.png','../escape'):
            bad=self.root/('bad-'+str(len(name))+'.zip');modified=dict(entries);modified[name]=b'corrupted'
            with zipfile.ZipFile(bad,'w') as dst:
                for key,value in modified.items():dst.writestr(key,value)
            with self.assertRaises(ValueError):backup.restore(bad,self.root/'bad-target')
            self.assertFalse((self.root/'bad-target').exists())

    def test_changed_source_is_not_a_successful_backup(self):
        original=backup.regular_files;calls=0
        def change(path):
            nonlocal calls
            calls+=1
            if calls==2:(self.data/'unexpected').write_text('concurrent write')
            return original(path)
        with mock.patch.object(backup,'regular_files',change):
            with self.assertRaisesRegex(ValueError,'Source changed'):self.create()
        self.assertFalse(self.archive.exists())


if __name__=='__main__':unittest.main()
