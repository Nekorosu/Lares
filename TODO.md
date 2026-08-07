Ревью изменений Gemini + Диагностика 3 ошибок
Что было улучшено (хорошие изменения)
Пункт из аудита	Что сделано	Оценка
Критический баг логина	✅ Добавлен JSON API /api/auth/login с username + password + totp_code	Исправлено
Роль из localStorage	✅ Теперь роль определяется через /api/auth/me	Исправлено
Заголовки x-user-role	✅ Убраны из getAuthHeaders()	Исправлено
Пароль 'admin' предзаполнен	✅ loginPasswordInput теперь инициализируется пустой строкой	Исправлено
Rate limiting при неудачном логине	✅ Теперь используется token bucket перед блокировкой	Улучшено
Двойное расширение	✅ Теперь проверяет каждую часть на подозрительность, а не блокирует все файлы с 2+ точками	Исправлено
Bearer-токен через Authorization	✅ getSession() теперь проверяет и cookie, и Bearer header	Исправлено
Unlock после успешного логина	✅ Добавлен rateLimiter.Unlock()	Исправлено
🔴 Ошибка 1: Сессии «умирают» при перезапуске сервера
Симптом
После перезапуска сервиса вы видите сессии как «активные» в UI, но по факту они не работают — нужно логиниться заново.

Причина
Сессии хранятся в SQLite — они переживают перезапуск. Но проблема в том, как работает session_secret:

В 
config.go:86
:

go

Secrets: Secrets{
    SessionSecret: generateRandomSecret(32),  // НОВЫЙ при каждом запуске!
    IPHashSalt:    generateRandomSecret(32),
},
DefaultConfig() генерирует новый случайный session_secret при каждом запуске. Затем LoadConfig() мержит YAML-файл поверх. Если в вашем config.yaml не указан session_secret — при каждом рестарте сервер получает новый ключ. А все старые сессии хранят session_token_hash, вычисленный через старый ключ:

go

tokenHash := auth.HashWithSalt(cookie.Value, s.cfg.Secrets.SessionSecret)
//                              ^^^^^^^^^^^^         ^^^^^^^^^^^^^^^^^^
//                              тот же cookie        НОВЫЙ секрет → ДРУГОЙ хэш!
Результат: сервер не может найти сессию в БД, потому что хэш не совпадает. Сессия остаётся в таблице как revoked = 0, но валидироваться не может.

Как исправить
Вариант А (быстро): Задайте session_secret и ip_hash_salt в config.yaml:

yaml

secrets:
  session_secret: "<сгенерируйте фиксированный 64-символьный hex>"
  ip_hash_salt: "<другой фиксированный 64-символьный hex>"
Вариант Б (правильно): При первом запуске автоматически генерировать и сохранять секреты в config.yaml или в отдельный файл, чтобы они не менялись.

Автоочистка мёртвых сессий
Что касается вашего второго замечания — «если сессия перестала быть активной, нужно автоматически её отключать» — в текущем коде при проверке сессии уже есть 
строка 280-282
:

go

if now.After(sess.IdleExpiresAt) || (...) {
    _, _ = s.db.Exec("UPDATE device_sessions SET revoked = 1 WHERE id = ?", sess.ID)
    return nil, nil, nil
}
Но это отзывает сессию только при следующем запросе с этим токеном. Сессии, которые просто тихо истекли и больше никогда не используются, остаются revoked = 0 навсегда и отображаются как «Активна» в UI.

Нужно добавить в cleanup.go фоновую задачу:

go

_, _ = db.Exec("UPDATE device_sessions SET revoked = 1 WHERE revoked = 0 AND idle_expires_at < ?", time.Now().UTC())
🔴 Ошибка 2: «Failed to save file metadata»
Причина
В 
server.go:2642-2645
 — INSERT для загрузки файла:

go

