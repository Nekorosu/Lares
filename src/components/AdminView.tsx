import React, { useState, useEffect } from 'react';
import { 
  BarChart3, 
  Users, 
  Laptop, 
  Folder, 
  Clock, 
  ShieldAlert, 
  Activity, 
  ScrollText, 
  Settings, 
  ArrowLeft,
  Plus,
  Trash2,
  CheckCircle2,
  Copy,
  RefreshCw,
  Search,
  HardDrive,
  Key,
  UserCheck,
  UserX,
  X,
  Check,
  Edit2,
  Download,
  ExternalLink
} from 'lucide-react';

interface AdminViewProps {
  onNavigateToUser: () => void;
  onLogout: () => void;
}

export default function AdminView({ onNavigateToUser, onLogout }: AdminViewProps) {
  const [activeTab, setActiveTab] = useState<
    'dashboard' | 'people' | 'sessions' | 'files' | 'uploads' | 'quarantine' | 'traffic' | 'audit' | 'settings'
  >('dashboard');

  // Data states
  const [stats, setStats] = useState<any>(null);
  const [people, setPeople] = useState<any[]>([]);
  const [invites, setInvites] = useState<any[]>([]);
  const [sessions, setSessions] = useState<any[]>([]);
  const [files, setFiles] = useState<any[]>([]);
  const [activeUploads, setActiveUploads] = useState<any[]>([]);
  const [auditLogs, setAuditLogs] = useState<any[]>([]);
  const [settingsData, setSettingsData] = useState<any>(null);
  const [loading, setLoading] = useState<boolean>(false);

  // Create Person Form States
  const [newPersonLabel, setNewPersonLabel] = useState<string>('');
  const [newPersonNotes, setNewPersonNotes] = useState<string>('');
  const [newQuotaGB, setNewQuotaGB] = useState<number>(100);
  const [newUploadGB, setNewUploadGB] = useState<number>(200);
  const [newDownloadGB, setNewDownloadGB] = useState<number>(300);
  const [newMaxFileGB, setNewMaxFileGB] = useState<number>(50);

  // Edit Person Modal State
  const [editingPerson, setEditingPerson] = useState<any | null>(null);

  // Invite Modal States
  const [selectedPersonForInvite, setSelectedPersonForInvite] = useState<any | null>(null);
  const [inviteActivationsInput, setInviteActivationsInput] = useState<number>(1);
  const [inviteExpiryInput, setInviteExpiryInput] = useState<number>(30);
  const [generatedInviteResult, setGeneratedInviteResult] = useState<{ code: string; personLabel: string } | null>(null);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  const getHeaders = () => {
    const adminToken = localStorage.getItem('lares_admin_token');
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };
    if (adminToken) headers['authorization'] = `Bearer ${adminToken}`;
    return headers;
  };

  const loadTabData = async () => {
    setLoading(true);
    const headers = getHeaders();
    try {
      if (activeTab === 'dashboard') {
        const resStats = await fetch('/api/stats', { headers }).catch(() => null);
        if (resStats?.ok) setStats(await resStats.json());
      } else if (activeTab === 'people') {
        const [resPeople, resInvites] = await Promise.all([
          fetch('/api/admin/people', { headers }).catch(() => null),
          fetch('/api/admin/invites', { headers }).catch(() => null)
        ]);
        if (resPeople?.ok) setPeople(await resPeople.json());
        if (resInvites?.ok) setInvites(await resInvites.json());
      } else if (activeTab === 'sessions') {
        const res = await fetch('/api/admin/sessions', { headers }).catch(() => null);
        if (res?.ok) setSessions(await res.json());
      } else if (activeTab === 'files' || activeTab === 'quarantine') {
        const res = await fetch('/api/files', { headers }).catch(() => null);
        if (res?.ok) setFiles(await res.json());
      } else if (activeTab === 'uploads') {
        const res = await fetch('/api/admin/active-uploads', { headers }).catch(() => null);
        if (res?.ok) setActiveUploads(await res.json());
      } else if (activeTab === 'traffic') {
        const resStats = await fetch('/api/stats', { headers }).catch(() => null);
        if (resStats?.ok) setStats(await resStats.json());
      } else if (activeTab === 'audit') {
        const res = await fetch('/api/admin/audit', { headers }).catch(() => null);
        if (res?.ok) setAuditLogs(await res.json());
      } else if (activeTab === 'settings') {
        const res = await fetch('/api/admin/settings', { headers }).catch(() => null);
        if (res?.ok) setSettingsData(await res.json());
      }
    } catch (err) {
      console.error('Failed to load tab data:', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTabData();
  }, [activeTab]);

  const copyToClipboard = (text: string, key: string) => {
    navigator.clipboard.writeText(text);
    setCopiedKey(key);
    setTimeout(() => setCopiedKey(null), 2000);
  };

  // Actions
  const handleCreatePerson = async () => {
    if (!newPersonLabel.trim()) return;
    try {
      const res = await fetch('/api/admin/people/create', {
        method: 'POST',
        headers: getHeaders(),
        body: JSON.stringify({ 
          label: newPersonLabel, 
          notes: newPersonNotes,
          storage_quota_gb: newQuotaGB,
          monthly_upload_limit_gb: newUploadGB,
          monthly_download_limit_gb: newDownloadGB,
          max_file_size_gb: newMaxFileGB,
        }),
      });
      if (res.ok) {
        setNewPersonLabel('');
        setNewPersonNotes('');
        loadTabData();
      }
    } catch (err) {
      alert('Ошибка создания профиля');
    }
  };

  const handleSaveEditedPerson = async () => {
    if (!editingPerson || !editingPerson.label.trim()) return;
    try {
      const res = await fetch('/api/admin/people/create', {
        method: 'POST',
        headers: getHeaders(),
        body: JSON.stringify({
          id: editingPerson.id,
          label: editingPerson.label,
          notes: editingPerson.notes,
          storage_quota_gb: editingPerson.storage_quota_gb,
          monthly_upload_limit_gb: editingPerson.monthly_upload_limit_gb,
          monthly_download_limit_gb: editingPerson.monthly_download_limit_gb,
          max_file_size_gb: editingPerson.max_file_size_gb,
        }),
      });
      if (res.ok) {
        setEditingPerson(null);
        loadTabData();
      }
    } catch (err) {
      alert('Ошибка редактирования профиля');
    }
  };

  const handleTogglePerson = async (id: number, currentEnabled: boolean) => {
    const action = currentEnabled ? 'disable' : 'enable';
    try {
      await fetch(`/api/admin/people/${action}/${id}`, { method: 'POST', headers: getHeaders() });
      loadTabData();
    } catch (err) {}
  };

  const handleDeletePerson = async (id: number, label: string) => {
    if (!confirm(`Удалить профиль «${label}»? Сессии пользователя будут отозваны.`)) return;
    try {
      await fetch(`/api/admin/people/delete/${id}`, { method: 'POST', headers: getHeaders() });
      loadTabData();
    } catch (err) {}
  };

  const handleGenerateInviteForPerson = async () => {
    if (!selectedPersonForInvite) return;
    try {
      const res = await fetch('/api/admin/invites', {
        method: 'POST',
        headers: getHeaders(),
        body: JSON.stringify({
          person_id: selectedPersonForInvite.id,
          max_activations: inviteActivationsInput,
          expiry_days: inviteExpiryInput,
        }),
      });

      if (res.ok) {
        const data = await res.json();
        const code = data.code || data.invite_code;
        setGeneratedInviteResult({
          code,
          personLabel: selectedPersonForInvite.label,
        });
        setSelectedPersonForInvite(null);
        loadTabData();
      }
    } catch (err) {
      alert('Ошибка создания инвайта');
    }
  };

  const handleRevokeInvite = async (id: string) => {
    try {
      await fetch(`/api/admin/invites/revoke/${id}`, { method: 'POST', headers: getHeaders() });
      loadTabData();
    } catch (err) {}
  };

  const handleRevokeSession = async (id: number) => {
    try {
      await fetch(`/api/admin/sessions/${id}`, { method: 'DELETE', headers: getHeaders() });
      loadTabData();
    } catch (err) {}
  };

  const handleApproveQuarantine = async (id: string) => {
    try {
      await fetch(`/api/admin/quarantine/${id}`, { method: 'POST', headers: getHeaders() });
      loadTabData();
    } catch (err) {}
  };

  const handleDeleteFile = async (id: string) => {
    if (!confirm('Удалить файл безоткатно?')) return;
    try {
      await fetch(`/api/files/delete/${id}`, { method: 'POST', headers: getHeaders() });
      loadTabData();
    } catch (err) {}
  };

  const formatBytes = (bytes: number): string => {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  const bytesToGB = (bytes: number): number => {
    return Math.round((bytes / (1024 * 1024 * 1024)) * 100) / 100;
  };

  const tabs = [
    { id: 'dashboard', label: 'Дашборд', icon: BarChart3 },
    { id: 'people', label: 'Пользователи и Инвайты', icon: Users },
    { id: 'sessions', label: 'Сессии устройств', icon: Laptop },
    { id: 'files', label: 'Все файлы', icon: Folder },
    { id: 'uploads', label: 'Активные загрузки', icon: Clock },
    { id: 'quarantine', label: 'Карантин', icon: ShieldAlert },
    { id: 'traffic', label: 'Трафик и квоты', icon: Activity },
    { id: 'audit', label: 'Журнал аудита', icon: ScrollText },
    { id: 'settings', label: 'Настройки', icon: Settings },
  ];

  const adminToken = localStorage.getItem('lares_admin_token') || '';

  return (
    <div className="min-h-screen bg-[#f4f4ee] text-[#1a1a15] flex flex-col font-sans">
      {/* Top Header */}
      <header className="bg-[#1a1a15] text-white px-6 py-4 flex justify-between items-center shadow-md">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-xl bg-[#5A5A40] text-white flex items-center justify-center font-bold font-serif text-lg">
            A
          </div>
          <div>
            <h1 className="font-serif text-lg font-bold">Панель Администратора Lares</h1>
            <p className="text-[11px] text-[#a0a090]">Управление пользователями, инвайтами и квотами</p>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <button
            onClick={onNavigateToUser}
            className="px-4 py-2 rounded-xl bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors flex items-center gap-1.5 cursor-pointer"
          >
            <ArrowLeft className="w-4 h-4" />
            Вернуться к хранилищу
          </button>
          <button
            onClick={onLogout}
            className="px-3.5 py-2 rounded-xl bg-rose-900/40 border border-rose-700/50 text-rose-200 text-xs font-semibold hover:bg-rose-900/60 transition-colors cursor-pointer"
          >
            Выйти
          </button>
        </div>
      </header>

      {/* Admin Body with Sidebar */}
      <div className="flex-1 flex flex-col md:flex-row max-w-7xl w-full mx-auto p-4 md:p-6 gap-6">
        {/* Sidebar */}
        <aside className="w-full md:w-64 bg-white rounded-3xl p-3 border border-[#e2e2d5] shadow-xs shrink-0 self-start">
          <nav className="space-y-1">
            {tabs.map((tab) => {
              const Icon = tab.icon;
              const isActive = activeTab === tab.id;
              return (
                <button
                  key={tab.id}
                  onClick={() => setActiveTab(tab.id as any)}
                  className={`w-full px-3.5 py-2.5 rounded-2xl text-xs font-semibold flex items-center gap-3 transition-colors cursor-pointer ${
                    isActive
                      ? 'bg-[#5A5A40] text-white shadow-xs'
                      : 'text-[#5A5A40] hover:bg-[#f0f0e0]'
                  }`}
                >
                  <Icon className="w-4 h-4 shrink-0" />
                  <span className="truncate">{tab.label}</span>
                </button>
              );
            })}
          </nav>
        </aside>

        {/* Tab Content Area */}
        <main className="flex-1 bg-white rounded-3xl p-6 border border-[#e2e2d5] shadow-xs min-h-[500px]">
          {loading ? (
            <div className="flex items-center justify-center py-24 text-[#8c8c7a]">
              <RefreshCw className="w-6 h-6 animate-spin mr-2" />
              Загрузка раздела...
            </div>
          ) : (
            <>
              {/* TAB 1: DASHBOARD */}
              {activeTab === 'dashboard' && (
                <div className="space-y-6">
                  <h2 className="font-serif text-xl font-bold text-[#1a1a15]">Обзор состояния системы</h2>
                  
                  <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                    <div className="bg-[#fcfcf9] p-5 rounded-2xl border border-[#e2e2d5]">
                      <span className="text-xs text-[#5A5A40] font-semibold uppercase block tracking-wider">Свободно на диске</span>
                      <div className="text-2xl font-bold font-mono text-[#1a1a15] mt-1">
                        {formatBytes(stats?.storage?.free_disk_bytes || 0)}
                      </div>
                      <span className="text-xs text-[#8c8c7a] mt-1 block">
                        Из {formatBytes(stats?.storage?.total_disk_bytes || 0)} всего
                      </span>
                    </div>

                    <div className="bg-[#fcfcf9] p-5 rounded-2xl border border-[#e2e2d5]">
                      <span className="text-xs text-[#5A5A40] font-semibold uppercase block tracking-wider">Занято файлами</span>
                      <div className="text-2xl font-bold font-mono text-[#1a1a15] mt-1">
                        {formatBytes(stats?.storage?.used_bytes || 0)}
                      </div>
                      <span className="text-xs text-[#8c8c7a] mt-1 block">
                        Файлов в системе: {stats?.storage?.files_count || 0}
                      </span>
                    </div>

                    <div className="bg-[#fcfcf9] p-5 rounded-2xl border border-[#e2e2d5]">
                      <span className="text-xs text-[#5A5A40] font-semibold uppercase block tracking-wider">Внешний трафик (External)</span>
                      <div className="text-2xl font-bold font-mono text-[#1a1a15] mt-1">
                        {formatBytes(stats?.traffic?.external_total_bytes || 0)}
                      </div>
                      <span className="text-xs text-[#8c8c7a] mt-1 block font-mono">
                        ↑ {formatBytes(stats?.traffic?.external_upload_bytes || 0)} | ↓ {formatBytes(stats?.traffic?.external_download_bytes || 0)}
                      </span>
                    </div>

                    <div className="bg-[#fcfcf9] p-5 rounded-2xl border border-[#e2e2d5]">
                      <span className="text-xs text-[#5A5A40] font-semibold uppercase block tracking-wider">Локальный трафик (Local)</span>
                      <div className="text-2xl font-bold font-mono text-emerald-800 mt-1">
                        {formatBytes(stats?.traffic?.local_total_bytes || 0)}
                      </div>
                      <span className="text-xs text-[#8c8c7a] mt-1 block font-mono">
                        ↑ {formatBytes(stats?.traffic?.local_upload_bytes || 0)} | ↓ {formatBytes(stats?.traffic?.local_download_bytes || 0)}
                      </span>
                    </div>

                    <div className="bg-[#fcfcf9] p-5 rounded-2xl border border-[#e2e2d5]">
                      <span className="text-xs text-[#5A5A40] font-semibold uppercase block tracking-wider">Суммарный трафик за месяц</span>
                      <div className="text-2xl font-bold font-mono text-[#1a1a15] mt-1">
                        {formatBytes(stats?.traffic?.total_bytes || 0)}
                      </div>
                      <span className="text-xs text-[#8c8c7a] mt-1 block">
                        Внешний + Локальный обмен
                      </span>
                    </div>

                    <div className="bg-[#fcfcf9] p-5 rounded-2xl border border-[#e2e2d5]">
                      <span className="text-xs text-[#5A5A40] font-semibold uppercase block tracking-wider">Активные сессии</span>
                      <div className="text-2xl font-bold font-mono text-[#1a1a15] mt-1">
                        {stats?.active_sessions || 0}
                      </div>
                      <span className="text-xs text-[#8c8c7a] mt-1 block">Устройств онлайн</span>
                    </div>
                  </div>
                </div>
              )}

              {/* TAB 2: PEOPLE & INTEGRATED INVITES */}
              {activeTab === 'people' && (
                <div className="space-y-6">
                  <div className="flex justify-between items-center border-b border-[#f0f0e0] pb-4">
                    <div>
                      <h2 className="font-serif text-xl font-bold text-[#1a1a15]">Пользователи и Инвайты</h2>
                      <p className="text-xs text-[#8c8c7a]">Управление профилями пользователей, настраиваемые лимиты и инвайт-коды</p>
                    </div>
                  </div>

                  {/* Generated Code Alert Notification */}
                  {generatedInviteResult && (
                    <div className="bg-emerald-50 border border-emerald-300 text-emerald-950 p-4 rounded-2xl space-y-2 animate-in fade-in duration-200">
                      <div className="flex justify-between items-center">
                        <div className="flex items-center gap-2 font-semibold text-xs text-emerald-800">
                          <CheckCircle2 className="w-4 h-4 text-emerald-600" />
                          <span>Инвайт-код сгенерирован для: <strong>{generatedInviteResult.personLabel}</strong></span>
                        </div>
                        <button 
                          onClick={() => setGeneratedInviteResult(null)}
                          className="text-emerald-700 hover:text-emerald-900 p-1"
                        >
                          <X className="w-4 h-4" />
                        </button>
                      </div>

                      <div className="flex items-center gap-3 bg-white p-3 rounded-xl border border-emerald-200">
                        <span className="font-mono text-sm font-bold tracking-wider text-[#1a1a15]">{generatedInviteResult.code}</span>
                        <button
                          onClick={() => copyToClipboard(generatedInviteResult.code, 'alert_invite')}
                          className="px-3 py-1 bg-emerald-700 hover:bg-emerald-800 text-white rounded-lg text-xs font-semibold flex items-center gap-1 cursor-pointer transition-colors"
                        >
                          {copiedKey === 'alert_invite' ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                          {copiedKey === 'alert_invite' ? 'Скопировано!' : 'Скопировать код'}
                        </button>
                      </div>
                    </div>
                  )}

                  {/* Create New Person Form with Custom Limits */}
                  <div className="bg-[#fcfcf9] p-5 rounded-2xl border border-[#e2e2d5] space-y-4">
                    <span className="text-xs font-semibold text-[#5A5A40] uppercase block tracking-wider">Создать новый профиль пользователя</span>
                    
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                      <div>
                        <label className="block text-[11px] font-semibold text-[#5A5A40] mb-1">Имя профиля (Label)</label>
                        <input
                          type="text"
                          placeholder="например, 'Алексей'"
                          value={newPersonLabel}
                          onChange={(e) => setNewPersonLabel(e.target.value)}
                          className="w-full px-3.5 py-2 rounded-xl border border-[#e2e2d5] text-xs bg-white focus:outline-none focus:border-[#5A5A40]"
                        />
                      </div>
                      <div>
                        <label className="block text-[11px] font-semibold text-[#5A5A40] mb-1">Заметки</label>
                        <input
                          type="text"
                          placeholder="например, 'Личный телефон и ноутбук'"
                          value={newPersonNotes}
                          onChange={(e) => setNewPersonNotes(e.target.value)}
                          className="w-full px-3.5 py-2 rounded-xl border border-[#e2e2d5] text-xs bg-white focus:outline-none focus:border-[#5A5A40]"
                        />
                      </div>
                    </div>

                    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 pt-1 border-t border-[#f0f0e0]">
                      <div>
                        <label className="block text-[10px] font-semibold text-[#8c8c7a] mb-1">Квота диска (ГБ)</label>
                        <input
                          type="number"
                          min="1"
                          max="10000"
                          value={newQuotaGB}
                          onChange={(e) => setNewQuotaGB(Number(e.target.value))}
                          className="w-full px-3 py-1.5 rounded-xl border border-[#e2e2d5] text-xs bg-white font-mono"
                        />
                      </div>
                      <div>
                        <label className="block text-[10px] font-semibold text-[#8c8c7a] mb-1">Лимит загрузки (ГБ/мес)</label>
                        <input
                          type="number"
                          min="1"
                          max="10000"
                          value={newUploadGB}
                          onChange={(e) => setNewUploadGB(Number(e.target.value))}
                          className="w-full px-3 py-1.5 rounded-xl border border-[#e2e2d5] text-xs bg-white font-mono"
                        />
                      </div>
                      <div>
                        <label className="block text-[10px] font-semibold text-[#8c8c7a] mb-1">Лимит отдачи (ГБ/мес)</label>
                        <input
                          type="number"
                          min="1"
                          max="10000"
                          value={newDownloadGB}
                          onChange={(e) => setNewDownloadGB(Number(e.target.value))}
                          className="w-full px-3 py-1.5 rounded-xl border border-[#e2e2d5] text-xs bg-white font-mono"
                        />
                      </div>
                      <div>
                        <label className="block text-[10px] font-semibold text-[#8c8c7a] mb-1">Макс. файл (ГБ)</label>
                        <input
                          type="number"
                          min="1"
                          max="1000"
                          value={newMaxFileGB}
                          onChange={(e) => setNewMaxFileGB(Number(e.target.value))}
                          className="w-full px-3 py-1.5 rounded-xl border border-[#e2e2d5] text-xs bg-white font-mono"
                        />
                      </div>
                    </div>

                    <div className="flex justify-end pt-1">
                      <button
                        onClick={handleCreatePerson}
                        className="px-6 py-2.5 rounded-xl bg-[#5A5A40] text-white text-xs font-bold hover:bg-[#484833] transition-colors flex items-center justify-center gap-1.5 cursor-pointer shrink-0 shadow-xs"
                      >
                        <Plus className="w-4 h-4" />
                        Создать пользователя с лимитами
                      </button>
                    </div>
                  </div>

                  {/* People Cards / Accordions */}
                  <div className="space-y-4 pt-2">
                    {people.length === 0 ? (
                      <p className="text-xs text-[#8c8c7a] py-8 text-center">Нет профилей пользователей.</p>
                    ) : (
                      people.map((person) => {
                        const personInvites = invites.filter(
                          (inv) => String(inv.person_id) === String(person.id) && inv.enabled !== false
                        );

                        return (
                          <div key={person.id} className="bg-white rounded-2xl border border-[#e2e2d5] p-5 space-y-4 shadow-xs hover:border-[#5A5A40]/40 transition-colors">
                            {/* Person Header & Info */}
                            <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-3 border-b border-[#f0f0e0] pb-3">
                              <div className="flex items-center gap-3">
                                <div className="w-10 h-10 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center font-bold text-sm">
                                  #{person.id}
                                </div>
                                <div>
                                  <div className="flex items-center gap-2">
                                    <h3 className="font-serif text-lg font-bold text-[#1a1a15]">{person.label}</h3>
                                    <span className={`px-2.5 py-0.5 rounded-full text-[10px] font-semibold ${person.enabled ? 'bg-emerald-100 text-emerald-800' : 'bg-rose-100 text-rose-800'}`}>
                                      {person.enabled ? 'Активен' : 'Отключен'}
                                    </span>
                                  </div>
                                  <p className="text-xs text-[#8c8c7a]">{person.notes || 'Без заметок'}</p>
                                </div>
                              </div>

                              {/* Action Buttons for Person */}
                              <div className="flex items-center gap-2">
                                <button
                                  onClick={() => setSelectedPersonForInvite(person)}
                                  className="px-3.5 py-1.5 rounded-xl bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors flex items-center gap-1.5 cursor-pointer shadow-xs"
                                >
                                  <Key className="w-3.5 h-3.5" />
                                  + Сгенерировать инвайт
                                </button>

                                <button
                                  onClick={() => setEditingPerson({
                                    id: person.id,
                                    label: person.label,
                                    notes: person.notes || '',
                                    storage_quota_gb: bytesToGB(person.storage_quota_bytes),
                                    monthly_upload_limit_gb: bytesToGB(person.monthly_upload_limit_bytes),
                                    monthly_download_limit_gb: bytesToGB(person.monthly_download_limit_bytes),
                                    max_file_size_gb: bytesToGB(person.max_file_size_bytes),
                                  })}
                                  title="Редактировать лимиты"
                                  className="p-1.5 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-[#5A5A40] hover:bg-[#e2e2d5]/50 transition-colors cursor-pointer"
                                >
                                  <Edit2 className="w-4 h-4" />
                                </button>

                                <button
                                  onClick={() => handleTogglePerson(person.id, person.enabled)}
                                  title={person.enabled ? 'Отключить пользователя' : 'Включить пользователя'}
                                  className={`p-1.5 rounded-xl border text-xs font-semibold transition-colors cursor-pointer ${
                                    person.enabled 
                                      ? 'border-amber-200 bg-amber-50 text-amber-800 hover:bg-amber-100' 
                                      : 'border-emerald-200 bg-emerald-50 text-emerald-800 hover:bg-emerald-100'
                                  }`}
                                >
                                  {person.enabled ? <UserX className="w-4 h-4" /> : <UserCheck className="w-4 h-4" />}
                                </button>
                                
                                <button
                                  onClick={() => handleDeletePerson(person.id, person.label)}
                                  title="Удалить профиль"
                                  className="p-1.5 rounded-xl border border-rose-200 bg-rose-50 text-rose-600 hover:bg-rose-100 transition-colors cursor-pointer"
                                >
                                  <Trash2 className="w-4 h-4" />
                                </button>
                              </div>
                            </div>

                            {/* Person Quota Stats */}
                            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs text-[#8c8c7a]">
                              <div>
                                <span className="block text-[10px] uppercase font-semibold text-[#5A5A40]">Дисковая квота</span>
                                <span className="font-mono text-[#1a1a15] font-semibold">{formatBytes(person.storage_quota_bytes)}</span>
                              </div>
                              <div>
                                <span className="block text-[10px] uppercase font-semibold text-[#5A5A40]">Upload limit</span>
                                <span className="font-mono text-[#1a1a15] font-semibold">{formatBytes(person.monthly_upload_limit_bytes)}</span>
                              </div>
                              <div>
                                <span className="block text-[10px] uppercase font-semibold text-[#5A5A40]">Download limit</span>
                                <span className="font-mono text-[#1a1a15] font-semibold">{formatBytes(person.monthly_download_limit_bytes)}</span>
                              </div>
                              <div>
                                <span className="block text-[10px] uppercase font-semibold text-[#5A5A40]">Макс. размер файла</span>
                                <span className="font-mono text-[#1a1a15] font-semibold">{formatBytes(person.max_file_size_bytes)}</span>
                              </div>
                            </div>

                            {/* Active Invites Section for this Person */}
                            <div className="bg-[#fcfcf9] p-3.5 rounded-xl border border-[#e2e2d5] space-y-2">
                              <div className="flex justify-between items-center text-xs font-semibold text-[#5A5A40]">
                                <span>Активные инвайты пользователя ({personInvites.length})</span>
                              </div>

                              {personInvites.length === 0 ? (
                                <p className="text-xs text-[#8c8c7a]">У этого пользователя нет неиспользованных инвайт-кодов.</p>
                              ) : (
                                <div className="space-y-1.5">
                                  {personInvites.map((inv) => {
                                    const codeText = inv.code_prefix || inv.code || 'XXXX-XXXX';
                                    const keyId = `inv_${inv.id}`;
                                    return (
                                      <div key={inv.id} className="bg-white p-2.5 rounded-lg border border-[#e2e2d5] flex justify-between items-center text-xs">
                                        <div className="flex items-center gap-2">
                                          <span className="font-mono font-bold text-[#1a1a15]">{codeText}</span>
                                          <button
                                            onClick={() => copyToClipboard(codeText, keyId)}
                                            title="Скопировать инвайт"
                                            className="p-1 rounded text-[#8c8c7a] hover:text-[#1a1a15] hover:bg-[#e2e2d5]/50 transition-colors cursor-pointer"
                                          >
                                            {copiedKey === keyId ? <Check className="w-3.5 h-3.5 text-emerald-600" /> : <Copy className="w-3.5 h-3.5" />}
                                          </button>
                                          <span className="text-[10px] text-[#8c8c7a]">
                                            (Использовано {inv.activations_used ?? 0} / {inv.max_activations ?? 1})
                                          </span>
                                        </div>
                                        <button
                                          onClick={() => handleRevokeInvite(inv.id)}
                                          title="Отозвать инвайт"
                                          className="p-1 rounded text-rose-600 hover:bg-rose-50 transition-colors cursor-pointer"
                                        >
                                          <Trash2 className="w-3.5 h-3.5" />
                                        </button>
                                      </div>
                                    );
                                  })}
                                </div>
                              )}
                            </div>
                          </div>
                        );
                      })
                    )}
                  </div>
                </div>
              )}

              {/* TAB 3: SESSIONS */}
              {activeTab === 'sessions' && (
                <div className="space-y-6">
                  <h2 className="font-serif text-xl font-bold text-[#1a1a15]">Сессии устройств</h2>
                  <div className="overflow-x-auto">
                    <table className="w-full text-left border-collapse">
                      <thead>
                        <tr className="border-b border-[#f0f0e0] text-[11px] font-semibold text-[#8c8c7a] uppercase">
                          <th className="py-2 px-3">Устройство / Пользователь</th>
                          <th className="py-2 px-3">IP Hash</th>
                          <th className="py-2 px-3">Статус</th>
                          <th className="py-2 px-3 text-right">Действие</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[#f5f5f0] text-xs">
                        {sessions.map((s) => (
                          <tr key={s.id}>
                            <td className="py-3 px-3 font-medium">{s.device || s.name}</td>
                            <td className="py-3 px-3 font-mono text-[#8c8c7a]">{s.ip || 'local'}</td>
                            <td className="py-3 px-3">
                              <span className={`px-2 py-0.5 rounded-full text-[10px] font-semibold ${!s.revoked ? 'bg-emerald-100 text-emerald-800' : 'bg-rose-100 text-rose-800'}`}>
                                {!s.revoked ? 'Активна' : 'Отозвана'}
                              </span>
                            </td>
                            <td className="py-3 px-3 text-right">
                              {!s.revoked && (
                                <button
                                  onClick={() => handleRevokeSession(s.id)}
                                  className="px-2.5 py-1 rounded bg-rose-50 text-rose-600 hover:bg-rose-100 text-[11px]"
                                >
                                  Отозвать
                                </button>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}

              {/* TAB 4: FILES */}
              {activeTab === 'files' && (
                <div className="space-y-6">
                  <h2 className="font-serif text-xl font-bold text-[#1a1a15]">Все файлы в системе ({files.length})</h2>
                  <div className="overflow-x-auto">
                    <table className="w-full text-left border-collapse">
                      <thead>
                        <tr className="border-b border-[#f0f0e0] text-[11px] font-semibold text-[#8c8c7a] uppercase">
                          <th className="py-2 px-3">Имя файла</th>
                          <th className="py-2 px-3">Размер</th>
                          <th className="py-2 px-3">Статус</th>
                          <th className="py-2 px-3 text-right">Действия</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[#f5f5f0] text-xs">
                        {files.map((f) => (
                          <tr key={f.id}>
                            <td className="py-3 px-3 font-medium">{f.original_name}</td>
                            <td className="py-3 px-3 font-mono text-[#8c8c7a]">{formatBytes(f.size)}</td>
                            <td className="py-3 px-3">
                              <span className={`px-2 py-0.5 rounded-full text-[10px] font-semibold ${f.status === 'ready' ? 'bg-emerald-100 text-emerald-800' : 'bg-amber-100 text-amber-800'}`}>
                                {f.status}
                              </span>
                            </td>
                            <td className="py-3 px-3 text-right space-x-1.5">
                              <a
                                href={`/api/files/download/${f.id}?token=${adminToken}`}
                                download={f.original_name}
                                className="px-2.5 py-1 rounded bg-[#5A5A40] text-white hover:bg-[#484833] text-[11px] font-semibold transition-colors inline-flex items-center gap-1"
                              >
                                <Download className="w-3 h-3" />
                                Скачать
                              </a>
                              <button
                                onClick={() => handleDeleteFile(f.id)}
                                className="px-2.5 py-1 rounded bg-rose-50 text-rose-600 hover:bg-rose-100 text-[11px] font-semibold transition-colors cursor-pointer"
                              >
                                Удалить
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}

              {/* TAB 5: ACTIVE UPLOADS */}
              {activeTab === 'uploads' && (
                <div className="space-y-6">
                  <h2 className="font-serif text-xl font-bold text-[#1a1a15]">Активные недогруженные файлы</h2>
                  {activeUploads.length === 0 ? (
                    <p className="text-xs text-[#8c8c7a]">Нет незавершенных загрузок</p>
                  ) : (
                    <div className="space-y-2">
                      {activeUploads.map((up) => (
                        <div key={up.id} className="bg-[#fcfcf9] p-3 rounded-xl border border-[#e2e2d5] flex justify-between items-center text-xs">
                          <div>
                            <div className="font-semibold">{up.original_name}</div>
                            <div className="text-[10px] text-[#8c8c7a] font-mono">{formatBytes(up.received_bytes)} / {formatBytes(up.declared_size)}</div>
                          </div>
                          <span className="px-2 py-0.5 rounded-full bg-amber-100 text-amber-800 font-semibold text-[10px]">
                            {up.status}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {/* TAB 6: QUARANTINE */}
              {activeTab === 'quarantine' && (
                <div className="space-y-6">
                  <div className="border-b border-[#f0f0e0] pb-3">
                    <h2 className="font-serif text-xl font-bold text-[#1a1a15]">Файлы в карантине</h2>
                    <p className="text-xs text-[#8c8c7a]">Подозрительные файлы, ожидающие проверки администратором перед допуском к общему скачиванию</p>
                  </div>

                  {files.filter(f => f.status === 'quarantined').length === 0 ? (
                    <p className="text-xs text-[#8c8c7a] py-8 text-center">Нет файлов на проверке в карантине.</p>
                  ) : (
                    files.filter(f => f.status === 'quarantined').map((f) => (
                      <div key={f.id} className="bg-amber-50 p-4.5 rounded-2xl border border-amber-200 flex flex-col sm:flex-row justify-between items-start sm:items-center gap-3 text-xs">
                        <div>
                          <div className="font-bold text-amber-950 text-sm">{f.original_name}</div>
                          <div className="text-[11px] text-amber-800 font-mono mt-0.5">
                            Размер: {formatBytes(f.size)} • Загрузил: {f.uploader_label || 'Пользователь'}
                          </div>
                          {f.flag_reason && (
                            <div className="text-[11px] text-rose-700 mt-1">Причина проверки: {f.flag_reason}</div>
                          )}
                        </div>

                        <div className="flex items-center gap-2 shrink-0">
                          <a
                            href={`/api/files/download/${f.id}?token=${adminToken}`}
                            download={f.original_name}
                            className="px-3.5 py-2 rounded-xl bg-[#5A5A40] text-white text-xs font-semibold hover:bg-[#484833] transition-colors flex items-center gap-1.5 shadow-xs"
                          >
                            <Download className="w-4 h-4" />
                            Скачать для проверки
                          </a>
                          <button
                            onClick={() => handleApproveQuarantine(f.id)}
                            className="px-3.5 py-2 rounded-xl bg-emerald-700 text-white text-xs font-semibold hover:bg-emerald-800 transition-colors shadow-xs cursor-pointer"
                          >
                            Одобрить
                          </button>
                          <button
                            onClick={() => handleDeleteFile(f.id)}
                            className="px-3.5 py-2 rounded-xl bg-rose-700 text-white text-xs font-semibold hover:bg-rose-800 transition-colors shadow-xs cursor-pointer"
                          >
                            Удалить
                          </button>
                        </div>
                      </div>
                    ))
                  )}
                </div>
              )}

              {/* TAB 7: TRAFFIC */}
              {activeTab === 'traffic' && (
                <div className="space-y-6">
                  <div className="border-b border-[#f0f0e0] pb-3">
                    <h2 className="font-serif text-xl font-bold text-[#1a1a15]">Детализированный учет трафика</h2>
                    <p className="text-xs text-[#8c8c7a]">Статистика передачи данных за месяц ({stats?.traffic?.month || 'Текущий'})</p>
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-3 gap-4 text-xs">
                    <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-2">
                      <span className="font-bold text-[#5A5A40] uppercase tracking-wider block">Внешний трафик (Интернет)</span>
                      <div className="text-xl font-bold font-mono text-[#1a1a15]">
                        {formatBytes(stats?.traffic?.external_total_bytes || 0)}
                      </div>
                      <div className="text-[11px] text-[#8c8c7a] space-y-1 font-mono pt-1">
                        <div>Загружено (Upload): <strong>{formatBytes(stats?.traffic?.external_upload_bytes || 0)}</strong></div>
                        <div>Скачано (Download): <strong>{formatBytes(stats?.traffic?.external_download_bytes || 0)}</strong></div>
                      </div>
                      <span className="text-[10px] text-amber-800 bg-amber-50 px-2 py-0.5 rounded block">
                        Учитывается в месячной квоте
                      </span>
                    </div>

                    <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-2">
                      <span className="font-bold text-[#5A5A40] uppercase tracking-wider block">Локальный трафик (LAN / 192.168.32.x)</span>
                      <div className="text-xl font-bold font-mono text-emerald-800">
                        {formatBytes(stats?.traffic?.local_total_bytes || 0)}
                      </div>
                      <div className="text-[11px] text-[#8c8c7a] space-y-1 font-mono pt-1">
                        <div>Загружено (Upload): <strong>{formatBytes(stats?.traffic?.local_upload_bytes || 0)}</strong></div>
                        <div>Скачано (Download): <strong>{formatBytes(stats?.traffic?.local_download_bytes || 0)}</strong></div>
                      </div>
                      <span className="text-[10px] text-emerald-800 bg-emerald-50 px-2 py-0.5 rounded block">
                        Без лимитов (не сносят квоту)
                      </span>
                    </div>

                    <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-2">
                      <span className="font-bold text-[#5A5A40] uppercase tracking-wider block">Общий суммарный трафик</span>
                      <div className="text-xl font-bold font-mono text-[#1a1a15]">
                        {formatBytes(stats?.traffic?.total_bytes || 0)}
                      </div>
                      <div className="text-[11px] text-[#8c8c7a] space-y-1 font-mono pt-1">
                        <div>Всего загружено: <strong>{formatBytes(stats?.traffic?.upload_bytes || 0)}</strong></div>
                        <div>Всего скачано: <strong>{formatBytes(stats?.traffic?.download_bytes || 0)}</strong></div>
                      </div>
                      <span className="text-[10px] text-gray-700 bg-gray-100 px-2 py-0.5 rounded block">
                        Сумма внешнего и локального обмена
                      </span>
                    </div>
                  </div>
                </div>
              )}

              {/* TAB 8: AUDIT LOG */}
              {activeTab === 'audit' && (
                <div className="space-y-6">
                  <h2 className="font-serif text-xl font-bold text-[#1a1a15]">Журнал событий аудита</h2>
                  <div className="overflow-x-auto max-h-[450px] overflow-y-auto">
                    <table className="w-full text-left border-collapse">
                      <thead>
                        <tr className="border-b border-[#f0f0e0] text-[11px] font-semibold text-[#8c8c7a] uppercase">
                          <th className="py-2 px-3">Время</th>
                          <th className="py-2 px-3">Субъект</th>
                          <th className="py-2 px-3">Событие</th>
                          <th className="py-2 px-3">Детали</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[#f5f5f0] text-xs">
                        {auditLogs.map((log) => (
                          <tr key={log.id}>
                            <td className="py-2 px-3 font-mono text-[#8c8c7a] text-[11px]">{log.time}</td>
                            <td className="py-2 px-3 font-semibold">{log.actor_type} #{log.actor_id}</td>
                            <td className="py-2 px-3"><span className="px-2 py-0.5 rounded bg-[#f0f0e0] text-[10px] font-mono">{log.event}</span></td>
                            <td className="py-2 px-3 text-[#8c8c7a]">{log.details}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}

              {/* TAB 9: SETTINGS */}
              {activeTab === 'settings' && (
                <div className="space-y-6">
                  <h2 className="font-serif text-xl font-bold text-[#1a1a15]">Глобальные настройки системы</h2>
                  {settingsData ? (
                    <div className="bg-[#fcfcf9] p-4 rounded-2xl border border-[#e2e2d5] space-y-3 text-xs">
                      <div>Срок хранения по умолчанию: <strong>{settingsData.storage_defaults?.default_expiry} дней</strong></div>
                      <div>Лимит диска: <strong>{settingsData.storage_defaults?.quota_gb} GB</strong></div>
                      <div>Лимит скорости скачивания: <strong>{settingsData.speed_limits?.download_mbps} Mbps</strong></div>
                      <div>Минимальный свободный диск: <strong>{settingsData.disk_reserve?.min_free_gb} GB</strong></div>
                    </div>
                  ) : (
                    <p className="text-xs text-[#8c8c7a]">Загрузка настроек...</p>
                  )}
                </div>
              )}
            </>
          )}
        </main>
      </div>

      {/* MODAL 1: GENERATE INVITE FOR A SPECIFIC PERSON */}
      {selectedPersonForInvite && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-md w-full p-6 shadow-xl relative border border-[#e2e2d5] space-y-4">
            <button
              onClick={() => setSelectedPersonForInvite(null)}
              className="absolute top-4 right-4 p-1 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
            >
              <X className="w-5 h-5" />
            </button>

            <div>
              <h3 className="font-serif text-xl font-bold text-[#1a1a15]">Сгенерировать инвайт</h3>
              <p className="text-xs text-[#8c8c7a]">Создание ключа доступа для пользователя <strong>«{selectedPersonForInvite.label}»</strong></p>
            </div>

            <div className="space-y-3 pt-2">
              <div>
                <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider mb-1">
                  Количество использований (активаций)
                </label>
                <input
                  type="number"
                  min="1"
                  max="100"
                  value={inviteActivationsInput}
                  onChange={(e) => setInviteActivationsInput(Math.max(1, Number(e.target.value)))}
                  className="w-full px-3.5 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-xs font-semibold text-[#1a1a15]"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider mb-1">
                  Срок действия ключа (дней)
                </label>
                <input
                  type="number"
                  min="1"
                  max="365"
                  value={inviteExpiryInput}
                  onChange={(e) => setInviteExpiryInput(Math.max(1, Number(e.target.value)))}
                  className="w-full px-3.5 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-xs font-semibold text-[#1a1a15]"
                />
              </div>
            </div>

            <div className="flex justify-end gap-2 pt-3">
              <button
                onClick={() => setSelectedPersonForInvite(null)}
                className="px-4 py-2 rounded-xl bg-[#f0f0e0] text-[#5A5A40] text-xs font-semibold hover:bg-[#e2e2d5] transition-colors cursor-pointer"
              >
                Отмена
              </button>
              <button
                onClick={handleGenerateInviteForPerson}
                className="px-5 py-2 rounded-xl bg-[#5A5A40] text-white text-xs font-bold hover:bg-[#484833] transition-colors cursor-pointer shadow-xs flex items-center gap-1.5"
              >
                <Key className="w-3.5 h-3.5" />
                Сгенерировать код
              </button>
            </div>
          </div>
        </div>
      )}

      {/* MODAL 2: EDIT PERSON QUOTAS & LIMITS */}
      {editingPerson && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl max-w-md w-full p-6 shadow-xl relative border border-[#e2e2d5] space-y-4">
            <button
              onClick={() => setEditingPerson(null)}
              className="absolute top-4 right-4 p-1 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
            >
              <X className="w-5 h-5" />
            </button>

            <div>
              <h3 className="font-serif text-xl font-bold text-[#1a1a15]">Редактировать лимиты</h3>
              <p className="text-xs text-[#8c8c7a]">Изменение квот для пользователя <strong>«{editingPerson.label}»</strong></p>
            </div>

            <div className="space-y-3 pt-1">
              <div>
                <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider mb-1">Имя пользователя (Label)</label>
                <input
                  type="text"
                  value={editingPerson.label}
                  onChange={(e) => setEditingPerson({ ...editingPerson, label: e.target.value })}
                  className="w-full px-3.5 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-xs font-medium"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider mb-1">Заметки</label>
                <input
                  type="text"
                  value={editingPerson.notes}
                  onChange={(e) => setEditingPerson({ ...editingPerson, notes: e.target.value })}
                  className="w-full px-3.5 py-2 rounded-xl border border-[#e2e2d5] bg-[#fcfcf9] text-xs font-medium"
                />
              </div>

              <div className="grid grid-cols-2 gap-3 pt-2">
                <div>
                  <label className="block text-[10px] font-semibold text-[#8c8c7a] uppercase mb-1">Квота диска (ГБ)</label>
                  <input
                    type="number"
                    min="1"
                    value={editingPerson.storage_quota_gb}
                    onChange={(e) => setEditingPerson({ ...editingPerson, storage_quota_gb: Number(e.target.value) })}
                    className="w-full px-3 py-1.5 rounded-xl border border-[#e2e2d5] text-xs font-mono bg-[#fcfcf9]"
                  />
                </div>
                <div>
                  <label className="block text-[10px] font-semibold text-[#8c8c7a] uppercase mb-1">Upload limit (ГБ/мес)</label>
                  <input
                    type="number"
                    min="1"
                    value={editingPerson.monthly_upload_limit_gb}
                    onChange={(e) => setEditingPerson({ ...editingPerson, monthly_upload_limit_gb: Number(e.target.value) })}
                    className="w-full px-3 py-1.5 rounded-xl border border-[#e2e2d5] text-xs font-mono bg-[#fcfcf9]"
                  />
                </div>
                <div>
                  <label className="block text-[10px] font-semibold text-[#8c8c7a] uppercase mb-1">Download limit (ГБ/мес)</label>
                  <input
                    type="number"
                    min="1"
                    value={editingPerson.monthly_download_limit_gb}
                    onChange={(e) => setEditingPerson({ ...editingPerson, monthly_download_limit_gb: Number(e.target.value) })}
                    className="w-full px-3 py-1.5 rounded-xl border border-[#e2e2d5] text-xs font-mono bg-[#fcfcf9]"
                  />
                </div>
                <div>
                  <label className="block text-[10px] font-semibold text-[#8c8c7a] uppercase mb-1">Макс. файл (ГБ)</label>
                  <input
                    type="number"
                    min="1"
                    value={editingPerson.max_file_size_gb}
                    onChange={(e) => setEditingPerson({ ...editingPerson, max_file_size_gb: Number(e.target.value) })}
                    className="w-full px-3 py-1.5 rounded-xl border border-[#e2e2d5] text-xs font-mono bg-[#fcfcf9]"
                  />
                </div>
              </div>
            </div>

            <div className="flex justify-end gap-2 pt-3">
              <button
                onClick={() => setEditingPerson(null)}
                className="px-4 py-2 rounded-xl bg-[#f0f0e0] text-[#5A5A40] text-xs font-semibold hover:bg-[#e2e2d5] transition-colors cursor-pointer"
              >
                Отмена
              </button>
              <button
                onClick={handleSaveEditedPerson}
                className="px-5 py-2 rounded-xl bg-[#5A5A40] text-white text-xs font-bold hover:bg-[#484833] transition-colors cursor-pointer shadow-xs"
              >
                Сохранить лимиты
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
