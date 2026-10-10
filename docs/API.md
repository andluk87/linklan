# REST / WebSocket API

Корень через Nginx: https://host:8191/api; через внешний Caddy: https://host/api.
Для защищённых методов:
Authorization: Bearer ACCESS_TOKEN. JSON bodies не больше 16 KiB.
Длительные access tokens в URL не используются. JWT TTL 15 min, refresh family
TTL 30 days. Вращение refresh одноразовое; replay отзывает всю family.

| Метод | Путь | Основные поля JSON |
|---|---|---|
| POST | /auth/device/challenge | public_key |
| POST | /auth/device | name, public_key, challenge, signature, version |
| POST | /auth/register | email, name, password |
| POST | /auth/login | email, password |
| POST | /auth/refresh | refresh_token |
| POST | /auth/logout | {} |
| POST | /auth/forgot | email |
| POST | /auth/reset | token, password |
| GET | /me | — |
| GET | /networks?device_id=UUID | — |
| GET | /networks/state?device_id=UUID | —; список {network, peers} |
| POST | /networks | name, description, mode, password, max_members, device_id |
| GET / DELETE | /networks/{id} | — |
| POST | /networks/{id-or-name}/join | device_id, password, token |
| POST | /networks/{id}/leave | device_id |
| GET | /networks/{id}/members | — |
| DELETE | /networks/{id}/members/{peer} | — |
| POST | /networks/{id}/members/{peer}/approve | {} |
| POST | /networks/{id}/members/{peer}/block | {} |
| POST | /invite | network_id, uses, hours |
| GET | /invite/{token} | — |
| POST | /devices/register | name, public_key, signature, version |
| GET | /devices | — |
| POST | /devices/{id}/challenge | {} |
| POST | /devices/{id}/session | network_id, challenge, signature, ephemeral_key, local_endpoint |
| POST | /devices/{id}/heartbeat | generation |
| DELETE | /devices/{id}/session | generation |
| GET | /ws | WebSocket Upgrade + Authorization header |

Admin страницы вне /api: /admin, /admin.js, /admin/state (GET), /admin/block
(POST kind=user/network/device, id, blocked). Последние два требуют admin JWT.
Health /api/health; /api/metrics закрыт на публичном Nginx и доступен Prometheus
на private Docker network.

Public key / signature encoded RawStd base64. Relay key encoded RawURL base64.
В 0.2.0 клиент использует автоматический доступ по ключу устройства:
`/auth/device/challenge` выдаёт challenge на 30 секунд, связанный с public_key.
Сообщение Ed25519 для `/auth/device`:

~~~text
LinkLAN device access|CHALLENGE|PUBLIC_KEY|NAME|VERSION
~~~

Challenge принимается один раз. Ответ содержит обычные access_token,
refresh_token, user и объект device. Один ключ всегда возвращает тот же
device/user, включая устройства старых email-аккаунтов. Заблокированный
ключ не получает новую личность. Новые устройства получают внутреннего
пользователя device_only без возможности парольного входа. Email-методы
сохранены для совместимости и панели администратора.
`/networks/state` проверяет принадлежность device_id текущему пользователю;
возвращает сети устройства и допустимый список участников для каждой.

Ed25519 registration message:
LinkLAN registration|NAME|PUBLIC_KEY

Session message:
LinkLAN session|CHALLENGE|DEVICE_UUID|NETWORK_UUID|EPHEMERAL_KEY|LOCAL_ENDPOINT

Create возвращает 201, join approval — 202, остальные успешные mutation — 200.
Ошибка JSON: {"error":"описание"}. 401 — нет сессии/отзыв; 403 — нет прав;
409 — конфликт/заменённый tunnel session; 429 — rate limit.

WebSocket messages: {"type":"peer_online","network_id":"UUID"} и другие:
ready, peer_offline, peer_joined, peer_removed, network_updated, endpoint_updated.
События — сигнал повторно прочитать актуальный REST snapshot, не полная копия
состояния. Периодический REST sync нужен и при работающем WebSocket.
