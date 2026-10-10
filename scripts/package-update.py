#!/usr/bin/env python3
"""Package an update from the complete source ZIP while preserving host config."""
from pathlib import Path, PurePosixPath
import copy
import hashlib
import zipfile

root = Path(__file__).resolve().parent.parent
version = (root / 'release-version').read_text().strip()
downloads = root / 'web' / 'downloads'
source = downloads / f'LinkLAN-source-v{version}.zip'
output = downloads / f'LinkLAN-update-v{version}.zip'
temporary = output.with_suffix('.zip.partial')
# Match exact paths at project root. In particular, vendor/**/compose.go
# is a Go dependency and must never be treated as a Docker Compose file.
preserved = {'.env', 'compose.yml', 'compose.caddy.yml', 'compose.tls.yml', 'compose.test.yml', 'compose.api.yml'}
selected = [f'LinkLAN-Windows-x64-v{version}.zip', 'LinkLAN.exe', 'latest.json', source.name]
expected = set()
try:
    with zipfile.ZipFile(source) as src, zipfile.ZipFile(temporary, 'w', zipfile.ZIP_DEFLATED) as dst:
        for info in src.infolist():
            path = PurePosixPath(info.filename)
            if info.is_dir():
                continue
            if not path.parts or path.parts[0] != 'LinkLAN' or '..' in path.parts:
                raise ValueError(f'Unexpected source archive path: {path}')
            relative = PurePosixPath(*path.parts[1:]).as_posix()
            if relative in preserved:
                continue
            entry = copy.copy(info)
            entry.filename = relative
            dst.writestr(entry, src.read(info))
            expected.add(relative)
        for name in selected:
            dst.write(downloads / name, PurePosixPath('web/downloads') / name)
        dst.write(downloads / selected[0], PurePosixPath('releases') / selected[0])
        checks = ''.join(
            f'{hashlib.sha256((downloads / name).read_bytes()).hexdigest()}  {name}\n'
            for name in selected if name.endswith(('.zip', '.exe'))
        )
        dst.writestr('web/downloads/SHA256SUMS.txt', checks)
    with zipfile.ZipFile(temporary) as archive:
        paths = set(archive.namelist())
        if not expected <= paths or preserved & paths:
            raise ValueError('Source inventory or preserved configuration mismatch')
        for path in expected:
            if (root / path).read_bytes() != archive.read(path):
                raise ValueError(f'Source archive is stale: {path}; run publish-downloads.py first')
        if archive.testzip() is not None:
            raise ValueError('Update ZIP failed its CRC check')
    temporary.replace(output)
finally:
    temporary.unlink(missing_ok=True)

checks = ''.join(
    f'{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n'
    for path in sorted(downloads.iterdir()) if path.is_file() and path.suffix in ('.zip', '.exe')
)
(downloads / 'SHA256SUMS.txt').write_text(checks)
print(f'Created {output.name}; verified {len(expected)} source files including all vendored dependencies')