_, err = s.db.Exec(`
    INSERT INTO files (id, person_id, session_id, uploader_name, original_name, stored_path, size, content_type, status, flagged, flag_reason, created_at, expires_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, fileID, personIDVal, sessID, uploaderName, filename, storedRelPath, size, contentType, status, flagged, flagReason, now, expAt)
Две проблемы:

Колонка session_id не существует в таблице files. Схема в 
db.go:125-142
 не содержит такого поля. SQLite выдаст ошибку table files has no column named session_id.

Отсутствует client_ip_hash — обязательная колонка (NOT NULL). Даже если убрать session_id, INSERT упадёт на NOT NULL constraint failed: files.client_ip_hash.

person_id передаётся как nil для админа (
строка 2582-2588
): если загрузку делает админ, personIDVal остаётся nil, но колонка person_id INTEGER NOT NULL не допускает NULL.

Как исправить
В handleAPIUploadDirect, строки 2642-2645, нужно:

Убрать session_id из INSERT
Добавить client_ip_hash
Вычислить client_ip_hash из clientIP
Для админа задать fallback person_id (0 или создавать системного пользователя)
diff

+clientIP := netutils.GetClientIP(r)
+ipHash := auth.HashWithSalt(clientIP, s.cfg.Secrets.IPHashSalt)
+if personIDVal == nil {
+    personIDVal = int64(0)
+}
 _, err = s.db.Exec(`
-    INSERT INTO files (id, person_id, session_id, uploader_name, original_name, stored_path, size, content_type, status, flagged, flag_reason, created_at, expires_at)
-    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
-`, fileID, personIDVal, sessID, uploaderName, filename, storedRelPath, size, contentType, status, flagged, flagReason, now, expAt)
+    INSERT INTO files (id, person_id, uploader_name, original_name, stored_path, size, content_type, status, flagged, flag_reason, created_at, expires_at, client_ip_hash)
+    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
+`, fileID, personIDVal, uploaderName, filename, storedRelPath, size, contentType, status, flagged, flagReason, now, expAt, ipHash)
🔴 Ошибка 3: «Failed to create invite»
Причина
В 
server.go:2073-2077
, когда нет пользователей, код пытается создать «Standard User»:

go

res, err := s.db.Exec("INSERT INTO people (label, enabled, created_at) VALUES (?, 1, ?)", "Standard User", time.Now().UTC())
Но таблица people (
db.go:57-73
) содержит 6 колонок NOT NULL без DEFAULT:

sql

storage_quota_bytes INTEGER NOT NULL,          -- нет DEFAULT!
monthly_upload_limit_bytes INTEGER NOT NULL,   -- нет DEFAULT!
monthly_download_limit_bytes INTEGER NOT NULL,  -- нет DEFAULT!
max_file_size_bytes INTEGER NOT NULL,           -- нет DEFAULT!
INSERT пропускает эти колонки → NOT NULL constraint failed → инвайт не создаётся.

Затем personID остаётся 0, INSERT в invite_codes с person_id = 0 нарушает FOREIGN KEY constraint → «Failed to create invite».

Как исправить
Добавить все обязательные поля при создании пользователя:

diff

-res, err := s.db.Exec("INSERT INTO people (label, enabled, created_at) VALUES (?, 1, ?)", "Standard User", time.Now().UTC())
+res, err := s.db.Exec(`INSERT INTO people (label, notes, enabled, storage_quota_bytes, monthly_upload_limit_bytes, monthly_download_limit_bytes, max_file_size_bytes, max_concurrent_uploads, created_at)
+VALUES (?, '', 1, ?, ?, ?, ?, 1, ?)`,
+    "Standard User",
+    s.cfg.StorageDefaults.QuotaBytes,
+    s.cfg.StorageDefaults.MonthlyUploadLimit,
+    s.cfg.StorageDefaults.MonthlyDownloadLimit,
+    s.cfg.StorageDefaults.MaxFileSize,
+    time.Now().UTC())
Также есть вторая проблема: хэширование кода. При создании инвайта (API) используется auth.HashString(code), а при активации инвайта (
server.go:363
) используется auth.HashWithSalt(code, s.cfg.Secrets.IPHashSalt). Это разные хэш-функции — активация никогда не найдёт инвайт!

go

// При создании (строка 2069):
codeHash := auth.HashString(code)           // SHA256(code)
// При активации (строка 363):
codeHash := auth.HashWithSalt(code, s.cfg.Secrets.IPHashSalt)  // SHA256(code + salt)
Нужно использовать одну и ту же функцию хэширования. Правильный вариант — HashWithSalt в обоих местах.

Предлагаемые изменения
Приоритет 1 — Исправление 3 ошибок
[MODIFY] 
server.go
Строки 2642-2645: Убрать session_id из INSERT файла, добавить client_ip_hash, убрать NULL для person_id
Строка 2069: Заменить auth.HashString(code) на auth.HashWithSalt(code, s.cfg.Secrets.IPHashSalt)
Строки 2073-2077: Добавить все NOT NULL поля при INSERT people
[MODIFY] 
cleanup.go
Добавить фоновую задачу очистки истекших сессий
Приоритет 2 — Фиксация session_secret
[MODIFY] 
config.go
При первом запуске автоматически сохранять сгенерированные секреты в файл
