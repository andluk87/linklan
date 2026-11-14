#!/usr/bin/env bash
set -euo pipefail
umask 077
version=0.5.0
project=${LINKLAN_PROJECT_DIR:-/root/LinkLAN}
cd "$project"
files=(-f compose.yml)
if [[ -f compose.api-8191.yml ]]; then files+=(-f compose.api-8191.yml)
elif [[ -f compose.api.yml ]]; then files+=(-f compose.api.yml)
else echo 'API gateway compose file not found'; exit 1; fi
backup=$(mktemp -d "${project}-backup-${version}-XXXXXXXX")
echo "Резервная копия: $backup"
docker compose "${files[@]}" exec -T postgres pg_dump -U linklan linklan | gzip > "$backup/database.sql.gz"
tar --exclude=.git --exclude=web/downloads --exclude=releases -czf "$backup/project.tar.gz" -C "$project" .
release="https://github.com/andluk87/linklan/releases/download/v${version}"
archive="LinkLAN-update-v${version}.zip"
curl -fL --retry 3 "$release/$archive" -o "$backup/$archive"
curl -fL --retry 3 "$release/SHA256SUMS.txt" -o "$backup/SHA256SUMS.txt"
python3 - "$backup" "$archive" "$project" <<'PY'
from pathlib import Path, PurePosixPath
import hashlib, shutil, sys, zipfile
backup, name, project = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
checks = dict((line.split()[1].lstrip('*'),line.split()[0]) for line in (backup/'SHA256SUMS.txt').read_text().splitlines() if len(line.split())==2)
if hashlib.sha256((backup/name).read_bytes()).hexdigest()!=checks.get(name):
    raise SystemExit('SHA256 mismatch')
with zipfile.ZipFile(backup/name) as z:
    for entry in z.infolist():
        path=PurePosixPath(entry.filename)
        if path.is_absolute() or '..' in path.parts or '\\' in entry.filename:
            raise SystemExit('Unsafe ZIP path')
        if not path.parts: continue
        if path.parts[0] in ('.env','.secrets','certs','.git'): continue
        # Keep every existing host Compose file, including API-8191 overrides.
        if len(path.parts)==1 and path.name.startswith('compose') and (project/path).exists(): continue
        target=project.joinpath(*path.parts)
        if not target.resolve().is_relative_to(project.resolve()): raise SystemExit('Unsafe destination')
        if entry.is_dir(): target.mkdir(parents=True,exist_ok=True); continue
        target.parent.mkdir(parents=True,exist_ok=True)
        with z.open(entry) as src, target.open('wb') as dst: shutil.copyfileobj(src,dst)
PY
find web -type d -exec chmod 755 {} +
find web -type f -exec chmod 644 {} +
python3 scripts/init-admin-token.py
files+=(-f compose.admin.yml)
docker compose "${files[@]}" config --quiet
docker compose "${files[@]}" build api
docker compose "${files[@]}" up -d --no-deps api
for attempt in $(seq 1 30); do
 if health=$(curl -fsS --max-time 5 --resolve linklan.lknet.ru:8191:127.0.0.1 https://linklan.lknet.ru:8191/api/health) && \
    python3 -c 'import json,sys; d=json.loads(sys.argv[1]); sys.exit(0 if d.get("version")==sys.argv[2] and d.get("status")=="ok" else 1)' "$health" "$version"; then
   echo "$health"
   echo 'Админ-панель: https://linklan.lknet.ru:8191/api/admin'
   echo "Токен хранится в $project/.secrets/admin-token (не отправляйте его в чат)."
   exit 0
 fi
 sleep 2
done
echo "Проверка API не прошла. Резервная копия: $backup"
docker compose "${files[@]}" logs --tail=60 api
exit 1
