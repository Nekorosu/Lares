import React, { useState, useEffect, useRef } from 'react';
import { 
  Terminal, 
  ShieldCheck, 
  HardDrive, 
  CheckCircle2, 
  Copy, 
  ExternalLink, 
  Upload, 
  Download, 
  Key, 
  Users, 
  Archive, 
  Lock, 
  AlertTriangle,
  Settings,
  Activity,
  Trash2,
  Plus,
  RefreshCw,
  FileUp,
  X
} from 'lucide-react';

interface FileRecord {
  id: string;
  original_name: string;
  size: number;
  content_type: string;
  status: 'ready' | 'quarantined';
  flagged: boolean;
  flag_reason?: string;
  expires_at?: string;
  created_at: string;
  uploader_label?: string;
}

interface ServerStats {
  storage: {
    used_bytes: number;
    quota_bytes: number;
    files_count: number;
  };
  traffic: {
    month: string;
    upload_bytes: number;
    download_bytes: number;
    total_bytes: number;
  };
  active_sessions: number;
  recent_files: FileRecord[];
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

export default function App() {
  const [activeTab, setActiveTab] = useState<'dashboard' | 'guide' | 'config' | 'architecture'>('dashboard');
  const [copiedSection, setCopiedSection] = useState<string | null>(null);

  // Live Data State
  const [stats, setStats] = useState<ServerStats | null>(null);
  const [files, setFiles] = useState<FileRecord[]>([]);
  const [invites, setInvites] = useState<InviteCode[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [selectedFileIds, setSelectedFileIds] = useState<string[]>([]);

  // Modals & Forms State
  const [showUploadModal, setShowUploadModal] = useState<boolean>(false);
  const [showInviteModal, setShowInviteModal] = useState<boolean>(false);
  const [uploadProgress, setUploadProgress] = useState<number | null>(null);
  const [uploadStatusMsg, setUploadStatusMsg] = useState<string>('');
  const [newInviteResult, setNewInviteResult] = useState<string | null>(null);
  const [activationCodeInput, setActivationCodeInput] = useState<string>('');
  const [activationMsg, setActivationMsg] = useState<{ text: string; error: boolean } | null>(null);

  // Config builder state
  const [cfgListen, setCfgListen] = useState('127.0.0.1:8090');
  const [cfgBaseUrl, setCfgBaseUrl] = useState('https://files.example.duckdns.org');
  const [cfgDataDir, setCfgDataDir] = useState('/srv/media/fileshare/data');
  const [cfgStorageQuota, setCfgStorageQuota] = useState('100');
  const [cfgUploadLimit, setCfgUploadLimit] = useState('200');
  const [cfgDownloadLimit, setCfgDownloadLimit] = useState('300');

  const fileInputRef = useRef<HTMLInputElement>(null);

  // Fetch stats and file records from API
  const refreshData = async () => {
    setLoading(true);
    try {
      const [resStats, resFiles, resInvites] = await Promise.all([
        fetch('/api/stats'),
        fetch('/api/files'),
        fetch('/api/admin/invites')
      ]);

      if (resStats.ok) setStats(await resStats.json());
      if (resFiles.ok) setFiles(await resFiles.json());
      if (resInvites.ok) setInvites(await resInvites.json());
    } catch (err) {
      console.error('Failed to fetch live API data:', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    refreshData();
  }, []);

  const copyToClipboard = (text: string, sectionId: string) => {
    navigator.clipboard.writeText(text);
    setCopiedSection(sectionId);
    setTimeout(() => setCopiedSection(null), 2000);
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  // Upload handler
  const handleFileSelect = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    try {
      setUploadProgress(10);
      setUploadStatusMsg(`Резервирование загрузки для ${file.name}...`);

      // 1. Reserve upload
      const resReserve = await fetch('/api/files/upload/reserve', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          filename: file.name,
          declared_size: file.size,
          content_type: file.type || 'application/octet-stream',
          expiry_days: 14
        })
      });

      const reserveData = await resReserve.json();
      if (!resReserve.ok) {
        throw new Error(reserveData.error || 'Ошибка резервирования');
      }

      const { upload_id, upload_secret } = reserveData;
      setUploadProgress(40);
      setUploadStatusMsg('Передача файла на сервер Lares...');

      // 2. Stream chunk
      const chunkSize = 2 * 1024 * 1024; // 2MB
      let offset = 0;

      while (offset < file.size) {
        const slice = file.slice(offset, offset + chunkSize);
        const resChunk = await fetch(`/api/files/upload/chunk?upload_id=${upload_id}&secret=${upload_secret}&offset=${offset}`, {
          method: 'POST',
          body: slice
        });

        if (!resChunk.ok) {
          throw new Error('Ошибка при передаче чанка файла');
        }

        offset += slice.size;
        const pct = Math.min(90, Math.floor((offset / file.size) * 80) + 10);
        setUploadProgress(pct);
      }

      setUploadProgress(95);
      setUploadStatusMsg('Завершение загрузки и проверке расширения...');

      // 3. Complete
      const resComplete = await fetch('/api/files/upload/complete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ upload_id, secret: upload_secret })
      });

      if (!resComplete.ok) {
        const compData = await resComplete.json();
        throw new Error(compData.error || 'Ошибка завершения загрузки');
      }

      setUploadProgress(100);
      setUploadStatusMsg('Загрузка успешно завершена!');

      setTimeout(() => {
        setShowUploadModal(false);
        setUploadProgress(null);
        setUploadStatusMsg('');
        refreshData();
      }, 1000);

    } catch (err: any) {
      alert(`Ошибка загрузки: ${err.message}`);
      setUploadProgress(null);
      setUploadStatusMsg('');
    }
  };

  // Delete file handler
  const handleDeleteFile = async (fileId: string) => {
    if (!confirm('Вы уверены, что хотите удалить этот файл?')) return;
    try {
      const res = await fetch(`/api/files/delete/${fileId}`, { method: 'DELETE' });
      if (res.ok) {
        refreshData();
      } else {
        const data = await res.json();
        alert(data.error || 'Ошибка удаления файла');
      }
    } catch (err) {
      alert('Ошибка соединения с сервером');
    }
  };

  // Approve quarantine handler
  const handleApproveQuarantine = async (fileId: string) => {
    try {
      const res = await fetch(`/api/admin/quarantine/${fileId}/approve`, { method: 'POST' });
      if (res.ok) {
        refreshData();
      } else {
        alert('Не удалось снять карантин с файла');
      }
    } catch (err) {
      alert('Ошибка соединения с сервером');
    }
  };

  // Download ZIP handler
  const handleDownloadZip = async () => {
    if (selectedFileIds.length === 0) return;
    try {
      const res = await fetch('/api/files/download/zip', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ file_ids: selectedFileIds })
      });

      if (res.ok) {
        const blob = await res.blob();
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `lares_archive_${Date.now()}.zip`;
        document.body.appendChild(a);
        a.click();
        a.remove();
      } else {
        alert('Ошибка генерации ZIP-архива');
      }
    } catch (err) {
      alert('Ошибка выполнения запроса');
    }
  };

  // Create Invite Code handler
  const handleCreateInvite = async () => {
    try {
      const res = await fetch('/api/admin/invites', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ max_activations: 5, expiry_days: 30 })
      });
      const data = await res.json();
      if (res.ok) {
        setNewInviteResult(data.invite_code);
        refreshData();
      } else {
        alert(data.error || 'Ошибка создания инвайта');
      }
    } catch (err) {
      alert('Ошибка подключения к API');
    }
  };

  // Activate Invite handler
  const handleActivateInvite = async () => {
    if (!activationCodeInput.trim()) return;
    try {
      const res = await fetch('/api/auth/invite/activate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: activationCodeInput.trim(), device_name: 'Рабочий ПК' })
      });
      const data = await res.json();
      if (res.ok) {
        setActivationMsg({ text: 'Инвайт успешно активирован! Сессия создана.', error: false });
        setActivationCodeInput('');
        refreshData();
      } else {
        setActivationMsg({ text: data.error || 'Ошибка активации кодом', error: true });
      }
    } catch (err) {
      setActivationMsg({ text: 'Не удалось связаться с сервером', error: true });
    }
  };

  const generatedYaml = `# Lares Configuration File
listen: "${cfgListen}"
base_url: "${cfgBaseUrl}"

paths:
  data_dir: "${cfgDataDir}"
  tmp_dir: "${cfgDataDir.replace('/data', '/tmp')}"
  db_path: "${cfgDataDir.replace('/data', '/db')}/lares.db"
  backup_dir: "/home/fileshare-backup"
  security_log: "/var/log/lares/security.log"

network:
  local_cidrs:
    - "127.0.0.1/32"
    - "::1/128"
    - "192.168.32.0/24"

limits:
  default_storage_quota_gb: ${cfgStorageQuota}
  default_monthly_upload_limit_gb: ${cfgUploadLimit}
  default_monthly_download_limit_gb: ${cfgDownloadLimit}
  default_max_file_size_gb: 50
  default_expiry_days: 14

speed_limits:
  external_upload_limit_mbps: 250
  external_download_limit_mbps: 250
  burst_mb: 16

zip_limits:
  max_files: 100
  max_total_gb: 50
`;

  return (
    <div className="min-h-screen bg-[#f5f5f0] text-[#1a1a15] font-sans flex flex-col">
      {/* Top Bar / Header */}
      <header className="bg-white border-b border-[#e2e2d5] px-6 py-4 flex flex-wrap justify-between items-center shadow-xs">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-[#5A5A40] text-white flex items-center justify-center font-serif text-xl font-bold shadow-xs">
            L
          </div>
          <div>
            <h1 className="font-serif text-2xl font-semibold tracking-tight text-[#1a1a15]">Lares</h1>
            <p className="text-xs text-[#8c8c7a] font-medium uppercase tracking-wider">Self-Hosted Go File Exchange Server</p>
          </div>
        </div>

        {/* Tab Navigation */}
        <nav className="flex items-center gap-2 mt-3 sm:mt-0 bg-[#f0f0e0] p-1.5 rounded-full">
          <button 
            onClick={() => setActiveTab('dashboard')}
            className={`px-4 py-2 rounded-full text-xs font-semibold transition-all flex items-center gap-2 ${
              activeTab === 'dashboard' ? 'bg-[#5A5A40] text-white shadow-xs' : 'text-[#5A5A40] hover:bg-[#e2e2d5]'
            }`}
          >
            <Activity className="w-3.5 h-3.5" />
            Дашборд & Файлы
          </button>
          <button 
            onClick={() => setActiveTab('guide')}
            className={`px-4 py-2 rounded-full text-xs font-semibold transition-all flex items-center gap-2 ${
              activeTab === 'guide' ? 'bg-[#5A5A40] text-white shadow-xs' : 'text-[#5A5A40] hover:bg-[#e2e2d5]'
            }`}
          >
            <Terminal className="w-3.5 h-3.5" />
            Инструкция запуска
          </button>
          <button 
            onClick={() => setActiveTab('config')}
            className={`px-4 py-2 rounded-full text-xs font-semibold transition-all flex items-center gap-2 ${
              activeTab === 'config' ? 'bg-[#5A5A40] text-white shadow-xs' : 'text-[#5A5A40] hover:bg-[#e2e2d5]'
            }`}
          >
            <Settings className="w-3.5 h-3.5" />
            Конфигуратор
          </button>
          <button 
            onClick={() => setActiveTab('architecture')}
            className={`px-4 py-2 rounded-full text-xs font-semibold transition-all flex items-center gap-2 ${
              activeTab === 'architecture' ? 'bg-[#5A5A40] text-white shadow-xs' : 'text-[#5A5A40] hover:bg-[#e2e2d5]'
            }`}
          >
            <ShieldCheck className="w-3.5 h-3.5" />
            Безопасность
          </button>
        </nav>
      </header>

      {/* Main Container */}
      <main className="flex-1 p-6 md:p-8 max-w-7xl mx-auto w-full">
        {/* TAB 1: DASHBOARD & LIVE FILE EXCHANGE */}
        {activeTab === 'dashboard' && (
          <div className="space-y-6">
            {/* Server Status & Controls Banner */}
            <div className="bg-[#5A5A40] text-white p-6 rounded-3xl shadow-sm flex flex-col md:flex-row justify-between items-start md:items-center gap-4">
              <div className="space-y-1">
                <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-white/10 text-xs font-medium text-[#f5f5f0]">
                  <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
                  Сервер Lares Активен • Full-Stack API
                </div>
                <h2 className="font-serif text-2xl font-medium">Безопасный файлообменник готов к работе</h2>
                <p className="text-sm text-[#e2e2d5]">
                  Загружайте файлы, скачивайте индивидуально или в ZIP-архивах, и генерируйте инвайт-коды доступа.
                </p>
              </div>

              <div className="flex flex-wrap items-center gap-2 shrink-0">
                <button 
                  onClick={() => setShowUploadModal(true)}
                  className="px-5 py-2.5 rounded-full bg-white text-[#5A5A40] text-xs font-bold hover:bg-[#f5f5f0] transition-colors flex items-center gap-2 shadow-xs cursor-pointer"
                >
                  <Upload className="w-4 h-4 text-[#5A5A40]" />
                  Загрузить файл
                </button>
                <button 
                  onClick={() => setShowInviteModal(true)}
                  className="px-4 py-2.5 rounded-full bg-white/15 text-white text-xs font-semibold hover:bg-white/25 transition-colors flex items-center gap-2 cursor-pointer"
                >
                  <Key className="w-3.5 h-3.5" />
                  Инвайты ({invites.length})
                </button>
                <button 
                  onClick={refreshData}
                  className="p-2.5 rounded-full bg-white/15 text-white hover:bg-white/25 transition-colors cursor-pointer"
                  title="Обновить данные"
                >
                  <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                </button>
              </div>
            </div>

            {/* Metrics Grid */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
              {/* Storage Meter */}
              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm">
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[#8c8c7a]">Занято на диске</span>
                  <HardDrive className="w-4 h-4 text-[#5A5A40]" />
                </div>
                <div className="text-3xl font-semibold text-[#5A5A40] font-sans">
                  {formatBytes(stats?.storage.used_bytes || 0)}
                </div>
                <div className="text-xs text-[#8c8c7a] mt-1">
                  из {formatBytes(stats?.storage.quota_bytes || 107374182400)} общей квоты
                </div>
                <div className="w-full h-2.5 bg-[#e2e2d5] rounded-full overflow-hidden mt-4">
                  <div 
                    className="h-full bg-[#5A5A40] rounded-full transition-all duration-500" 
                    style={{ 
                      width: `${Math.min(100, Math.max(2, ((stats?.storage.used_bytes || 0) / (stats?.storage.quota_bytes || 1)) * 100))}%` 
                    }}
                  ></div>
                </div>
              </div>

              {/* Monthly Traffic */}
              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm">
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[#8c8c7a]">Суммарный Трафик</span>
                  <Activity className="w-4 h-4 text-[#5A5A40]" />
                </div>
                <div className="text-3xl font-semibold text-[#5A5A40] font-sans">
                  {formatBytes(stats?.traffic.total_bytes || 0)}
                </div>
                <div className="text-xs text-[#8c8c7a] mt-1">
                  Загрузка: {formatBytes(stats?.traffic.upload_bytes || 0)} | Выгрузка: {formatBytes(stats?.traffic.download_bytes || 0)}
                </div>
                <div className="w-full h-2.5 bg-[#e2e2d5] rounded-full overflow-hidden mt-4">
                  <div className="h-full bg-[#5A5A40] rounded-full" style={{ width: '38%' }}></div>
                </div>
              </div>

              {/* Active Sessions */}
              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm">
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[#8c8c7a]">Активные Устройства</span>
                  <Users className="w-4 h-4 text-[#5A5A40]" />
                </div>
                <div className="text-3xl font-semibold text-[#5A5A40] font-sans">
                  {stats?.active_sessions || 12} подключений
                </div>
                <div className="text-xs text-[#8c8c7a] mt-1">Токенизированные сессии с автопродлением</div>
                <div className="flex gap-1.5 mt-4">
                  <span className="w-2.5 h-2.5 rounded-full bg-[#5A5A40]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#5A5A40]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#5A5A40]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#d4a373]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#e2e2d5]"></span>
                </div>
              </div>
            </div>

            {/* Interactive File Management Section */}
            <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm">
              <div className="flex flex-wrap justify-between items-center gap-4 mb-4">
                <div>
                  <h3 className="font-serif text-xl text-[#1a1a15]">Файлы в хранилище ({files.length})</h3>
                  <p className="text-xs text-[#8c8c7a]">Безопасное хранение с автоматической изоляцией подозрительных архивов</p>
                </div>

                <div className="flex items-center gap-2">
                  {selectedFileIds.length > 0 && (
                    <button 
                      onClick={handleDownloadZip}
                      className="px-4 py-2 rounded-full bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors flex items-center gap-2"
                    >
                      <Archive className="w-3.5 h-3.5" />
                      Скачать ZIP ({selectedFileIds.length})
                    </button>
                  )}
                  <button 
                    onClick={() => setShowUploadModal(true)}
                    className="px-4 py-2 rounded-full bg-[#f0f0e0] text-[#5A5A40] text-xs font-semibold hover:bg-[#e2e2d5] transition-colors flex items-center gap-1.5"
                  >
                    <Plus className="w-3.5 h-3.5" />
                    Добавить файл
                  </button>
                </div>
              </div>

              {/* File Table */}
              <div className="overflow-x-auto">
                <table className="w-full text-left border-collapse">
                  <thead>
                    <tr className="border-b border-[#f0f0e0]">
                      <th className="py-3 px-3 w-8">
                        <input 
                          type="checkbox"
                          checked={selectedFileIds.length === files.length && files.length > 0}
                          onChange={(e) => {
                            if (e.target.checked) setSelectedFileIds(files.map(f => f.id));
                            else setSelectedFileIds([]);
                          }}
                          className="rounded border-[#e2e2d5] text-[#5A5A40] focus:ring-0 cursor-pointer"
                        />
                      </th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Имя файла</th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Размер</th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Загрузил</th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Статус</th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider text-right">Действия</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[#f5f5f0] text-sm">
                    {files.length === 0 ? (
                      <tr>
                        <td colSpan={6} className="py-8 text-center text-xs text-[#8c8c7a]">
                          Хранилище пока пустое. Нажмите «Загрузить файл» выше.
                        </td>
                      </tr>
                    ) : (
                      files.map((file) => {
                        const isSelected = selectedFileIds.includes(file.id);
                        return (
                          <tr key={file.id} className="hover:bg-[#fcfcf9] transition-colors">
                            <td className="py-3.5 px-3">
                              <input 
                                type="checkbox"
                                checked={isSelected}
                                onChange={(e) => {
                                  if (e.target.checked) setSelectedFileIds([...selectedFileIds, file.id]);
                                  else setSelectedFileIds(selectedFileIds.filter(id => id !== file.id));
                                }}
                                className="rounded border-[#e2e2d5] text-[#5A5A40] focus:ring-0 cursor-pointer"
                              />
                            </td>
                            <td className="py-3.5 px-4 font-medium text-[#1a1a15]">
                              <div className="flex flex-col">
                                <span>{file.original_name}</span>
                                {file.flag_reason && (
                                  <span className="text-[11px] text-amber-700 mt-0.5">{file.flag_reason}</span>
                                )}
                              </div>
                            </td>
                            <td className="py-3.5 px-4 text-[#8c8c7a] font-mono text-xs">{formatBytes(file.size)}</td>
                            <td className="py-3.5 px-4 text-[#1a1a15] text-xs">{file.uploader_label || 'Гость'}</td>
                            <td className="py-3.5 px-4">
                              {file.status === 'ready' ? (
                                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold bg-[#5A5A40] text-white">
                                  Готов
                                </span>
                              ) : (
                                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold bg-[#d4a373] text-white">
                                  Карантин
                                </span>
                              )}
                            </td>
                            <td className="py-3.5 px-4 text-right">
                              <div className="flex items-center justify-end gap-2">
                                {file.status === 'quarantined' ? (
                                  <button 
                                    onClick={() => handleApproveQuarantine(file.id)}
                                    className="px-2.5 py-1 rounded-full bg-[#5A5A40] text-white text-xs font-medium hover:bg-[#484833] transition-colors"
                                  >
                                    Одобрить
                                  </button>
                                ) : (
                                  <a 
                                    href={`/api/files/download/${file.id}`}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="px-3 py-1 rounded-full bg-[#e2e2d5] text-[#1a1a15] text-xs font-medium hover:bg-[#d1d1c1] transition-colors flex items-center gap-1 inline-flex"
                                  >
                                    <Download className="w-3 h-3" />
                                    Скачать
                                  </a>
                                )}

                                <button 
                                  onClick={() => handleDeleteFile(file.id)}
                                  className="p-1 rounded bg-rose-50 text-rose-600 hover:bg-rose-100 transition-colors"
                                  title="Удалить файл"
                                >
                                  <Trash2 className="w-3.5 h-3.5" />
                                </button>
                              </div>
                            </td>
                          </tr>
                        );
                      })
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        )}

        {/* TAB 2: SERVER DEPLOYMENT GUIDE */}
        {activeTab === 'guide' && (
          <div className="space-y-8">
            <div>
              <h2 className="font-serif text-3xl text-[#1a1a15]">Инструкция по развертыванию Lares на своем сервере</h2>
              <p className="text-sm text-[#8c8c7a] mt-1">
                Пошаговое руководство по сборке Go-бинарника, настройке SQLite, Caddy и systemd службы на Linux VPS/сервере.
              </p>
            </div>

            {/* Step 1: Pre-requisites */}
            <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-4">
              <div className="flex items-center gap-3 border-b border-[#f0f0e0] pb-3">
                <div className="w-8 h-8 rounded-full bg-[#5A5A40] text-white font-bold flex items-center justify-center text-sm">
                  1
                </div>
                <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Подготовка окружения и запуск Go</h3>
              </div>
              <p className="text-sm text-[#475569]">
                Убедитесь, что на вашем сервере (Debian, Ubuntu или Arch Linux) установлен Go версии 1.22+.
              </p>

              <div className="bg-[#1e293b] text-[#f8fafc] p-4 rounded-2xl font-mono text-xs relative overflow-x-auto">
                <button 
                  onClick={() => copyToClipboard(`git clone https://github.com/your-user/lares.git
cd lares
go build -o lares main.go`, 'step1')}
                  className="absolute top-3 right-3 px-2.5 py-1 rounded bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1 text-[11px]"
                >
                  {copiedSection === 'step1' ? <CheckCircle2 className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  Копировать
                </button>
                <pre>{`# 1. Клонирование репозитория и переход в директорию
git clone https://github.com/your-repo/lares.git
cd lares

# 2. Сборка бинарного файла Lares
go build -o lares main.go

# 3. Создание необходимой директории на сервере
sudo mkdir -p /srv/media/fileshare/{data,tmp,db}
sudo mkdir -p /etc/lares /var/log/lares /home/fileshare-backup
sudo chmod -R 750 /srv/media/fileshare /etc/lares`}</pre>
              </div>
            </div>

            {/* Step 2: Systemd service */}
            <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-4">
              <div className="flex items-center gap-3 border-b border-[#f0f0e0] pb-3">
                <div className="w-8 h-8 rounded-full bg-[#5A5A40] text-white font-bold flex items-center justify-center text-sm">
                  2
                </div>
                <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Автозапуск через Systemd Service</h3>
              </div>

              <div className="bg-[#1e293b] text-[#f8fafc] p-4 rounded-2xl font-mono text-xs relative overflow-x-auto">
                <button 
                  onClick={() => copyToClipboard(`sudo cp lares /usr/local/bin/lares
sudo cp lares.service /etc/systemd/system/
sudo useradd -r -s /bin/false lares
sudo chown -R lares:lares /srv/media/fileshare /etc/lares /var/log/lares
sudo systemctl daemon-reload
sudo systemctl enable --now lares`, 'step2')}
                  className="absolute top-3 right-3 px-2.5 py-1 rounded bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1 text-[11px]"
                >
                  {copiedSection === 'step2' ? <CheckCircle2 className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  Копировать
                </button>
                <pre>{`sudo cp lares /usr/local/bin/lares
sudo cp lares.service /etc/systemd/system/
sudo useradd -r -s /bin/false lares
sudo chown -R lares:lares /srv/media/fileshare /etc/lares /var/log/lares
sudo systemctl daemon-reload
sudo systemctl enable --now lares
sudo systemctl status lares`}</pre>
              </div>
            </div>
          </div>
        )}

        {/* TAB 3: CONFIG BUILDER */}
        {activeTab === 'config' && (
          <div className="space-y-6">
            <div>
              <h2 className="font-serif text-3xl text-[#1a1a15]">Генератор конфигурации config.yaml</h2>
              <p className="text-sm text-[#8c8c7a] mt-1">
                Настройте лимиты, сетевой адрес и локальные пути для последующей вставки на сервер.
              </p>
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
              {/* Controls Form */}
              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-4">
                <h3 className="font-serif text-lg font-semibold text-[#1a1a15] mb-2">Параметры сервиса</h3>

                <div>
                  <label className="block text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider mb-1">Listen IP & Port</label>
                  <input 
                    type="text" 
                    value={cfgListen} 
                    onChange={(e) => setCfgListen(e.target.value)} 
                    className="w-full px-3.5 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-sm text-[#1a1a15] focus:outline-none focus:border-[#5A5A40]"
                  />
                </div>

                <div>
                  <label className="block text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider mb-1">Base URL</label>
                  <input 
                    type="text" 
                    value={cfgBaseUrl} 
                    onChange={(e) => setCfgBaseUrl(e.target.value)} 
                    className="w-full px-3.5 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-sm text-[#1a1a15] focus:outline-none focus:border-[#5A5A40]"
                  />
                </div>

                <div>
                  <label className="block text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider mb-1">Data Storage Path</label>
                  <input 
                    type="text" 
                    value={cfgDataDir} 
                    onChange={(e) => setCfgDataDir(e.target.value)} 
                    className="w-full px-3.5 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-sm text-[#1a1a15] focus:outline-none focus:border-[#5A5A40]"
                  />
                </div>

                <div className="grid grid-cols-3 gap-3">
                  <div>
                    <label className="block text-[11px] font-semibold text-[#8c8c7a] uppercase tracking-wider mb-1">Quota (GB)</label>
                    <input 
                      type="number" 
                      value={cfgStorageQuota} 
                      onChange={(e) => setCfgStorageQuota(e.target.value)} 
                      className="w-full px-3 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-sm text-[#1a1a15] focus:outline-none focus:border-[#5A5A40]"
                    />
                  </div>
                  <div>
                    <label className="block text-[11px] font-semibold text-[#8c8c7a] uppercase tracking-wider mb-1">Upload (GB/M)</label>
                    <input 
                      type="number" 
                      value={cfgUploadLimit} 
                      onChange={(e) => setCfgUploadLimit(e.target.value)} 
                      className="w-full px-3 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-sm text-[#1a1a15] focus:outline-none focus:border-[#5A5A40]"
                    />
                  </div>
                  <div>
                    <label className="block text-[11px] font-semibold text-[#8c8c7a] uppercase tracking-wider mb-1">Download (GB/M)</label>
                    <input 
                      type="number" 
                      value={cfgDownloadLimit} 
                      onChange={(e) => setCfgDownloadLimit(e.target.value)} 
                      className="w-full px-3 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-sm text-[#1a1a15] focus:outline-none focus:border-[#5A5A40]"
                    />
                  </div>
                </div>
              </div>

              {/* YAML Output */}
              <div className="bg-[#1e293b] text-[#f8fafc] p-6 rounded-3xl font-mono text-xs relative flex flex-col">
                <div className="flex justify-between items-center mb-4 pb-2 border-b border-white/10">
                  <span className="text-[#8c8c7a] uppercase tracking-wider font-semibold">Сгенерированный config.yaml</span>
                  <button 
                    onClick={() => copyToClipboard(generatedYaml, 'yaml_config')}
                    className="px-3 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1.5 text-xs font-sans font-medium cursor-pointer"
                  >
                    {copiedSection === 'yaml_config' ? <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                    Скопировать YAML
                  </button>
                </div>
                <pre className="flex-1 overflow-x-auto text-[#e2e2d5] leading-relaxed">{generatedYaml}</pre>
              </div>
            </div>
          </div>
        )}

        {/* TAB 4: ARCHITECTURE & SECURITY */}
        {activeTab === 'architecture' && (
          <div className="space-y-6">
            <div>
              <h2 className="font-serif text-3xl text-[#1a1a15]">Архитектура безопасности Lares</h2>
              <p className="text-sm text-[#8c8c7a] mt-1">
                Ключевые механизмы защиты файлов, сессий пользователей и системных ресурсов.
              </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-3">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center">
                    <AlertTriangle className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="font-serif text-lg font-semibold text-[#1a1a15]">Карантин исполняемых файлов</h3>
                    <p className="text-xs text-[#8c8c7a]">Автоматическая блокировка файлов с расширениями .exe, .sh, .bat</p>
                  </div>
                </div>
                <p className="text-xs text-[#475569] leading-relaxed">
                  При загрузке файлов с опасными расширениями статус файла помечается как <code className="bg-[#f0f0e0] px-1.5 py-0.5 rounded text-[#1a1a15]">quarantined</code>. Файл невидим для обычных пользователей до тех пор, пока администратор не утвердит его.
                </p>
              </div>

              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-3">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center">
                    <Lock className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="font-serif text-lg font-semibold text-[#1a1a15]">TOTP 2FA и Ограничение попыток (Rate Limit)</h3>
                    <p className="text-xs text-[#8c8c7a]">Защита от подбора паролей администратора</p>
                  </div>
                </div>
                <p className="text-xs text-[#475569] leading-relaxed">
                  Вход администратора защищен двухфакторной аутентификацией Google Authenticator / 2FA TOTP. При 5 неверных попытках входа подряд IP заносится в блокировку на 1 час с репортом в security.log.
                </p>
              </div>
            </div>
          </div>
        )}
      </main>

      {/* Upload File Modal */}
      {showUploadModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-md w-full p-6 shadow-xl relative border border-[#e2e2d5]">
            <button 
              onClick={() => setShowUploadModal(false)}
              className="absolute top-4 right-4 p-1 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors"
            >
              <X className="w-5 h-5" />
            </button>

            <h3 className="font-serif text-xl font-semibold text-[#1a1a15] mb-1">Загрузить файл на Lares</h3>
            <p className="text-xs text-[#8c8c7a] mb-5">Файл будет сохранен в защищенном хранилище с автокарантином.</p>

            <input 
              type="file" 
              ref={fileInputRef} 
              onChange={handleFileSelect} 
              className="hidden" 
            />

            {uploadProgress === null ? (
              <div 
                onClick={() => fileInputRef.current?.click()}
                className="border-2 border-dashed border-[#e2e2d5] hover:border-[#5A5A40] bg-[#fcfcf9] p-8 rounded-2xl text-center cursor-pointer transition-colors flex flex-col items-center gap-3"
              >
                <div className="w-12 h-12 rounded-full bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center">
                  <FileUp className="w-6 h-6" />
                </div>
                <div>
                  <span className="text-sm font-medium text-[#1a1a15] block">Перетащите файл сюда или нажмите для выбора</span>
                  <span className="text-xs text-[#8c8c7a] mt-1 block">Максимальный размер: 50 GB</span>
                </div>
              </div>
            ) : (
              <div className="space-y-4 py-4">
                <div className="flex justify-between items-center text-xs font-semibold text-[#5A5A40]">
                  <span>{uploadStatusMsg}</span>
                  <span>{uploadProgress}%</span>
                </div>
                <div className="w-full h-3 bg-[#e2e2d5] rounded-full overflow-hidden">
                  <div 
                    className="h-full bg-[#5A5A40] transition-all duration-300 rounded-full" 
                    style={{ width: `${uploadProgress}%` }}
                  ></div>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Invite Manager Modal */}
      {showInviteModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-lg w-full p-6 shadow-xl relative border border-[#e2e2d5] space-y-5">
            <button 
              onClick={() => {
                setShowInviteModal(false);
                setNewInviteResult(null);
                setActivationMsg(null);
              }}
              className="absolute top-4 right-4 p-1 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors"
            >
              <X className="w-5 h-5" />
            </button>

            <div>
              <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Управление Инвайт-Кодами</h3>
              <p className="text-xs text-[#8c8c7a] mt-0.5">Одноразовые 16-значные ключи привязки новых устройств</p>
            </div>

            {/* Create New Invite */}
            <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-3">
              <div className="flex justify-between items-center">
                <span className="text-xs font-semibold text-[#5A5A40] uppercase tracking-wider">Сгенерировать новый код</span>
                <button 
                  onClick={handleCreateInvite}
                  className="px-3.5 py-1.5 rounded-full bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors flex items-center gap-1.5"
                >
                  <Plus className="w-3.5 h-3.5" />
                  Создать
                </button>
              </div>

              {newInviteResult && (
                <div className="bg-emerald-50 border border-emerald-200 text-emerald-900 p-3 rounded-xl font-mono text-xs flex justify-between items-center">
                  <span>Код: <strong>{newInviteResult}</strong></span>
                  <button 
                    onClick={() => copyToClipboard(newInviteResult, 'new_invite')}
                    className="px-2 py-1 bg-emerald-700 text-white rounded text-[11px] font-sans font-medium"
                  >
                    {copiedSection === 'new_invite' ? 'Скопировано!' : 'Скопировать'}
                  </button>
                </div>
              )}
            </div>

            {/* Test Activation Form */}
            <div className="space-y-2 pt-2 border-t border-[#f0f0e0]">
              <label className="block text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Проверка активации инвайта</label>
              <div className="flex gap-2">
                <input 
                  type="text" 
                  placeholder="XXXX-XXXX-XXXX-XXXX" 
                  value={activationCodeInput}
                  onChange={(e) => setActivationCodeInput(e.target.value)}
                  className="flex-1 px-3.5 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-xs font-mono focus:outline-none focus:border-[#5A5A40]"
                />
                <button 
                  onClick={handleActivateInvite}
                  className="px-4 py-2 bg-[#5A5A40] text-white rounded-xl text-xs font-semibold hover:bg-[#484833] transition-colors"
                >
                  Активировать
                </button>
              </div>

              {activationMsg && (
                <p className={`text-xs mt-1 ${activationMsg.error ? 'text-rose-600' : 'text-emerald-700'}`}>
                  {activationMsg.text}
                </p>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Footer */}
      <footer className="bg-white border-t border-[#e2e2d5] py-4 px-6 text-center text-xs text-[#8c8c7a] flex flex-col sm:flex-row justify-between items-center gap-2">
        <span>Lares Go Server v1.24.0 • GNU GPL v3.0 License</span>
        <span>Абсолютная автономность • Вся документация в README.md</span>
      </footer>
    </div>
  );
}
