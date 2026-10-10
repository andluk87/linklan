# LinkLAN 0.2.5

Автоматическая сборка, выпуск версий и режим API на 8191 без сайта: [docs/RELEASES.md](docs/RELEASES.md).
Исходная база этого выпуска — 0.2.1; готовый EXE 0.2.2 не содержит исходников.

Тестовый MVP виртуальной IPv4 LAN по приложенному ТЗ. Веб работает на **8191/TCP**.
Сервер и relay написаны на Go. Windows-клиент — Go + native Win32 UI + Wintun.
**Windows x64 EXE собирается в Linux Docker** без Windows build-host и без CGO.
Go-зависимости включены в `vendor`. Docker компилирует с `-mod=vendor` и
отключённой сетью; этапа `go mod download` нет. Для скачивания базовых Docker
образов, системных пакетов сборщика клиента и Wintun интернет всё ещё нужен.

В клиенте больше нет регистрации и формы входа: при первом запуске укажите
домен сервера. Устройство получает доступ автоматически по своему сохранённому
ключу. Окно клиента — 360 × 480 при масштабе 100%, с выбором светлой/тёмной темы.
Свой ПК и его IP показаны в верхней панели; в списке остаются другие участники.
Слева от них расположены четыре палочки: доступность и задержка настоящего ICMP
через виртуальную сеть. При первом запуске клиент предлагает настроить брандмауэр
и сохраняет согласие. Для клиента 0.2.5 подходит работающий API 0.2.0.
Инструкция обновления: [docs/UPGRADE-0.2.5.md](docs/UPGRADE-0.2.5.md).

Это первая реализация, а не завершение всех 35 разделов ТЗ: реальная работа
Wintun, разных NAT, Docker-стека и целевая производительность ещё требуют
испытаний. Статус функций — в docs/SCOPE.md; план проверки — в docs/ACCEPTANCE.md.

## Быстрый запуск

Нужны Docker Engine/Compose v2 с Linux-контейнерами и Python 3 для двух
подготовительных скриптов. В полном архиве в releases лежит готовый клиент ZIP.
В архиве только исходников сначала выполните сборку клиента.

~~~bash
python3 scripts/init-env.py
# В .env укажите адрес доступного всем клиентам IPv4-сервера:
# RELAY_PUBLIC_ADDRESS=vpn.example.com:51820
# STUN_PUBLIC_ADDRESS=vpn.example.com:3478
python3 scripts/publish-downloads.py
docker compose up -d --build
~~~

Локальный лендинг: http://localhost:8191. API: http://localhost:8191/api.
Панель администратора: http://localhost:8191/admin.
Откройте 3478/UDP, 51820/UDP и 8191/TCP в firewall сервера. Если сервер за NAT,
настройте перенаправление этих портов на Docker-host.

Если порт 3478 уже занят, например coturn, задайте в `.env`
`STUN_HOST_PORT=13478` и `STUN_PUBLIC_ADDRESS=vpn.example.com:13478`.
Внутри контейнера STUN продолжает слушать 3478. Для внешнего relay-порта есть
`RELAY_HOST_PORT`; при его изменении также обновите `RELAY_PUBLIC_ADDRESS`.
После изменения выполните `docker compose -f compose.yml up -d api relay web`
(для HTTPS добавьте `-f compose.tls.yml`). Откройте выбранные UDP-порты в firewall.
Подробности и проверка: [docs/PORTS.md](docs/PORTS.md).

Для интернета используйте сертификат доверенного CA на ваш домен:

~~~bash
mkdir -p deploy/certs
# deploy/certs/server.crt — full chain; deploy/certs/server.key — private key
docker compose -f compose.yml -f compose.tls.yml up -d --build
~~~

TLS 1.3 слушает внутри контейнера на 80, опубликованном как **8191**.
Адрес клиента: https://vpn.example.com:8191. Сертификат должен соответствовать
домену. Закрытый ключ сертификата не включайте в исходники/публичные архивы.

Если HTTPS уже обслуживает Caddy на 443, подключите `web` к его Docker-сети
с alias `linklan-web` и добавьте в Caddy `reverse_proxy linklan-web:80` для
вашего домена. В клиенте достаточно `vpn.example.com`. Порт 8191 у самого
LinkLAN сохраняется. В этом случае `compose.tls.yml` не нужен; пример для
работающего стека — в инструкции обновления.

