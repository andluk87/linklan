# Компоненты и протокол LinkLAN/1

API и WebSocket передают только account/network/peer metadata. Backend не имеет
private Ed25519/X25519 keys и traffic session keys. Redis содержит presence,
relay authorization keys, challenge и pub/sub; PostgreSQL — долговременные данные.

1. Клиент создаёт Ed25519 identity; private key сохраняет только под DPAPI.
2. Backend проверяет proof-of-possession при регистрации устройства.
3. На каждый Connect генерируется новая ephemeral X25519 private key.
4. One-use backend challenge связывает Ed25519 signature с device ID, network ID,
   ephemeral public key и local endpoint.
5. Peers проверяют signature и pinned identity, затем выполняют X25519/HKDF.
6. AES-256-GCM keys раздельны по направлениям и сети; transcript включает обе
   ephemeral public keys, оба peer ID и network ID.
7. UDP socket используется одновременно для STUN, P2P и relay registration.
8. Relay выдаёт endpoint-bound HMAC cookie. Клиент доказывает владение отдельным
   authorization key, выданным по TLS. Сам key не передаётся UDP registration.
9. Direct probes/ack шифруются. Relay ack не переводит маршрут в P2P.
10. Direct route живёт 6 s после authenticated packet; новые probes идут каждые
    2 s. При отсутствии direct используется relay. Inner TCP выполняет свой retry.

## Frame

| Байты | Поле |
|---|---|
| 0–3 | LLP1 |
| 4 | protocol version 1 |
| 5 | Data / Probe / ProbeAck |
| 6–7 | ciphertext length, big endian |
| 8–23 | network UUID |
| 24–39 | source device UUID |
| 40–55 | destination device UUID |
| 56–63 | 64-bit sequence, big endian |
| 64–71 | transcript session ID |
| 72… | AES-GCM ciphertext + 16-byte tag |

Полный header — AAD. Nonce: четыре нуля + 64-bit sequence. При 2^32 sent packets
нужна новая session; GUI также переподключается через 6 часов. Receive window
допускает reordering на 1024 пакета и обновляется только после успешной AEAD.
Channel state сохраняется при исчезновении peer из presence и при повторном
появлении того же ephemeral key: counters/windows не сбрасываются. Это покрыто
отдельным regression test.

Relay envelope: LLR1 (4), sender UUID (16), sequence (8), HMAC-SHA256 (32),
готовый LLP1 frame. Relay проверяет HMAC/replay, source address, authorization
generation и совпадение network ID отправителя/получателя. Payload не расшифровывает.

## Трафик и маршруты

Wintun содержит только IPv4. Из OS принимаются пакеты с собственным assigned
source IP, от удалённого peer — с его assigned source и своим destination.
MTU 1280 задаётся на виртуальном адаптере. OS выполняет TCP segmentation и IPv4
fragmentation; отдельной UDP fragmentation/reassembly над этим протоколом нет.
Broadcast/multicast отбрасываются. /24 routes добавляются Windows вместе с IP;
default route/DNS не меняются. Наличие конфликтующего local route останавливает
подключение вместо изменения чужого маршрута.

## Масштабирование

API stateless относительно live WebSocket deliveries; общий Redis pub/sub,
PostgreSQL row locks и refresh family locks. Добавление replicas не требует
sticky HTTP sessions. WebSocket остаётся на выбранной replica до reconnect.
База требует backup; Redis не заменяет PostgreSQL.

Relay имеет process-local UDP bindings. Масштабировать его простым --scale
без распределения конкретных relay endpoints нельзя. Следующая итерация —
RelayNode records, region selection, auth tickets per node, fleet telemetry,
multiple public UDP endpoints и проверенные capacity limits.

Без рабочего UDP маршрут невозможен. P2P TCP, QUIC, TURN/TCP, Ethernet L2 и
прозрачное объединение physical subnets в этой версии отсутствуют.

