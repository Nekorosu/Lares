import React, { useState } from 'react';
import { 
  Server, 
  Terminal, 
  ShieldCheck, 
  HardDrive, 
  FileText, 
  CheckCircle2, 
  Copy, 
  ExternalLink, 
  Cpu, 
  Upload, 
  Download, 
  Key, 
  Users, 
  Clock, 
  Archive, 
  Lock, 
  AlertTriangle,
  Settings,
  Activity,
  Layers
} from 'lucide-react';

export default function App() {
  const [activeTab, setActiveTab] = useState<'dashboard' | 'guide' | 'config' | 'architecture'>('dashboard');
  const [copiedSection, setCopiedSection] = useState<string | null>(null);

  // Config builder state
  const [cfgListen, setCfgListen] = useState('127.0.0.1:8090');
  const [cfgBaseUrl, setCfgBaseUrl] = useState('https://files.example.duckdns.org');
  const [cfgDataDir, setCfgDataDir] = useState('/srv/media/fileshare/data');
  const [cfgStorageQuota, setCfgStorageQuota] = useState('100');
  const [cfgUploadLimit, setCfgUploadLimit] = useState('200');
  const [cfgDownloadLimit, setCfgDownloadLimit] = useState('300');

  const copyToClipboard = (text: string, sectionId: string) => {
    navigator.clipboard.writeText(text);
    setCopiedSection(sectionId);
    setTimeout(() => setCopiedSection(null), 2000);
  };

  const generatedYaml = `# Homeshare Configuration File
listen: "${cfgListen}"
base_url: "${cfgBaseUrl}"

paths:
  data_dir: "${cfgDataDir}"
  tmp_dir: "${cfgDataDir.replace('/data', '/tmp')}"
  db_path: "${cfgDataDir.replace('/data', '/db')}/homeshare.db"
  backup_dir: "/home/fileshare-backup"
  security_log: "/var/log/homeshare/security.log"

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
            h
          </div>
          <div>
            <h1 className="font-serif text-2xl font-semibold tracking-tight text-[#1a1a15]">homeshare</h1>
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
            Дашборд (Preview)
          </button>
          <button 
            onClick={() => setActiveTab('guide')}
            className={`px-4 py-2 rounded-full text-xs font-semibold transition-all flex items-center gap-2 ${
              activeTab === 'guide' ? 'bg-[#5A5A40] text-white shadow-xs' : 'text-[#5A5A40] hover:bg-[#e2e2d5]'
            }`}
          >
            <Terminal className="w-3.5 h-3.5" />
            Инструкция по запуску
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
        {/* TAB 1: DASHBOARD PREVIEW */}
        {activeTab === 'dashboard' && (
          <div className="space-y-6">
            {/* Server Status Banner */}
            <div className="bg-[#5A5A40] text-white p-6 rounded-3xl shadow-sm flex flex-col md:flex-row justify-between items-start md:items-center gap-4">
              <div className="space-y-1">
                <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-white/10 text-xs font-medium text-[#f5f5f0]">
                  <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
                  Go Backend Architecture (Caddy Reverse Proxy)
                </div>
                <h2 className="font-serif text-2xl font-medium">Сервер Homeshare готовит файлы для обмена</h2>
                <p className="text-sm text-[#e2e2d5]">
                  Безопасный файлообменник для семейной или корпоративной сети с токенами доступа, квотами и фильтрацией расширений.
                </p>
              </div>
              <button 
                onClick={() => setActiveTab('guide')}
                className="px-5 py-2.5 rounded-full bg-white text-[#5A5A40] text-xs font-bold hover:bg-[#f5f5f0] transition-colors flex items-center gap-2 shrink-0"
              >
                Как запустить на сервере <ExternalLink className="w-3.5 h-3.5" />
              </button>
            </div>

            {/* Metrics Grid */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
              {/* Storage Meter */}
              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm hover:shadow-md transition-shadow">
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[#8c8c7a]">Хранилище диска</span>
                  <HardDrive className="w-4 h-4 text-[#5A5A40]" />
                </div>
                <div className="text-3xl font-semibold text-[#5A5A40] font-sans">84.2 GB</div>
                <div className="text-xs text-[#8c8c7a] mt-1">из 100.0 GB выделенной квоты</div>
                <div className="w-full h-2.5 bg-[#e2e2d5] rounded-full overflow-hidden mt-4">
                  <div className="h-full bg-[#5A5A40] rounded-full" style={{ width: '84%' }}></div>
                </div>
              </div>

              {/* Monthly Traffic */}
              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm hover:shadow-md transition-shadow">
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[#8c8c7a]">Внешний Трафик</span>
                  <Activity className="w-4 h-4 text-[#5A5A40]" />
                </div>
                <div className="text-3xl font-semibold text-[#5A5A40] font-sans">142.5 GB</div>
                <div className="text-xs text-[#8c8c7a] mt-1">Загрузка: 58.1 GB | Скачивание: 84.4 GB</div>
                <div className="w-full h-2.5 bg-[#e2e2d5] rounded-full overflow-hidden mt-4">
                  <div className="h-full bg-[#5A5A40] rounded-full" style={{ width: '47%' }}></div>
                </div>
              </div>

              {/* Active Sessions */}
              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm hover:shadow-md transition-shadow">
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-semibold uppercase tracking-wider text-[#8c8c7a]">Активные сессии</span>
                  <Users className="w-4 h-4 text-[#5A5A40]" />
                </div>
                <div className="text-3xl font-semibold text-[#5A5A40] font-sans">12 устройств</div>
                <div className="text-xs text-[#8c8c7a] mt-1">5 локальных (LAN), 7 внешних (HTTPS)</div>
                <div className="flex gap-1.5 mt-4">
                  <span className="w-2.5 h-2.5 rounded-full bg-[#5A5A40]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#5A5A40]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#5A5A40]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#d4a373]"></span>
                  <span className="w-2.5 h-2.5 rounded-full bg-[#e2e2d5]"></span>
                </div>
              </div>
            </div>

            {/* File Table Showcase */}
            <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm">
              <div className="flex justify-between items-center mb-4">
                <h3 className="font-serif text-xl text-[#1a1a15]">Последние загруженные файлы</h3>
                <span className="text-xs text-[#5A5A40] font-semibold cursor-pointer hover:underline">
                  Все файлы (248)
                </span>
              </div>

              <div className="overflow-x-auto">
                <table className="w-full text-left border-collapse">
                  <thead>
                    <tr className="border-b border-[#f0f0e0]">
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Имя файла</th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Размер</th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Загрузил</th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Срок хранения</th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider">Статус</th>
                      <th className="py-3 px-4 text-xs font-semibold text-[#8c8c7a] uppercase tracking-wider text-right">Действие</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[#f5f5f0] text-sm">
                    <tr className="hover:bg-[#fcfcf9] transition-colors">
                      <td className="py-3.5 px-4 font-medium text-[#1a1a15]">vacation_july_2024.mp4</td>
                      <td className="py-3.5 px-4 text-[#8c8c7a]">1.2 GB</td>
                      <td className="py-3.5 px-4 text-[#1a1a15]">mikhail_ivanov</td>
                      <td className="py-3.5 px-4 text-[#8c8c7a]">14 дней</td>
                      <td className="py-3.5 px-4">
                        <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold bg-[#5A5A40] text-white">
                          Готов
                        </span>
                      </td>
                      <td className="py-3.5 px-4 text-right">
                        <button className="px-3 py-1 rounded-full bg-[#e2e2d5] text-[#1a1a15] text-xs font-medium hover:bg-[#d1d1c1] transition-colors">
                          Скачать
                        </button>
                      </td>
                    </tr>

                    <tr className="hover:bg-[#fcfcf9] transition-colors">
                      <td className="py-3.5 px-4 font-medium text-[#1a1a15]">homeshare_db_backup.sql</td>
                      <td className="py-3.5 px-4 text-[#8c8c7a]">42.8 MB</td>
                      <td className="py-3.5 px-4 text-[#1a1a15]">system_auto</td>
                      <td className="py-3.5 px-4 text-[#8c8c7a]">Бессрочно</td>
                      <td className="py-3.5 px-4">
                        <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold bg-[#5A5A40] text-white">
                          Защищен
                        </span>
                      </td>
                      <td className="py-3.5 px-4 text-right">
                        <button className="px-3 py-1 rounded-full bg-[#e2e2d5] text-[#1a1a15] text-xs font-medium hover:bg-[#d1d1c1] transition-colors">
                          Скачать
                        </button>
                      </td>
                    </tr>

                    <tr className="hover:bg-[#fcfcf9] transition-colors">
                      <td className="py-3.5 px-4 font-medium text-[#1a1a15]">setup_installer.exe</td>
                      <td className="py-3.5 px-4 text-[#8c8c7a]">145.2 MB</td>
                      <td className="py-3.5 px-4 text-[#1a1a15]">guest_user_2</td>
                      <td className="py-3.5 px-4 text-[#8c8c7a]">7 дней</td>
                      <td className="py-3.5 px-4">
                        <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold bg-[#d4a373] text-white">
                          Карантин
                        </span>
                      </td>
                      <td className="py-3.5 px-4 text-right">
                        <button className="px-3 py-1 rounded-full bg-[#5A5A40] text-white text-xs font-medium hover:bg-[#484833] transition-colors">
                          Проверить
                        </button>
                      </td>
                    </tr>
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
              <h2 className="font-serif text-3xl text-[#1a1a15]">Инструкция по развертыванию Homeshare на своем сервере</h2>
              <p className="text-sm text-[#8c8c7a] mt-1">
                Пошаговое руководство по сборке Go-бинарника, настройке SQLite, Caddy и systemd службы на Linux VPS/сервере.
              </p>
            </div>

            {/* Step 1: Requirements & Pre-requisites */}
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
                  onClick={() => copyToClipboard(`git clone https://github.com/your-user/homeshare.git
cd homeshare
go build -o homeshare main.go`, 'step1')}
                  className="absolute top-3 right-3 px-2.5 py-1 rounded bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1 text-[11px]"
                >
                  {copiedSection === 'step1' ? <CheckCircle2 className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  Копировать
                </button>
                <pre>{`# 1. Клонирование репозитория и переход в директорию
git clone https://github.com/your-repo/homeshare.git
cd homeshare

# 2. Сборка бинарного файла Homeshare
go build -o homeshare main.go

# 3. Создание необходимой директории на сервере
sudo mkdir -p /srv/media/fileshare/{data,tmp,db}
sudo mkdir -p /etc/homeshare /var/log/homeshare /home/fileshare-backup
sudo chmod -R 750 /srv/media/fileshare /etc/homeshare`}</pre>
              </div>
            </div>

            {/* Step 2: Config file */}
            <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-4">
              <div className="flex items-center gap-3 border-b border-[#f0f0e0] pb-3">
                <div className="w-8 h-8 rounded-full bg-[#5A5A40] text-white font-bold flex items-center justify-center text-sm">
                  2
                </div>
                <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Конфигурирование (/etc/homeshare/config.yaml)</h3>
              </div>
              <p className="text-sm text-[#475569]">
                Скопируйте пример файла конфигурации или сгенерируйте собственный во вкладке «Конфигуратор».
              </p>

              <div className="bg-[#1e293b] text-[#f8fafc] p-4 rounded-2xl font-mono text-xs relative overflow-x-auto">
                <button 
                  onClick={() => copyToClipboard(`sudo cp config.yaml.example /etc/homeshare/config.yaml
sudo chmod 640 /etc/homeshare/config.yaml`, 'step2')}
                  className="absolute top-3 right-3 px-2.5 py-1 rounded bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1 text-[11px]"
                >
                  {copiedSection === 'step2' ? <CheckCircle2 className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  Копировать
                </button>
                <pre>{`sudo cp config.yaml.example /etc/homeshare/config.yaml
sudo nano /etc/homeshare/config.yaml`}</pre>
              </div>
            </div>

            {/* Step 3: Systemd service */}
            <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-4">
              <div className="flex items-center gap-3 border-b border-[#f0f0e0] pb-3">
                <div className="w-8 h-8 rounded-full bg-[#5A5A40] text-white font-bold flex items-center justify-center text-sm">
                  3
                </div>
                <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Автозапуск через Systemd Service</h3>
              </div>
              <p className="text-sm text-[#475569]">
                Установите службу `homeshare.service`, чтобы файлообменник автоматически запускался при старте системы.
              </p>

              <div className="bg-[#1e293b] text-[#f8fafc] p-4 rounded-2xl font-mono text-xs relative overflow-x-auto">
                <button 
                  onClick={() => copyToClipboard(`sudo cp homeshare /usr/local/bin/homeshare
sudo cp homeshare.service /etc/systemd/system/
sudo useradd -r -s /bin/false homeshare
sudo chown -R homeshare:homeshare /srv/media/fileshare /etc/homeshare /var/log/homeshare
sudo systemctl daemon-reload
sudo systemctl enable --now homeshare`, 'step3')}
                  className="absolute top-3 right-3 px-2.5 py-1 rounded bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1 text-[11px]"
                >
                  {copiedSection === 'step3' ? <CheckCircle2 className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  Копировать
                </button>
                <pre>{`# 1. Перемещение бинарного файла и файла сервиса
sudo cp homeshare /usr/local/bin/homeshare
sudo cp homeshare.service /etc/systemd/system/

# 2. Создание системного пользователя и запуск
sudo useradd -r -s /bin/false homeshare
sudo chown -R homeshare:homeshare /srv/media/fileshare /etc/homeshare /var/log/homeshare
sudo systemctl daemon-reload
sudo systemctl enable --now homeshare
sudo systemctl status homeshare`}</pre>
              </div>
            </div>

            {/* Step 4: Caddy Proxy */}
            <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-4">
              <div className="flex items-center gap-3 border-b border-[#f0f0e0] pb-3">
                <div className="w-8 h-8 rounded-full bg-[#5A5A40] text-white font-bold flex items-center justify-center text-sm">
                  4
                </div>
                <h3 className="font-serif text-xl font-semibold text-[#1a1a15]">Reverse Proxy (Caddy / Nginx) & SSL</h3>
              </div>
              <p className="text-sm text-[#475569]">
                Caddy автоматически выпустит бесплатный SSL сертификат Let's Encrypt и настроит перенаправление HTTP Header IP для аудита.
              </p>

              <div className="bg-[#1e293b] text-[#f8fafc] p-4 rounded-2xl font-mono text-xs relative overflow-x-auto">
                <button 
                  onClick={() => copyToClipboard(`files.yourdomain.com {
    reverse_proxy 127.0.0.1:8090 {
        header_up X-Real-IP {http.request.remote.host}
        header_up X-Forwarded-Proto {scheme}
    }
}`, 'step4')}
                  className="absolute top-3 right-3 px-2.5 py-1 rounded bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1 text-[11px]"
                >
                  {copiedSection === 'step4' ? <CheckCircle2 className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  Копировать
                </button>
                <pre>{`# /etc/caddy/Caddyfile
files.yourdomain.com {
    reverse_proxy 127.0.0.1:8090 {
        header_up X-Real-IP {http.request.remote.host}
        header_up X-Forwarded-Proto {scheme}
    }
}`}</pre>
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
                    className="px-3 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-white transition-colors flex items-center gap-1.5 text-xs font-sans font-medium"
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
              <h2 className="font-serif text-3xl text-[#1a1a15]">Архитектура безопасности Homeshare</h2>
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
                  При загрузке файлов с опасными расширениями статус файла помечается как <code className="bg-[#f0f0e0] px-1.5 py-0.5 rounded text-[#1a1a15]">quarantined</code>. Файл невидим для обычных пользователей до тех пор, пока администратор не утвердит его в панели управления.
                </p>
              </div>

              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-3">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center">
                    <Lock className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="font-serif text-lg font-semibold text-[#1a1a15]">TOTP 2FA и Ограничение попыток (Rate Limit)</h3>
                    <p className="text-xs text-[#8c8c7a]">Защита от подобрать пароли администратора</p>
                  </div>
                </div>
                <p className="text-xs text-[#475569] leading-relaxed">
                  Вход администратора защищен двухфакторной аутентификацией Google Authenticator / 2FA TOTP. При 5 неверных попытках входа подряд IP заносится в блокировку на 1 час с репортом в security.log.
                </p>
              </div>

              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-3">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center">
                    <Key className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="font-serif text-lg font-semibold text-[#1a1a15]">Одноразовые Инвайт-коды</h3>
                    <p className="text-xs text-[#8c8c7a]">16-значные коды подключения с лимитом активаций</p>
                  </div>
                </div>
                <p className="text-xs text-[#475569] leading-relaxed">
                  Пользователи привязываются к системе с помощью одноразовых инвайтов. Сами коды хранятся в БД SQLite исключительно в виде SHA256 хэшей.
                </p>
              </div>

              <div className="bg-white p-6 rounded-3xl border border-[#f0f0e0] shadow-sm space-y-3">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-2xl bg-[#5A5A40]/10 text-[#5A5A40] flex items-center justify-center">
                    <Archive className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="font-serif text-lg font-semibold text-[#1a1a15]">Потоковое формирование ZIP</h3>
                    <p className="text-xs text-[#8c8c7a]">Генерация архивов без создания временных копий на диске</p>
                  </div>
                </div>
                <p className="text-xs text-[#475569] leading-relaxed">
                  Скачивание нескольких файлов одновременно производит динамический ZIP-стриминг с поддержкой ограничения скорости для внешней сети.
                </p>
              </div>
            </div>
          </div>
        )}
      </main>

      {/* Footer */}
      <footer className="bg-white border-t border-[#e2e2d5] py-4 px-6 text-center text-xs text-[#8c8c7a] flex flex-col sm:flex-row justify-between items-center gap-2">
        <span>Homeshare Go Service v1.24.0-stable • GNU GPL v3.0 License</span>
        <span>Абсолютная автономность • Вся документация в README.md</span>
      </footer>
    </div>
  );
}