Панель администратора использует отдельный email/password аккаунт. Существующий
аккаунт продолжает работать. При новой установке создайте его через API
`POST /api/auth/register` с полями `email`, `name`, `password` (12–256 байт),
затем назначьте права:

~~~bash
docker compose exec api /app/linklan-server --make-admin admin@example.com
~~~

Мониторинг включается отдельно:

~~~bash
docker compose --profile monitoring up -d
~~~

Prometheus: 127.0.0.1:9090; Grafana: 127.0.0.1:3000, пользователь admin, пароль
GRAFANA_PASSWORD из .env. Для удалённого просмотра используйте SSH forwarding.
У Grafana в MVP используется latest; перед production закрепите протестированный
образ по digest. Метрики остальных сервисов не опубликованы наружу.

## Windows-клиент

Распакуйте ZIP целиком, запустите LinkLAN.exe и разрешите UAC. Wintun DLL должна
лежать рядом с EXE. В первом окне введите только домен, например
`linklan.lknet.ru`, и нажмите «Подключиться». HTTPS и порт 443 выбираются
автоматически; для другого порта укажите `vpn.example.com:8191`.
Имя компьютера берётся из Windows и меняется в меню «Система».

На первом компьютере нажмите «Создать сеть», задайте имя и пароль.
На остальных — «Присоединиться», введите то же имя и пароль либо код
приглашения. После создания или вступления VPN подключается автоматически.
Кнопка ON/OFF включает и выключает выбранную сеть; двойной щелчок по сети
переключает подключение. В настройках доступны тёмная/светлая тема и
автоматическое подключение последней сети при запуске.

В разделе устройств доступны копирование IP, Ping, сведения о соединении,
удаление, блокировка и подтверждение заявки (последние действия — владельцу).
Игра подключается по IP участника. Broadcast discovery ещё не поддерживается.
Одновременно активна одна выбранная сеть. DNS и маршрут по умолчанию не меняются.

Профили разных серверов сохраняются отдельно в
%APPDATA%/LinkLAN/profile.dpapi. Существующий профиль 0.1.0 для того же сервера
переносится автоматически с сохранением устройства, сетей и прав владельца.
Не удаляйте этот файл при обновлении. Ed25519 private key и refresh token
защищены DPAPI текущего пользователя. Публичные ключи других устройств закрепляются
при первом соединении и не заменяются автоматически. Лог: %APPDATA%/LinkLAN/client.log;
включён вывод аварий Go runtime без логирования IP payload и секретных ключей.

HTTP разрешён только для localhost. Для изолированного теста LAN:
LinkLAN.exe --allow-http. Флаг не сохраняется между запусками.
После первого подключения к серверу доступен headless режим:

~~~powershell
.\LinkLAN.exe --headless --network NETWORK_UUID
~~~

Headless — запуск процесса, не Windows Service. GUI выполняет переподключение с
новой ephemeral key; при отзыве прав оно прекращается. После согласия в GUI
клиент разрешает входящий/исходящий трафик LinkLAN.exe и все протоколы на своём
виртуальном адаптере, ограничивая их IP-адресом и подсетью выбранной сети.
Правила адаптера удаляются при отключении; согласие используется и в headless.
Без согласия клиент продолжает запуск, но новые разрешающие правила не создаёт.
Повторить запрос можно через «Система → Доступ через брандмауэр…».

## Сборка клиента в Docker

~~~bash
docker build -f Dockerfile.client --target artifacts \
  --output type=local,dest=./releases .
python3 scripts/publish-downloads.py
~~~

Выход: portable/LinkLAN.exe, wintun.dll, ZIP, NSIS Setup EXE, latest.json и SHA256.
Официальный Wintun 0.14.1 скачивается с wintun.net; ZIP проверяется по опубликованной
SHA-256, лицензия переносится в дистрибутив. Исходники драйвера не модифицируются.

Для новой версии обновляйте release-version,
ссылки/тексты лендинга. В предоставленном ZIP EXE не имеет Authenticode-подписи
издателя. Сборка установщика описана в Dockerfile; здесь NSIS/Docker не запускались.

## Подписанные обновления

Updater проверяет HTTPS, Ed25519 подпись manifest, размер ZIP, SHA-256,
монотонность версии, безопасную структуру ZIP и самопроверку EXE. После закрытия
процесса PowerShell заменяет файлы; при ошибке/раннем выходе возвращает .bak.
В тестовой сборке ключ издателя не задан — установка обновлений отключена.
Для собственного релиза создайте ключ за пределами каталога исходников:

