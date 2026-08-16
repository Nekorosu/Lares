import React, { useState, useEffect } from 'react';
import UserView from './components/UserView';
import AdminView from './components/AdminView';
import { LogIn, X, AlertTriangle, Key, ShieldCheck, RefreshCw } from 'lucide-react';

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

export default function App() {
  const [currentPath, setCurrentPath] = useState<string>(window.location.pathname);
  const [isAuthenticated, setIsAuthenticated] = useState<boolean>(false);
  const [checkingAuth, setCheckingAuth] = useState<boolean>(true);
  const [userRole, setUserRole] = useState<'user' | 'admin'>('user');
  const [adminToken, setAdminToken] = useState<string | null>(() => {
    return localStorage.getItem('lares_admin_token') || null;
  });

  // Invite activation state
  const [inviteCodeInput, setInviteCodeInput] = useState<string>('');
  const [inviteErrorMsg, setInviteErrorMsg] = useState<string | null>(null);
  const [activatingInvite, setActivatingInvite] = useState<boolean>(false);

  // Admin login modal state
  const [showLoginModal, setShowLoginModal] = useState<boolean>(false);
  const [loginUsernameInput, setLoginUsernameInput] = useState<string>('admin');
  const [loginPasswordInput, setLoginPasswordInput] = useState<string>('');
  const [loginTotpInput, setLoginTotpInput] = useState<string>('');
  const [loginErrorMsg, setLoginErrorMsg] = useState<string | null>(null);

  // Data
  const [stats, setStats] = useState<ServerStats | null>(null);
  const [files, setFiles] = useState<FileRecord[]>([]);
  const [loadingData, setLoadingData] = useState<boolean>(false);

  // Listen to popstate for URL changes
  useEffect(() => {
    const handleLocationChange = () => {
      setCurrentPath(window.location.pathname);
    };
    window.addEventListener('popstate', handleLocationChange);
    return () => window.removeEventListener('popstate', handleLocationChange);
  }, []);

  const navigateTo = (path: string) => {
    window.history.pushState({}, '', path);
    setCurrentPath(path);
  };

  const getAuthHeaders = () => {
    const headers: Record<string, string> = {};
    if (adminToken) {
      headers['authorization'] = `Bearer ${adminToken}`;
    }
    return headers;
  };

  const refreshData = async () => {
    setLoadingData(true);
    const reqHeaders = getAuthHeaders();
    try {
      const resMe = await fetch('/api/auth/me', { headers: reqHeaders }).catch(() => null);
      if (resMe && resMe.ok) {
        const meData = await resMe.json().catch(() => null);
        if (meData && meData.authenticated) {
          setIsAuthenticated(true);
          setUserRole(meData.role || 'user');
        } else {
          setIsAuthenticated(false);
        }
      } else {
        setIsAuthenticated(false);
      }

      // If authenticated or admin, fetch stats & files
      if (adminToken || (resMe && resMe.ok)) {
        const [resStats, resFiles] = await Promise.all([
          fetch('/api/stats', { headers: reqHeaders }).catch(() => null),
          fetch('/api/files', { headers: reqHeaders }).catch(() => null)
        ]);

        if (resStats && resStats.ok) {
          const data = await resStats.json().catch(() => null);
          if (data && typeof data === 'object') setStats(data);
        }
        if (resFiles && resFiles.ok) {
          const fetchedFiles = await resFiles.json().catch(() => null);
          if (Array.isArray(fetchedFiles)) setFiles(fetchedFiles);
        }
      }
    } catch (err) {
      console.error('Failed to fetch live API data:', err);
    } finally {
      setCheckingAuth(false);
      setLoadingData(false);
    }
  };

  useEffect(() => {
    refreshData();
  }, [adminToken]);

  const handleInviteActivate = async (e: React.FormEvent) => {
    e.preventDefault();
    setInviteErrorMsg(null);

    const code = inviteCodeInput.trim();
    if (!code) {
      setInviteErrorMsg('Пожалуйста, введите инвайт-код');
      return;
    }

    setActivatingInvite(true);
    try {
      const res = await fetch('/api/auth/invite/activate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code }),
      });

      const data = await res.json().catch(() => ({ error: 'Неизвестный ответ сервера' }));

      if (!res.ok) {
        throw new Error(data.error || 'Неверный или просроченный инвайт-код');
      }

      setInviteCodeInput('');
      await refreshData();
    } catch (err: any) {
      setInviteErrorMsg(err.message || 'Ошибка активации инвайт-кода');
    } finally {
      setActivatingInvite(false);
    }
  };

  const handleAdminLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoginErrorMsg(null);

    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: loginUsernameInput,
          password: loginPasswordInput,
          totp: loginTotpInput
        })
      });

      if (!res.ok) {
        const errData = await res.json().catch(() => ({ error: 'Неверные данные входа' }));
        throw new Error(errData.error || 'Ошибка авторизации');
      }

      const data = await res.json();
      if (data.token) {
        localStorage.setItem('lares_admin_token', data.token);
        setAdminToken(data.token);
        setUserRole('admin');
        setIsAuthenticated(true);
        setShowLoginModal(false);
        setLoginPasswordInput('');
        setLoginTotpInput('');
        navigateTo('/admin');
      }
    } catch (err: any) {
      setLoginErrorMsg(err.message);
    }
  };

  const handleLogout = async () => {
    try {
      await fetch('/logout').catch(() => null);
    } catch (e) {}

    localStorage.removeItem('lares_admin_token');
    setAdminToken(null);
    setUserRole('user');
    setIsAuthenticated(false);
    setStats(null);
    setFiles([]);
    navigateTo('/');
  };

  const isAdminPath = currentPath.startsWith('/admin');

  // If on /admin path but not admin, show Admin Login Modal
  useEffect(() => {
    if (isAdminPath && userRole !== 'admin') {
      setShowLoginModal(true);
    }
  }, [isAdminPath, userRole]);

  // Render Loading Spinner
  if (checkingAuth) {
    return (
      <div className="min-h-screen bg-[#f4f4ee] flex flex-col items-center justify-center text-[#1a1a15]">
        <div className="flex items-center gap-3 mb-4">
          <div className="w-10 h-10 rounded-2xl bg-[#5A5A40] text-white flex items-center justify-center font-serif text-xl font-bold shadow-md">
            L
          </div>
          <span className="font-serif text-xl font-bold">Lares Homeshare</span>
        </div>
        <div className="flex items-center gap-2 text-xs text-[#8c8c7a]">
          <RefreshCw className="w-4 h-4 animate-spin text-[#5A5A40]" />
          <span>Проверка сессии...</span>
        </div>
      </div>
    );
  }

  return (
    <>
      {isAdminPath && userRole === 'admin' ? (
        <AdminView
          onNavigateToUser={() => navigateTo('/')}
          onLogout={handleLogout}
        />
      ) : isAuthenticated ? (
        <UserView
          stats={stats}
          files={files}
          userRole={userRole}
          loading={loadingData}
          refreshData={refreshData}
          onOpenAdminLogin={() => setShowLoginModal(true)}
          onNavigateToAdmin={() => navigateTo('/admin')}
          onLogout={handleLogout}
        />
      ) : (
        /* Invite Code Activation Screen for Unauthenticated Users */
        <div className="min-h-screen bg-[#f4f4ee] flex flex-col justify-between items-center p-4 md:p-6 text-[#1a1a15] font-sans">
          <div className="w-full flex flex-wrap justify-between items-center gap-3 max-w-4xl pt-2">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-2xl bg-[#5A5A40] text-white flex items-center justify-center font-serif text-xl font-bold shadow-sm">
                L
              </div>
              <div>
                <h1 className="font-serif text-lg font-bold text-[#1a1a15]">Lares Homeshare</h1>
                <p className="text-[11px] text-[#8c8c7a]">Защищенное хранилище файлов</p>
              </div>
            </div>

            <button
              onClick={() => setShowLoginModal(true)}
              className="px-4 py-2 rounded-full bg-white border border-[#e2e2d5] text-[#5A5A40] text-xs font-bold hover:bg-[#f0f0e0] transition-colors flex items-center gap-1.5 cursor-pointer shadow-xs"
            >
              <LogIn className="w-4 h-4" />
              Вход для админа
            </button>
          </div>

          <div className="my-auto max-w-md w-full bg-white rounded-3xl p-6 md:p-8 border border-[#e2e2d5] shadow-xl space-y-6 animate-in fade-in zoom-in duration-200">
            <div className="text-center space-y-2">
              <div className="w-14 h-14 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center mx-auto mb-3">
                <Key className="w-7 h-7 text-[#5A5A40]" />
              </div>
              <h2 className="font-serif text-2xl font-bold text-[#1a1a15]">Активация доступа</h2>
              <p className="text-xs text-[#8c8c7a] leading-relaxed">
                Для входа в персональное хранилище файлов введите ваш инвайт-код. Дополнительные данные вводить не требуется.
              </p>
            </div>

            <form onSubmit={handleInviteActivate} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider mb-2">
                  Инвайт-код
                </label>
                <input
                  type="text"
                  value={inviteCodeInput}
                  onChange={(e) => setInviteCodeInput(e.target.value)}
                  placeholder="Введите полученный инвайт-код..."
                  className="w-full px-4 py-3 rounded-2xl border border-[#e2e2d5] text-sm focus:outline-none focus:border-[#5A5A40] bg-[#fcfcf9] font-mono tracking-wider text-center"
                  autoFocus
                />
              </div>

              {inviteErrorMsg && (
                <div className="p-3.5 rounded-2xl bg-rose-50 border border-rose-200 text-xs text-rose-800 flex items-center gap-2">
                  <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0" />
                  <span>{inviteErrorMsg}</span>
                </div>
              )}

              <button
                type="submit"
                disabled={activatingInvite}
                className="w-full py-3.5 rounded-2xl bg-[#5A5A40] text-white text-sm font-bold hover:bg-[#484833] transition-all shadow-md flex items-center justify-center gap-2 cursor-pointer disabled:opacity-50"
              >
                {activatingInvite ? (
                  <>
                    <RefreshCw className="w-4 h-4 animate-spin" />
                    <span>Активация...</span>
                  </>
                ) : (
                  <>
                    <Key className="w-4 h-4" />
                    <span>Войти по инвайту</span>
                  </>
                )}
              </button>
            </form>
          </div>

          <div className="text-center text-[11px] text-[#8c8c7a] pb-2">
            Lares Homeshare • Защищённое файловое хранилище • Доступ по персональным инвайтам
          </div>
        </div>
      )}

      {/* Admin Login Modal */}
      {showLoginModal && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-xs z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-3xl p-4 sm:p-6 md:p-8 max-w-md w-full max-h-[calc(100vh-2rem)] overflow-y-auto shadow-2xl border border-[#e2e2d5] relative animate-in fade-in zoom-in duration-200">
            <button 
              onClick={() => {
                setShowLoginModal(false);
                if (isAdminPath && userRole !== 'admin') navigateTo('/');
              }}
              className="absolute top-4 right-4 p-2 rounded-full text-[#8c8c7a] hover:bg-[#f0f0e0] transition-colors cursor-pointer"
            >
              <X className="w-4 h-4" />
            </button>

            <div className="flex items-center gap-3 mb-5">
              <div className="w-11 h-11 rounded-2xl bg-[#f0f0e0] border border-[#e2e2d5] text-[#5A5A40] flex items-center justify-center shrink-0 shadow-xs">
                <LogIn className="w-6 h-6 text-[#5A5A40]" />
              </div>
              <div>
                <h3 className="font-serif text-xl font-bold text-[#1a1a15]">Вход для Администратора</h3>
                <p className="text-xs text-[#8c8c7a]">Вход в панель управления Lares (/admin)</p>
              </div>
            </div>

            <form onSubmit={handleAdminLogin} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-[#5A5A40] uppercase tracking-wider mb-1.5">
                  Логин администратора
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
                  Код TOTP (2FA)
                </label>
                <input 
                  type="text"
                  value={loginTotpInput}
                  onChange={(e) => setLoginTotpInput(e.target.value)}
                  placeholder="6-значный код (например 123456)"
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
                  onClick={() => {
                    setShowLoginModal(false);
                    if (isAdminPath && userRole !== 'admin') navigateTo('/');
                  }}
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
    </>
  );
}
