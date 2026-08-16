import React, { useState, useRef } from 'react';
import { 
  HardDrive, 
  Upload, 
  Download, 
  FileUp, 
  X, 
  Search, 
  Trash2, 
  CheckCircle2, 
  AlertTriangle,
  Lock,
  Users,
  ShieldCheck,
  Check,
  LogIn,
  ExternalLink,
  Laptop
} from 'lucide-react';

interface FileRecord {
  id: string;
  person_id?: number;
  is_owner?: boolean;
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

interface UserQuota {
  label: string;
  used_bytes: number;
  quota_bytes: number;
  upload_used_bytes: number;
  upload_limit_bytes: number;
  download_used_bytes: number;
  download_limit_bytes: number;
  max_file_size_bytes: number;
  allow_user_keep_forever?: boolean;
}

interface ServerStats {
  storage: {
    used_bytes: number;
    quota_bytes: number;
    files_count: number;
  };
  user_quota?: UserQuota;
  traffic: {
    month: string;
    upload_bytes: number;
    download_bytes: number;
    total_bytes: number;
  };
  active_sessions: number;
}

interface UserViewProps {
  stats: ServerStats | null;
  files: FileRecord[];
  userRole: 'user' | 'admin';
  loading: boolean;
  refreshData: () => void;
  onOpenAdminLogin: () => void;
  onNavigateToAdmin: () => void;
  onLogout?: () => void;
}

export default function UserView({
  stats,
  files,
  userRole,
  loading,
  refreshData,
  onOpenAdminLogin,
  onNavigateToAdmin,
  onLogout,
}: UserViewProps) {
  const [searchQuery, setSearchQuery] = useState<string>('');
  const [showUploadModal, setShowUploadModal] = useState<boolean>(false);
  const [selectedExpiryDays, setSelectedExpiryDays] = useState<number>(14);
  const [keepForever, setKeepForever] = useState<boolean>(false);
  const [selectedFileForUpload, setSelectedFileForUpload] = useState<File | null>(null);
  const [uploadProgress, setUploadProgress] = useState<number | null>(null);
  const [uploadStatusMsg, setUploadStatusMsg] = useState<string>('');
  const [isDragging, setIsDragging] = useState<boolean>(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const formatBytes = (bytes: number): string => {
    if (!bytes || bytes === 0) return '0 Б';
    const k = 1024;
    const sizes = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  const isPreviewableType = (name: string): boolean => {
    const ext = name.split('.').pop()?.toLowerCase() || '';
    return ['jpg', 'jpeg', 'png', 'gif', 'webp', 'mp4', 'webm', 'mp3', 'ogg', 'pdf'].includes(ext);
  };

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files[0]) {
      setSelectedFileForUpload(e.target.files[0]);
    }
  };

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(true);
  };

  const handleDragLeave = () => {
    setIsDragging(false);
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
    if (e.dataTransfer.files && e.dataTransfer.files[0]) {
      setSelectedFileForUpload(e.dataTransfer.files[0]);
    }
  };

