import express from 'express';
import fs from 'fs';
import path from 'path';
import crypto from 'crypto';
import * as archiverModule from 'archiver';
import { createServer as createViteServer } from 'vite';

const archiver = (archiverModule as any).default || archiverModule;

const PORT = 3000;
const DATA_DIR = process.env.DATA_DIR || path.join(process.cwd(), 'data');
const UPLOADS_DIR = path.join(DATA_DIR, 'uploads');
const PARTIALS_DIR = path.join(DATA_DIR, 'partials');
const STATE_FILE = path.join(DATA_DIR, 'lares_state.json');

// Ensure storage directories exist
[DATA_DIR, UPLOADS_DIR, PARTIALS_DIR].forEach((dir) => {
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }
});

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
  secret: string;
  original_name: string;
  declared_size: number;
  received_bytes: number;
  expiry_days: number;
  created_at: string;
}

interface InviteCode {
  id: string;
  code: string;
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
        code: 'LARE-98A2-4B1C-8812',
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
  const app = express();

  app.use(express.json());

  // --- API Routes ---

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
      active_sessions: 12,
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

    const uploadId = crypto.randomBytes(16).toString('hex');
    const secret = crypto.randomBytes(16).toString('hex');

    const reservation: UploadReservation = {
      id: uploadId,
      secret,
      original_name: filename,
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
    if (!reservation || reservation.secret !== secret) {
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

    if (!reservation || reservation.secret !== secret) {
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
      uploader_label: 'Пользователь Web',
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
      res.setHeader('Content-Type', fileRecord.content_type || 'application/octet-stream');
      res.setHeader('Content-Disposition', `attachment; filename="${encodeURIComponent(fileRecord.original_name)}"`);
      res.send(`Содержимое файла ${fileRecord.original_name}`);
      return;
    }

    state.stats.total_download_bytes += fileRecord.size;
    saveState(state);

    const inline = req.query.inline === 'true';
    res.setHeader('Content-Type', fileRecord.content_type || 'application/octet-stream');
    res.setHeader(
      'Content-Disposition',
      `${inline ? 'inline' : 'attachment'}; filename="${encodeURIComponent(fileRecord.original_name)}"`
    );

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
    const { code, device_name } = req.body;
    const invite = state.invites.find((i) => i.code === code && i.enabled);

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
    res.json(state.invites);
  });

  app.post('/api/admin/invites', (req, res) => {
    const { max_activations, expiry_days } = req.body;
    const rawCode = `LARE-${crypto.randomBytes(2).toString('hex').toUpperCase()}-${crypto.randomBytes(2).toString('hex').toUpperCase()}-${crypto.randomBytes(2).toString('hex').toUpperCase()}`;

    const newInvite: InviteCode = {
      id: crypto.randomBytes(8).toString('hex'),
      code: rawCode,
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

  app.listen(PORT, '0.0.0.0', () => {
    console.log(`Lares full-stack service running on http://localhost:${PORT}`);
  });
}

startServer();
