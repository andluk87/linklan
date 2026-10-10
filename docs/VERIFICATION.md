# Результаты проверки 0.1.0

- Go unit/core tests: PASS (auth, protocol, STUN, tunnel security, update signature,
  nonce persistence и authenticated relay forwarding).
- Windows x64 cross compilation: PASS, CGO=0, native Win32 executable.
- Linux server/relay compilation: PASS.
- PostgreSQL schema и полные SQL literals: parser PASS.
- Wintun ZIP SHA-256: совпадает с опубликованной официальной checksum.
- JavaScript syntax и локальные ссылки/скачивание: проверены при упаковке.
- govulncheck (Linux): 0 reachable findings; 1 finding in required modules is not called by this code.
- Go vet: проверка Linux packages; native pointer interop требует Windows runtime QA.
- PostgreSQL integration: SKIP без LINKLAN_TEST_DATABASE; сценарий и Compose приложены.
- Docker images/Compose runtime/NSIS: не запускались — Docker daemon отсутствует.
- Wintun/UI/DPAPI/Firewall/Updater rollback на Windows: не запускались.
- Реальный NAT, bandwidth, latency, capacity: не измерялись.

Go toolchain 1.26.8; зависимости закреплены в go.mod/go.sum.
Тестовые binaries без Authenticode; publisher key для updater не задан.

## Исправление Docker build: встроенные зависимости

Go-зависимости включены в vendor; в Dockerfile.server и Dockerfile.client
нет `go mod download`. Компиляция использует `-mod=vendor` и
`RUN --network=none`. Перед созданием vendor локальные module files проверены
`go mod verify`: PASS. Версии зависимостей не изменены.

Linux API/relay и Windows x64 EXE повторно собраны с пустым module cache,
`GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local`; unit/core tests также выполнены
в этом окружении. Полный отчёт: VENDOR-BUILD-RESULTS.txt.
Это проверка прямых команд Go; Docker runtime по-прежнему недоступен.