  const uploadFileProcess = async (file: File) => {
    setUploadProgress(10);
    setUploadStatusMsg('Резервирование пространства...');

    const adminToken = localStorage.getItem('lares_admin_token');
    const customHeaders: Record<string, string> = {
      'X-Expiry-Days': String(selectedExpiryDays),
      'X-Keep-Forever': String(keepForever),
    };
    if (adminToken) {
      customHeaders['authorization'] = `Bearer ${adminToken}`;
    }

    try {
      setUploadProgress(30);
      setUploadStatusMsg('Передача файла...');

      const formData = new FormData();
      formData.append('file', file);
      formData.append('expiry_days', String(selectedExpiryDays));
      formData.append('keep_forever', String(keepForever));

      const res = await fetch('/api/files/upload/direct', {
        method: 'POST',
        headers: customHeaders,
        body: formData,
      });

      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'Неизвестная ошибка сервера' }));
        throw new Error(data.error || 'Ошибка при загрузке');
      }

      setUploadProgress(100);
      setUploadStatusMsg('Успешно загружено!');

      setTimeout(() => {
        setShowUploadModal(false);
        setUploadProgress(null);
        setUploadStatusMsg('');
        setSelectedFileForUpload(null);
        setKeepForever(false);
        refreshData();
      }, 800);
    } catch (err: any) {
      alert(`Ошибка загрузки файла: ${err.message}`);
      setUploadProgress(null);
      setUploadStatusMsg('');
    }
  };

  const handleDeleteFile = async (fileID: string) => {
    if (!confirm('Вы уверены, что хотите удалить этот файл?')) return;
    try {
      const adminToken = localStorage.getItem('lares_admin_token');
      const headers: Record<string, string> = {};
      if (adminToken) headers['authorization'] = `Bearer ${adminToken}`;

      const res = await fetch(`/api/files/delete/${fileID}`, {
        method: 'POST',
        headers,
      });

      if (res.ok) {
        refreshData();
      } else {
        const data = await res.json().catch(() => ({ error: 'Ошибка удаления' }));
        alert(`Не удалось удалить файл: ${data.error}`);
      }
    } catch (err: any) {
      alert(`Ошибка: ${err.message}`);
    }
  };

  const filteredFiles = files.filter(f => 
    f.status === 'ready' &&
    f.original_name.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const quarantinedFiles = files.filter(f => f.status === 'quarantined');

  const userQuota = stats?.user_quota;
  const userUsedBytes = userQuota?.used_bytes ?? stats?.storage.used_bytes ?? 0;
  const userQuotaBytes = userQuota?.quota_bytes ?? stats?.storage.quota_bytes ?? 107374182400;
  const usedPercent = Math.min(100, Math.round((userUsedBytes / (userQuotaBytes || 1)) * 100));

  const uploadUsed = userQuota?.upload_used_bytes ?? 0;
  const uploadLimit = userQuota?.upload_limit_bytes ?? 214748364800;
  const uploadPercent = Math.min(100, Math.round((uploadUsed / (uploadLimit || 1)) * 100));

  const downloadUsed = userQuota?.download_used_bytes ?? 0;
  const downloadLimit = userQuota?.download_limit_bytes ?? 322122547200;
  const downloadPercent = Math.min(100, Math.round((downloadUsed / (downloadLimit || 1)) * 100));

  const maxFileSize = userQuota?.max_file_size_bytes ?? 53687091200;

  return (
    <div className="min-h-screen bg-[#f7f7f2] text-[#1a1a15] flex flex-col font-sans">
      {/* User Header */}
      <header className="bg-white border-b border-[#e2e2d5] sticky top-0 z-30 px-3 md:px-8 py-3.5 flex flex-wrap justify-between items-center gap-2 shadow-xs">
        <div className="flex flex-wrap items-center gap-2 sm:gap-3 w-full sm:w-auto">
          <div className="w-10 h-10 rounded-2xl bg-[#5A5A40] text-white flex items-center justify-center font-serif text-xl font-bold shadow-sm">
            L
          </div>
          <div>
            <h1 className="font-serif text-lg font-bold tracking-tight text-[#1a1a15]">Lares Homeshare</h1>
            <p className="text-[11px] text-[#8c8c7a]">Защищенное семейное хранилище файлов</p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2 sm:gap-3">
          {/* Storage summary pill */}
          <div className="hidden sm:flex items-center gap-2.5 px-3 py-1.5 rounded-full bg-[#f0f0e0] border border-[#e2e2d5] text-xs font-medium text-[#5A5A40]">
            <HardDrive className="w-4 h-4 text-[#5A5A40]" />
            <span>{formatBytes(userUsedBytes)} / {formatBytes(userQuotaBytes)}</span>
            <div className="w-12 h-2 bg-[#e2e2d5] rounded-full overflow-hidden">
              <div className="h-full bg-[#5A5A40] rounded-full" style={{ width: `${usedPercent}%` }}></div>
            </div>
          </div>

          {userRole === 'admin' ? (
            <button
              onClick={onNavigateToAdmin}
              className="px-4 py-2 rounded-full bg-[#5A5A40] text-white text-xs font-bold hover:bg-[#484833] transition-colors shadow-xs flex items-center gap-1.5 cursor-pointer"
            >
              <ShieldCheck className="w-4 h-4" />
              Панель администратора
            </button>
          ) : (
            <button
              onClick={onOpenAdminLogin}
              className="px-4 py-2 rounded-full bg-[#f0f0e0] text-[#5A5A40] text-xs font-bold hover:bg-[#e2e2d5] transition-colors flex items-center gap-1.5 cursor-pointer"
            >
              <LogIn className="w-4 h-4" />
              Вход для админа
            </button>
          )}

          {onLogout && (
            <button
              onClick={onLogout}
              className="px-3.5 py-2 rounded-full bg-rose-50 text-rose-700 border border-rose-200 text-xs font-semibold hover:bg-rose-100 transition-colors cursor-pointer"
            >
              Выйти
            </button>
          )}
        </div>
      </header>

      {/* Main Content */}
      <main className="flex-1 max-w-6xl w-full mx-auto p-4 md:p-8 space-y-6">
        {/* Top Banner & Quick Upload Action */}
        <div className="bg-white rounded-3xl p-6 md:p-8 border border-[#e2e2d5] shadow-xs flex flex-col md:flex-row justify-between items-start md:items-center gap-6">
          <div className="space-y-2 max-w-xl">
            <span className="px-3 py-1 rounded-full bg-[#5A5A40]/10 text-[#5A5A40] font-semibold text-xs inline-block">
              Общий доступ • Безопасность
            </span>
            <h2 className="font-serif text-2xl md:text-3xl font-bold text-[#1a1a15]">
              Хранилище файлов сети
            </h2>
            <p className="text-xs md:text-sm text-[#8c8c7a] leading-relaxed">
              Загружайте файлы для общего доступа. Загруженные файлы автоматически имеют срок хранения и удаляются сервером по истечении таймера.
            </p>
          </div>

          <button
            onClick={() => setShowUploadModal(true)}
            className="w-full md:w-auto px-6 py-3.5 rounded-2xl bg-[#5A5A40] text-white font-bold text-sm hover:bg-[#484833] transition-all shadow-md flex items-center justify-center gap-2 cursor-pointer shrink-0"
          >
            <Upload className="w-5 h-5" />
            Загрузить файл
          </button>
        </div>

        {/* User Quota & Limits Dashboard Widget */}
        <div className="bg-white rounded-3xl p-6 border border-[#e2e2d5] shadow-xs space-y-4">
          <div className="flex justify-between items-center border-b border-[#f0f0e0] pb-3">
            <div>
              <h3 className="font-serif text-lg font-bold text-[#1a1a15]">Мои лимиты и использование</h3>
              <p className="text-xs text-[#8c8c7a]">Персональная квота хранилища и расхода месячного трафика</p>
            </div>
            {userQuota?.label && (
              <span className="px-3 py-1 rounded-full bg-[#5A5A40]/10 text-[#5A5A40] text-xs font-semibold">
                Профиль: {userQuota.label}
              </span>
            )}
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 text-xs">
            {/* Card 1: Storage Quota */}
            <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-2">
              <span className="text-[#5A5A40] font-semibold uppercase tracking-wider block text-[10px]">Дисковая квота</span>
              <div className="text-lg font-bold font-mono text-[#1a1a15]">
                {formatBytes(userUsedBytes)} <span className="text-[#8c8c7a] font-normal text-xs">/ {formatBytes(userQuotaBytes)}</span>
              </div>
              <div className="w-full h-2 bg-[#e2e2d5] rounded-full overflow-hidden">
                <div className={`h-full rounded-full ${usedPercent > 90 ? 'bg-rose-600' : 'bg-[#5A5A40]'}`} style={{ width: `${usedPercent}%` }}></div>
              </div>
              <span className="text-[10px] text-[#8c8c7a] block">Занято {usedPercent}% от выделенного места</span>
            </div>

            {/* Card 2: Upload Traffic */}
            <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-2">
              <span className="text-[#5A5A40] font-semibold uppercase tracking-wider block text-[10px]">Трафик загрузки за месяц</span>
              <div className="text-lg font-bold font-mono text-[#1a1a15]">
                {formatBytes(uploadUsed)} <span className="text-[#8c8c7a] font-normal text-xs">/ {formatBytes(uploadLimit)}</span>
              </div>
              <div className="w-full h-2 bg-[#e2e2d5] rounded-full overflow-hidden">
                <div className={`h-full rounded-full ${uploadPercent > 90 ? 'bg-amber-600' : 'bg-[#5A5A40]'}`} style={{ width: `${uploadPercent}%` }}></div>
              </div>
              <span className="text-[10px] text-[#8c8c7a] block">Израсходовано {uploadPercent}% в этом месяце</span>
            </div>

            {/* Card 3: Download Traffic */}
            <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-2">
              <span className="text-[#5A5A40] font-semibold uppercase tracking-wider block text-[10px]">Трафик скачивания за месяц</span>
              <div className="text-lg font-bold font-mono text-[#1a1a15]">
                {formatBytes(downloadUsed)} <span className="text-[#8c8c7a] font-normal text-xs">/ {formatBytes(downloadLimit)}</span>
              </div>
              <div className="w-full h-2 bg-[#e2e2d5] rounded-full overflow-hidden">
                <div className={`h-full rounded-full ${downloadPercent > 90 ? 'bg-amber-600' : 'bg-[#5A5A40]'}`} style={{ width: `${downloadPercent}%` }}></div>
              </div>
              <span className="text-[10px] text-[#8c8c7a] block">Израсходовано {downloadPercent}% в этом месяце</span>
            </div>

            {/* Card 4: Max File Size */}
            <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-2">
              <span className="text-[#5A5A40] font-semibold uppercase tracking-wider block text-[10px]">Максимальный файл</span>
              <div className="text-lg font-bold font-mono text-[#1a1a15]">
                {formatBytes(maxFileSize)}
              </div>
              <span className="text-[10px] text-[#8c8c7a] block pt-3">Лимит размера для 1 файла</span>
            </div>
          </div>
        </div>

        {/* Quarantined Files Alert (If any) */}
        {quarantinedFiles.length > 0 && (
          <div className="bg-amber-50 border border-amber-200 rounded-2xl p-4 text-amber-900 text-xs flex items-start gap-3">
            <AlertTriangle className="w-5 h-5 text-amber-600 shrink-0 mt-0.5" />
            <div>
              <span className="font-bold">Файлы на проверке ({quarantinedFiles.length}):</span>
              <p className="text-amber-800 mt-0.5">
                Загруженные вами подозрительные файлы (например, .exe или скрипты) отправлены на проверку администратору. Они станут доступны другим пользователям после подтверждения.
              </p>
            </div>
          </div>
        )}

        {/* Search & File List Header */}
        <div className="bg-white rounded-3xl border border-[#e2e2d5] p-6 shadow-xs space-y-4">
          <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 border-b border-[#f0f0e0] pb-4">
            <h3 className="font-serif text-xl font-bold text-[#1a1a15] flex items-center gap-2">
              <HardDrive className="w-5 h-5 text-[#5A5A40]" />
              Файлы в хранилище ({filteredFiles.length})
            </h3>

            {/* Search Input */}
            <div className="relative w-full sm:w-64">
              <Search className="w-4 h-4 text-[#8c8c7a] absolute left-3 top-2.5" />
              <input
                type="text"
                placeholder="Поиск по файлам..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="w-full pl-9 pr-4 py-1.5 bg-[#fcfcf9] border border-[#e2e2d5] rounded-full text-xs focus:outline-none focus:border-[#5A5A40]"
              />
            </div>
          </div>

          {/* Files Table */}
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-left border-collapse">
              <thead>
                <tr className="border-b border-[#f0f0e0] text-[11px] font-semibold text-[#8c8c7a] uppercase tracking-wider">
                  <th className="py-3 px-4">Имя файла</th>
                  <th className="py-3 px-4">Размер</th>
                  <th className="py-3 px-4">Загрузил</th>
                  <th className="py-3 px-4 text-right">Действия</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[#f5f5f0] text-xs">
                {filteredFiles.length === 0 ? (
                  <tr>
                    <td colSpan={4} className="py-12 text-center text-[#8c8c7a]">
                      Файлы не найдены.
                    </td>
                  </tr>
                ) : (
                  filteredFiles.map((file) => (
                    <tr key={file.id} className="hover:bg-[#fcfcf9] transition-colors">
                      <td className="py-3.5 px-4 font-medium text-[#1a1a15]">
                        <div className="flex items-center gap-2.5">
                          <div className="w-8 h-8 rounded-xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center font-bold text-xs shrink-0">
                            📄
                          </div>
                          <span className="truncate max-w-xs md:max-w-md">{file.original_name}</span>
                        </div>
                      </td>
                      <td className="py-3.5 px-4 font-mono text-[#8c8c7a]">{formatBytes(file.size)}</td>
                      <td className="py-3.5 px-4 text-[#8c8c7a]">
                        <span className="px-2.5 py-1 rounded-full bg-[#f0f0e0] text-[#5A5A40] font-medium text-[11px]">
                          {file.uploader_label || 'Пользователь'}
                        </span>
                      </td>
                      <td className="py-3.5 px-4 text-right space-x-2">
                        {isPreviewableType(file.original_name) && (
                          <a
                            href={`/preview/${file.id}`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="px-2.5 py-1 rounded bg-[#f0f0e0] text-[#5A5A40] hover:bg-[#e2e2d5] text-[11px] font-semibold transition-colors inline-flex items-center gap-1"
                          >
                            <ExternalLink className="w-3 h-3" />
                            Просмотр
                          </a>
                        )}
                        <a
                          href={`/api/files/download/${file.id}`}
                          download={file.original_name}
                          className="px-2.5 py-1 rounded bg-[#5A5A40] text-white hover:bg-[#484833] text-[11px] font-semibold transition-colors inline-flex items-center gap-1"
                        >
                          <Download className="w-3 h-3" />
                          Скачать
                        </a>
                        {(file.is_owner || userRole === 'admin') && (
                          <button
                            onClick={() => handleDeleteFile(file.id)}
                            title="Удалить файл"
                            className="p-1 rounded text-rose-600 hover:bg-rose-50 transition-colors cursor-pointer"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        )}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      </main>

      {/* MODAL: UPLOAD FILE WITH CONFIRMATION & RETENTION SELECTOR */}
      {showUploadModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-lg w-full max-h-[calc(100vh-2rem)] overflow-y-auto p-4 sm:p-6 md:p-8 shadow-2xl relative border border-[#e2e2d5] space-y-6 animate-in fade-in zoom-in duration-200">
            <button
              onClick={() => {
                setShowUploadModal(false);
                setSelectedFileForUpload(null);
                setUploadProgress(null);
                setKeepForever(false);
              }}
              className="absolute top-4 right-4 p-2 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
            >
              <X className="w-5 h-5" />
            </button>

            <div className="space-y-1">
              <h3 className="font-serif text-2xl font-bold text-[#1a1a15]">Загрузка файла</h3>
              <p className="text-xs text-[#8c8c7a]">Выберите файл и укажите желаемый срок хранения</p>
            </div>

            {!selectedFileForUpload ? (
              <div
                onDragOver={handleDragOver}
                onDragLeave={handleDragLeave}
                onDrop={handleDrop}
                onClick={() => fileInputRef.current?.click()}
                className={`border-2 border-dashed rounded-3xl p-4 sm:p-8 text-center cursor-pointer transition-all ${
                  isDragging ? 'border-[#5A5A40] bg-[#5A5A40]/5' : 'border-[#e2e2d5] bg-[#fcfcf9] hover:border-[#5A5A40]/50'
                }`}
              >
                <input
                  ref={fileInputRef}
                  type="file"
                  onChange={handleFileSelect}
                  className="hidden"
                />
                <FileUp className="w-10 h-10 text-[#5A5A40] mx-auto mb-3" />
                <span className="font-semibold text-sm block text-[#1a1a15]">Перетащите файл сюда</span>
                <span className="text-xs text-[#8c8c7a] block mt-1">или нажмите для выбора с устройства</span>
              </div>
            ) : (
              <div className="space-y-5">
                {/* File summary card */}
                <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] flex items-center justify-between">
                  <div className="flex items-center gap-3 overflow-hidden">
                    <div className="w-10 h-10 rounded-xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center font-bold text-sm shrink-0">
                      📄
                    </div>
                    <div className="truncate">
                      <div className="font-bold text-xs text-[#1a1a15] truncate">{selectedFileForUpload.name}</div>
                      <div className="text-[11px] text-[#8c8c7a] font-mono">{formatBytes(selectedFileForUpload.size)}</div>
                    </div>
                  </div>
                  <button
                    onClick={() => setSelectedFileForUpload(null)}
                    className="p-1.5 text-rose-600 hover:bg-rose-50 rounded-xl transition-colors shrink-0"
                  >
                    <X className="w-4 h-4" />
                  </button>
                </div>

                {/* Retention Selector */}
                <div className="space-y-2">
                  <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider">
                    Срок хранения файла
                  </label>
                  <div className="grid grid-cols-3 sm:grid-cols-5 gap-2">
                    {[1, 7, 14, 21, 30].map((days) => (
                      <button
                        key={days}
                        type="button"
                        onClick={() => {
                          setSelectedExpiryDays(days);
                          setKeepForever(false);
                        }}
                        className={`py-2 rounded-xl text-xs font-bold border transition-colors cursor-pointer ${
                          selectedExpiryDays === days
                            ? 'bg-[#5A5A40] text-white border-[#5A5A40]'
                            : 'bg-white text-[#5A5A40] border-[#e2e2d5] hover:bg-[#f0f0e0]'
                        }`}
                      >
                        {days} дн.
                      </button>
                    ))}
                  </div>
                  {userQuota?.allow_user_keep_forever && (
                    <label className="flex items-center gap-2 text-xs font-semibold text-[#5A5A40] cursor-pointer">
                      <input type="checkbox" checked={keepForever} onChange={(e) => setKeepForever(e.target.checked)} />
                      Хранить бессрочно
                    </label>
                  )}
                </div>

                {/* Progress bar if uploading */}
                {uploadProgress !== null && (
                  <div className="space-y-2">
                    <div className="flex justify-between text-xs font-semibold">
                      <span>{uploadStatusMsg}</span>
                      <span>{uploadProgress}%</span>
                    </div>
                    <div className="w-full h-2.5 bg-[#e2e2d5] rounded-full overflow-hidden">
                      <div
                        className="h-full bg-[#5A5A40] transition-all duration-300 rounded-full"
                        style={{ width: `${uploadProgress}%` }}
                      ></div>
                    </div>
                  </div>
                )}

                {/* Confirm upload button */}
                <div className="flex flex-col-reverse sm:flex-row justify-end gap-3 pt-2">
                  <button
                    type="button"
                    onClick={() => setSelectedFileForUpload(null)}
                    className="px-4 py-2.5 rounded-xl bg-[#f0f0e0] text-[#5A5A40] text-xs font-semibold hover:bg-[#e2e2d5] transition-colors cursor-pointer"
                  >
                    Сбросить
                  </button>
                  <button
                    type="button"
                    disabled={uploadProgress !== null}
                    onClick={() => uploadFileProcess(selectedFileForUpload)}
                    className="px-6 py-2.5 rounded-xl bg-[#5A5A40] text-white text-xs font-bold hover:bg-[#484833] transition-all shadow-md cursor-pointer flex items-center gap-1.5 disabled:opacity-50"
                  >
                    <Check className="w-4 h-4" />
                    Подтвердить загрузку
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
