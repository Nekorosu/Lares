const CHUNK_SIZE = 32 * 1024 * 1024; // 32 MB chunks

document.addEventListener('DOMContentLoaded', () => {
  const dropzone = document.getElementById('dropzone');
  const fileInput = document.getElementById('fileInput');

  if (!dropzone || !fileInput) return;

  dropzone.addEventListener('click', () => fileInput.click());

  dropzone.addEventListener('dragover', (e) => {
    e.preventDefault();
    dropzone.classList.add('dragover');
  });

  dropzone.addEventListener('dragleave', () => {
    dropzone.classList.remove('dragover');
  });

  dropzone.addEventListener('drop', (e) => {
    e.preventDefault();
    dropzone.classList.remove('dragover');
    if (e.dataTransfer.files.length > 0) {
      handleFileSelect(e.dataTransfer.files[0]);
    }
  });

  fileInput.addEventListener('change', (e) => {
    if (e.target.files.length > 0) {
      handleFileSelect(e.target.files[0]);
    }
  });
});

async function handleFileSelect(file) {
  const expiryDaysSelect = document.getElementById('expiryDays');
  const expiryDays = expiryDaysSelect ? parseInt(expiryDaysSelect.value, 10) : 14;

  const progressSection = document.getElementById('progressSection');
  const progressBar = document.getElementById('progressBar');
  const progressStatus = document.getElementById('progressStatus');
  const cancelBtn = document.getElementById('cancelUploadBtn');

  if (progressSection) progressSection.style.display = 'block';
  if (progressStatus) progressStatus.innerText = 'Создание загрузки...';

  try {
    // 1. Create upload session
    const res = await fetch('/api/uploads', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        filename: file.name,
        size: file.size,
        content_type: file.type || 'application/octet-stream',
        expiry_days: expiryDays
      })
    });

    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      throw new Error(data.error || 'Ошибка инициализации загрузки');
    }

    const { upload_id, upload_secret } = await res.json();

    // Store in localStorage for resume/cancel
    localStorage.setItem(`upload_${upload_id}`, upload_secret);

    let offset = 0;
    let isCanceled = false;

    if (cancelBtn) {
      cancelBtn.onclick = async () => {
        isCanceled = true;
        await fetch(`/api/uploads/${upload_id}`, {
          method: 'DELETE',
          headers: { 'X-Upload-Secret': upload_secret }
        });
        localStorage.removeItem(`upload_${upload_id}`);
        if (progressStatus) progressStatus.innerText = 'Загрузка отменена';
      };
    }

    // 2. Upload chunks sequentially
    while (offset < file.size) {
      if (isCanceled) break;

      const chunkEnd = Math.min(offset + CHUNK_SIZE, file.size);
      const chunk = file.slice(offset, chunkEnd);

      const patchRes = await fetch(`/api/uploads/${upload_id}?offset=${offset}`, {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/octet-stream',
          'X-Upload-Secret': upload_secret
        },
        body: chunk
      });

      if (!patchRes.ok) {
        const errData = await patchRes.json().catch(() => ({}));
        throw new Error(errData.error || 'Ошибка загрузки фрагмента');
      }

      offset = chunkEnd;
      const percent = Math.round((offset / file.size) * 100);
      if (progressBar) progressBar.style.width = `${percent}%`;
      if (progressStatus) progressStatus.innerText = `Загружено ${percent}% (${formatBytes(offset)} / ${formatBytes(file.size)})`;
    }

    if (isCanceled) return;

    // 3. Complete upload
    const compRes = await fetch(`/api/uploads/${upload_id}/complete`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Upload-Secret': upload_secret
      }
    });

    if (!compRes.ok) {
      const errData = await compRes.json().catch(() => ({}));
      throw new Error(errData.error || 'Ошибка завершения загрузки');
    }

    localStorage.removeItem(`upload_${upload_id}`);
    if (progressStatus) progressStatus.innerText = 'Загрузка успешно завершена!';
    setTimeout(() => window.location.reload(), 1000);

  } catch (err) {
    if (progressStatus) progressStatus.innerText = `Ошибка: ${err.message}`;
    alert(`Ошибка загрузки: ${err.message}`);
  }
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}
