# Порты LinkLAN на сервере с другими сервисами

Веб-порт фиксирован: 8191/TCP. По умолчанию STUN опубликован на 3478/UDP,
relay на 51820/UDP. Внешние UDP-порты можно менять через `.env`:

| Переменная | Значение по умолчанию | Назначение |
|---|---|---|
| STUN_HOST_PORT | 3478 | UDP-порт на Docker-host для STUN |
| RELAY_HOST_PORT | 51820 | UDP-порт на Docker-host для relay |
| STUN_PUBLIC_ADDRESS | 127.0.0.1:3478 | Доступный клиентам адрес STUN |
| RELAY_PUBLIC_ADDRESS | 127.0.0.1:51820 | Доступный клиентам адрес relay |

Внутри контейнера остаются 3478 и 51820; клиент получает внешние адреса из API.
Значения PUBLIC_ADDRESS должны учитывать выбранные внешние порты. Для удалённых
клиентов указывайте доступный домен/IPv4 сервера, а не 127.0.0.1.

## Порт 3478 уже занят

Сначала проверьте предлагаемый новый порт:

```bash
ss -lunp 'sport = :13478'
```

Если есть строка сокета, выберите другой свободный UDP-порт. Пример `.env`
для сервера с публичным адресом vpn.example.com:

```dotenv
STUN_HOST_PORT=13478
STUN_PUBLIC_ADDRESS=vpn.example.com:13478
RELAY_HOST_PORT=51820
RELAY_PUBLIC_ADDRESS=vpn.example.com:51820
```

Редактируйте существующий `.env`; патч не содержит рабочую конфигурацию,
пароли БД или данные. После применения патча и изменения `.env`:

```bash
docker compose -f compose.yml up -d --no-deps --force-recreate api relay web
docker compose -f compose.yml ps
docker compose -f compose.yml logs --tail=50 api relay web
```

Для HTTPS используйте оба конфигурационных файла:

```bash
docker compose -f compose.yml -f compose.tls.yml up -d \
  --no-deps --force-recreate api relay web
```

Образы не нужно пересобирать для изменения published port или environment.
В выводе `docker compose ps` ожидается `13478->3478/udp` для relay и
`8191->80/tcp` для web. Разрешите 13478/UDP в firewall сервера, внешних ACL
и перенаправлении роутера, если они используются. Повторно подключите клиент,
чтобы он получил новый STUN endpoint.

Для подтверждения занятого старого порта можно использовать:

```bash
ss -lunp 'sport = :3478'
docker inspect volna-turn-1 --format '{{.HostConfig.NetworkMode}}'
```

Отсутствие PORTS в `docker ps` у контейнера не исключает использование сокетов
хоста: контейнер с `network_mode: host` использует сетевой namespace хоста.

Референсы:
https://docs.docker.com/reference/compose-file/services/#ports
https://docs.docker.com/engine/network/drivers/host/
