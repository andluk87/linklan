#!/usr/bin/env python3
"""Publish one consistent release and package sources without runtime secrets."""
from pathlib import Path
import hashlib
import json
import shutil
import zipfile


def publish(root):
    root = Path(root)
    version = (root / 'release-version').read_text().strip()
    releases = root / 'releases'
    name = f'LinkLAN-Windows-x64-v{version}.zip'
    bundle = releases / name
    manifest = json.loads((releases / 'latest.json').read_text())
    if (manifest['version'] != version or manifest['url'] != '/downloads/' + name
            or manifest['sha256'] != hashlib.sha256(bundle.read_bytes()).hexdigest()
            or manifest['size'] != bundle.stat().st_size):
        raise ValueError('Release version, ZIP and manifest must match; build the client first')
    downloads = root / 'web' / 'downloads'
    downloads.mkdir(parents=True, exist_ok=True)
    shutil.copy2(bundle, downloads / name)
    shutil.copy2(releases / 'latest.json', downloads / 'latest.json')
    installer = releases / f'LinkLAN-Setup-x64-v{version}.exe'
    if installer.exists():
        shutil.copy2(installer, downloads / installer.name)
    with zipfile.ZipFile(bundle) as archive:
        (downloads / 'LinkLAN.exe').write_bytes(archive.read('LinkLAN.exe'))
    source = downloads / f'LinkLAN-source-v{version}.zip'
    with zipfile.ZipFile(source, 'w', zipfile.ZIP_DEFLATED) as archive:
        for item in sorted(root.rglob('*')):
            rel = item.relative_to(root)
            if not item.is_file() or any(part in ('.git', 'releases', 'downloads', '__pycache__', 'artifacts', 'backups', 'certs') for part in rel.parts):
                continue
            if (item.name.startswith('.env') and item.name != '.env.example') or item.suffix.lower() in ('.key', '.pfx', '.p12', '.pem', '.dpapi', '.exe', '.syso'):
                continue
            archive.write(item, Path('LinkLAN') / rel)
    checks = []
    for item in sorted(downloads.iterdir()):
        if item.is_file() and item.suffix in ('.zip', '.exe'):
            checks.append(f'{hashlib.sha256(item.read_bytes()).hexdigest()}  {item.name}')
    (downloads / 'SHA256SUMS.txt').write_text('\n'.join(checks) + '\n')
    print(f'Published LinkLAN {version} to web/downloads')


if __name__ == '__main__':
    publish(Path(__file__).resolve().parent.parent)
