import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('landlord_release', Path(__file__).with_name('release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ArtifactValidationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.commit = 'a' * 40
        self.image = 'landlord:0.10.0-aaaaaaaaaaaa-123-1'
        self.build = {'commit': self.commit, 'image': self.image, 'run_id': '123', 'platform': 'linux/amd64', 'base_path': '/landlord/'}
        self.files = {'web/index.html': b'<html>synthetic</html>', 'compose.yml': ('services:\n  landlord:\n    image: ' + self.image + '\n').encode(), 'build.json': json.dumps(self.build).encode()}
        self.manifest = {'web/index.html': hashlib.sha256(self.files['web/index.html']).hexdigest()}

    def package(self, extra=None):
        files = dict(self.files, **{'web-manifest.json': json.dumps(self.manifest).encode()})
        files.update(extra or {})
        with tarfile.open(self.root / 'release.tar.gz', 'w:gz') as archive:
            for name, raw in files.items():
                member = tarfile.TarInfo(name); member.size = len(raw)
                archive.addfile(member, io.BytesIO(raw))
        (self.root / 'build.json').write_text(json.dumps(self.build), encoding='utf-8')
        (self.root / 'image.tar.gz').write_bytes(b'synthetic image archive')
        self.checksums()

    def checksums(self):
        (self.root / 'SHA256SUMS').write_text(''.join(release.sha(self.root / n) + '  ' + n + '\n' for n in ('build.json', 'image.tar.gz', 'release.tar.gz')), encoding='utf-8')

    def verify(self, **override):
        return release.validate_package(self.root, override.get('commit', self.commit), override.get('image', self.image), override.get('run_id', '123'))

    def test_valid_matching_package_and_default_plan_do_not_write(self):
        self.package()
        build, sums, manifest = self.verify()
        self.assertEqual(build, self.build); self.assertEqual(len(sums), 3); self.assertEqual(manifest, self.manifest)
        destination = self.root / 'never-created'
        result = release.main(['--artifact-dir', str(self.root), '--commit', self.commit, '--image', self.image, '--run-id', '123', '--root-dir', str(destination)])
        self.assertFalse(result['apply']); self.assertFalse(destination.exists())

    def test_tampered_archive_rejected(self):
        self.package(); (self.root / 'image.tar.gz').write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError, 'Checksum'): self.verify()

    def test_wrong_commit_image_or_run_rejected(self):
        self.package()
        for args in ({'commit': 'b' * 40}, {'image': 'landlord:other'}, {'run_id': '124'}):
            with self.subTest(args=args), self.assertRaisesRegex(ValueError, 'provenance'): self.verify(**args)

    def test_manifest_and_real_frontend_must_match(self):
        self.manifest['web/index.html'] = '0' * 64; self.package()
        with self.assertRaisesRegex(ValueError, 'hash mismatch'): self.verify()

    def test_extra_file_missing_from_manifest_rejected(self):
        self.package({'web/hidden.js': b'extra'})
        with self.assertRaisesRegex(ValueError, 'manifest differs'): self.verify()

    def test_traversal_and_symlink_rejected(self):
        self.package({'../outside.txt': b'bad'})
        with self.assertRaisesRegex(ValueError, 'Unsafe'): self.verify()
        self.package()
        with tarfile.open(self.root / 'release.tar.gz', 'w:gz') as archive:
            member = tarfile.TarInfo('web/escape'); member.type = tarfile.SYMTYPE; member.linkname = '/etc/passwd'; archive.addfile(member)
        self.checksums()
        with self.assertRaisesRegex(ValueError, 'Unsafe'): self.verify()

    def test_checksum_path_injection_rejected(self):
        self.package()
        with (self.root / 'SHA256SUMS').open('a') as stream: stream.write('0' * 64 + '  ../secret\n')
        with self.assertRaisesRegex(ValueError, 'Unexpected'): self.verify()

    def test_schema_change_requires_matching_migration_proof(self):
        self.package()
        args = ['--artifact-dir', str(self.root), '--commit', self.commit, '--image', self.image, '--run-id', '123', '--require-migration-proof']
        with self.assertRaisesRegex(ValueError, 'rehearsal'): release.main(args)
        proof = self.root / 'proof.json'
        proof.write_text(json.dumps({'commit': 'b' * 40, 'pass': True, 'old_version_startup_compatible': True}))
        with self.assertRaisesRegex(ValueError, 'match'): release.main(args + ['--migration-proof', str(proof)])


if __name__ == '__main__': unittest.main()
