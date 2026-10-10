#!/usr/bin/env python3
"""Build a versioned Windows bundle with verified official Wintun artifacts."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parent.parent
WINTUN_URL = 'https://www.wintun.net/builds/wintun-0.14.1.zip'
WINTUN_SHA256 = '07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51'


def release_version():
    version = (ROOT / 'release-version').read_text().strip()
    if not re.fullmatch(r'(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})', version) or version == '0.0.0' or any(int(part) > 100000 for part in version.split('.')):
        raise ValueError('release-version must contain a positive canonical X.Y.Z version')
    return version


def build(output, wintun_archive=None, installer=False):
    version = release_version()
    output = Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, CGO_ENABLED='0', GOTOOLCHAIN='local', GOTELEMETRY='off')
    with tempfile.TemporaryDirectory(prefix='linklan-build-') as temporary:
        tmp = Path(temporary)
        archive = Path(wintun_archive) if wintun_archive else tmp / 'wintun.zip'
        if not wintun_archive:
            with urllib.request.urlopen(WINTUN_URL, timeout=60) as response:
                archive.write_bytes(response.read())
        if hashlib.sha256(archive.read_bytes()).hexdigest() != WINTUN_SHA256:
            raise ValueError('Official Wintun ZIP checksum mismatch')
        bundle = tmp / 'portable'
        bundle.mkdir()
        with zipfile.ZipFile(archive) as z:
            (bundle / 'wintun.dll').write_bytes(z.read('wintun/bin/amd64/wintun.dll'))
            (bundle / 'WINTUN-LICENSE.txt').write_bytes(z.read('wintun/LICENSE.txt'))
        windows_env = dict(env, GOOS='windows', GOARCH='amd64')
        flags = f'-H=windowsgui -s -w -X linklan/internal/model.Version={version}'
        subprocess.run(['go', 'build', '-mod=vendor', '-buildvcs=false', '-trimpath',
                        f'-ldflags={flags}', '-o', str(bundle / 'LinkLAN.exe'), './cmd/client'],
                       cwd=ROOT, env=windows_env, check=True)
        for source, target in [('docs/CLIENT.txt', 'README.txt'),
                               ('docs/THIRD_PARTY_NOTICES.txt', 'THIRD_PARTY_NOTICES.txt'),
                               ('scripts/Run-LinkLAN.cmd', 'Run-LinkLAN.cmd')]:
            shutil.copy2(ROOT / source, bundle / target)
        subprocess.run(['go', 'run', '-mod=vendor', '-buildvcs=false', './cmd/release',
                        '-bundle', str(bundle), '-output', str(output), '-version', version],
                       cwd=ROOT, env=dict(env, GOOS='', GOARCH=''), check=True)
        if installer:
            subprocess.run(['makensis', f'-DVERSION={version}', f'-DBUNDLE={bundle}',
                            f'-DOUTPUT={output / ("LinkLAN-Setup-x64-v" + version + ".exe")}',
                            str(ROOT / 'scripts/installer.nsi')], cwd=ROOT, check=True)
    checks = []
    for path in sorted(output.iterdir()):
        if path.name in (f'LinkLAN-Windows-x64-v{version}.zip', f'LinkLAN-Setup-x64-v{version}.exe'):
            checks.append(f'{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}')
    (output / 'SHA256SUMS.txt').write_text('\n'.join(checks) + '\n')
    print(f'Built LinkLAN {version}: {output}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT / 'releases')
    parser.add_argument('--wintun-archive', type=Path, help='Cached official ZIP (checksum still verified)')
    parser.add_argument('--installer', action='store_true', help='Also build the NSIS installer')
    args = parser.parse_args()
    build(args.output, args.wintun_archive, args.installer)