~~~bash
go run -buildvcs=false ./cmd/release -generate-key ../publisher.key
# Команда выводит только public key; сохраните его как PUBLIC_KEY.
docker build -f Dockerfile.client --target artifacts \
  --build-arg UPDATE_PUBLIC_KEY=PUBLIC_KEY --build-arg VERSION=0.2.2 \
  --secret id=update_signing_key,src=../publisher.key \
  --output type=local,dest=./releases .
python3 scripts/publish-downloads.py
~~~

Обновления обслуживаются /downloads/latest.json и ZIP из manifest. Private
signing key остаётся у издателя и не попадает в образ/пакет. Ed25519 package
signature и Windows Authenticode — разные подписи; для Authenticode добавьте
подпись EXE/Setup сертификатом издателя в вашей release pipeline.

## Архитектура и ограничения

API реплики используют общие PostgreSQL/Redis, Redis pub/sub и атомарные
операции PostgreSQL. Миграция сериализуется advisory lock. Масштабировать API:
docker compose up -d --scale api=3. DNS-based upstream Nginx и Prometheus находят
реплики. Нагрузочная проверка этого сценария пока не выполнена.

Relay — один узел MVP. Его UDP endpoint закреплён за процессом, поэтому обычный
round-robin load balancer для relay не подходит. Несколько регионов, выбор
RelayNode, QUIC/TCP fallback, мульти-активные сети и отдельная Windows-служба —
дальнейшие задачи. Требования 100 Mbps, 1 000/20 000 устройств и сроков подключения
не заявляются как достигнутые: нужны измерения.

SMTP восстановления требует STARTTLS (обычно host:587) и SMTP_FROM. Без SMTP
endpoint сообщает, что восстановление не настроено. Коды не возвращаются API
и не записываются в лог.

## Проверки

~~~bash
CGO_ENABLED=0 go test -mod=vendor -buildvcs=false ./...
go vet -mod=vendor -buildvcs=false ./...
docker compose -f compose.test.yml up --abort-on-container-exit --exit-code-from tests
docker compose -f compose.test.yml down -v
~~~

Криптография, replay window, сохранение nonce при online/offline, STUN, подпись
peer metadata, TLS validation, relay MAC/cookie и запрет межсетевого forwarding
проверены автоматическими тестами. Клиент cross-compiled Windows x64; server/relay
собраны Linux. Схема и полные SQL statements прошли синтаксический PostgreSQL parser.
Интеграционные PostgreSQL тесты включены, но без LINKLAN_TEST_DATABASE пропускаются.
Docker daemon и реальных Windows-устройств в среде разработки нет.
После исправления сборки проверка Go выполняется с пустым module cache и
`GOPROXY=off`: Linux server/relay, Windows x64 клиент и unit tests используют
только `vendor`. Если у вас падает старый `RUN go mod download`, примените
инструкцию из [docs/BUILD-TROUBLESHOOTING.md](docs/BUILD-TROUBLESHOOTING.md).

В integration tests создаётся отдельная случайная schema. Используйте только
выделенную тестовую БД, например Compose test-db; production database для тестов
не нужна. В compose.test.yml БД временная, без host ports.

## Структура

- cmd/server, internal/backend — REST, auth, сети, устройства, WebSocket, admin.
- cmd/relay, internal/relay — UDP auth, E2E ciphertext relay, bandwidth counters.
- internal/protocol, internal/stun — frame codec, X25519/HKDF/AES-GCM, RFC 5389 subset.
- cmd/client, internal/desktop, internal/tunnel — Win32 UI, Wintun, DPAPI, route engine.
- internal/updates, cmd/release — signed package verification и release ZIP.
- web — самостоятельный статический лендинг, включая downloads после публикации.
- deploy — Nginx/TLS, Prometheus/Grafana.
- docs — архитектура, API, ограничения и проверка на Windows.

У внешне опубликованного лендинга есть только статические страницы и скачивание;
VPN API и UDP-сервисы необходимо развернуть на вашем Docker-host.

Референсы: https://go.dev/ref/mod#vendoring ; https://www.wintun.net/ ;
https://git.zx2c4.com/wintun/about/ ;
https://go.dev/doc/install/source ;
https://pkg.go.dev/golang.org/x/crypto/argon2 ;
https://www.rfc-editor.org/rfc/rfc5389 .
