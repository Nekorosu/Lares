import express from 'express';
import fs from 'fs';
import path from 'path';
import crypto from 'crypto';
import * as archiverModule from 'archiver';
import { createServer as createViteServer } from 'vite';

const archiver = (archiverModule as any).default || archiverModule;

const PORT = 8090;
const DATA_DIR = process.env.DATA_DIR || path.join(process.cwd(), 'data');
const UPLOADS_DIR = path.join(DATA_DIR, 'uploads');
const PARTIALS_DIR = path.join(DATA_DIR, 'partials');
const STATE_FILE = path.join(DATA_DIR, 'lares_state.json');
const CREDENTIALS_FILE = path.join(DATA_DIR, 'admin_credentials.json');

// Ensure storage directories exist
[DATA_DIR, UPLOADS_DIR, PARTIALS_DIR].forEach((dir) => {
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }
});

// --- Admin Credentials & Session Management ---

interface AdminCredentials {
  username: string;
  password_hash: string;
  salt: string;
  created_at: string;
}

interface SessionData {
  id: string;
  role: string;
  username: string;
  created_at: string;
  expires_at: string;
}

const activeSessions = new Map<string, SessionData>();

function hashPassword(password: string, salt?: string): { hash: string; salt: string } {
  const useSalt = salt || crypto.randomBytes(16).toString('hex');
  const hash = crypto.scryptSync(password, useSalt, 64).toString('hex');
  return { hash, salt: useSalt };
}

function verifyPassword(password: string, storedHash: string, salt: string): boolean {
  const { hash } = hashPassword(password, salt);
  return crypto.timingSafeEqual(Buffer.from(hash, 'hex'), Buffer.from(storedHash, 'hex'));
}

function loadAdminCredentials(): AdminCredentials | null {
  try {
    if (fs.existsSync(CREDENTIALS_FILE)) {
      return JSON.parse(fs.readFileSync(CREDENTIALS_FILE, 'utf-8'));
    }
  } catch { /* fallthrough */ }
  return null;
}

function initAdminCredentials(): void {
  if (fs.existsSync(CREDENTIALS_FILE)) return;
  const rawPassword = crypto.randomBytes(12).toString('base64url');
  const { hash, salt } = hashPassword(rawPassword);
  const creds: AdminCredentials = {
    username: 'admin',
    password_hash: hash,
    salt,
    created_at: new Date().toISOString()
  };
  fs.writeFileSync(CREDENTIALS_FILE, JSON.stringify(creds, null, 2), { mode: 0o600 });
  console.log('╔══════════════════════════════════════════════════════════╗');
  console.log('║  ПЕРВЫЙ ЗАПУСК: Создан администратор                    ║');
  console.log(`║  Логин:  admin                                          ║`);
  console.log(`║  Пароль: ${rawPassword.padEnd(46)}║`);
  console.log('║  СОХРАНИТЕ ПАРОЛЬ! Он больше не будет показан.           ║');
  console.log('╚══════════════════════════════════════════════════════════╝');
}

// --- Invite Code Hashing ---

function hashInviteCode(code: string): string {
  return crypto.createHash('sha256').update(code).digest('hex');
}

// --- Filename Sanitization ---

