'use strict';

const csrf = () => document.querySelector('meta[name="csrf-token"]')?.content || '';
const records = new Map();
const active = new Map();
const key = id => 'lares_upload_' + id;
const host = document.getElementById('transfers');
const personId = host?.dataset.personId;

async function api(path, options = {}) {
  const headers = { ...options.headers, 'X-CSRF-Token': csrf() };
  const res = await fetch(path, { credentials: 'same-origin', ...options, headers });
  if (!res.ok) {
    const body = await res.text();
    let message = body || 'Ошибка запроса';
    try { message = JSON.parse(body).error || message; } catch {}
    const error = new Error(message);
    error.status = res.status;
    throw error;
  }
  return res;
}

function save(record) {
  localStorage.setItem(key(record.id), JSON.stringify(record));
  records.set(record.id, record);
}

function remove(record) {
  try { localStorage.removeItem(key(record.id)); } catch {}
  records.delete(record.id);
  record.element?.remove();
}

function card(record) {
  if (!host) return;
  const element = document.createElement('section');
  element.className = 'transfer';
  const name = document.createElement('strong');
  name.textContent = record.name;
  const state = document.createElement('p');
  state.textContent = 'Выберите этот файл, чтобы продолжить загрузку.';
  const progress = document.createElement('progress');
  progress.max = record.size || 1;
  progress.value = 0;
  const select = document.createElement('input');
  select.type = 'file';
  select.setAttribute('aria-label', 'Продолжить ' + record.name);
  select.addEventListener('change', () => {
    const file = select.files[0];
    if (!file) return;
    if (file.name !== record.name || file.size !== record.size || file.lastModified !== record.modified) {
      state.textContent = 'Нужен тот же исходный файл с неизменённым размером и датой.';
      return;
    }
    send(record, file);
  });
  const cancel = document.createElement('button');
  cancel.textContent = 'Отменить';
  cancel.type = 'button';
  cancel.addEventListener('click', async () => {
    cancel.disabled = true;
    active.get(record.id)?.abort();
    try {
      await api('/api/uploads/' + record.id, {
        method: 'DELETE', headers: { 'X-Upload-Secret': record.secret }
      });
      remove(record);
    } catch (error) {
      if ([409, 410, 404].includes(error.status)) remove(record);
      else { state.textContent = error.message; cancel.disabled = false; }
    }
  });
  element.append(name, state, progress, select, cancel);
  host.append(element);
  // DOM properties must not enter the persisted JSON record.
  Object.defineProperties(record, {
    element: { value: element }, state: { value: state },
    progress: { value: progress }, select: { value: select }
  });
}

async function send(record, file) {
  if (active.has(record.id)) return;
  const controller = new AbortController();
  active.set(record.id, controller);
  record.select.disabled = true;
  const headers = { 'X-Upload-Secret': record.secret };
  try {
    const head = await api('/api/uploads/' + record.id, { method: 'HEAD', headers, signal: controller.signal });
    let offset = Number(head.headers.get('Upload-Offset'));
    if (!Number.isSafeInteger(offset) || offset < 0 || offset > file.size) throw new Error('Неверная позиция загрузки');
    record.progress.value = offset;
    while (offset < file.size) {
      const end = Math.min(offset + 32 * 1024 * 1024, file.size);
      record.state.textContent = 'Загрузка ' + Math.floor(100 * offset / file.size) + '%';
      const res = await api('/api/uploads/' + record.id + '?offset=' + offset, {
        method: 'PATCH', headers: { ...headers, 'Content-Type': 'application/octet-stream' },
        body: file.slice(offset, end), signal: controller.signal
      });
      offset = Number(res.headers.get('Upload-Offset'));
      if (!Number.isSafeInteger(offset) || offset !== end) throw new Error('Неверная позиция загрузки');
      record.progress.value = offset;
    }
    await api('/api/uploads/' + record.id + '/complete', { method: 'POST', headers, signal: controller.signal });
    remove(record);
    const note = document.createElement('p');
    note.textContent = 'Файл «' + file.name + '» сохранён. ';
    const link = document.createElement('a');
    link.href = '/';
    link.textContent = 'Обновить список';
    note.append(link);
    host.append(note);
  } catch (error) {
    record.state.textContent = error.name === 'AbortError' ? 'Передача остановлена.' :
      error.message + ' Выберите исходный файл для повторного продолжения.';
  } finally {
    active.delete(record.id);
    record.select.disabled = false;
  }
}

