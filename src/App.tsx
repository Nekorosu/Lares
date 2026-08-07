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
  X,
  Search,
  Filter,
  PieChart,
  Wifi,
  Laptop,
  Check,
  ShieldAlert,
  SlidersHorizontal,
  ChevronRight,
  Info,
  LogIn
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

interface DeviceSession {
  id: number;
  person_id: number;
  person_label: string;
  device_name: string;
  client_ip_hash: string;
  created_at: string;
  last_seen_at: string;
  idle_expires_at: string;
  revoked: boolean;
}

export default function App() {
  const [activeTab, setActiveTab] = useState<'dashboard' | 'guide' | 'config' | 'architecture'>('dashboard');
  const [copiedSection, setCopiedSection] = useState<string | null>(null);

  // Role & Authentication State
  const [userRole, setUserRole] = useState<'user' | 'admin'>('user');
  const [adminToken, setAdminToken] = useState<string | null>(() => {
    return localStorage.getItem('lares_admin_token') || null;
  });
  const [showLoginModal, setShowLoginModal] = useState<boolean>(false);
  const [loginUsernameInput, setLoginUsernameInput] = useState<string>('admin');
  const [loginPasswordInput, setLoginPasswordInput] = useState<string>('');
  const [loginTotpInput, setLoginTotpInput] = useState<string>('');
  const [loginErrorMsg, setLoginErrorMsg] = useState<string | null>(null);

  // Live Data State
  const [stats, setStats] = useState<ServerStats | null>(null);
  const [files, setFiles] = useState<FileRecord[]>([]);
  const [invites, setInvites] = useState<InviteCode[]>([]);
  const [sessions, setSessions] = useState<DeviceSession[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [selectedFileIds, setSelectedFileIds] = useState<string[]>([]);

  // Filtering & Search State
  const [searchQuery, setSearchQuery] = useState<string>('');
  const [statusFilter, setStatusFilter] = useState<'all' | 'ready' | 'quarantined'>('all');

  // Modals State
  const [showUploadModal, setShowUploadModal] = useState<boolean>(false);
  const [showInviteModal, setShowInviteModal] = useState<boolean>(false);
  const [showStorageModal, setShowStorageModal] = useState<boolean>(false);
  const [showTrafficModal, setShowTrafficModal] = useState<boolean>(false);
  const [showSessionsModal, setShowSessionsModal] = useState<boolean>(false);

  // Upload Progress State
  const [uploadProgress, setUploadProgress] = useState<number | null>(null);
  const [uploadStatusMsg, setUploadStatusMsg] = useState<string>('');
  const [isDragging, setIsDragging] = useState<boolean>(false);

  // Invites & Activation State
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

  // Build role headers
  const getAuthHeaders = (customHeaders?: Record<string, string>) => {
    const headers: Record<string, string> = {
      ...customHeaders,
    };
    if (adminToken) {
      headers['authorization'] = `Bearer ${adminToken}`;
    }
    return headers;
  };

  // Fetch stats, files, invites, and sessions with RBAC headers
  const refreshData = async () => {
    setLoading(true);
    const reqHeaders = getAuthHeaders();
    try {
      const resMe = await fetch('/api/auth/me', { headers: reqHeaders }).catch(() => null);
      if (resMe && resMe.ok) {
        const meData = await resMe.json().catch(() => null);
        if (meData && meData.role) {
          setUserRole(meData.role);
        }
      }

      const [resStats, resFiles, resInvites, resSessions] = await Promise.all([
        fetch('/api/stats', { headers: reqHeaders }).catch(() => null),
        fetch('/api/files', { headers: reqHeaders }).catch(() => null),
        fetch('/api/admin/invites', { headers: reqHeaders }).catch(() => null),
        fetch('/api/admin/sessions', { headers: reqHeaders }).catch(() => null)
      ]);

      if (resStats && resStats.ok) {
        const data = await resStats.json().catch(() => null);
        if (data && typeof data === 'object') setStats(data);
      }
      if (resFiles && resFiles.ok) {
        const fetchedFiles = await resFiles.json().catch(() => null);
        if (Array.isArray(fetchedFiles)) setFiles(fetchedFiles);
      }
      if (resInvites && resInvites.ok) {
        const fetchedInvites = await resInvites.json().catch(() => null);
        if (Array.isArray(fetchedInvites)) setInvites(fetchedInvites);
        else setInvites([]);
      } else {
        setInvites([]);
      }
      if (resSessions && resSessions.ok) {
        const fetchedSessions = await resSessions.json().catch(() => null);
        if (Array.isArray(fetchedSessions)) setSessions(fetchedSessions);
        else setSessions([]);
      } else {
        setSessions([]);
      }
    } catch (err) {
      console.error('Failed to fetch live API data:', err);
    } finally {
      setLoading(false);
    }
  };

  // Admin login handler
  const handleAdminLogin = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setLoginErrorMsg(null);
    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: loginUsernameInput,
          password: loginPasswordInput,
          totp_code: loginTotpInput,
        })
      });
      const data = await res.json();
      if (res.ok) {
        setUserRole('admin');
        setAdminToken(data.token);
        if (data.token) {
          localStorage.setItem('lares_admin_token', data.token);
        }
        setShowLoginModal(false);
        setLoginPasswordInput('');
        setLoginTotpInput('');
        // Re-fetch data with new role
        setTimeout(() => refreshData(), 100);
      } else {
        setLoginErrorMsg(data.error || 'Неверное имя пользователя, пароль или TOTP-код');
      }
    } catch (err) {
      setLoginErrorMsg('Ошибка соединения с сервером');
    }
  };

  // Switch back to standard user role
  const handleLogoutToUser = () => {
    setUserRole('user');
    setAdminToken(null);
    localStorage.removeItem('lares_admin_token');
    if (activeTab === 'config') {
      setActiveTab('dashboard');
    }
    setTimeout(() => refreshData(), 100);
  };

  useEffect(() => {
    refreshData();

    // Global drag and drop event listeners for anywhere on screen
    const handleWindowDragOver = (e: DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (e.dataTransfer?.types?.includes('Files')) {
        setIsDragging(true);
      }
    };

    const handleWindowDragLeave = (e: DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (!e.relatedTarget || (e.relatedTarget as HTMLElement).nodeName === 'HTML') {
        setIsDragging(false);
      }
    };

    const handleWindowDrop = (e: DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      setIsDragging(false);
      if (e.dataTransfer?.files && e.dataTransfer.files.length > 0) {
        const file = e.dataTransfer.files[0];
        setShowUploadModal(true);
        setStatusFilter('all');
        uploadFileProcess(file);
      }
    };

    window.addEventListener('dragover', handleWindowDragOver);
    window.addEventListener('dragleave', handleWindowDragLeave);
    window.addEventListener('drop', handleWindowDrop);

    return () => {
      window.removeEventListener('dragover', handleWindowDragOver);
      window.removeEventListener('dragleave', handleWindowDragLeave);
      window.removeEventListener('drop', handleWindowDrop);
    };
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

  // Upload core processor function with resilient fallback
  const uploadFileProcess = async (file: File) => {
    try {
      setUploadProgress(10);
      setUploadStatusMsg(`Подготовка к загрузке: ${file.name}`);

      let uploadSuccess = false;
      let newRecord: FileRecord | null = null;

      // 1. Try chunked upload flow
      try {
        const resReserve = await fetch('/api/files/upload/reserve', {
          method: 'POST',
          headers: getAuthHeaders({ 'Content-Type': 'application/json' }),
          body: JSON.stringify({
            filename: file.name,
            declared_size: file.size,
            content_type: file.type || 'application/octet-stream',
            expiry_days: 14
          })
        });

        if (resReserve.ok) {
          const reserveData = await resReserve.json();
          const { upload_id, upload_secret } = reserveData;

          setUploadProgress(30);
          setUploadStatusMsg('Передача данных на сервер Lares...');

          const chunkSize = 2 * 1024 * 1024; // 2MB
          let offset = 0;

          while (offset < file.size) {
            const slice = file.slice(offset, offset + chunkSize);
            const resChunk = await fetch(`/api/files/upload/chunk?upload_id=${upload_id}&secret=${upload_secret}&offset=${offset}`, {
              method: 'POST',
              headers: getAuthHeaders(),
              body: slice
            });

            if (!resChunk.ok) {
              throw new Error('Chunk send failed');
            }

            offset += slice.size;
            const pct = Math.min(85, Math.floor((offset / file.size) * 60) + 25);
            setUploadProgress(pct);
          }

          setUploadProgress(90);
          setUploadStatusMsg('Финализация файла и проверка расширения...');

          const resComplete = await fetch('/api/files/upload/complete', {
            method: 'POST',
            headers: getAuthHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify({ upload_id, secret: upload_secret })
          });

          if (resComplete.ok) {
            newRecord = await resComplete.json();
            uploadSuccess = true;
          }
        }
      } catch (chunkErr) {
        console.warn('Chunked upload failed, falling back to direct upload:', chunkErr);
      }

      // 2. Direct upload fallback if chunked failed
      if (!uploadSuccess) {
        setUploadProgress(50);
        setUploadStatusMsg('Прямая передача файла...');

        const formData = new FormData();
        formData.append('file', file);

        const resDirect = await fetch('/api/files/upload/direct', {
          method: 'POST',
          headers: getAuthHeaders({
            'x-file-name': encodeURIComponent(file.name)
          }),
          body: formData
        });

        if (resDirect.ok) {
          newRecord = await resDirect.json();
          uploadSuccess = true;
        } else {
          const errData = await resDirect.json().catch(() => ({}));
          throw new Error(errData.error || 'Ошибка прямой загрузки');
        }
      }

      setUploadProgress(100);
      setUploadStatusMsg('Загрузка успешно завершена!');

      if (newRecord) {
        setFiles(prev => [newRecord!, ...prev.filter(f => f.id !== newRecord!.id)]);
      }

      setTimeout(() => {
        setShowUploadModal(false);
        setUploadProgress(null);
        setUploadStatusMsg('');
        refreshData();
      }, 1000);

    } catch (err: any) {
      alert(`Ошибка загрузки файла: ${err.message}`);
      setUploadProgress(null);
      setUploadStatusMsg('');
    } finally {
      if (fileInputRef.current) {
        fileInputRef.current.value = '';
      }
    }
  };

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      uploadFileProcess(file);
    }
  };

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(true);
  };

  const handleDragLeave = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
    const file = e.dataTransfer.files?.[0];
    if (file) {
      uploadFileProcess(file);
    }
  };

  // Delete file handler
  const handleDeleteFile = async (file: FileRecord | string) => {
    const targetFile = typeof file === 'string' ? (Array.isArray(files) ? files.find(f => f && f.id === file) : null) : file;
    if (!targetFile) return;
    if (userRole !== 'admin' && targetFile.uploader_label === 'Администратор') {
      alert('Ошибка доступа: Обычному пользователю запрещено удалять файлы Администратора! Чтобы удалить этот файл, войдите как Администратор.');
      setShowLoginModal(true);
      return;
    }

    if (!confirm(`Вы уверены, что хотите удалить файл "${targetFile.original_name}"?`)) return;
    try {
      const res = await fetch(`/api/files/delete/${targetFile.id}`, { 
        method: 'DELETE',
        headers: getAuthHeaders()
      });
      if (res.ok) {
        setFiles(prev => (Array.isArray(prev) ? prev.filter(f => f && f.id !== targetFile.id) : []));
        refreshData();
      } else {
        const data = await res.json().catch(() => ({}));
        alert(data.error || 'Ошибка удаления файла');
      }
    } catch (err) {
      alert('Ошибка соединения с сервером');
    }
  };

  // Approve quarantine handler
  const handleApproveQuarantine = async (fileId: string) => {
    if (userRole !== 'admin') {
      alert('Ошибка доступа: Одобрение и вывод файла из карантина доступно только Администратору.');
      setShowLoginModal(true);
      return;
    }

    try {
      const res = await fetch(`/api/admin/quarantine/${fileId}/approve`, { 
        method: 'POST',
        headers: getAuthHeaders()
      });
      if (res.ok) {
        setFiles(prev => prev.map(f => f.id === fileId ? { ...f, status: 'ready', flagged: false, flag_reason: undefined } : f));
        refreshData();
      } else {
        const data = await res.json();
        alert(data.error || 'Не удалось снять карантин с файла');
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
        headers: getAuthHeaders({ 'Content-Type': 'application/json' }),
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
    if (userRole !== 'admin') {
      alert('Ошибка доступа: Создание инвайт-кодов доступно только Администратору.');
      setShowLoginModal(true);
      return;
    }

    try {
      const res = await fetch('/api/admin/invites', {
        method: 'POST',
        headers: getAuthHeaders({ 'Content-Type': 'application/json' }),
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
        headers: getAuthHeaders({ 'Content-Type': 'application/json' }),
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

  // Revoke device session handler
  const handleRevokeSession = async (sessId: number) => {
    if (userRole !== 'admin') {
      alert('Ошибка доступа: Отзыв сессий устройств доступен только Администратору.');
      setShowLoginModal(true);
      return;
    }

    if (!confirm('Отозвать сессию этого устройства?')) return;
    try {
      const res = await fetch(`/api/admin/sessions/${sessId}`, { 
        method: 'DELETE',
        headers: getAuthHeaders()
      });
      if (res.ok) {
        setSessions(prev => prev.map(s => s.id === sessId ? { ...s, revoked: true } : s));
        refreshData();
      } else {
        const data = await res.json();
        alert(data.error || 'Ошибка отзыва сессии');
      }
    } catch (err) {
      alert('Ошибка при отзыве сессии');
    }
  };

  const safeFiles = Array.isArray(files) ? files : [];
  const safeInvites = Array.isArray(invites) ? invites : [];
  const safeSessions = Array.isArray(sessions) ? sessions : [];

  // Filtered files calculation
  const filteredFiles = safeFiles.filter(f => {
    if (!f) return false;
    const name = f.original_name || '';
    const uploader = f.uploader_label || '';
    const q = searchQuery || '';
    const matchesSearch = name.toLowerCase().includes(q.toLowerCase()) ||
                          uploader.toLowerCase().includes(q.toLowerCase());
    const matchesStatus = statusFilter === 'all' || f.status === statusFilter;
    return matchesSearch && matchesStatus;
  });

  const readyFilesCount = safeFiles.filter(f => f && f.status === 'ready').length;
  const quarantinedFilesCount = safeFiles.filter(f => f && f.status === 'quarantined').length;

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
    <div className="min-h-screen bg-[#f5f5f0] text-[#1a1a15] font-sans flex flex-col relative">
      {/* Global Drag & Drop Overlay */}
      {isDragging && (
        <div className="fixed inset-0 bg-[#5A5A40]/80 backdrop-blur-md z-[100] flex flex-col items-center justify-center text-white border-4 border-dashed border-white m-4 rounded-3xl pointer-events-none transition-all">
          <FileUp className="w-16 h-16 animate-bounce mb-4" />
          <h2 className="text-2xl font-bold font-serif">Отпустите файл для загрузки в Lares</h2>
          <p className="text-sm opacity-90 mt-1">Файл будет передан и сохранен в файлообменнике</p>
        </div>
      )}

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

        {/* Tab Navigation & Role Switcher */}
        <div className="flex flex-wrap items-center gap-3 mt-3 sm:mt-0">
          <nav className="flex items-center gap-2 bg-[#f0f0e0] p-1.5 rounded-full">
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

          {/* Role Status Badge & Auth Toggle */}
          <div className="flex items-center gap-2">
            {userRole === 'admin' ? (
              <div className="flex items-center gap-2 bg-amber-50 border border-amber-300 px-3 py-1.5 rounded-full text-xs font-semibold text-amber-900 shadow-xs">
                <ShieldCheck className="w-4 h-4 text-amber-700 shrink-0" />
                <span>Роль: <strong>Администратор</strong></span>
                <button 
                  onClick={handleLogoutToUser}
                  className="ml-1 text-xs text-amber-800 hover:text-amber-950 underline cursor-pointer"
                  title="Выйти из прав администратора"
                >
                  Выйти
                </button>
              </div>
            ) : (
              <div className="flex items-center gap-2 bg-[#f0f0e0] border border-[#e2e2d5] px-3 py-1.5 rounded-full text-xs font-medium text-[#5A5A40]">
                <Users className="w-4 h-4 text-[#8c8c7a] shrink-0" />
                <span>Роль: <strong>Пользователь</strong></span>
                <button 
                  onClick={() => { setLoginErrorMsg(null); setShowLoginModal(true); }}
                  className="ml-1 px-3 py-1 rounded-full bg-[#5A5A40] text-white text-[11px] font-semibold hover:bg-[#484833] transition-colors cursor-pointer flex items-center gap-1.5 shadow-xs"
                >
                  <LogIn className="w-3.5 h-3.5" />
                  Войти
                </button>
              </div>
            )}
          </div>
        </div>
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

            {/* Metrics Grid — FULLY CLICKABLE METRICS CARDS */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
              {/* Card 1: Storage Meter (CLICKABLE) */}
              <div 
                role="button"
                tabIndex={0}
                onClick={() => setShowStorageModal(true)}
                onKeyDown={(e) => e.key === 'Enter' && setShowStorageModal(true)}
                className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm hover:border-[#5A5A40] hover:shadow-md transition-all text-left group cursor-pointer relative overflow-hidden select-none"
              >
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[#8c8c7a] group-hover:text-[#5A5A40] transition-colors">Занято на диске</span>
                  <div className="p-1.5 rounded-full bg-[#f0f0e0] group-hover:bg-[#5A5A40] group-hover:text-white transition-colors">
                    <HardDrive className="w-4 h-4 text-[#5A5A40] group-hover:text-white" />
                  </div>
                </div>
                <div className="text-3xl font-semibold text-[#5A5A40] font-sans">
                  {formatBytes(stats?.storage?.used_bytes || 0)}
                </div>
                <div className="text-xs text-[#8c8c7a] mt-1 flex justify-between items-center">
                  <span>из {formatBytes(stats?.storage?.quota_bytes || 107374182400)} квоты</span>
                  <span className="text-[11px] font-semibold text-[#5A5A40] underline opacity-0 group-hover:opacity-100 transition-opacity">Подробнее &rarr;</span>
                </div>
                <div className="w-full h-2.5 bg-[#e2e2d5] rounded-full overflow-hidden mt-4">
                  <div 
                    className="h-full bg-[#5A5A40] rounded-full transition-all duration-500" 
                    style={{ 
                      width: `${Math.min(100, Math.max(2, (((stats?.storage?.used_bytes || 0) / (stats?.storage?.quota_bytes || 1)) * 100)))}%` 
                    }}
                  ></div>
                </div>
              </div>

              {/* Card 2: Monthly Traffic (CLICKABLE) */}
              <div 
                role="button"
                tabIndex={0}
                onClick={() => setShowTrafficModal(true)}
                onKeyDown={(e) => e.key === 'Enter' && setShowTrafficModal(true)}
                className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm hover:border-[#5A5A40] hover:shadow-md transition-all text-left group cursor-pointer relative overflow-hidden select-none"
              >
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[#8c8c7a] group-hover:text-[#5A5A40] transition-colors">Суммарный Трафик</span>
                  <div className="p-1.5 rounded-full bg-[#f0f0e0] group-hover:bg-[#5A5A40] group-hover:text-white transition-colors">
                    <Activity className="w-4 h-4 text-[#5A5A40] group-hover:text-white" />
                  </div>
                </div>
                <div className="text-3xl font-semibold text-[#5A5A40] font-sans">
                  {formatBytes(stats?.traffic?.total_bytes || 0)}
                </div>
                <div className="text-xs text-[#8c8c7a] mt-1 flex justify-between items-center">
                  <span>Загрузка: {formatBytes(stats?.traffic?.upload_bytes || 0)} | Выгрузка: {formatBytes(stats?.traffic?.download_bytes || 0)}</span>
                  <span className="text-[11px] font-semibold text-[#5A5A40] underline opacity-0 group-hover:opacity-100 transition-opacity">График &rarr;</span>
                </div>
                <div className="w-full h-2.5 bg-[#e2e2d5] rounded-full overflow-hidden mt-4">
                  <div className="h-full bg-[#5A5A40] rounded-full" style={{ width: '42%' }}></div>
                </div>
              </div>

              {/* Card 3: Active Sessions (CLICKABLE) */}
              <div 
                role="button"
                tabIndex={0}
                onClick={() => setShowSessionsModal(true)}
                onKeyDown={(e) => e.key === 'Enter' && setShowSessionsModal(true)}
                className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm hover:border-[#5A5A40] hover:shadow-md transition-all text-left group cursor-pointer relative overflow-hidden select-none"
              >
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[#8c8c7a] group-hover:text-[#5A5A40] transition-colors">Активные Устройства</span>
                  <div className="p-1.5 rounded-full bg-[#f0f0e0] group-hover:bg-[#5A5A40] group-hover:text-white transition-colors">
                    <Users className="w-4 h-4 text-[#5A5A40] group-hover:text-white" />
                  </div>
                </div>
                <div className="text-3xl font-semibold text-[#5A5A40] font-sans">
                  {stats?.active_sessions || safeSessions.filter(s => s && !s.revoked).length || 2} подключения
                </div>
                <div className="text-xs text-[#8c8c7a] mt-1 flex justify-between items-center">
                  <span>Сессии устройств с автопродлением</span>
                  <span className="text-[11px] font-semibold text-[#5A5A40] underline opacity-0 group-hover:opacity-100 transition-opacity">Сессии &rarr;</span>
                </div>
                <div className="flex gap-1.5 mt-4">
                  <span className="w-2.5 h-2.5 rounded-full bg-[#5A5A40]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#5A5A40]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#d4a373]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#e2e2d5]"></span>
                </div>
              </div>
            </div>

            {/* Interactive File Management Section */}
            <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-4">
              <div className="flex flex-wrap justify-between items-center gap-4">
                <div>
                  <h3 className="font-serif text-xl text-[#1a1a15]">Файлы в хранилище ({filteredFiles.length} из {files.length})</h3>
                  <p className="text-xs text-[#8c8c7a]">Безопасное хранение с автоматической изоляцией подозрительных архивов</p>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  {/* Status Filters */}
                  <div className="flex items-center bg-[#f0f0e0] p-1 rounded-full text-xs">
                    <button 
                      onClick={() => setStatusFilter('all')}
                      className={`px-3 py-1 rounded-full font-medium transition-colors ${statusFilter === 'all' ? 'bg-[#5A5A40] text-white shadow-xs' : 'text-[#5A5A40]'}`}
                    >
                      Все ({files.length})
                    </button>
                    <button 
                      onClick={() => setStatusFilter('ready')}
                      className={`px-3 py-1 rounded-full font-medium transition-colors ${statusFilter === 'ready' ? 'bg-[#5A5A40] text-white shadow-xs' : 'text-[#5A5A40]'}`}
                    >
                      Готовые ({readyFilesCount})
                    </button>
                    <button 
                      onClick={() => setStatusFilter('quarantined')}
                      className={`px-3 py-1 rounded-full font-medium transition-colors ${statusFilter === 'quarantined' ? 'bg-[#5A5A40] text-white shadow-xs' : 'text-[#5A5A40]'}`}
                    >
                      Карантин ({quarantinedFilesCount})
                    </button>
                  </div>

                  {/* Search Bar */}
                  <div className="relative">
                    <Search className="w-3.5 h-3.5 text-[#8c8c7a] absolute left-3 top-2.5" />
                    <input 
                      type="text" 
                      placeholder="Поиск по имени..." 
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      className="pl-8 pr-3 py-1.5 rounded-full bg-[#fcfcf9] border border-[#e2e2d5] text-xs focus:outline-none focus:border-[#5A5A40] w-40 sm:w-56"
                    />
                  </div>

                  {selectedFileIds.length > 0 && (
                    <button 
                      onClick={handleDownloadZip}
                      className="px-4 py-1.5 rounded-full bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors flex items-center gap-1.5 cursor-pointer"
                    >
                      <Archive className="w-3.5 h-3.5" />
                      Скачать ZIP ({selectedFileIds.length})
                    </button>
                  )}

                  <button 
                    onClick={() => setShowUploadModal(true)}
                    className="px-4 py-1.5 rounded-full bg-[#f0f0e0] text-[#5A5A40] text-xs font-semibold hover:bg-[#e2e2d5] transition-colors flex items-center gap-1.5 cursor-pointer"
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
                          checked={selectedFileIds.length === filteredFiles.length && filteredFiles.length > 0}
                          onChange={(e) => {
                            if (e.target.checked) setSelectedFileIds(filteredFiles.map(f => f.id));
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
                    {filteredFiles.length === 0 ? (
                      <tr>
                        <td colSpan={6} className="py-8 text-center text-xs text-[#8c8c7a]">
                          {files.length === 0 ? 'Хранилище пока пустое. Нажмите «Загрузить файл» выше.' : 'Файлы по вашему фильтру не найдены.'}
                        </td>
                      </tr>
                    ) : (
                      filteredFiles.map((file) => {
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
                                <span className="font-semibold text-[#1a1a15]">{file.original_name}</span>
                                {file.flag_reason && (
                                  <span className="text-[11px] text-amber-700 mt-0.5 flex items-center gap-1">
                                    <AlertTriangle className="w-3 h-3 text-amber-600 inline" />
                                    {file.flag_reason}
                                  </span>
                                )}
                              </div>
                            </td>
                            <td className="py-3.5 px-4 text-[#8c8c7a] font-mono text-xs">{formatBytes(file.size)}</td>
                            <td className="py-3.5 px-4 text-[#1a1a15] text-xs">{file.uploader_label || 'Гость'}</td>
                            <td className="py-3.5 px-4">
                              {file.status === 'ready' ? (
                                <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-[#5A5A40] text-white">
                                  <Check className="w-3 h-3" />
                                  Готов
                                </span>
                              ) : (
                                <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-[#d4a373] text-white">
                                  <ShieldAlert className="w-3 h-3" />
                                  Карантин
                                </span>
                              )}
                            </td>
                            <td className="py-3.5 px-4 text-right">
                              <div className="flex items-center justify-end gap-2">
                                {file.status === 'quarantined' ? (
                                  <button 
                                    onClick={() => handleApproveQuarantine(file.id)}
                                    className="px-2.5 py-1 rounded-full bg-[#5A5A40] text-white text-xs font-medium hover:bg-[#484833] transition-colors cursor-pointer"
                                  >
                                    Одобрить
                                  </button>
                                ) : (
                                  <a 
                                    href={`/api/files/download/${file.id}`}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="px-3 py-1 rounded-full bg-[#e2e2d5] text-[#1a1a15] text-xs font-medium hover:bg-[#d1d1c1] transition-colors flex items-center gap-1 inline-flex cursor-pointer"
                                  >
                                    <Download className="w-3 h-3" />
                                    Скачать
                                  </a>
                                )}

                                <button 
                                  onClick={() => handleDeleteFile(file)}
                                  className="p-1 rounded bg-rose-50 text-rose-600 hover:bg-rose-100 transition-colors cursor-pointer"
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
                  onClick={() => copyToClipboard(`git clone https://github.com/Nekorosu/Lares.git
cd Lares
go build -o lares main.go`, 'step1')}
                  className="absolute top-3 right-3 px-2.5 py-1 rounded bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1 text-[11px]"
                >
                  {copiedSection === 'step1' ? <CheckCircle2 className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  Копировать
                </button>
                <pre>{`# 1. Клонирование репозитория и переход в директорию
git clone https://github.com/Nekorosu/Lares.git
cd Lares

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
sudo systemctl daemon-reload
sudo systemctl restart lares.service
sudo systemctl status lares.service`, 'step2')}
                  className="absolute top-3 right-3 px-2.5 py-1 rounded bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1 text-[11px]"
                >
                  {copiedSection === 'step2' ? <CheckCircle2 className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  Копировать
                </button>
                <pre>{`sudo systemctl stop lares.service
sudo cp lares /usr/local/bin/lares
sudo cp lares.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl restart lares.service
sudo systemctl status lares.service`}</pre>
              </div>
            </div>

            {/* Step 3: CLI Administration */}
            <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-4">
              <div className="flex items-center gap-3 border-b border-[#f0f0e0] pb-3">
                <div className="w-8 h-8 rounded-full bg-[#5A5A40] text-white font-bold flex items-center justify-center text-sm">
                  3
                </div>
                <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Управление администраторами через CLI</h3>
              </div>
              <p className="text-sm text-[#475569]">
                Используйте CLI подкоманды <code className="bg-[#f0f0e0] px-1.5 py-0.5 rounded text-[#1a1a15]">homeshare admin</code> для управления аккаунтами администраторов, сброса 2FA и снятия блокировок.
              </p>

              <div className="bg-[#1e293b] text-[#f8fafc] p-4 rounded-2xl font-mono text-xs relative overflow-x-auto">
                <button 
                  onClick={() => copyToClipboard(`# 1. Интерактивное создание администратора (с генерацией TOTP QR-кода)
./homeshare admin create

# 2. Удаление администратора из БД
./homeshare admin delete --username newadmin

# 3. Сброс TOTP секрета администратора и генерация нового QR-кода
./homeshare admin reset-totp --username newadmin

# 4. Снятие rate limit блокировок с администратора
./homeshare admin unlock --username newadmin`, 'step3')}
                  className="absolute top-3 right-3 px-2.5 py-1 rounded bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1 text-[11px]"
                >
                  {copiedSection === 'step3' ? <CheckCircle2 className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  Копировать
                </button>
                <pre>{`# 1. Интерактивное создание администратора (с валидацией пароля и TOTP QR)
./homeshare admin create

# 2. Удаление администратора из БД
./homeshare admin delete --username admin_name

# 3. Сброс TOTP секрета и генерация нового QR-кода
./homeshare admin reset-totp --username admin_name

# 4. Снятие rate limit блокировок с администратора
./homeshare admin unlock --username admin_name`}</pre>
              </div>
            </div>
          </div>
        )}

        {/* TAB 3: CONFIG BUILDER */}
        {activeTab === 'config' && (
          userRole !== 'admin' ? (
            <div className="bg-white p-8 md:p-12 rounded-3xl border border-[#e2e2d5] shadow-sm text-center max-w-2xl mx-auto space-y-4 my-6">
              <div className="w-16 h-16 rounded-2xl bg-amber-100 border border-amber-300 text-amber-800 flex items-center justify-center mx-auto shadow-xs">
                <Lock className="w-8 h-8 text-amber-700" />
              </div>
              <h2 className="font-serif text-2xl font-bold text-[#1a1a15]">Доступ к Конфигуратору ограничен</h2>
              <p className="text-sm text-[#8c8c7a]">
                Изменение параметров сервера, настройка портов, лимитов хранилища и генерация файла <code className="bg-[#f0f0e0] px-1 py-0.5 rounded text-xs font-mono text-[#5A5A40]">config.yaml</code> доступны только авторизованным <strong>Администраторам</strong>.
              </p>
              <div className="pt-2 flex justify-center gap-3">
                <button
                  onClick={() => setShowLoginModal(true)}
                  className="px-6 py-2.5 rounded-full bg-[#5A5A40] text-white text-xs font-bold hover:bg-[#484833] transition-colors flex items-center gap-2 shadow-xs cursor-pointer"
                >
                  <LogIn className="w-4 h-4" />
                  Войти в систему
                </button>
                <button
                  onClick={() => setActiveTab('dashboard')}
                  className="px-5 py-2.5 rounded-full bg-[#f0f0e0] text-[#5A5A40] text-xs font-semibold hover:bg-[#e2e2d5] transition-colors cursor-pointer"
                >
                  Вернуться на дашборд
                </button>
              </div>
            </div>
          ) : (
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
          )
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

      {/* --- MODAL 1: STORAGE DETAILS MODAL --- */}
      {showStorageModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-lg w-full p-6 shadow-xl relative border border-[#e2e2d5] space-y-5">
            <button 
              onClick={() => setShowStorageModal(false)}
              className="absolute top-4 right-4 p-1 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
            >
              <X className="w-5 h-5" />
            </button>

            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center">
                <HardDrive className="w-5 h-5" />
              </div>
              <div>
                <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Детализация Диска</h3>
                <p className="text-xs text-[#8c8c7a]">Анализ использования дискового пространства Lares</p>
              </div>
            </div>

            {/* Gauge bar */}
            <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-3">
              <div className="flex justify-between items-center text-xs">
                <span className="font-semibold text-[#5A5A40] uppercase">Занятое пространство</span>
                <span className="font-bold font-mono text-[#1a1a15]">{formatBytes(stats?.storage?.used_bytes || (stats as any)?.storage_used || 0)} / {formatBytes(stats?.storage?.quota_bytes || (stats as any)?.storage_total || 107374182400)}</span>
              </div>

              <div className="w-full h-3 bg-[#e2e2d5] rounded-full overflow-hidden">
                <div 
                  className="h-full bg-[#5A5A40] rounded-full transition-all duration-500"
                  style={{ width: `${Math.min(100, Math.max(3, (((stats?.storage?.used_bytes || (stats as any)?.storage_used || 0) / (stats?.storage?.quota_bytes || (stats as any)?.storage_total || 1)) * 100)))}%` }}
                ></div>
              </div>

              <div className="grid grid-cols-3 gap-2 text-center pt-2">
                <div className="bg-white p-2 rounded-xl border border-[#f0f0e0]">
                  <span className="block text-[10px] text-[#8c8c7a] uppercase">Всего файлов</span>
                  <span className="text-base font-bold text-[#1a1a15]">{files.length}</span>
                </div>
                <div className="bg-white p-2 rounded-xl border border-[#f0f0e0]">
                  <span className="block text-[10px] text-[#8c8c7a] uppercase">Готовых</span>
                  <span className="text-base font-bold text-emerald-700">{readyFilesCount}</span>
                </div>
                <div className="bg-white p-2 rounded-xl border border-[#f0f0e0]">
                  <span className="block text-[10px] text-[#8c8c7a] uppercase">В карантине</span>
                  <span className="text-base font-bold text-amber-700">{quarantinedFilesCount}</span>
                </div>
              </div>
            </div>

            {/* Quick Actions */}
            <div className="flex justify-end gap-2">
              <button 
                onClick={() => {
                  setStatusFilter('quarantined');
                  setShowStorageModal(false);
                }}
                className="px-4 py-2 rounded-xl bg-[#f0f0e0] text-[#5A5A40] text-xs font-semibold hover:bg-[#e2e2d5] transition-colors cursor-pointer"
              >
                Показать файлы в карантине
              </button>
              <button 
                onClick={() => setShowStorageModal(false)}
                className="px-4 py-2 rounded-xl bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors cursor-pointer"
              >
                Закрыть
              </button>
            </div>
          </div>
        </div>
      )}

      {/* --- MODAL 2: TRAFFIC DETAILS MODAL --- */}
      {showTrafficModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-lg w-full p-6 shadow-xl relative border border-[#e2e2d5] space-y-5">
            <button 
              onClick={() => setShowTrafficModal(false)}
              className="absolute top-4 right-4 p-1 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
            >
              <X className="w-5 h-5" />
            </button>

            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center">
                <Activity className="w-5 h-5" />
              </div>
              <div>
                <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Сетевой Трафик и Ограничения</h3>
                <p className="text-xs text-[#8c8c7a]">Учет объемов передачи за текущий месяц ({stats?.traffic?.month || 'Текущий месяц'})</p>
              </div>
            </div>

            {/* Traffic Cards */}
            <div className="grid grid-cols-2 gap-3">
              <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5]">
                <span className="text-[11px] font-semibold text-[#8c8c7a] uppercase block mb-1">Загружено на сервер</span>
                <span className="text-xl font-bold font-mono text-[#5A5A40]">{formatBytes(stats?.traffic?.upload_bytes || 0)}</span>
              </div>
              <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5]">
                <span className="text-[11px] font-semibold text-[#8c8c7a] uppercase block mb-1">Скачано клиентами</span>
                <span className="text-xl font-bold font-mono text-[#5A5A40]">{formatBytes(stats?.traffic?.download_bytes || 0)}</span>
              </div>
            </div>

            {/* Speed Limits Overview */}
            <div className="bg-[#f5f5f0] p-4 rounded-2xl border border-[#e2e2d5] space-y-2 text-xs">
              <span className="font-semibold text-[#1a1a15] block uppercase text-[11px] tracking-wider">Параметры шейпера полосы пропускания:</span>
              <div className="flex justify-between py-1 border-b border-[#e2e2d5]">
                <span className="text-[#8c8c7a]">Лимит внешней загрузки:</span>
                <span className="font-mono font-medium text-[#1a1a15]">250 Mbps</span>
              </div>
              <div className="flex justify-between py-1 border-b border-[#e2e2d5]">
                <span className="text-[#8c8c7a]">Лимит внешней выгрузки:</span>
                <span className="font-mono font-medium text-[#1a1a15]">250 Mbps</span>
              </div>
              <div className="flex justify-between py-1">
                <span className="text-[#8c8c7a]">Буфер всплеска (Burst):</span>
                <span className="font-mono font-medium text-[#1a1a15]">16 MB</span>
              </div>
            </div>

            <div className="flex justify-end">
              <button 
                onClick={() => setShowTrafficModal(false)}
                className="px-4 py-2 rounded-xl bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors cursor-pointer"
              >
                Понятно
              </button>
            </div>
          </div>
        </div>
      )}

      {/* --- MODAL 3: ACTIVE SESSIONS MODAL --- */}
      {showSessionsModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-2xl w-full p-6 shadow-xl relative border border-[#e2e2d5] space-y-5">
            <button 
              onClick={() => setShowSessionsModal(false)}
              className="absolute top-4 right-4 p-1 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
            >
              <X className="w-5 h-5" />
            </button>

            <div className="flex justify-between items-center pr-8">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center">
                  <Users className="w-5 h-5" />
                </div>
                <div>
                  <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Активные Сессии Устройств</h3>
                  <p className="text-xs text-[#8c8c7a]">Управление привязанными клиентами и токенами доступа</p>
                </div>
              </div>

              <button 
                onClick={() => {
                  setShowSessionsModal(false);
                  setShowInviteModal(true);
                }}
                className="px-3 py-1.5 rounded-full bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors flex items-center gap-1 cursor-pointer"
              >
                <Plus className="w-3.5 h-3.5" />
                Новый Инвайт
              </button>
            </div>

            {/* Sessions Table */}
            <div className="overflow-x-auto max-h-80 overflow-y-auto">
              <table className="w-full text-left border-collapse">
                <thead>
                  <tr className="border-b border-[#f0f0e0] text-[11px] font-semibold text-[#8c8c7a] uppercase">
                    <th className="py-2 px-3">Устройство / Польз.</th>
                    <th className="py-2 px-3">IP Hash</th>
                    <th className="py-2 px-3">Активность</th>
                    <th className="py-2 px-3">Статус</th>
                    <th className="py-2 px-3 text-right">Действие</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#f5f5f0] text-xs">
                  {sessions.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="py-6 text-center text-[#8c8c7a]">
                        Нет активных внешних сессий устройств.
                      </td>
                    </tr>
                  ) : (
                    sessions.map((sess) => (
                      <tr key={sess.id} className="hover:bg-[#fcfcf9]">
                        <td className="py-3 px-3">
                          <div className="font-medium text-[#1a1a15]">{sess.device_name}</div>
                          <div className="text-[10px] text-[#8c8c7a]">{sess.person_label}</div>
                        </td>
                        <td className="py-3 px-3 font-mono text-[#8c8c7a]">{sess.client_ip_hash || 'local_hash'}</td>
                        <td className="py-3 px-3 text-[#8c8c7a]">
                          {sess.last_seen_at ? new Date(sess.last_seen_at).toLocaleDateString('ru-RU') : 'Сегодня'}
                        </td>
                        <td className="py-3 px-3">
                          {sess.revoked ? (
                            <span className="px-2 py-0.5 rounded-full bg-rose-100 text-rose-700 font-semibold text-[10px]">
                              Отозвана
                            </span>
                          ) : (
                            <span className="px-2 py-0.5 rounded-full bg-emerald-100 text-emerald-800 font-semibold text-[10px]">
                              Активна
                            </span>
                          )}
                        </td>
                        <td className="py-3 px-3 text-right">
                          {!sess.revoked && (
                            <button 
                              onClick={() => handleRevokeSession(sess.id)}
                              className="px-2.5 py-1 rounded bg-rose-50 text-rose-600 hover:bg-rose-100 text-[11px] font-medium transition-colors cursor-pointer"
                            >
                              Отозвать
                            </button>
                          )}
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            <div className="flex justify-end">
              <button 
                onClick={() => setShowSessionsModal(false)}
                className="px-4 py-2 rounded-xl bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors cursor-pointer"
              >
                Закрыть
              </button>
            </div>
          </div>
        </div>
      )}

      {/* --- MODAL 4: UPLOAD FILE MODAL (WITH DRAG & DROP & PROGRESS) --- */}
      {showUploadModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-md w-full p-6 shadow-xl relative border border-[#e2e2d5]">
            <button 
              onClick={() => setShowUploadModal(false)}
              className="absolute top-4 right-4 p-1 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
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
                onDragOver={handleDragOver}
                onDragLeave={handleDragLeave}
                onDrop={handleDrop}
                className={`border-2 border-dashed ${isDragging ? 'border-[#5A5A40] bg-[#f0f0e0]' : 'border-[#e2e2d5] hover:border-[#5A5A40] bg-[#fcfcf9]'} p-8 rounded-2xl text-center cursor-pointer transition-colors flex flex-col items-center gap-3`}
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

      {/* --- MODAL 5: INVITE MANAGER MODAL --- */}
      {showInviteModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-lg w-full p-6 shadow-xl relative border border-[#e2e2d5] space-y-5">
            <button 
              onClick={() => {
                setShowInviteModal(false);
                setNewInviteResult(null);
                setActivationMsg(null);
              }}
              className="absolute top-4 right-4 p-1 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
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
                  className="px-3.5 py-1.5 rounded-full bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors flex items-center gap-1.5 cursor-pointer"
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
                    className="px-2 py-1 bg-emerald-700 text-white rounded text-[11px] font-sans font-medium cursor-pointer"
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
                  className="px-4 py-2 bg-[#5A5A40] text-white rounded-xl text-xs font-semibold hover:bg-[#484833] transition-colors cursor-pointer"
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

      {/* --- MODAL 6: ADMIN LOGIN MODAL --- */}
      {showLoginModal && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl p-6 md:p-8 max-w-md w-full shadow-2xl border border-[#e2e2d5] relative animate-in fade-in zoom-in duration-200">
            <button 
              onClick={() => setShowLoginModal(false)}
              className="absolute top-4 right-4 p-2 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
            >
              <X className="w-4 h-4" />
            </button>

            <div className="flex items-center gap-3 mb-5">
              <div className="w-11 h-11 rounded-2xl bg-[#f0f0e0] border border-[#e2e2d5] text-[#5A5A40] flex items-center justify-center shrink-0 shadow-xs">
                <LogIn className="w-6 h-6 text-[#5A5A40]" />
              </div>
              <div>
                <h3 className="font-serif text-xl font-bold text-[#1a1a15]">Вход в систему</h3>
                <p className="text-xs text-[#8c8c7a]">Авторизация пользователя или администратора</p>
              </div>
            </div>

            <form onSubmit={handleAdminLogin} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider mb-1.5">
                  Логин / Имя пользователя
                </label>
                <input 
                  type="text"
                  value={loginUsernameInput}
                  onChange={(e) => setLoginUsernameInput(e.target.value)}
                  placeholder="admin"
                  className="w-full px-4 py-2.5 rounded-xl border border-[#e2e2d5] text-sm focus:outline-none focus:border-[#5A5A40] bg-[#fcfcf9]"
                  autoFocus
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider mb-1.5">
                  Пароль
                </label>
                <input 
                  type="password"
                  value={loginPasswordInput}
                  onChange={(e) => setLoginPasswordInput(e.target.value)}
                  placeholder="Введите пароль..."
                  className="w-full px-4 py-2.5 rounded-xl border border-[#e2e2d5] text-sm focus:outline-none focus:border-[#5A5A40] bg-[#fcfcf9]"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider mb-1.5">
                  Код TOTP (2FA, если включен)
                </label>
                <input 
                  type="text"
                  value={loginTotpInput}
                  onChange={(e) => setLoginTotpInput(e.target.value)}
                  placeholder="6-значный код (например, 123456)"
                  className="w-full px-4 py-2.5 rounded-xl border border-[#e2e2d5] text-sm focus:outline-none focus:border-[#5A5A40] bg-[#fcfcf9] font-mono"
                  maxLength={6}
                />
              </div>

              {loginErrorMsg && (
                <div className="p-3 rounded-xl bg-rose-50 border border-rose-200 text-xs text-rose-700 flex items-center gap-2">
                  <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0" />
                  <span>{loginErrorMsg}</span>
                </div>
              )}

              <div className="flex justify-end gap-2 pt-2">
                <button
                  type="button"
                  onClick={() => setShowLoginModal(false)}
                  className="px-4 py-2 rounded-full bg-[#f0f0e0] text-[#5A5A40] text-xs font-semibold hover:bg-[#e2e2d5] transition-colors cursor-pointer"
                >
                  Отмена
                </button>
                <button
                  type="submit"
                  className="px-6 py-2 rounded-full bg-[#5A5A40] text-white text-xs font-bold hover:bg-[#484833] transition-colors shadow-xs cursor-pointer flex items-center gap-1.5"
                >
                  <LogIn className="w-4 h-4" />
                  Войти
                </button>
              </div>
            </form>
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
