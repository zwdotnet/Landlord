import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('prepare', Path(__file__).parents[1] / 'prepare-miniprogram.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PrepareTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.old_root = module.ROOT
        module.ROOT = self.root
        (self.root / 'miniprogram').mkdir()
        self.secret = 'sample-' + 'secret-value'
        (self.root / '.env').write_text('WX_APP_ID=wx0123456789abcdef\nWX_APP_SECRET=' + self.secret + '\n')
        (self.root / 'miniprogram/project.config.example.json').write_text('{"setting":{}}')

    def tearDown(self):
        module.ROOT = self.old_root
        self.temp.cleanup()

    def test_secret_excluded_and_private_overrides_updated(self):
        private = self.root / 'miniprogram/project.private.config.json'
        private.write_text('{"setting":{"urlCheck":true},"preserve":42}')
        module.prepare('http://127.0.0.1:18940/api')
        self.assertEqual(json.loads(private.read_text())['preserve'], 42)
        self.assertFalse(json.loads(private.read_text())['setting']['urlCheck'])
        module.prepare('https://www.example.com/landlord/api', production=True)
        self.assertTrue(json.loads(private.read_text())['setting']['urlCheck'])
        for path in (self.root / 'miniprogram').iterdir():
            self.assertNotIn(self.secret, path.read_text())

    def test_unsafe_bases_rejected(self):
        for base in ('http://example.com/api', 'https://user:pass@example.com/api', 'https://example.com/api?q=1', 'https://example.com/'):
            with self.subTest(base=base), self.assertRaises(ValueError):
                module.prepare(base)
        with self.assertRaises(ValueError):
            module.prepare('http://localhost:18940/api', production=True)

    def test_invalid_appid_rejected(self):
        (self.root / '.env').write_text('WX_APP_ID=invalid')
        with self.assertRaises(ValueError):
            module.prepare('https://example.com/api')


class PageAssetsTest(unittest.TestCase):
    def test_registered_pages_have_compilable_assets(self):
        root = Path(__file__).parents[2] / 'miniprogram'
        config = json.loads((root / 'app.json').read_text(encoding='utf-8'))
        for page in config['pages']:
            for suffix in ('.js', '.json', '.wxml'):
                with self.subTest(page=page, suffix=suffix):
                    asset = root / (page + suffix)
                    self.assertTrue(asset.is_file(), str(asset))
                    if suffix == '.json':
                        self.assertIsInstance(json.loads(asset.read_text(encoding='utf-8')), dict)


if __name__ == '__main__':
    unittest.main()
