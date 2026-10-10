#!/usr/bin/env python3
"""Create .env with new CSPRNG secrets, preserving existing installations."""
from pathlib import Path
import secrets
import os

root = Path(__file__).resolve().parent.parent
path = root / '.env'
if path.exists():
    raise SystemExit('.env уже существует; он не изменён.')
data = (root / '.env.example').read_text()
for name in ('POSTGRES_PASSWORD', 'REDIS_PASSWORD', 'JWT_SECRET', 'RELAY_COOKIE_SECRET', 'GRAFANA_PASSWORD'):
    data = data.replace(f'{name}=CHANGE_ME', f'{name}={secrets.token_hex(32)}')
with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as f:
    f.write(data)
print('Создан .env. Укажите внешний адрес STUN и Relay перед подключением удалённых компьютеров.')
