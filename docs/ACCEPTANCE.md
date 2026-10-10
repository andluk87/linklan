# Приёмка на Windows и реальных сетях

Автоматические тесты core уже выполнены. Этот сценарий ещё нужно исполнить:
в среде разработки нет Windows-драйвера/GUI, Docker daemon и доступных устройств.

1. На Linux-host собрать Dockerfile.server и Dockerfile.client. Проверить:
   docker compose config; docker compose up -d; health /api/health; веб 8191.
   Отдельно запустить compose.test.yml; все PostgreSQL integration tests должны
   исполниться (без SKIP). Проверить сохранение сетей/IP после restart PostgreSQL.
2. Windows 10 x64 и Windows 11 x64: распаковать portable, проверить UAC, появление
   Wintun, MTU=1280, /24 route и отсутствие изменения default route/DNS. Проверить
   Setup/Uninstall, очистку правил виртуального адаптера при отключении и
   отсутствие повреждения других адаптеров.
3. Два компьютера: ввод только домена, automatic device access, network create,
   password/invite join, разные виртуальные
   IP, ping в обе стороны; локальный TCP echo/file server и UDP echo. Проверить
   payload bytes/checksum, крупный TCP transfer и inner IPv4 fragments.
4. На одной LAN проверить direct local endpoint; за разными NAT — внешний P2P.
   Выполнить Full/Restricted/Port-Restricted/Symmetric/CGNAT матрицу. При блокировке
   direct peer UDP должен работать relay; после снятия блокировки — переход в P2P.
   Остановить relay при живом P2P: прямой трафик должен продолжаться.
5. Потеря control/API, Wi-Fi reconnect, изменение внешнего UDP port, истечение
   access token: reconnect по сохранённому ключу без формы входа. 30 s без контроля должны
   остановить forwarding. Проверить nonce/replay после восстановления.
6. Удаление/блокировка peer, device, user, network и logout: старые access/refresh,
   relay auth и прямой маршрут перестают давать доступ. Pending request не получает
   IP/traffic до approval. Неправильный пароль/invite и full subnet отклоняются.
7. Сфальсифицировать signature, AEAD, MAC, source IP, network UUID, replay sequence,
   expired challenge и старую session. Ни один пакет не должен попасть в Wintun.
8. Перенести DPAPI profile другому Windows-user: расшифровка должна отказать.
   Проверить ключи/токены в logs и diagnostics: их быть не должно.
9. Настроить SMTP STARTTLS. Reset code одноразовый, истекает через 15 min,
   смена пароля отзывает все refresh families. Неизвестный email — тот же ответ.
10. Собрать версии 0.2.0/0.2.1 с тестовым publisher key. Проверить signed update,
    отказ wrong signature/hash/size, rollback при broken EXE/ошибке copy,
    отсутствие downgrade и сохранение DPAPI profile.
11. Измерить P2P/relay throughput, CPU/RAM, p50/p95/p99 latency, setup time,
    API errors и packet loss при 1 000 clients. Цели ТЗ — критерии будущей приёмки,
    а не результаты существующих тестов. Затем планировать 20 000 devices.
12. Запустить 3 API replicas; проверить events и refresh races между replicas,
    DB allocation concurrency, Redis outages, bounded rate/bandwidth и backups.
13. Обновить существующую БД и профиль 0.1.0 до 0.2.0: ID пользователя/устройства,
    сети/IP и права владельца должны сохраниться. Rename/restart/recovery после
    истечения refresh не создают новое устройство. Другой домен — другой профиль.
    Replay, expired challenge, неверная подпись и заблокированный ключ отвергаются.
14. Новый Windows UI: Create/Join, ON/OFF, переключение сети, дерево и контекстные
    действия, обе темы, автоподключение, клавиатура диалогов, масштаб DPI и resize.
    Проверить окно 360 × 480 при 100% и отсутствие своего ПК во всех группах.
15. На чистом профиле проверить запрос брандмауэра: «Нет» не создаёт разрешающих
    правил; «Да» добавляет правила LinkLAN.exe и сохраняет выбор. Повторный запуск
    не спрашивает снова. Виртуальные правила должны охватывать все протоколы и
    профили, но только свой адаптер, локальный IP и подсеть активной сети.
    Проверить TCP/UDP/ICMP в обе стороны и удаление этих правил при отключении.
16. Проверить палочки на двух Windows: настоящий ICMP отвечает/заблокирован,
    границы 50/150/300 ms, выключенная сеть, удаление участника, смена сети и
    завершение приложения во время проверки. UI остаётся отзывчивым, поздний
    ответ прошлой сети не изменяет новую. Кнопка Ping использует тот же ICMP API.

Не публиковать достигнутые скорость/ёмкость и не объявлять production release,
пока соответствующие пункты не подтверждены измерениями.
