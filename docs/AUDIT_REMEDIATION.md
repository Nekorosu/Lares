# Исправления аудита prompt.txt

Дата: 7 сентября 2026. Исходный аудит: коммит `a6667da963347931d2f750958f243ae748a70640`. Ветка исправлений: `fix/prompt-compliance`. ТЗ `prompt.txt` не изменено.

В код внесены исправления по всем 33 пунктам отчёта. Ниже отдельно указаны доказательства и ограничения проверки. Этот документ не утверждает, что настройки реального сервера, TLS или поведение браузеров уже проверены в эксплуатации.

## Реестр

Имена `TestCompliance…` находятся в `internal/api/compliance_test.go`, остальные тесты — в соответствующих пакетах. Общие пути реализации: `internal/api/upload.go`, `download.go`, `auth.go`, `admin.go`, `background.go`, `pages.go`, `server.go`.

| Пункт | Изменение | Проверка |
|---|---|---|
| F01 | Оба reserve входят в один обработчик со всеми проверками; direct закрыт | UnifiedReservationAndAtomicQuota |
| F02 | Строгая граница declared/32 МиБ, включая unknown Content-Length; превышение отменяет partial | UploadStateAndUnknownLength |
| F03 | Транзакция + сериализация жизненного цикла; полный declared для storage/traffic и глобальный remaining для диска | UnifiedReservationAndAtomicQuota, FullReservationAfterChunkAndGrace, StrictDiskAndInodeReservation |
| F04 | Последовательные chunks под upload-lock; terminal state не принимает запись; durable intent | UploadStateAndUnknownLength, CompleteIntentRetryAndCancel, race |
| F05 | Session + Person + upload secret обязательны; альтернативного complete нет | UploadSecretSessionAndOffset |
| F06 | Общий CSRF middleware; мутации только POST/PATCH/DELETE; Bearer исключение только без cookie | CSRFAndDeletedAdmin, штатные auth integration tests |
| F07 | Сессия требует существующего AdminUser; DB trigger отзывает сессии удалённого админа | CSRFAndDeletedAdmin |
| F08 | Secrets создаются один раз через exclusive config init; строгий load, environment, 0600 | PersistentSecretsAndStrictConfig, ConfigGuards |
| F09 | Disable отзывает доступ и отменяет uploads; default delete переносит metadata к orphan owner=0, сохраняет snapshot и bytes | PersonDeletionAndDisable |
| F10 | Убраны DROP/сопоставление владельцев по имени; user_version, pre-migration backup, транзакция; неизвестная схема останавливается | UpgradeActualPreAuditSchema на точном SQL прежнего коммита; MigrationPreservesExistingRowsAndBackup; UnknownLegacyStopsWithoutDataLoss |
| F11 | Range/HEAD/обрыв учитываются по фактическому HTTP Write; pending download в БД | DownloadRangeHeadPreviewZip, AbortAndDownloadConcurrency |
| F12 | Preview проходит тот же quota/rate/speed/accounting путь; whitelist MIME+extension | DownloadRangeHeadPreviewZip, QuarantineOtherUserAndProtected |
| F13 | ZIP безопасно открывает относительные пути, ограничивает сумму, Store mode, actual bytes/abort, HEAD без передачи | DownloadRangeHeadPreviewZip; общий transferWriter в AbortAndDownloadConcurrency |
| F14 | Persisted sliding windows; login/IP/username, invite, create/chunk/download/ZIP/list/admin; 429+Retry-After; UI reset точного ключа | InviteLocksAndAtomicUse, ZIPLockCanActuallyBeReset, SlidingWindowsPersistAndReset, AbortAndDownloadConcurrency |
| F15 | Cookie-only browser auth, per-Person idle/absolute, admin hard cap; TOTP обязателен независимо от legacy flag, replay запрещён | SessionIdleAndCookie, MandatoryTOTPAndReplay, QueryStringTokenAuthentication |
| F16 | CLI unlock очищает те же prefix-ключи и окна; CLI выводит TOTP QR и скрывает пароль | Общая структура ключей проверена rate tests; CLI/QR проверены кодом; сканирование реальным аутентификатором остаётся пунктом приёмки |
| F17 | Recovery intent/part/orphan/download checkpoints; expires; retention; одна daily VACUUM copy и 14 файлов | RecoveryAndDailyBackup, ExpiredSessionDoesNotCascadeActiveUpload, MonthlyTrafficAndRetention, RetentionAndPermissions |
| F18 | Из логов убраны пароли/tokens/invites; raw IP только security.log; audit включает auth, админские и фоновые действия, CLI; ошибки журнала видимы | Проверка источников логирования; RetentionAndPermissions; HTTP-сценарии создают audit записи |
| F19 | Оба fail2ban-фильтра соответствуют RFC3339 + `[event] ip=…`; jail polling и правильный путь | Реальный fail2ban-regex: 4 опасных события IPv4/IPv6 совпали, 1 нейтральное не совпало |
| F20 | Все payload-пути используют общие limiter objects; runtime меняет их на месте; read/write progress deadline 60s | RuntimeUpdatesKeepSharedLimiters; common download/upload paths; race. Длительный тест за Caddy нужен на сервере |
| F21 | Полный RuntimeSettings DTO в SQLite; validate → persist → atomic snapshot → UpdateLimits | RuntimePersistenceAndValidation, ConfigGuards |
| F22 | React/Vite/Node entrypoints и внешний dist удалены; SSR + vanilla + embed | Сборка CGO_ENABLED=0; PagesAndQuarantine; проверка дерева исходников |
| F23 | Русские устройства/logout all, resume/cancel, реальные confirmed offsets, ZIP, effective stats, безопасный preview | UploadSecretSessionAndOffset, DownloadRangeHeadPreviewZip, SessionIdleAndCookie; HTML всех страниц; JS syntax check |
| F24 | Все 10 разделов админки, Person overrides, invites, uploads, files, quarantine, traffic, audit, settings и reset | PagesAndQuarantine; PersonDeletionAndDisable; RuntimePersistenceAndValidation; тесты POST действий |
| F25 | Double-extension + MIME mismatch; quarantine toggle сохраняет флаг при отключённом карантине | PagesAndQuarantine |
| F26 | UTF-8/255-byte sanitize без panic; dirfd no-follow включая ancestors; права; loopback-only config и запрет root | FilenameBoundsAndControls, SymlinkContainmentAndModes, DirectoryAncestorSymlinkRejected, ConfigGuards; запуск serve от root отклонён |
| F27 | Один config `/etc/homeshare/config.yaml`, unit lares, user homeshare, readonly config, единый install/deploy | Проверка shell syntax; systemd-analyze проверяет директивы (production binary в /usr/local/bin в среде не установлен); README |
| F28 | Удалены ложные checklist-галочки и устаревшие TODO; добавлены тесты, migration fixture, curl smoke и конкретная приёмка | Полный test/vet/race/build; этот реестр и implementation_checklist.md |
| F29 | Каждый chunk записывает external/local и месяц; completed/canceled/expired/failed используют один ledger | MonthlyTrafficAndRetention, FullReservationAfterChunkAndGrace, UploadStateAndUnknownLength |
| F30 | Multipart/direct больше не принимает тело в staging; 410 до записи; браузер использует только chunks | UploadStateAndUnknownLength; исходники Routes/upload.js |
| F31 | Invite default 24h/1 activation; expiry 1/7/14/30 default14; forever только разрешённому Person | Формы SSR, проверка createUpload, InviteLocksAndAtomicUse, ConfigGuards |
| F32 | Обновляются last activity, login, IP/UA hashes, upload speed, completed_at и статусы; ошибки основных транзакций не игнорируются | API integration suite, recovery/migration tests; инспекция SQL и error paths |
| F33 | Удалены root main.go, server.ts и npm scripts; единственная production точка cmd/homeshare | Сборка и проверка дерева; README/deploy.sh |

