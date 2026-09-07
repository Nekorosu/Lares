# Lares / homeshare

Домашний файлообменник для Debian 13. Один Go-бинарь `homeshare`, SQLite (pure Go), русские серверные HTML-страницы и встроенные CSS/vanilla JS. Доступ по invite, администрирование по паролю и обязательному TOTP. Node, CDN, Docker и внешняя БД для сборки и работы не нужны.

## Архитектура

- `cmd/homeshare`: запуск, конфигурация, резервное копирование и команды администратора.
- `internal/api`: HTTP, SSR, авторизация, админка, единый upload/download, recovery.
- `internal/db`: версионированная схема SQLite, миграция в транзакции, backup.
- `internal/storage`: потоковые операции через directory file descriptors с `O_NOFOLLOW`.
- `internal/auth`, `netutils`, `ratelimit`, `speedlimit`, `traffic`: контроль доступа и ресурсов.
- `internal/cleanup`, `securitylog`: TTL и журнал безопасности.
- `web/templates`, `web/static`: встроены `go:embed`; внешний `dist` не используется.

Схема — в [`internal/db/db.go`](internal/db/db.go). Помимо сущностей ТЗ, таблицы `upload_traffic`, `transfer_pending` и `file_deletions` сохраняют учёт и намерения для восстановления. `PRAGMA user_version=1`; WAL, foreign keys, busy timeout; резервирование атомарно. Один процесс сервера на одну БД, защищён `flock`.

## Сборка и проверки

Установите Go 1.24+ под архитектуру сервера (проверьте `go version`), Git и curl. Для проверки race нужен C toolchain; сам сервис собирается без CGO.

```bash
git clone https://github.com/Nekorosu/Lares.git
cd Lares
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o homeshare ./cmd/homeshare
```

Регрессионные сценарии и соответствие исправлений отчёту: [`docs/AUDIT_REMEDIATION.md`](docs/AUDIT_REMEDIATION.md). Приёмка на реальном сервере: [`implementation_checklist.md`](implementation_checklist.md).

## Первая установка

Команды выполняются обычным администратором ОС через sudo. Не запускайте HTTP-сервер от root — бинарь это запрещает.

```bash
sudo apt update
sudo apt install git curl ca-certificates sqlite3 fail2ban ufw
sudo useradd --system --user-group --home-dir /var/lib/homeshare --shell /usr/sbin/nologin homeshare
sudo install -d -o homeshare -g homeshare -m 0750 \
  /srv/media/fileshare /srv/media/fileshare/data /srv/media/fileshare/tmp \
  /srv/media/fileshare/db /home/fileshare-backup /var/log/homeshare /var/lib/homeshare
sudo install -d -o root -g homeshare -m 0750 /etc/homeshare
sudo install -o root -g root -m 0755 homeshare /usr/local/bin/homeshare
sudo /usr/local/bin/homeshare config init
sudo chown homeshare:homeshare /etc/homeshare/config.yaml
sudo chmod 0600 /etc/homeshare/config.yaml
sudoedit /etc/homeshare/config.yaml
sudo -u homeshare /usr/local/bin/homeshare admin create --username admin
sudo install -m 0644 lares.service /etc/systemd/system/lares.service
sudo systemctl daemon-reload
sudo systemctl enable --now lares.service
sudo systemctl status lares.service
curl --fail http://127.0.0.1:8090/robots.txt
```

Если пользователь ОС уже существует, пропустите `useradd`. В YAML задайте реальный `base_url`; остальные настройки сравните с [`config.yaml.example`](config.yaml.example). Не заменяйте сгенерированные secrets примерными строками. `config init` генерирует secrets один раз с правами 0600 и отказывается перезаписывать файл. Обычный запуск требует заданных секретов; ошибки/неизвестные поля YAML останавливают запуск. `LARES_CONFIG`, `LARES_SESSION_SECRET`, `LARES_IP_SALT` поддерживаются как переменные процесса, `.env` автоматически не читается. В systemd путь явно задан `Environment=LARES_CONFIG=/etc/homeshare/config.yaml`.

Пароль CLI вводится без эха; затем показываются TOTP secret и QR. Сканируйте QR в аутентификатор. Проверьте синхронизацию времени (`timedatectl`). Повторное применение уже использованного TOTP-кода запрещено; при повторном входе дождитесь следующего 30-секундного кода.

