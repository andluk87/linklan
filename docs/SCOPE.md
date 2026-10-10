# Реализация и границы версии 0.2.1

| Пункт | Состояние |
|---|---|
| Windows 10/11 x64 | Native Win32 UI и cross-compiled EXE. Runtime QA на Windows нужен |
| Подключение по домену без аккаунта | Одноразовый Ed25519 challenge, постоянный ключ устройства, JWT/refresh; совместимость со старыми устройствами |
| Компактный Windows UI | 360 × 480 при 100% DPI, ON/OFF, создание/вступление, тёмная/светлая тема; свой ПК только в верхней панели |
| Доступность участников | Настоящий ICMP через виртуальный IP, четыре палочки слева, обновление примерно раз в 4 s в активной сети |
| Брандмауэр | Запрос согласия при первом запуске; сохранённое разрешение для LinkLAN.exe и всех протоколов своего виртуального адаптера/подсети |
| Email/password, JWT, rotating refresh, logout | API и admin; формы регистрации/входа удалены из Windows-клиента. Argon2id 64 MiB / t=3 / p=2 |
| Восстановление пароля | Одноразовый hashed token, SMTP STARTTLS, отзыв сессий; SMTP надо настроить |
| Сети и уникальные IPv4 | PostgreSQL, блокировка строки сети при IP allocation, /24 из 10.32.0.0–10.255.255.0 |
| Private/password/invite/approval | Реализованы. Invite — код, не OS deep-link handler |
| Список участников | Другие устройства: имя, IP и ICMP RTT; server online/last seen/version и tunnel route в сведениях |
| Wintun | Реальные DLL calls и конфигурация интерфейса. Проверить Windows + Defender |
| TCP, UDP, ICMP | Передача IPv4 через адаптер; end-to-end Windows тест ещё нужен |
| NAT traversal | IPv4 STUN Binding/XOR-MAPPED-ADDRESS на tunnel socket + UDP probes |
| P2P UDP и Relay | Автоматический выбор, проверка direct каждые 2 s, fallback после 6 s |
| E2E | X25519 ephemeral, HKDF-SHA256, AES-256-GCM, Ed25519 identity proof, TOFU pinning |
| Replay и IP spoofing | 1024-frame receive window, MAC/AEAD, source/destination IPv4 validation |
| MTU | Inner 1280, frame/relay overhead укладываются в обычный Ethernet MTU |
| Переподключение | GUI, fresh ephemeral key, exponential retry. Headless не supervised service |
| Peer actions | Copy IP, Ping, info, remove, block, approve |
| Admin | Users/networks/devices, блокировка, последние 100 audit events, relay stats |
| Логи | Backend JSON, audit_log, client log + crash output; payload/secrets не пишутся |
| Мониторинг | Prometheus counters/gauges + Grafana dashboard |
| Updater | Verify/stage/install/rollback код; нужен publisher key, Windows QA и Authenticode |
| Горизонтальные API реплики | Общая БД/Redis/pubsub; схема защищена advisory lock, нагрузка не измерена |
| Несколько relay регионов | Не реализовано, один узел в Compose |
| Windows системная служба | Не реализована; core в процессе GUI или headless |
| Одновременно несколько активных сетей | Не реализовано; можно хранить несколько и переключать одну |
| Broadcast/multicast/mDNS/NetBIOS | Вторая версия; подключение к играм по IP |
| IPv6/macOS/Linux/MFA/SSO/ACL/DNS/exit nodes | Вторая версия |
| Целевые 100 Mbps / 20 000 devices / latency | Не измерены и не гарантируются |

Владелец сервера доверен при предоставлении устройствам доступа и сетевых
прав и первом знакомстве с peer identity. После первого соединения замена
знакомого ключа автоматически отвергается. Для защиты первого знакомства от
злонамеренного coordinator необходима независимая проверка fingerprint/identity.
Это не аудированный Noise/WireGuard protocol: нужен отдельный security review
до внедрения в чувствительной инфраструктуре.

Отзыв Redis authorization relay виден с задержкой до 2 s (cache). Клиенты
проверяют membership каждые 5 s и по WebSocket events; при недоступности control
plane более 30 s останавливают передачу до восстановления. IP пакеты передаются
только между approved peers выбранной сети. Данные между сетями не маршрутизируются.

Admin «Online» и gauges relay относятся к текущим UDP bindings данного relay;
они не являются полным глобальным числом устройств. Packet drop counter — сбросы
на relay, а не измеренная потеря по всей трассе. P2P success rate, client latency
aggregation и global connection rate нужно добавить после нагрузочных испытаний.
