# Lares (Go File Exchange Server)

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg)](https://golang.org)

**Lares** — это высокопроизводительный автономный сервер обмена файлами, разработанный на языке **Go** с использованием базы данных **SQLite**, поддержкой двухфакторной аутентификации (2FA TOTP), карантином исполняемых файлов, ограничениями трафика и динамическим ZIP-стримингом.

Сервер спроектирован для установки на собственный домашний или корпоративный сервер (VPS / Bare-Metal Linux), обеспечивая 100% приватность без сторонних облачных сервисов.

---

## 🚀 Основные возможности

- **🔒 Безопасность и Доступ**:
  - Двухфакторная аутентификация администратора (TOTP / Google Authenticator).
  - Одноразовые 16-значные инвайт-коды (хэшируются в БД с использованием SHA256).
  - Карантин для опасных исполняемых файлов (`.exe`, `.sh`, `.bat`, `.cmd`, `.msi`).
  - Система предотвращения брутфорса (Rate Limiting: 5 неудачных попыток = 1 час бана).
  - Аудит внешних и локальных IP-адресов с возможностью соль-хэширования (`IPSalt`).

- **⚡ Производительность и Ресурсы**:
  - Потоковое скачивание множества файлов в ZIP без созидания временных архивов на диске.
  - Гибкие лимиты загрузки/скачивания в месяц (GB) и квоты хранилища на пользователя.
  - Ограничение скорости отдачи/приема внешней сети (Mbps) с поддержкой всплесков (Burst MB).
  - Фоновый воркер очистки устаревших временных файлов и временных ссылок.

---

## 📋 Требования

- **ОС**: Linux (Debian 11/12, Ubuntu 22.04+, Arch Linux)
- **Go**: Version 1.22 или новее
- **База данных**: SQLite3
- **Веб-сервер (Reverse Proxy)**: Caddy или Nginx с поддержкой SSL (Let's Encrypt)

---

## 🛠️ Быстрый запуск на собственном сервере

### 1. Сборка бинарного файла

```bash
# Клонируйте репозиторий
git clone https://github.com/your-username/lares.git
cd lares

# Скомпилируйте исполняемый Go-файл
go build -o lares main.go
```

### 2. Подготовка каталогов на сервере

```bash
# Создайте рабочие папки для файлов, временных файлов и базы данных
sudo mkdir -p /srv/media/fileshare/{data,tmp,db}
sudo mkdir -p /etc/lares /var/log/lares /home/fileshare-backup

# Ограничьте права доступа
sudo chmod -R 750 /srv/media/fileshare /etc/lares
```

### 3. Настройка конфигурации (`config.yaml`)

Скопируйте пример файла конфигурации в `/etc/lares/config.yaml`:

```bash
sudo cp config.yaml.example /etc/lares/config.yaml
sudo chmod 640 /etc/lares/config.yaml
```

Отредактируйте необходимые параметры в `/etc/lares/config.yaml`:

```yaml
listen: "127.0.0.1:8090"
base_url: "https://files.yourdomain.com"

paths:
  data_dir: "/srv/media/fileshare/data"
  tmp_dir: "/srv/media/fileshare/tmp"
  db_path: "/srv/media/fileshare/db/lares.db"
  backup_dir: "/home/fileshare-backup"
  security_log: "/var/log/lares/security.log"

network:
  local_cidrs:
    - "127.0.0.1/32"
    - "::1/128"
    - "192.168.1.0/24"

limits:
  default_storage_quota_gb: 100
  default_monthly_upload_limit_gb: 200
  default_monthly_download_limit_gb: 300
  default_max_file_size_gb: 50
  default_expiry_days: 14

speed_limits:
  external_upload_limit_mbps: 250
  external_download_limit_mbps: 250
  burst_mb: 16
```

### 4. Настройка автозапуска (systemd)

Скопируйте бинарный файл и сервис:

```bash
# Переместите бинарник в системную директорию
sudo cp lares /usr/local/bin/lares

# Скопируйте файл службы
sudo cp lares.service /etc/systemd/system/

# Создайте отдельного системного пользователя
sudo useradd -r -s /bin/false lares
sudo chown -R lares:lares /srv/media/fileshare /etc/lares /var/log/lares

# Перезагрузите systemd и запустите службу
sudo systemctl daemon-reload
sudo systemctl enable --now lares

# Проверьте статус службы
sudo systemctl status lares
```

### 5. Настройка Reverse Proxy (Caddy)

Создайте запись в `/etc/caddy/Caddyfile`:

```caddy
files.yourdomain.com {
    reverse_proxy 127.0.0.1:8090 {
        header_up X-Real-IP {http.request.remote.host}
        header_up X-Forwarded-Proto {scheme}
    }
}
```

Перезапустите Caddy:
```bash
sudo systemctl reload caddy
```

---

## 📁 Структура Проекта

```text
.
├── main.go               # Точка входа Go-сервера
├── config.yaml.example   # Шаблон конфигурационного файла
├── lares.service         # Юнит-файл systemd для автозапуска
├── Caddyfile.example     # Пример конфигурации прокси Caddy
├── LICENSE               # Текст лицензии GNU General Public License v3.0
├── README.md             # Документация проекта
├── internal/
│   ├── audit/            # Аудит действий и хэширование IP
│   ├── auth/             # Хэширование пассфраз и 2FA TOTP
│   ├── cleanup/          # Фоновые процессы очистки истекших файлов
│   ├── config/           # Парсер YAML-конфигурации
│   ├── db/               # Инициализация и миграции SQLite
│   ├── download/         # Сервис отдачи файлов с поддержкой докачки
│   ├── ratelimit/        # Защита от брутфорса и лимиты запросов
│   ├── securitylog/      # Логирование событий безопасности
│   ├── speedlimit/       # Ограничение скорости сети (Token Bucket)
│   ├── storage/          # Управление файловой системой и квотами
│   ├── traffic/          # Учет помесячного входящего/исходящего трафика
│   ├── upload/           # Прием файлов и проверка на карантин
│   └── zip/              # Динамический ZIP-стриминг
└── src/                  # React Дашборд и визуальный веб-интерфейс
```

---

## 📜 Лицензия

Проект распространяется под открытой лицензией **GNU General Public License v3.0 (GPL-3.0)**. Смотрите файл [LICENSE](LICENSE) для получения подробной информации.
