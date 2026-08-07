Анализ проекта Lares — Безопасность и Функционал
Обзор проекта
Lares — это self-hosted файлообменник с двумя бэкендами:

Go-сервер (
cmd/homeshare/main.go
) — основной production-бэкенд с SQLite, SSR-шаблонами, chunked upload, TOTP, rate limiting
Node.js/TypeScript сервер (
server.ts
) — dev-сервер / SPA-бэкенд с in-memory state + JSON-файл
React SPA (
src/App.tsx
) — фронтенд для Node.js варианта
🔴 КРИТИЧЕСКИЙ БАГ: Админ не может войти
CAUTION

Это тот баг, который вы описали — вход админа всегда отвергается.

Причина
Фронтенд SPA (
App.tsx:196-221
) отправляет на /api/auth/login только пароль:

typescript

body: JSON.stringify({ password: loginPasswordInput })
Бэкенд 
server.ts:385-423
 также принимает только пароль (без username и TOTP).

Но Go-сервер (
server.go:374-467
) ожидает три поля через HTML-форму: username, password, totp_code, и отправляет данные на другой URL (/admin/login).

Проблема №1: Два сервера используют совершенно разные механизмы аутентификации, которые несовместимы:

Node.js (server.ts)	Go (server.go)
URL	/api/auth/login	/admin/login
Метод	JSON API + Bearer token	HTML form + cookie
Поля	только password	username + password + totp_code
Хранение учётных данных	JSON файл (admin_credentials.json)	SQLite (admin_users таблица)
TOTP	❌ Не поддерживается	✅ Обязательный
Проблема №2: Если вы запускаете Go-сервер, но заходите через React SPA — форма логина обращается к /api/auth/login, а Go-сервер не имеет этого эндпоинта для admin-логина. В main.go (стабовый Go) есть stub-обработчик, который всегда возвращает 501:

go

// Auth Login — Go server is a stub; auth handled by Node.js server
mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
    errorResponse(w, http.StatusNotImplemented, "Auth handled by Node.js server")
})
Решение
Нужно решить, какой бэкенд является основным, и привести фронтенд в соответствие:

Вариант A: Использовать Go-бэкенд (рекомендуется — он полноценнее)

Добавить JSON API endpoint /api/auth/login в Go-сервер, принимающий { username, password, totp_code }
Обновить React SPA для отправки всех трёх полей
Добавить поля username и totp_code в модальное окно логина в App.tsx
Вариант B: Использовать Node.js бэкенд

Добавить TOTP в Node.js бэкенд
Добавить поля username и TOTP в форму фронтенда
🟠 Проблемы безопасности
1. CSRF-токены не валидируются (Go-сервер)
WARNING

CSRF-токены генерируются (
server.go:1560-1562
), но нигде не проверяются.

Функция generateCSRFToken() просто генерирует случайный токен и передаёт в шаблон, но серверная сторона никогда не валидирует полученный csrf_token из form data. Любой POST-запрос будет принят без проверки.

go

func (s *Server) generateCSRFToken() string {
    return auth.GenerateRandomToken(16)  // Создаёт, но не хранит и не проверяет!
}
Что делать: Реализовать stateful CSRF — хранить токен в сессии и сравнивать при POST.

2. Админские действия через GET (Go-сервер)
WARNING

Многие админские действия выполняются через GET-запросы без проверки HTTP-метода.

Пример: удаление людей, инвайтов, сессий — все обработчики принимают любой HTTP метод:

handleAdminPeopleDelete
 — удаление пользователя
handleAdminSessionsRevoke
 — отзыв сессий
handleAdminFilesDelete
 — удаление файлов
handleAdminLocksClearAll
 — очистка всех блокировок
Все эти обработчики не проверяют r.Method == "POST". Это означает, что атака возможна через обычный <img src="/admin/people/delete/1">.

Что делать: Добавить if r.Method != "POST" { return 405 } ко всем мутирующим эндпоинтам.

3. Фронтенд хранит токен и роль в localStorage
IMPORTANT

App.tsx:92-96
: Роль ('admin') и Bearer-токен хранятся в localStorage, что уязвимо для XSS.