Данные: `/srv/media/fileshare/data`, временные `.part` — там же в shard-каталогах, чтобы rename был атомарным. `tmp_dir` зарезервирован для служебных задач; multipart staging отсутствует. БД: `/srv/media/fileshare/db/lares.db`. Резервные копии: `/home/fileshare-backup`. Журнал безопасности: `/var/log/homeshare/security.log` (0600). Каталоги 0750, файлы данных 0640. systemd разрешает запись только в каталоги данных, backup и журналов; YAML не меняется из UI.

## HTTPS, DuckDNS, Caddy и firewall

Создайте поддомен в [DuckDNS](https://www.duckdns.org/), укажите статический публичный IP; не публикуйте неверную AAAA-запись. Настройте проброс TCP 80/443 на сервер. Caddy получает сертификат Let's Encrypt и проксирует только на loopback. Установите официальный пакет Caddy по [инструкции Debian](https://caddyserver.com/docs/install#debian-ubuntu-raspbian):

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl gnupg
curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install caddy
sudo install -m 0644 Caddyfile.example /etc/caddy/Caddyfile
sudoedit /etc/caddy/Caddyfile
sudo caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
sudo systemctl reload caddy
```

Замените домен и локальный адрес в Caddyfile. Для локального IP используется `tls internal`; установите корневой сертификат локального CA Caddy в доверенные на своих устройствах. Не отключайте проверку TLS в браузере. Устройство извне не должно доверять этому CA. Механизм сертификатов описан в [Automatic HTTPS](https://caddyserver.com/docs/automatic-https).

Если TCP 80 заблокирован, Caddy может использовать TLS-ALPN-01 через 443; для DNS-01 нужен дополнительный модуль `dns.providers.duckdns`, отсутствующий в стандартном пакете. Получите Caddy с модулем через официальный download builder или `xcaddy`, проверьте `caddy list-modules`, установите отдельный бинарь `/usr/local/bin/caddy-duckdns` и направьте на него `ExecStart`/`ExecReload` systemd drop-in Caddy. Добавьте в TLS-блок внешнего сайта:

```caddyfile
tls {
    ca https://acme-v02.api.letsencrypt.org/directory
    dns duckdns {env.DUCKDNS_TOKEN}
}
```

Передайте `DUCKDNS_TOKEN` через защищённый `EnvironmentFile=/etc/caddy/duckdns.env` (root:root, 0600), добавленный в systemd drop-in. После изменения unit выполните `daemon-reload` и `restart caddy`. Не храните DuckDNS token в репозитории. Инструкции модуля: [caddy-dns/duckdns](https://github.com/caddy-dns/duckdns); протокол [DNS-01](https://letsencrypt.org/docs/challenge-types/). Для скачивания файлов клиентам всё равно нужен доступ к 443.

```bash
sudo ufw status numbered
# Сначала сохраните разрешение для фактического SSH-порта и своего источника.
# При стандартном SSH-профиле:
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw status verbose
```

Включайте UFW только после проверки SSH-правил и наличия доступа к консоли. Не добавляйте allow для 8090. Удалите существующее внешнее разрешение 8090, если оно есть; убедитесь `ss -ltnp`, что приложение слушает только `127.0.0.1:8090`.

## Администрирование и пользовательская работа

`/admin/login`: username, password, TOTP. В «Пользователи» создайте Person и его квоты; в «Инвайты» выберите этого Person, создайте код (по умолчанию 24 часа, одна активация) и передайте пользователю. Код/QR видны один раз, в БД хранится только hash. Пользователь вводит код на `/login` и получает HttpOnly session cookie.

На главной странице есть drag-and-drop, сроки 1/7/14/30 дней (default 14), прогресс по подтверждённым байтам, resume/cancel, общий список, поиск, сортировка, ZIP и личные квоты. Для resume выберите тот же неизменённый исходный файл. Доступ к загрузке хранится в localStorage этого браузера; при потере `upload_secret` отменить её может администратор либо cleanup по TTL. Cookie сессии не хранится в localStorage и не передаётся в URL.

«Устройства» позволяет отозвать сессию или выйти со всех устройств. Администратор может отключить Person (сессии отзываются, uploads отменяются, файлы остаются) либо удалить. При удалении по умолчанию сохраняются файлы с подписью «(удалён)»; отдельный флаг удаляет и данные. Protected запрещает пользовательское удаление, но не отменяет expiry. Quarantine виден только владельцу и админам. Approve и снятие флага — независимые действия.

Настройки квот, сроков, списка расширений, quarantine, ZIP и скорости сохраняются в SQLite и переживают restart. Изменения defaults применяются к новым Person; существующие меняются в карточке Person. Уже допущенная grace-передача может завершиться после изменения лимита. User idle продлевается по текущей настройке Person, absolute фиксируется при выдаче сессии; для пересоздания absolute отзовите старую сессию и выдайте invite. `session_absolute_days=0` — только явное разрешение админа, с предупреждением в UI. Admin всегда имеет конечный срок ≤12h idle/7d absolute.

```bash
sudo -u homeshare homeshare admin create --username second_admin
sudo -u homeshare homeshare admin reset-totp --username admin
sudo -u homeshare homeshare admin unlock --username admin
sudo -u homeshare homeshare admin delete --username second_admin
sudo -u homeshare homeshare backup
sudo journalctl -u lares.service -n 100 --no-pager
```

Reset TOTP и delete отзывают админские сессии. Unlock снимает оба вида блокировок username/IP и распределённую блокировку username вместе с окнами попыток.

## Протокол и квоты

| Метод / путь | Назначение |
|---|---|
| POST `/api/auth/login` | Пароль + обязательный `totp_code`, cookie |
| POST `/api/auth/invite/activate` | `code`, необязательное `device_name`, cookie |
| GET `/api/auth/me` | Состояние входа, без token |
| POST `/api/uploads` | JSON `filename,size,content_type,expiry_days`; возвращает secret один раз |
| HEAD `/api/uploads/{id}` | Подтверждённые `Upload-Offset` и `Upload-Length` |
| PATCH `/api/uploads/{id}?offset=N` | Последовательный фрагмент ≤32 МиБ |
| POST `/api/uploads/{id}/complete` | Завершение, допускается actual ≤ declared |
| DELETE `/api/uploads/{id}` | Отмена и удаление partial |
| GET `/api/uploads`, `/api/files`, `/api/stats` | Активные загрузки, файлы и статистика |
| GET/HEAD `/download/{id}`, `/preview/{id}` | Range; preview только проверенных медиа |
| GET/HEAD `/api/zip?ids=id1,id2` | Потоковый ZIP Store |
| POST `/files/delete/{id}`, `/devices/revoke`, `/logout` | Пользовательские действия |
| GET `/admin/{section}`, POST `/admin/action/{action}` | Админские SSR-страницы и формы |

Все изменения с cookie требуют совпадающих CSRF cookie и `X-CSRF-Token` либо поля формы `csrf_token`. Для upload-действий дополнительно нужен `X-Upload-Secret`. `Authorization: Bearer` поддерживается для небраузерного клиента без session cookie; token в query string не принимается. Старый reserve направлен в тот же обработчик; direct/multipart и старые chunk/complete возвращают 410 без записи данных.

Storage = ready files + полные declared_size активных uploads. Диск учитывает физическое свободное место, оставшиеся байты **всех** резервов и inode. Traffic external-only: completed + max(0, aborted − allowance), allowance upload=limit/2, download=limit. Grace: used + pending + ceil(size/2) ≤ limit. Ноль — нулевая квота; обход месячных квот задаётся отдельным `ignore_traffic_quota`. Размеры хранения в UI — ГиБ/МиБ; Mbps — десятичные мегабиты (250 Mbps = 31 250 000 B/s).

В ТЗ противоречат друг другу освобождение LAN от обычных rate limits и отдельные LAN-пороги. Выбран явный default `rate_limits.enforce_local: false`: локальные обычные операции без лимита частоты, вход/инвайты всегда ограничены. При `true` включаются табличные LAN-пороги. Внешние пороги соответствуют ТЗ и доступны в YAML. Локальными считаются loopback и 192.168.32.0/24; `X-Real-IP` и proto доверяются только от loopback.

Карантин определяется расширением (включая двойные) и несоответствием распознанного MIME; это не антивирус. HTML/SVG/XML/JS/CSS всегда attachment. Checksum по умолчанию выключен, отдельного режима проверки SHA-256 сейчас нет (опциональная возможность ТЗ).

## Backup, миграция, восстановление

Cleanup запускается при старте и раз в 60 секунд. Завершение файла сохраняет intent до rename; recovery завершает публикацию или отменяет отключённого владельца. Незарегистрированные `.part` и файлы удаляются из выделенного data_dir. Не складывайте туда данные других программ. Резервы истекают через динамический TTL 1..72 часа; после каждого chunk остаётся минимум час. При критическом диске chunks прерываются, partials очищаются.

Daily backup: один `lares-YYYY-MM-DD.db` через `VACUUM INTO`, последние 14 дней; повторный restart не создаёт ещё одну копию за тот же день. `homeshare backup` создаёт отдельную `upgrade-*` копию **без миграции**; эти копии удаляются вручную. Копия SQLite не включает файлы: резервируйте `data/` и конфигурацию отдельно. Секреты нужны для сохранения доступа после восстановления.

При обновлении существующего сервера сначала проверьте фактические старые `db_path` и secrets. Если конфигурация лежала в `/etc/lares`, перенесите её содержимое в `/etc/homeshare/config.yaml`, сохранив DB path и secrets. Не создавайте пустую БД вместо рабочей. Для старого `/etc/homeshare` путь уже правильный. Старые неизвестные YAML-поля удаляйте только после сопоставления с примером.

Текущая прежняя схема `people`/текстовых uploads обновляется добавлением колонок и служебных таблиц в транзакции. Перед первым обновлением создаётся `.pre-v1.db`; прежние admin sessions отзываются, чтобы повторно пройти TOTP. Неизвестная схема `persons` или integer upload IDs останавливает запуск с объяснением **без DROP и потери данных**: нужен отдельный экспорт/импорт на копии. Старые active uploads не содержали достоверной классификации сети; имеющиеся received bytes консервативно мигрируют как external за месяц создания. Восстановить утраченную прежней версией информацию невозможно.

```bash
# Выберите требуемую ветку/коммит, проверьте diff, затем:
./deploy.sh
```

Скрипт тестирует и собирает checkout, останавливает сервис, делает backup до миграции, атомарно заменяет бинарь и запускает unit. При ошибке сервис остаётся остановленным; изучите journal. Скрипт не выполняет git pull и не перезаписывает YAML. До обновления сохраните предыдущий бинарь, unit, YAML и согласованную копию данных. Для отката остановите сервис, восстановите соответствующие друг другу БД и данные вместе с secrets, верните старый бинарь/unit и запустите. Не запускайте старую версию на уже мигрированной БД: её старые миграции были разрушительными.

При штатном обрыве считаются фактически принятые/записанные в HTTP writer байты. При `kill -9` скачивания восстанавливаются по последнему durable checkpoint (каждый 1 МиБ); последний хвост может быть неизвестен. Upload после crash сверяется с размером `.part`; для незаписанного в ledger хвоста точная прежняя сеть не восстанавливается, используется консервативная классификация резерва. TCP/TLS acknowledgement и физическая доставка клиенту не равны результату HTTP Write. Эти границы явно ограничивают точность crash-учёта, не обычного обрыва.

## Fail2ban

```bash
sudo install -m 0644 fail2ban/homeshare.conf /etc/fail2ban/filter.d/homeshare.conf
sudo install -m 0644 fail2ban/jail.local.example /etc/fail2ban/jail.d/homeshare.local
sudo fail2ban-regex /var/log/homeshare/security.log /etc/fail2ban/filter.d/homeshare.conf
sudo fail2ban-client -t
sudo systemctl restart fail2ban
sudo fail2ban-client status homeshare
```

Формат строки: RFC3339 timestamp, `[event] ip=ADDRESS "details"`. Только security.log содержит raw IP; audit хранит hash. Retention: security 7 дней, audit 180 дней, traffic текущий и 11 предыдущих календарных месяцев. Fail2ban использует polling, чтобы видеть атомарную ротацию. Алиасы `fail2ban/lares.conf` и `jail.local.snippet` оставлены с тем же форматом для существующей установки; включайте только один jail.

Для воспроизводимого curl-примера создайте отдельный тестовый Person и invite в админке, установите `jq` и запустите `LARES_URL=https://ваш-домен scripts/upload-smoke.sh`. Скрипт запрашивает invite без эха, хранит cookie/secret только во временных файлах с закрытыми правами, выполняет reserve → HEAD → PATCH → complete → download → Range → stats. Он создаёт маленький файл со сроком 1 день и сессию «curl smoke test»; отзовите сессию после проверки. Admin approve, создание Person/invite, лимиты и 429 проверяются интеграционными тестами и отдельной приёмкой через UI.