async function start(file) {
  const record = { name: file.name, size: file.size, modified: file.lastModified, personId };
  try {
    const expiry = Number(document.getElementById('expiry').value);
    const res = await api('/api/uploads', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ filename: file.name, size: file.size, content_type: file.type,
        expiry_days: expiry, keep_forever: expiry === 0 })
    });
    const data = await res.json();
    record.id = data.upload_id;
    record.secret = data.upload_secret;
    try { save(record); }
    catch {
      await api('/api/uploads/' + record.id, { method: 'DELETE', headers: { 'X-Upload-Secret': record.secret } });
      throw new Error('Не удалось сохранить доступ к загрузке в браузере. Проверьте разрешение на локальное хранение.');
    }
    card(record);
    await send(record, file);
  } catch (error) { alert(error.message); }
}

async function files(list) { for (const file of list) await start(file); }

async function restore() {
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const name = localStorage.key(i);
      if (!name?.startsWith('lares_upload_')) continue;
      try {
        const record = JSON.parse(localStorage.getItem(name));
        if (/^[a-f0-9]{32}$/.test(record.id) && typeof record.secret === 'string' && record.personId === personId) {
          records.set(record.id, record);
          card(record);
        }
      } catch {}
    }
  } catch {
    const note = document.createElement('p');
    note.textContent = 'Браузер запретил сохранение загрузок. Разрешите локальное хранение для продолжения.';
    host.append(note);
  }
  try {
    const uploads = await (await api('/api/uploads')).json();
    const ids = new Set(uploads.map(upload => upload.id));
    for (const record of records.values()) if (!ids.has(record.id)) remove(record);
    for (const upload of uploads) {
      if (records.has(upload.id)) continue;
      const note = document.createElement('p');
      note.textContent = '«' + upload.original_name + '»: продолжите в исходном браузере или попросите администратора отменить загрузку. Резерв истекает ' +
        new Date(upload.reservation_expires_at).toLocaleString('ru-RU') + '.';
      host.append(note);
    }
  } catch (error) {
    const note = document.createElement('p');
    note.textContent = 'Не удалось проверить активные загрузки: ' + error.message;
    host.append(note);
  }
}

document.querySelectorAll('form[data-confirm]').forEach(form => form.addEventListener('submit', event => {
  if (!confirm(form.dataset.confirm)) event.preventDefault();
}));

const input = document.getElementById('file-input');
if (input) {
  // Restore first so an in-flight list response cannot remove a newly started upload card.
  input.disabled = true;
  const ready = restore().finally(() => { input.disabled = false; });
  input.addEventListener('change', () => files([...input.files]));
  const drop = document.getElementById('dropzone');
  drop.addEventListener('dragover', event => { event.preventDefault(); drop.classList.add('dragging'); });
  drop.addEventListener('dragleave', () => drop.classList.remove('dragging'));
  drop.addEventListener('drop', async event => {
    event.preventDefault();
    drop.classList.remove('dragging');
    const selected = [...event.dataTransfer.files];
    await ready;
    files(selected);
  });
  drop.addEventListener('keydown', event => { if (event.key === 'Enter') input.click(); });
}

document.getElementById('zip-download')?.addEventListener('click', () => {
  const ids = [...document.querySelectorAll('.zip-check:checked')].map(element => element.value);
  if (!ids.length) { alert('Выберите файлы'); return; }
  window.location.href = '/api/zip?ids=' + encodeURIComponent(ids.join(','));
});
