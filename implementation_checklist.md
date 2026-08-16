# Homeshare Project Implementation Checklist

This checklist is based on the comprehensive requirements defined in `prompt.txt`. It breaks down the project into logical components and tracks their implementation status.

## 1. Core Architecture & Environment
- [x] Go 1.24+ backend with standard library and permitted dependencies (`modernc.org/sqlite`, `yaml.v3`, `crypto`).
- [x] Single Go binary (frontend built and embedded or served directly).
- [x] No Docker / No Kubernetes.
- [x] Target OS: Debian 13, systemd (`lares.service` implemented).
- [x] SQLite database with WAL mode (`internal/db/db.go`).
- [x] Data directories structure (`/srv/media/fileshare/data`, `tmp`, `db`).
- [x] Caddy setup examples (`Caddyfile.example`).

## 2. Database Schema (`internal/db/db.go`)
- [x] `admin_users` table (id, username, password_hash, totp_secret, etc.)
- [x] `people` table (id, label, quotas, session_limits, etc.)
- [x] `invite_codes` table (id, person_id, code_hash, max_activations, etc.)
- [x] `device_sessions` table (id, person_id, session_token_hash, limits, etc.)
- [x] `uploads` table (id, status, received_bytes, client_ip_hash, etc.)
- [x] `files` table (id, size, status, quarantined, keep_forever, etc.)
- [x] `traffic_counters` table (person_id, month, upload/download stats)
- [x] `audit_logs` table (id, actor, event, entity, details)
- [x] `security_log` file (`/var/log/lares/security.log`)
- [x] `rate_limit_locks` table
- [x] `settings` table

## 3. Access Model & Roles
- [x] Admin Login (password + TOTP).
- [x] Invite-based user access (admin generates invite, user activates).
- [x] Invite code stored as hash, single-use/multi-use limits.
- [x] HttpOnly, SameSite=Lax, Secure cookie sessions.
- [x] Session TTL enforcement (User default: 30d idle, 90d abs | Admin: 12h idle, 7d abs).
- [x] Local vs External traffic classification (`192.168.32.0/24` and `loopback`).

## 4. File Upload (Chunked Upload Protocol)
- [x] Chunk size default 32 MB.
- [x] Upload reservation with `CheckDiskSpaceForNewUpload`.
- [x] Chunk writing with `CheckDiskSpaceCritical`.
- [x] Atomic rename from `.part` to final file.
- [x] Safe generated filenames (Sharded directory structure).
- [x] Size mismatch validation (reject if actual > declared).
- [x] Storage quota checking (strict validation).
- [x] Upload status tracking (reserved -> uploading -> completed/aborted).

## 5. Downloads & ZIP
- [x] Secure download endpoints (Range requests, streaming).
- [x] Content-Disposition and safe inline preview (only safe media).
- [x] Security headers: `X-Content-Type-Options: nosniff`, `CSP`.
- [ ] Multiple file ZIP download (needs detailed validation in UI/API).
- [ ] ZIP traffic quota and speed limits enforcement.

## 6. Quarantine & Suspicious Files
- [x] Check extension against configurable suspicious list.
- [x] Quarantine mode: file marked as `quarantined`, `flagged`.
- [x] Admin approve/delete quarantined files.
- [x] Uploader visibility ("ожидает проверки").

## 7. Quotas, Traffic, & Speed Limits
- [x] Storage quota strict enforcement (ready files + active reservations).
- [x] Monthly traffic counter tracking.
- [ ] Grace rule implementation for traffic quota (needs validation if formula is accurate).
- [ ] External Speed Limiting (Global 250 Mbps down/up, `golang.org/x/time/rate`).
- [ ] Traffic history (12 months retention).

## 8. Rate Limiting & Fail2ban
- [x] Fail2ban config generation (`fail2ban/` dir exists).
- [x] Rate limiting middleware (Admin login, Invite activation, Upload, Download, API).
- [ ] Active Rate Limit Locks visible and resettable in Admin UI.
- [x] Security logging to `/var/log/lares/security.log`.

## 9. Admin CLI & Background Jobs
- [x] CLI `admin create`, `admin delete`, `admin reset-totp`, `admin unlock`.
- [x] Cleanup worker (every 60s) for expired files, orphaned uploads.
- [x] Daily SQLite online backup (`VACUUM INTO`), 14 days rotation.

## 10. User & Admin Interfaces (UI Features)
- [x] UI Language: Russian.
- [x] Admin Dashboard (Disk space, stats).
- [x] People & Invites management.
- [x] Sessions management & revoking.
- [x] Quarantined files view.
- [ ] Audit Log UI view.
- [ ] Traffic counters / History view in UI.
- [ ] Runtime-editable Settings UI.

## 11. UI Architecture & Role Separation
**Анализ текущего состояния:**
На данный момент интерфейс (`src/App.tsx`) реализован в виде единого монолитного компонента (`App`), который использует условный рендеринг (например, `if (userRole === 'admin')`) для скрытия или показа отдельных элементов (модальных окон, кнопок). 
Согласно `prompt.txt`, архитектура должна строго разделять пользовательскую и административную зоны:
- **Пользователь** после входа должен видеть максимально простой интерфейс (зона drag-and-drop, список файлов, квоты).
- **Администратор** имеет доступ к панели управления по отдельному пути (`/admin`), где расположены 10 полноценных разделов (Dashboard, People, Invites, Sessions, Files, Active Uploads, Quarantine, Traffic, Audit Log, Settings).

**Рекомендации по реализации разделения:**
1. **Внедрение маршрутизации:** Использовать React Router или легковесный Hash Router для разделения путей `/` (пользователь) и `/admin` (администратор).
2. **Разделение Layout-компонентов:** 
   - `UserLayout.tsx`: сфокусирован только на загрузке файлов, просмотре хранилища и квот. Никаких админских кнопок.
   - `AdminLayout.tsx`: полноценная панель управления с боковым меню (Sidebar) для навигации по 10 требуемым разделам.
3. **Изоляция компонентов:** Вынести логику модальных окон (Invites, Sessions, Traffic) в отдельные компоненты разделов внутри админской панели, чтобы разгрузить основной файл `App.tsx`.

**Чек-лист разделения интерфейса:**
- [x] Внедрить маршрутизацию (Router) для путей `/` и `/admin`.
- [x] Создать независимый пользовательский вид (`UserView`), убрав все административные элементы.
- [x] Создать структуру административной панели (`AdminView` / `AdminSidebar`).
- [x] Объединить «Пользователи» и «Инвайты» в единую удобную сущность в админке без дублирования функционала.
- [x] Рефакторинг `App.tsx`: вынос функционала в раздельные компоненты для упрощения поддержки.

*Note: This checklist reflects the high-level implementation status based on the project's codebase. Items marked unchecked may require further development or UI integration to strictly meet all `prompt.txt` specifications.*