typescript

const [userRole, setUserRole] = useState<'user' | 'admin'>(() => {
    return (localStorage.getItem('lares_user_role') as 'user' | 'admin') || 'user';
});
Пользователь может просто установить localStorage.setItem('lares_user_role', 'admin') в DevTools и UI покажет ему «админские» функции. Хотя API-запросы без валидного токена не пройдут, это нарушает принцип наименьшей поверхности атаки.

Что делать: Роль определять только по ответу API /api/auth/me, а не из localStorage.

4. CSRF-защита в Node.js пропускает JSON-запросы
server.ts:343-360
:

typescript

if (contentType.includes('application/json') || contentType.includes('application/octet-stream')) {
    return next();  // CSRF-проверка полностью пропускается!
}
Любой запрос с Content-Type: application/json автоматически проходит CSRF-проверку. Хотя fetch с JSON body обычно не может быть выполнен простой HTML-формой, существуют техники обхода (Flash/Java applets в старых браузерах, DNS rebinding).

5. Клиентские заголовки x-user-role и x-uploader-label
App.tsx:142-152
:

typescript

const getAuthHeaders = () => ({
    'x-user-role': userRole,
    'x-uploader-label': userRole === 'admin' ? 'Администратор' : 'Пользователь Web',
});
Фронтенд отправляет роль в HTTP-заголовке. Если бэкенд доверяет этому заголовку для принятия решений — это критическая дыра. Node.js бэкенд частично защищён (проверяет Bearer token), но Go-сервер вообще не использует эти заголовки (у него cookie-based auth). Однако эти заголовки вообще не нужны — роль должна определяться серверной сессией.

6. TOTP слишком широкое окно: ±5 шагов (±2.5 минуты)
totp.go:41
:

go

for _, offset := range []int64{-5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5} {
Стандартный TOTP допускает окно ±1 (30 секунд в каждую сторону). Здесь проверяется 11 значений (±2.5 минуты), что значительно снижает безопасность — одновременно валидны 11 кодов, что упрощает brute force.

Что делать: Сократить до {-1, 0, 1}.

7. Хэширование с солью через простой SHA-256
tokens.go:17-19
:

go

func HashWithSalt(input, salt string) string {
    h := sha256.Sum256([]byte(input + salt))
    return hex.EncodeToString(h[:])
}
Простая конкатенация input + salt подвержена length extension attacks. Правильнее использовать HMAC:

go

mac := hmac.New(sha256.New, []byte(salt))
mac.Write([]byte(input))
8. Content-Disposition без экранирования
server.go:935
:

go

w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, f.OriginalName))
Если имя файла содержит двойные кавычки или \r\n, это может привести к HTTP header injection. Нужно использовать RFC 5987 encoding (filename*=UTF-8''...).

9. Секреты в config.yaml.example
config.yaml.example:13-15
:

yaml

secrets:
  session_secret: "AUTO_GENERATED_SECRET_KEY_CHANGE_IN_PRODUCTION"
  ip_hash_salt: "AUTO_GENERATED_IP_SALT_CHANGE_IN_PRODUCTION"
Если пользователь скопирует пример без изменения — все инстансы будут использовать одинаковые секреты. Код DefaultConfig() генерирует рандомные секреты, но LoadConfig() мержит YAML поверх — то есть если файл содержит эти строки, они перезатрут рандомные.

🟡 Функциональные проблемы
10. Двойное расширение ложно блокирует файлы
server.go:808-813
:

go

if !flagged && strings.Count(u.OriginalName, ".") > 1 {
    status = models.FileStatusQuarantined
    flagged = true
    flagReason = "Двойное расширение файла"
}
Любой файл с двумя точками в имени будет отправлен в карантин: my.vacation.2024.mp4, report.v2.pdf, archive.tar.gz. Это заблокирует массу легитимных файлов.

Что делать: Проверять двойное расширение только если последнее расширение подозрительное (.pdf.exe, .doc.scr).

11. loginPasswordInput инициализируется строкой 'admin'
App.tsx:99
:

typescript

