# Обновление LinkLAN до 0.2.0

В клиенте остаётся только домен сервера. Компьютер автоматически подтверждает
доступ своим сохранённым ключом. Новый сервер поддерживает старый клиент и
существующие email-аккаунты, сети и устройства.

## Ваш работающий сервер linklan.lknet.ru

Скопируйте `LinkLAN-update-v0.2.0.zip` в `/root/LinkLAN` и выполните:

~~~bash
cd /root/LinkLAN
unzip -o LinkLAN-update-v0.2.0.zip -d .
cp LinkLAN-update-v0.2.0.zip web/downloads/
docker compose -f compose.yml -f compose.caddy.yml up -d --build api web
curl -fsS --max-time 15 https://linklan.lknet.ru/api/health
~~~

В ответе health должна быть версия `0.2.0`. Если API не запустился:

~~~bash
docker compose -f compose.yml -f compose.caddy.yml logs --tail 80 api
~~~

Архив обновляет исходники, сборочные файлы, документацию, лендинг и клиент для
скачивания. `.env`, `compose.yml` и ваш `compose.caddy.yml` в архив не входят.
PostgreSQL/Redis используют существующие тома; схема дополняется автоматически
при запуске API.
Relay совместим с новым клиентом и продолжает работать на прежних UDP-портах.
Веб остаётся на 8191; Caddy продолжает обслуживать HTTPS на 443.

У вас уже настроена внешняя сеть `volna_default` и адрес `linklan-web:80`.
Для справки, существующий `compose.caddy.yml` должен содержать:

~~~yaml
services:
  web:
    networks:
      backend: {}
      caddy:
        aliases:
          - linklan-web
networks:
  caddy:
    external: true
    name: volna_default
~~~

Существующий блок `/root/volna-messenger/docker/Caddyfile`:

~~~caddyfile
linklan.lknet.ru {
    reverse_proxy linklan-web:80
}
~~~

Для этого обновления менять или перезапускать Caddy не требуется.

Если первый архив обновления дал `undefined: language.Builder`, восстановите
`vendor/golang.org/x/text/internal/language/compose.go` из включённого ZIP
исходников по инструкции [BUILD-TROUBLESHOOTING.md](BUILD-TROUBLESHOOTING.md).

## Windows-клиент

1. Сначала обновите API приведённой командой.
2. Скачайте новый ZIP с `https://linklan.lknet.ru` или из поставленного архива.
3. Закройте прежний LinkLAN и замените файлы клиента, включая `wintun.dll`.
4. Оставьте `%APPDATA%\LinkLAN\profile.dpapi` на месте и запускайте клиент
   от того же Windows-пользователя. Это сохранённый ключ и права на ваши сети.
5. Запустите `LinkLAN.exe`. Если потребуется адрес, введите `linklan.lknet.ru`.
   Старый профиль того же сервера переносится автоматически без формы входа.

На новом компьютере достаточно домена, затем имени и пароля сети. Пароль сети
задаёт её создатель; пароль учётной записи не нужен. Одновременно подключена
одна выбранная сеть. Полная инструкция пользователя лежит в ZIP как `README.txt`.