## Выполненные проверки

- `go test ./...` — прошёл; в репозитории 42 top-level теста плюс табличные сценарии/subtests.
- `go test -race ./...` — прошёл, в том числе конкурентные reserve и одноразовые invites.
- `go vet ./...` — прошёл.
- `CGO_ENABLED=0 go build -trimpath -o homeshare ./cmd/homeshare` — прошёл.
- `node --check web/static/upload.js` и `bash -n deploy.sh scripts/upload-smoke.sh` — прошли. Node нужен здесь только как инструмент проверки синтаксиса; не является зависимостью проекта.
- `fail2ban-regex fail2ban/testdata/events.txt fail2ban/homeshare.conf` — 4/4 нужных совпадения, нейтральное событие исключено. Второй фильтр идентичен.
- Все 10 разделов админки, edit Person, home, devices и login страницы проверены реальным template execution с данными и экранированием.
- Миграция проверена на `internal/db/testdata/pre_audit.sql`, извлечённом из исходного коммита, а не только на новой БД.

## Принятые решения и пределы

1. В ТЗ есть противоречие по LAN rate limits. Default: обычные локальные endpoints не лимитируются; login/invite всегда лимитируются. `enforce_local=true` включает табличные LAN-пороги.
2. Для диска и хранилища используется ГиБ/МиБ; для скорости — десятичные Mbps. Grace для нечётного размера округляется вверх до целого байта, исключая обход нулевого лимита.
3. Файл меньшего размера разрешён: это прямое требование prompt.txt, а не ошибка. SHA-256 остаётся опциональным не реализованным режимом, по умолчанию выключен.
4. Неизвестные legacy schemas не преобразуются разрушительно. Автоматически поддерживается схема аудитируемого коммита; для более старой нужен импорт на копии. Предыдущие admin sessions отзываются миграцией, чтобы заново пройти обязательный TOTP. Секреты и `db_path` при обновлении надо сохранить.
5. Историческая external/local классификация старых active uploads отсутствовала; migration учитывает их полученные байты как external. Старые утраченные metadata, секреты и байты невозможно восстановить одним исправлением кода.
6. При обычном обрыве считаются байты HTTP Write. После kill -9 download учитывается по последнему durable checkpoint (1 МиБ); последний хвост и точная сеть незаписанного upload-хвоста могут быть неизвестны. Это описано в README.
7. Браузерный E2E не выполнен: Chromium не удалось скачать из-за сетевого timeout. Проверены серверный HTML, JS syntax и API; мобильная вёрстка и реальный drag/drop/resume требуют браузерной приёмки.
8. Реальный Debian/systemd/Caddy, DuckDNS/Let's Encrypt, UFW, fail2ban jail и продолжительные большие передачи на целевом диске в этой среде не развёртывались. Исходники, конфигурации и инструкции подготовлены; production-приёмка вынесена в checklist.

Изменения сделаны в отдельной ветке, чтобы весь объём исправлений можно было проверить и установить одним согласованным обновлением.