const [loginPasswordInput, setLoginPasswordInput] = useState<string>('admin');
Поле пароля предзаполнено значением 'admin'. В UI также написано «Пароль по умолчанию для теста: admin». Это — остаточный код разработки, который не должен быть в production.

12. Настройки не сохраняются на диск (Go-сервер)
handleAdminSettingsSave
 обновляет s.cfg в памяти, но не записывает изменения в config.yaml. После перезапуска сервера все изменения пропадут.

13. Одобрение карантина не сбрасывает flagged и flag_reason
server.go:1427-1431
:

go

func (s *Server) handleAdminQuarantineApprove(...) {
    _, _ = s.db.Exec("UPDATE files SET status = 'ready' WHERE id = ?", fileID)
}
Файл освобождается из карантина, но flagged остаётся true и flag_reason не очищается. В UI такой файл может отображаться с предупреждением даже после одобрения.

14. isAdmin() в main.go всегда возвращает false
main.go:119-123
:

go

func isAdmin(r *http.Request) bool {
    // Go server is a stub; real auth is in Node.js server
    return false
}
Stub Go-сервер в main.go содержит эндпоинты инвайтов, карантина и сессий, которые всегда отказывают из-за isAdmin() → false. Это, по сути, дубликат-стаб, который не должен использоваться.

15. ZIP download не учитывает квоту трафика
handleZipDownload
 не проверяет квоту скачивания и не записывает трафик. Пользователь может обойти лимит, скачивая файлы через ZIP.

🔵 Мелкие замечания
#	Файл	Проблема
16	
server.go:980
http.ServeFile(w, r, f.StoredPath) — preview позволяет сторонним скриптам выполняться если content-type угаданный сервером (ServeFile определяет MIME автоматически). Для preview нужно явно задавать Content-Type и добавлять CSP.
17	
server.go:559
rows.Close() вызывается без defer, но ошибки в цикле rows.Next() не закрывают rows. Лучше использовать defer rows.Close().
18	
server.ts:606-607
Download счётчик инкрементируется до отправки файла — если клиент оборвёт соединение, статистика будет неточной.
19	
config.go:90-93
DefaultConfig() использует 100 * 1024 * 1024 * 1024 для int64 литералов — на 32-bit платформах может переполниться. Нужно int64(100) * 1024 * 1024 * 1024.
20	
deploy.sh
Деплой-скрипт не изучен, но рекомендуется проверить, что скомпилированный бинарник lares-server (7 МБ) не коммитится в git.
Предлагаемые изменения
Приоритет 1 — Критический баг логина
[MODIFY] 
App.tsx
Добавить в модальное окно логина поля username и totp_code
Отправлять все три поля в POST-запросе
Убрать дефолтное значение 'admin' из loginPasswordInput
Убрать подсказку «Пароль по умолчанию для теста: admin»
[MODIFY] 
server.go
Добавить JSON API эндпоинт /api/auth/login для SPA-клиента, который:
Принимает { username, password, totp_code }
Проверяет credentials через admin_users таблицу
Возвращает Bearer-token
Использует тот же rate limiting, что и HTML форма
Приоритет 2 — Безопасность
Реализовать CSRF-валидацию (пункт 1)
Добавить проверку HTTP-метода к мутирующим эндпоинтам (пункт 2)
Сузить TOTP-окно до ±1 (пункт 6)
Исправить Content-Disposition экранирование (пункт 8)
Использовать HMAC вместо SHA256 конкатенации (пункт 7)
Приоритет 3 — Функционал
Исправить ложный карантин двойного расширения (пункт 10)
Убрать предзаполнение пароля 'admin' (пункт 11)
Сброс flagged/flag_reason при одобрении карантина (пункт 13)
Проверка квоты при ZIP-загрузке (пункт 15)
Verification Plan
Automated Tests
bash

cd /home/eugh/agent/Lares && go test ./internal/auth/ -v
Manual Verification
После исправлений: запустить Go-сервер, создать admin через CLI, попробовать войти через SPA
Проверить, что TOTP с правильным кодом пропускает
Проверить, что файлы с двойным расширением типа report.v2.pdf не блокируются 