function sanitizeFilename(raw: string): string {
  // Убираем path traversal
  let name = path.basename(raw);
  // Убираем null bytes
  name = name.replace(/\0/g, '');
  // Убираем управляющие символы
  name = name.replace(/[\x00-\x1f\x7f]/g, '');
  // Убираем опасные символы для FS
  name = name.replace(/[<>:"/\\|?*]/g, '_');
  // Ограничиваем длину
  if (name.length > 255) {
    const ext = path.extname(name);
    name = name.slice(0, 255 - ext.length) + ext;
  }
  // Если после санитизации пусто
  if (!name || name === '.' || name === '..') {
    name = 'unnamed_file';
  }
  return name;
}

// --- Rate Limiter ---

class SimpleRateLimiter {
  private attempts = new Map<string, { count: number; resetAt: number }>();

  check(key: string, maxAttempts: number, windowMs: number): { allowed: boolean; retryAfterSec: number } {
    const now = Date.now();
    const entry = this.attempts.get(key);

    if (!entry || now > entry.resetAt) {
      this.attempts.set(key, { count: 1, resetAt: now + windowMs });
      return { allowed: true, retryAfterSec: 0 };
    }

    if (entry.count >= maxAttempts) {
      const retryAfterSec = Math.ceil((entry.resetAt - now) / 1000);
      return { allowed: false, retryAfterSec };
    }

    entry.count++;
    return { allowed: true, retryAfterSec: 0 };
  }

  // Чистка устаревших записей (вызывать периодически)
  cleanup(): void {
    const now = Date.now();
    for (const [key, entry] of this.attempts) {
      if (now > entry.resetAt) this.attempts.delete(key);
    }
  }
}

const loginLimiter = new SimpleRateLimiter();
const inviteLimiter = new SimpleRateLimiter();

// Чистка каждые 5 минут
setInterval(() => {
  loginLimiter.cleanup();
  inviteLimiter.cleanup();
}, 300000);

// --- Safe Inline Types for Downloads ---

const SAFE_INLINE_TYPES = new Set([
  'image/jpeg', 'image/png', 'image/gif', 'image/webp',
  'video/mp4', 'video/webm',
  'audio/mpeg', 'audio/ogg'
]);

const DANGEROUS_EXTENSIONS = new Set([
  '.html', '.htm', '.svg', '.xml', '.xsl', '.xslt',
  '.js', '.mjs', '.css', '.json', '.wasm'
]);

// --- Data Interfaces ---

interface FileRecord {
  id: string;
  original_name: string;
  stored_path: string;
  size: number;
  content_type: string;
  status: 'ready' | 'quarantined';
  flagged: boolean;
  flag_reason?: string;
  expires_at?: string;
  created_at: string;
  uploader_label: string;
  keep_forever?: boolean;
}

interface UploadReservation {
  id: string;
  secret_hash: string;
  original_name: string;
  declared_size: number;
  received_bytes: number;
  expiry_days: number;
  created_at: string;
}

interface InviteCode {
  id: string;
  code_hash: string;
  code_prefix: string;
  enabled: boolean;
  max_activations: number;
  activations_used: number;
  expires_at: string;
  created_at: string;
}

interface AppState {
  files: FileRecord[];
  reservations: Record<string, UploadReservation>;
  invites: InviteCode[];
  stats: {
    total_upload_bytes: number;
    total_download_bytes: number;
  };
}

// Initial state load/save helpers
function loadState(): AppState {
  if (fs.existsSync(STATE_FILE)) {
    try {
      return JSON.parse(fs.readFileSync(STATE_FILE, 'utf-8'));
    } catch {
      // Fallback
    }
  }

  // Seed default demo state
  const demoInviteCode = 'LARE-98A2-4B1C-8812';
  const defaultState: AppState = {
    files: [
      {
        id: 'f101',
        original_name: 'lares_architecture_spec.pdf',
        stored_path: 'lares_architecture_spec.pdf',
        size: 1420000,
        content_type: 'application/pdf',
        status: 'ready',
        flagged: false,
        created_at: new Date(Date.now() - 3600000 * 5).toISOString(),
        expires_at: new Date(Date.now() + 86400000 * 14).toISOString(),
        uploader_label: 'Администратор',
      },
      {
        id: 'f102',
        original_name: 'vacation_july_2024.mp4',
        stored_path: 'vacation_july_2024.mp4',
        size: 1240000000,
        content_type: 'video/mp4',
        status: 'ready',
        flagged: false,
        created_at: new Date(Date.now() - 3600000 * 24).toISOString(),
        expires_at: new Date(Date.now() + 86400000 * 14).toISOString(),
        uploader_label: 'Михаил И.',
      },
      {
        id: 'f103',
        original_name: 'installer_update.exe',
        stored_path: 'installer_update.exe',
        size: 145200000,
        content_type: 'application/octet-stream',
        status: 'quarantined',
        flagged: true,
        flag_reason: 'Подозрительное исполняемое расширение (.exe)',
        created_at: new Date(Date.now() - 3600000 * 2).toISOString(),
        expires_at: new Date(Date.now() + 86400000 * 7).toISOString(),
        uploader_label: 'Гость #2',
      }
    ],
    reservations: {},
    invites: [
      {
        id: 'inv1',
        code_hash: hashInviteCode(demoInviteCode),
        code_prefix: 'LARE',
        enabled: true,
        max_activations: 10,
        activations_used: 1,
        expires_at: new Date(Date.now() + 86400000 * 30).toISOString(),
        created_at: new Date().toISOString(),
      }
    ],
    stats: {
      total_upload_bytes: 1420000000,
      total_download_bytes: 842000000,
    }
  };

  saveState(defaultState);
  return defaultState;
}

function saveState(state: AppState) {
  try {
    fs.writeFileSync(STATE_FILE, JSON.stringify(state, null, 2), 'utf-8');
  } catch (err) {
    console.error('Error saving state:', err);
  }
}

const state = loadState();

function detectContentType(filename: string): string {
  const ext = path.extname(filename).toLowerCase();
  switch (ext) {
    case '.jpg':
    case '.jpeg': return 'image/jpeg';
    case '.png': return 'image/png';
    case '.gif': return 'image/gif';
    case '.webp': return 'image/webp';
    case '.mp4': return 'video/mp4';
    case '.webm': return 'video/webm';
    case '.mp3': return 'audio/mpeg';
    case '.ogg': return 'audio/ogg';
    case '.pdf': return 'application/pdf';
    case '.zip': return 'application/zip';
    default: return 'application/octet-stream';
  }
}

function isSuspiciousExtension(filename: string): boolean {
  const ext = path.extname(filename).toLowerCase();
  return ['.exe', '.sh', '.bat', '.cmd', '.msi', '.vbs', '.ps1', '.scr'].includes(ext);
}

async function startServer() {
  // Initialize admin credentials on first run (before express app)
  initAdminCredentials();

  const app = express();

  app.use(express.json());

  // Security headers middleware
  app.use((req, res, next) => {
    res.setHeader('X-Content-Type-Options', 'nosniff');
    res.setHeader('X-Frame-Options', 'DENY');
    res.setHeader('Referrer-Policy', 'no-referrer');
    res.setHeader('X-XSS-Protection', '0');  // отключаем устаревший XSS-фильтр
    res.setHeader('Permissions-Policy', 'camera=(), microphone=(), geolocation=()');
    res.setHeader('Content-Security-Policy',
      "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
    );
    next();
  });

  // CSRF protection middleware
  app.use((req, res, next) => {
    if (['GET', 'HEAD', 'OPTIONS'].includes(req.method)) {
      return next();
    }
    const origin = req.headers['origin'];
    const host = req.headers['host'];
    // Разрешаем API-вызовы с правильным Content-Type (API clients)
    const contentType = req.headers['content-type'] || '';
    if (contentType.includes('application/json') || contentType.includes('application/octet-stream')) {
      return next();
    }
    // Для form submissions проверяем origin
    if (origin && host && !origin.includes(host)) {
      res.status(403).json({ error: 'CSRF: недопустимый источник запроса' });
      return;
    }
    next();
  });

  // --- Session-based Auth ---

  function getSession(req: express.Request): SessionData | null {
    const authHeader = req.headers['authorization'];
    if (!authHeader || typeof authHeader !== 'string') return null;
    const token = authHeader.startsWith('Bearer ') ? authHeader.slice(7) : authHeader;
    const session = activeSessions.get(token);
    if (!session) return null;
    if (new Date(session.expires_at) < new Date()) {
      activeSessions.delete(token);
      return null;
    }
    return session;
  }

  const isAdmin = (req: express.Request): boolean => {
    const session = getSession(req);
    return session !== null && session.role === 'admin';
  };

  // --- API Routes ---

  // Auth Login Endpoint
  app.post('/api/auth/login', (req, res) => {
    const clientIP = req.ip || req.socket.remoteAddress || 'unknown';
    const { allowed, retryAfterSec } = loginLimiter.check(clientIP, 5, 15 * 60 * 1000);
    if (!allowed) {
      res.setHeader('Retry-After', String(retryAfterSec));
      res.status(429).json({ error: `Слишком много попыток. Повторите через ${retryAfterSec} сек.` });
      return;
    }

    const { password } = req.body;
    if (!password || typeof password !== 'string') {
      res.status(400).json({ error: 'Пароль обязателен' });
      return;
    }
    const creds = loadAdminCredentials();
    if (!creds) {
      res.status(500).json({ error: 'Ошибка конфигурации сервера' });
      return;
    }
    if (!verifyPassword(password, creds.password_hash, creds.salt)) {
      // НЕ сообщай, что именно неверно (логин или пароль)
      res.status(401).json({ error: 'Неверные учётные данные' });
      return;
    }
    const sessionToken = crypto.randomBytes(32).toString('hex');
    const sessionId = crypto.randomBytes(8).toString('hex');
    activeSessions.set(sessionToken, {
      id: sessionId,
      role: 'admin',
      username: creds.username,
      created_at: new Date().toISOString(),
      expires_at: new Date(Date.now() + 12 * 3600000).toISOString()
    });
    res.json({
      role: 'admin',
      username: creds.username,
      token: sessionToken
    });
  });

  // Healthcheck
  app.get('/api/health', (req, res) => {
    res.json({ status: 'ok', service: 'lares', time: new Date().toISOString() });
  });

  // Server Stats & Dashboard Metrics
  app.get('/api/stats', (req, res) => {
    const totalFilesSize = state.files
      .filter((f) => f.status === 'ready')
      .reduce((sum, f) => sum + f.size, 0);

    const totalQuotaBytes = 100 * 1024 * 1024 * 1024; // 100 GB

    res.json({
      storage: {
        used_bytes: totalFilesSize,
        quota_bytes: totalQuotaBytes,
        files_count: state.files.length,
      },
      traffic: {
        month: new Date().toISOString().slice(0, 7),
        upload_bytes: state.stats.total_upload_bytes,
        download_bytes: state.stats.total_download_bytes,
        total_bytes: state.stats.total_upload_bytes + state.stats.total_download_bytes,
      },
      active_sessions: activeSessions.size,
      recent_files: state.files.slice(0, 10),
    });
  });

  // Get File List
  app.get('/api/files', (req, res) => {
    res.json(state.files);
  });

  // Reserve Upload
  app.post('/api/files/upload/reserve', (req, res) => {
    const { filename, declared_size, expiry_days } = req.body;

    if (!filename || !declared_size) {
      res.status(400).json({ error: 'Параметры filename и declared_size обязательны' });
      return;
    }

    const safeName = sanitizeFilename(filename);
    const uploadId = crypto.randomBytes(16).toString('hex');
    const secret = crypto.randomBytes(16).toString('hex');
    const secretHash = crypto.createHash('sha256').update(secret).digest('hex');

    const reservation: UploadReservation = {
      id: uploadId,
      secret_hash: secretHash,
      original_name: safeName,
      declared_size: Number(declared_size),
      received_bytes: 0,
      expiry_days: expiry_days || 14,
      created_at: new Date().toISOString(),
    };

    state.reservations[uploadId] = reservation;
    saveState(state);

    res.json({
      upload_id: uploadId,
      upload_secret: secret,
      reservation_expires_at: new Date(Date.now() + 3600000 * 24).toISOString(),
    });
  });

  // Stream/Append Chunk
  app.post('/api/files/upload/chunk', (req, res) => {
    const uploadId = req.query.upload_id as string;
    const secret = req.query.secret as string;

    const reservation = state.reservations[uploadId];
    const secretHash = secret ? crypto.createHash('sha256').update(secret).digest('hex') : '';
    if (!reservation || reservation.secret_hash !== secretHash) {
      res.status(400).json({ error: 'Неверный или истекший идентификатор загрузки' });
      return;
    }

    const partialPath = path.join(PARTIALS_DIR, `${uploadId}.part`);
    const writeStream = fs.createWriteStream(partialPath, { flags: 'a' });

    let written = 0;
    req.on('data', (chunk) => {
      written += chunk.length;
    });

    req.pipe(writeStream);

    writeStream.on('finish', () => {
      reservation.received_bytes += written;
      saveState(state);

      res.json({
        upload_id: uploadId,
        written_bytes: written,
        current_offset: reservation.received_bytes,
      });
    });

    writeStream.on('error', (err) => {
      res.status(500).json({ error: 'Ошибка записи на диск: ' + err.message });
    });
  });

  // Complete Upload
  app.post('/api/files/upload/complete', (req, res) => {
    const { upload_id, secret } = req.body;
    const reservation = state.reservations[upload_id];

    const secretHash = secret ? crypto.createHash('sha256').update(secret).digest('hex') : '';
    if (!reservation || reservation.secret_hash !== secretHash) {
      res.status(400).json({ error: 'Неверный идентификатор или секрет загрузки' });
      return;
    }

    const fileId = crypto.randomBytes(16).toString('hex');
    const ext = path.extname(reservation.original_name);
    const storedFileName = `${fileId}${ext}`;
    const partialPath = path.join(PARTIALS_DIR, `${upload_id}.part`);
    const finalPath = path.join(UPLOADS_DIR, storedFileName);

    let actualSize = reservation.declared_size;
    if (fs.existsSync(partialPath)) {
      const stats = fs.statSync(partialPath);
      actualSize = stats.size;
      fs.renameSync(partialPath, finalPath);
    } else {
      // Create a small placeholder if created directly via demo UI
      fs.writeFileSync(finalPath, Buffer.from(`Lares File Content for ${reservation.original_name}`));
      actualSize = fs.statSync(finalPath).size;
    }

    // Get uploader label from session, not from client header
    const session = getSession(req);
    const userLabel = session ? session.username : 'Пользователь Web';

    const suspicious = isSuspiciousExtension(reservation.original_name);
    const fileRecord: FileRecord = {
      id: fileId,
      original_name: reservation.original_name,
      stored_path: storedFileName,
      size: actualSize,
      content_type: detectContentType(reservation.original_name),
      status: suspicious ? 'quarantined' : 'ready',
      flagged: suspicious,
      flag_reason: suspicious ? 'Карантин: обнаружено исполняемое расширение файла' : undefined,
      created_at: new Date().toISOString(),
      expires_at: new Date(Date.now() + 86400000 * (reservation.expiry_days || 14)).toISOString(),
      uploader_label: userLabel,
    };

    state.files.unshift(fileRecord);
    state.stats.total_upload_bytes += actualSize;
    delete state.reservations[upload_id];
    saveState(state);

    res.json(fileRecord);
  });

  // Download File
  app.get('/api/files/download/:id', (req, res) => {
    const fileId = req.params.id;
    const fileRecord = state.files.find((f) => f.id === fileId);

    if (!fileRecord) {
      res.status(404).json({ error: 'Файл не найден' });
      return;
    }

    const filePath = path.join(UPLOADS_DIR, fileRecord.stored_path);
    if (!fs.existsSync(filePath)) {
      res.setHeader('Content-Type', 'application/octet-stream');
      res.setHeader('Content-Disposition', `attachment; filename="${encodeURIComponent(fileRecord.original_name)}"`);
      res.setHeader('X-Content-Type-Options', 'nosniff');
      res.send(`Содержимое файла ${fileRecord.original_name}`);
      return;
    }

    state.stats.total_download_bytes += fileRecord.size;
    saveState(state);

    const inline = req.query.inline === 'true';
    const ext = path.extname(fileRecord.original_name).toLowerCase();
    const canInline = inline
      && SAFE_INLINE_TYPES.has(fileRecord.content_type)
      && !DANGEROUS_EXTENSIONS.has(ext);

    res.setHeader('Content-Type', canInline ? fileRecord.content_type : 'application/octet-stream');
    res.setHeader('Content-Disposition',
      `${canInline ? 'inline' : 'attachment'}; filename="${encodeURIComponent(fileRecord.original_name)}"`
    );
    res.setHeader('X-Content-Type-Options', 'nosniff');

    fs.createReadStream(filePath).pipe(res);
  });

  // Download ZIP
  app.post('/api/files/download/zip', (req, res) => {
    const { file_ids } = req.body;
    if (!Array.isArray(file_ids) || file_ids.length === 0) {
      res.status(400).json({ error: 'Не выбраны файлы для архивации' });
      return;
    }

    const selectedFiles = state.files.filter((f) => file_ids.includes(f.id) && f.status === 'ready');

    res.setHeader('Content-Type', 'application/zip');
    res.setHeader('Content-Disposition', `attachment; filename="lares_archive_${Date.now()}.zip"`);

    const archive = archiver('zip', { zlib: { level: 5 } });
    archive.pipe(res);

    for (const f of selectedFiles) {
      const filePath = path.join(UPLOADS_DIR, f.stored_path);
      if (fs.existsSync(filePath)) {
        archive.file(filePath, { name: f.original_name });
      } else {
        archive.append(`Demo content for ${f.original_name}`, { name: f.original_name });
      }
    }

    archive.finalize();
  });

  // Delete File
  app.delete('/api/files/delete/:id', (req, res) => {
    const fileId = req.params.id;
    const index = state.files.findIndex((f) => f.id === fileId);

    if (index === -1) {
      res.status(404).json({ error: 'Файл не найден' });
      return;
    }

    const targetFile = state.files[index];
    // Use session-based auth instead of trusting client headers
    const session = getSession(req);
    const userIsAdmin = isAdmin(req);
    const userLabel = session ? session.username : 'Пользователь Web';

    // Users can only delete their own uploaded files. Admins can delete any file.
    if (!userIsAdmin) {
      if (targetFile.uploader_label === 'Администратор' || (targetFile.uploader_label && targetFile.uploader_label !== userLabel && targetFile.uploader_label !== 'Пользователь Web')) {
        res.status(403).json({ error: 'Пользователям запрещено удалять файлы администратора или других пользователей. Обычный пользователь может удалять только свои файлы.' });
        return;
      }
    }

    const [deleted] = state.files.splice(index, 1);
    const filePath = path.join(UPLOADS_DIR, deleted.stored_path);
    if (fs.existsSync(filePath)) {
      fs.unlinkSync(filePath);
    }

    saveState(state);
    res.json({ message: 'Файл успешно удален', file_id: fileId });
  });

  // Auth: Activate Invite Code
  app.post('/api/auth/invite/activate', (req, res) => {
    const clientIP = req.ip || req.socket.remoteAddress || 'unknown';
    const { allowed, retryAfterSec } = inviteLimiter.check(clientIP, 5, 15 * 60 * 1000);
    if (!allowed) {
      res.setHeader('Retry-After', String(retryAfterSec));
      res.status(429).json({ error: `Слишком много попыток. Повторите через ${retryAfterSec} сек.` });
      return;
    }

    const { code, device_name } = req.body;
    const codeHash = hashInviteCode(code);
    const invite = state.invites.find((i) => i.code_hash === codeHash && i.enabled);

    if (!invite || invite.activations_used >= invite.max_activations) {
      res.status(401).json({ error: 'Недействительный или исчерпанный код инвайта' });
      return;
    }

    invite.activations_used += 1;
    saveState(state);

    res.json({
      token: crypto.randomBytes(32).toString('hex'),
      message: 'Инвайт-код успешно активирован',
      device_name: device_name || 'Основное устройство',
    });
  });

  // Admin Invites API
  app.get('/api/admin/invites', (req, res) => {
    if (!isAdmin(req)) {
      res.status(403).json({ error: 'Доступ запрещен. Управление инвайтами доступно только администратору.' });
      return;
    }
    res.json(state.invites);
  });

  app.post('/api/admin/invites', (req, res) => {
    if (!isAdmin(req)) {
      res.status(403).json({ error: 'Доступ запрещен. Создание инвайтов доступно только администратору.' });
      return;
    }
    const { max_activations, expiry_days } = req.body;
    const rawCode = `LARE-${crypto.randomBytes(2).toString('hex').toUpperCase()}-${crypto.randomBytes(2).toString('hex').toUpperCase()}-${crypto.randomBytes(2).toString('hex').toUpperCase()}`;

    const newInvite: InviteCode = {
      id: crypto.randomBytes(8).toString('hex'),
      code_hash: hashInviteCode(rawCode),
      code_prefix: 'LARE',
      enabled: true,
      max_activations: Number(max_activations) || 5,
      activations_used: 0,
      expires_at: new Date(Date.now() + 86400000 * (Number(expiry_days) || 30)).toISOString(),
      created_at: new Date().toISOString(),
    };

    state.invites.unshift(newInvite);
    saveState(state);

    res.json({
      id: newInvite.id,
      invite_code: rawCode,
      expires_at: newInvite.expires_at,
      message: 'Инвайт-код успешно сгенерирован',
    });
  });

  // Admin Quarantine Approve
  app.post('/api/admin/quarantine/:id/approve', (req, res) => {
    if (!isAdmin(req)) {
      res.status(403).json({ error: 'Доступ запрещен. Одобрение карантина доступно только администратору.' });
      return;
    }
    const fileId = req.params.id;
    const file = state.files.find((f) => f.id === fileId);

    if (!file) {
      res.status(404).json({ error: 'Файл не найден' });
      return;
    }

    file.status = 'ready';
    file.flagged = false;
    saveState(state);

    res.json({ message: 'Файл подтвержден и выведен из карантина', file });
  });

  // Sessions API
  app.get('/api/admin/sessions', (req, res) => {
    if (!isAdmin(req)) {
      res.status(403).json({ error: 'Доступ запрещен. Просмотр сессий доступен только администратору.' });
      return;
    }
    res.json([
      {
        id: 1,
        person_id: 1,
        person_label: 'Администратор',
        device_name: 'Рабочая станция (Primary)',
        client_ip_hash: 'a8f3...71e9',
        created_at: new Date(Date.now() - 86400000 * 3).toISOString(),
        last_seen_at: new Date().toISOString(),
        idle_expires_at: new Date(Date.now() + 86400000 * 14).toISOString(),
        revoked: false
      },
      {
        id: 2,
        person_id: 2,
        person_label: 'Михаил И.',
        device_name: 'MacBook Pro M2',
        client_ip_hash: 'b2c1...90da',
        created_at: new Date(Date.now() - 86400000 * 10).toISOString(),
        last_seen_at: new Date(Date.now() - 3600000 * 2).toISOString(),
        idle_expires_at: new Date(Date.now() + 86400000 * 7).toISOString(),
        revoked: false
      }
    ]);
  });

  app.delete('/api/admin/sessions/:id', (req, res) => {
    if (!isAdmin(req)) {
      res.status(403).json({ error: 'Доступ запрещен. Отзыв сессий доступен только администратору.' });
      return;
    }
    res.json({ message: 'Сессия устройства отозвана', session_id: req.params.id });
  });

  // Direct File Upload Endpoint
  app.post('/api/files/upload/direct', (req, res) => {
    let filename = req.headers['x-file-name'] ? sanitizeFilename(decodeURIComponent(req.headers['x-file-name'] as string)) : 'uploaded_file.dat';
    const fileId = crypto.randomBytes(16).toString('hex');
    const ext = path.extname(filename);
    const storedFileName = `${fileId}${ext || '.bin'}`;
    const finalPath = path.join(UPLOADS_DIR, storedFileName);

    const writeStream = fs.createWriteStream(finalPath);
    req.pipe(writeStream);

    writeStream.on('finish', () => {
      const stats = fs.statSync(finalPath);
      const suspicious = isSuspiciousExtension(filename);
      // Get uploader label from session, not from client header
      const session = getSession(req);
      const userLabel = session ? session.username : 'Пользователь Web';

      const fileRecord: FileRecord = {
        id: fileId,
        original_name: filename,
        stored_path: storedFileName,
        size: stats.size,
        content_type: detectContentType(filename),
        status: suspicious ? 'quarantined' : 'ready',
        flagged: suspicious,
        flag_reason: suspicious ? 'Карантин: обнаружено исполняемое расширение файла' : undefined,
        created_at: new Date().toISOString(),
        expires_at: new Date(Date.now() + 86400000 * 14).toISOString(),
        uploader_label: userLabel,
      };

      state.files.unshift(fileRecord);
      state.stats.total_upload_bytes += stats.size;
      saveState(state);

      res.json(fileRecord);
    });

    writeStream.on('error', (err) => {
      res.status(500).json({ error: 'Ошибка сохранения файла: ' + err.message });
    });
  });

  // robots.txt — before Vite middleware
  app.get('/robots.txt', (req, res) => {
    res.type('text/plain');
    res.send('User-agent: *\nDisallow: /\n');
  });

  // --- Vite Middleware for Dev or Dist Serving for Production ---
  if (process.env.NODE_ENV !== 'production') {
    const vite = await createViteServer({
      server: { middlewareMode: true },
      appType: 'spa',
    });
    app.use(vite.middlewares);
  } else {
    const distPath = path.join(process.cwd(), 'dist');
    app.use(express.static(distPath));
    app.get('*', (req, res) => {
      res.sendFile(path.join(distPath, 'index.html'));
    });
  }

  const server = app.listen(PORT, '127.0.0.1', () => {
    console.log(`Lares listening on http://127.0.0.1:${PORT}`);
  });

  process.on('SIGTERM', () => {
    console.log('SIGTERM received. Shutting down...');
    server.close(() => { process.exit(0); });
  });
  process.on('SIGINT', () => {
    console.log('SIGINT received. Shutting down...');
    server.close(() => { process.exit(0); });
  });
}

startServer();
