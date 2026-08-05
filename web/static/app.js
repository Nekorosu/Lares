// Lares Vanilla JS Client
const CHUNK_SIZE = 32 * 1024 * 1024; // 32MB

document.addEventListener('DOMContentLoaded', () => {
  initUploader();
  initZipDownloader();
});

function initUploader() {
  const dropzone = document.getElementById('dropzone');
  const fileInput = document.getElementById('fileInput');
  if (!dropzone || !fileInput) return;

  dropzone.addEventListener('click', () => fileInput.click());
  dropzone.addEventListener('dragover', (e) => {
    e.preventDefault();
    dropzone.classList.add('dragover');
  });
  dropzone.addEventListener('dragleave', () => dropzone.classList.remove('dragover'));
  dropzone.addEventListener('drop', (e) => {
    e.preventDefault();
    dropzone.classList.remove('dragover');
    if (e.dataTransfer.files.length > 0) {
      handleFiles(e.dataTransfer.files);
    }
  });

  fileInput.addEventListener('change', () => {
    if (fileInput.files.length > 0) {
      handleFiles(fileInput.files);
    }
  });
}

async function handleFiles(files) {
  const expirySelect = document.getElementById('expirySelect');
  const expiryDays = expirySelect ? parseInt(expirySelect.value) : 14;

  for (let i = 0; i < files.length; i++) {
    await uploadFileChunked(files[i], expiryDays);
  }
}

async function uploadFileChunked(file, expiryDays) {
  const statusEl = document.getElementById('uploadStatus');
  const progressEl = document.getElementById('uploadProgress');
  if (statusEl) statusEl.textContent = `Зазервирование места для ${file.name}...`;

  try {
    // 1. Create Upload Reservation
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
      const errData = await res.json();
      throw new Error(errData.error || 'Ошибка резервирования загрузки');
    }

    const data = await res.json();
    const uploadId = data.upload_id;
    const uploadSecret = data.upload_secret;

    // Save secrets in localStorage for resume/cancel
    localStorage.setItem(`lares_upload_${uploadId}`, JSON.stringify({
      id: uploadId,
      secret: uploadSecret,
      filename: file.name
    }));

    // 2. Upload Chunks
    let offset = 0;
    while (offset < file.size) {
      const chunkEnd = Math.min(offset + CHUNK_SIZE, file.size);
      const chunk = file.slice(offset, chunkEnd);

      const percent = Math.round((offset / file.size) * 100);
      if (statusEl) statusEl.textContent = `Загрузка ${file.name}: ${percent}%`;
      if (progressEl) progressEl.style.width = `${percent}%`;

      const patchRes = await fetch(`/api/uploads/${uploadId}?offset=${offset}`, {
        method: 'PATCH',
        headers: {
          'X-Upload-Secret': uploadSecret,
          'Content-Type': 'application/octet-stream'
        },
        body: chunk
      });

      if (!patchRes.ok) {
        const errData = await patchRes.json();
        throw new Error(errData.error || 'Ошибка передачи фрагмента файла');
      }

      offset = chunkEnd;
    }

    // 3. Complete Upload
    if (statusEl) statusEl.textContent = `Финализация ${file.name}...`;
    const compRes = await fetch(`/api/uploads/${uploadId}/complete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ upload_secret: uploadSecret })
    });

    if (!compRes.ok) {
      const errData = await compRes.json();
      throw new Error(errData.error || 'Ошибка завершения загрузки');
    }

    localStorage.removeItem(`lares_upload_${uploadId}`);
    if (statusEl) statusEl.textContent = `Файл ${file.name} успешно загружен!`;
    setTimeout(() => window.location.reload(), 1000);

  } catch (err) {
    if (statusEl) statusEl.textContent = `Ошибка: ${err.message}`;
    alert(`Ошибка загрузки ${file.name}: ${err.message}`);
  }
}

function initZipDownloader() {
  const downloadZipBtn = document.getElementById('downloadZipBtn');
  if (!downloadZipBtn) return;

  downloadZipBtn.addEventListener('click', () => {
    const checkboxes = document.querySelectorAll('.file-checkbox:checked');
    if (checkboxes.length === 0) {
      alert('Выберите хотя бы один файл для скачивания в ZIP');
      return;
    }
    const ids = Array.from(checkboxes).map(cb => cb.value);
    window.location.href = `/api/zip?ids=${ids.join(',')}`;
  });
}
