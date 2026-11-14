#!/usr/bin/env python3
"""Create a server-local admin token without printing the secret or overwriting it."""
import os
from pathlib import Path
import secrets

root = Path(__file__).resolve().parent.parent
folder = root / '.secrets'
folder.mkdir(mode=0o700, exist_ok=True)
folder.chmod(0o700)
path = folder / 'admin-token'
try:
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
except FileExistsError:
    print('Token already exists:', path)
else:
    if os.geteuid() == 0:
        os.fchown(fd, 65532, -1)  # distroless nonroot API user
    else:
        os.fchmod(fd, 0o444)  # host folder remains private (0700)
    with os.fdopen(fd, 'w') as stream:
        stream.write(secrets.token_urlsafe(48) + '\n')
    print('Token created:', path)
