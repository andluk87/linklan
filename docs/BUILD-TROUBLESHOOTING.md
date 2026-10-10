# Ошибки сборки

## undefined: language.Builder после обновления до 0.2.0

В первом архиве обновления 0.2.0 фильтр Compose-файлов ошибочно исключил
`vendor/golang.org/x/text/internal/language/compose.go`. Этот файл содержит
`language.Builder`. В поставленном архиве исходников он есть; восстановите его:

```bash
cd /root/LinkLAN
unzip -o -j web/downloads/LinkLAN-source-v0.2.0.zip \
  LinkLAN/vendor/golang.org/x/text/internal/language/compose.go \
  -d vendor/golang.org/x/text/internal/language/
docker compose -f compose.yml -f compose.caddy.yml up -d --build api web
curl -fsS --max-time 15 https://linklan.lknet.ru/api/health
```

Ожидается версия `0.2.0`. До успешной пересборки работающий контейнер остаётся
на `0.1.0`. Перезапуск без пересборки не заменяет бинарник API.

Архив обновления исправлен: исключаются только точные имена конфигураций
в корне проекта. Все файлы исходного ZIP сверяются с архивом обновления;
сборки проверяются из распакованного обновления. Для выпуска архивов:

```bash
python3 scripts/publish-downloads.py
python3 scripts/package-update.py
```

## Исправление первоначальной сборки 0.1.0

В первоначальном архиве Dockerfile.server и Dockerfile.client вызывали
`go mod download`. Переданный лог содержит код выхода 1, но не содержит
сообщения Go. Он не позволяет определить, что именно произошло: ошибка DNS,
соединения, module proxy/checksum service или другая причина.

Исправленная сборка включает Go-зависимости в `vendor`. Версии go.mod/go.sum
не менялись; перед созданием vendor локальные модули проверены `go mod verify`.
Оба Dockerfile используют `-mod=vendor`, `GOPROXY=off`, `GOTOOLCHAIN=local`
и `RUN --network=none` для компиляции. Загрузка Go-модулей больше не выполняется.

## Применить исправление к существующей установке

Скопируйте LinkLAN-buildfix-v0.1.0.zip на сервер. Из существующей папки проекта:

```bash
unzip -o /path/to/LinkLAN-buildfix-v0.1.0.zip -d .
test -f vendor/modules.txt
docker compose --progress plain build --no-cache api relay
docker compose up -d
```

Подставьте фактический путь к ZIP. В патче нет `.env`, сертификатов, клиентских
профилей и данных БД. Повторно создавать `.env` не нужно. Если используете TLS,
последняя команда должна учитывать TLS-конфигурацию:

```bash
docker compose -f compose.yml -f compose.tls.yml up -d
```

Клиент также можно собрать из обновлённого проекта:

```bash
docker build --progress=plain -f Dockerfile.client --target artifacts \
  --output type=local,dest=./releases .
python3 scripts/publish-downloads.py
```

Веб-порт остаётся 8191. Интернет нужен для Docker images, apt packages и
официального Wintun. Исправление убирает сетевое скачивание именно Go-модулей.

## Получить полный лог при другой ошибке

```bash
docker compose --progress plain build --no-cache api relay > build.log 2>&1
tail -n 100 build.log
```

Для диагностики нужны строки сообщения ошибки перед итоговым `exit code: 1`.
Не прикладывайте `.env` и закрытые ключи.

## Обновлять зависимости

После изменения go.mod/go.sum на машине с Go и доступом к module proxy:

```bash
go mod tidy
go mod verify
go mod vendor
CGO_ENABLED=0 go test -mod=vendor -buildvcs=false ./...
```

Каталог vendor необходимо переносить в архив целиком. Подробности:
https://go.dev/ref/mod#vendoring .
