import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location('publish', Path(__file__).with_name('publish-downloads.py'))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)


class PublishTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'releases').mkdir()
        (self.root / 'release-version').write_text('1.2.3\n')
        self.name = 'LinkLAN-Windows-x64-v1.2.3.zip'
        self.bundle = self.root / 'releases' / self.name
        with zipfile.ZipFile(self.bundle, 'w') as z:
            z.writestr('LinkLAN.exe', b'new-client')
        self.manifest = {'version': '1.2.3', 'url': '/downloads/' + self.name,
                         'sha256': hashlib.sha256(self.bundle.read_bytes()).hexdigest(),
                         'size': self.bundle.stat().st_size}
        self.write_manifest()

    def write_manifest(self):
        (self.root / 'releases' / 'latest.json').write_text(json.dumps(self.manifest))

    def test_publishes_current_binary_and_preserves_source_config(self):
        for name in ['.env', '.env.production', 'publisher.key', 'private.pem', 'profile.dpapi',
                     'deploy/certs/server.key', 'backups/private.txt', 'compose.yml', '.env.example', 'main.go']:
            p = self.root / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(name)
        publisher.publish(self.root)
        publisher.publish(self.root)
        downloads = self.root / 'web' / 'downloads'
        self.assertEqual((downloads / 'LinkLAN.exe').read_bytes(), b'new-client')
        with zipfile.ZipFile(downloads / 'LinkLAN-source-v1.2.3.zip') as z:
            self.assertEqual(set(z.namelist()), {'LinkLAN/release-version', 'LinkLAN/compose.yml',
                                               'LinkLAN/.env.example', 'LinkLAN/main.go'})

    def test_rejects_stale_or_corrupt_release_before_publishing(self):
        for key, value in [('version', '1.2.2'), ('url', '/other.zip'), ('sha256', '0' * 64), ('size', 1)]:
            with self.subTest(key=key):
                original = self.manifest[key]
                self.manifest[key] = value
                self.write_manifest()
                with self.assertRaises(ValueError):
                    publisher.publish(self.root)
                self.assertFalse((self.root / 'web').exists())
                self.manifest[key] = original


if __name__ == '__main__':
    unittest.main()
