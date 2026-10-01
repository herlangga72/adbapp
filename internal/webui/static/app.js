'use strict';

const $ = (id) => document.getElementById(id);
const state = { apks: [], packages: [], selectedApks: new Set(), selectedPkgs: new Set(), jobs: new Map() };

function toast(message, isError) {
  const el = $('toast');
  el.textContent = message;
  el.style.background = isError ? '#7f1d1d' : '#111827';
  el.classList.remove('hidden');
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => el.classList.add('hidden'), 4000);
}

async function api(path, options) {
  const res = await fetch(path, options);
  if (!res.ok) {
    let detail = res.statusText;
    try { detail = (await res.json()).error || detail; } catch (e) { /* biarkan */ }
    throw new Error(detail);
  }
  if (res.headers.get('content-type')?.includes('application/json')) return res.json();
  return res.text();
}

function switchTab(group, name) {
  document.querySelectorAll('.tabs').forEach((nav) => {
    if (!nav.contains(document.querySelector(`[data-tab="${name}"]`))) return;
    nav.querySelectorAll('.tab').forEach((t) => t.classList.toggle('active', t.dataset.tab === name));
  });
  document.querySelectorAll('.panel').forEach((p) => p.classList.remove('active'));
  const panel = $(`tab-${name}`);
  if (panel) panel.classList.add('active');
}

document.querySelectorAll('.tab').forEach((tab) => {
  tab.addEventListener('click', () => switchTab(tab.parentElement, tab.dataset.tab));
});

function humanSize(bytes) {
  if (!bytes) return '-';
  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0, v = bytes;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function renderDevice(status) {
  const dot = $('dot');
  dot.className = 'dot ' + status.state;
  const label = $('device-label');
  const hint = $('hint');
  if (status.state === 'ready') {
    label.textContent = `${status.model || status.serial} · siap`;
    hint.classList.add('hidden');
  } else if (status.state === 'unauthorized') {
    label.textContent = 'Perangkat belum diizinkan';
    hint.textContent = 'Lihat layar HP dan tekan "Allow" untuk USB debugging, lalu pindai ulang.';
    hint.classList.remove('hidden');
  } else if (status.state === 'offline') {
    label.textContent = 'Perangkat offline';
    hint.textContent = 'Cek kabel USB, pilih mode File Transfer, dan pastikan USB debugging menyala.';
    hint.classList.remove('hidden');
  } else {
    label.textContent = 'Tidak ada perangkat';
    hint.textContent = 'Sambungkan HP dengan kabel USB dan pastikan USB debugging menyala.';
    hint.classList.remove('hidden');
  }
}

function renderApks() {
  const tbody = $('apk-table').querySelector('tbody');
  tbody.innerHTML = '';
  state.apks.forEach((apk) => {
    const tr = document.createElement('tr');
    if (state.selectedApks.has(apk.path)) tr.classList.add('selected');
    tr.innerHTML = `
      <td><input type="checkbox" ${state.selectedApks.has(apk.path) ? 'checked' : ''}></td>
      <td>${apk.name}</td>
      <td>${apk.package || '<span class="muted">tidak terbaca</span>'}</td>
      <td>${apk.versionName || '-'}</td>
      <td>${humanSize(apk.size)}</td>`;
    if (apk.error) tr.title = apk.error;
    tr.querySelector('input').addEventListener('change', (e) => {
      if (e.target.checked) state.selectedApks.add(apk.path); else state.selectedApks.delete(apk.path);
      renderApks();
    });
    tbody.appendChild(tr);
  });
  $('install-selected').textContent = `Pasang terpilih (${state.selectedApks.size})`;
  $('install-selected').disabled = state.selectedApks.size === 0;
}

function addApk(entry) {
  const existing = state.apks.findIndex((a) => a.path === entry.path);
  if (existing >= 0) state.apks[existing] = entry; else state.apks.push(entry);
  renderApks();
}

function renderPackages() {
  const term = $('search').value.toLowerCase();
  const sortMode = $('sort').value;
  const tbody = $('pkg-table').querySelector('tbody');
  tbody.innerHTML = '';

  let rows = state.packages.filter((p) => p.name.toLowerCase().includes(term));
  if (!sortMode.startsWith('system')) rows = rows.slice().sort((a, b) => a.name.localeCompare(b.name));

  rows.forEach((pkg) => {
    const tr = document.createElement('tr');
    const locked = pkg.system;
    tr.innerHTML = `
      <td><input type="checkbox" ${state.selectedPkgs.has(pkg.name) ? 'checked' : ''} ${locked ? 'disabled' : ''}></td>
      <td>${pkg.name} ${locked ? '<span class="badge">sistem</span>' : ''}</td>
      <td>${pkg.versionCode || '-'}</td>
      <td>
        <button class="secondary" data-act="detail">Detail</button>
        <button class="danger" data-act="uninstall" ${locked ? 'disabled' : ''}>Copot</button>
      </td>`;
    tr.querySelector('input').addEventListener('change', (e) => {
      if (e.target.checked) state.selectedPkgs.add(pkg.name); else state.selectedPkgs.delete(pkg.name);
      renderPackages();
    });
    tr.querySelector('[data-act=detail]').addEventListener('click', () => showDetail(pkg.name));
    tr.querySelector('[data-act=uninstall]').addEventListener('click', () => {
      if (confirm(`Copot ${pkg.name}?`)) createJobs('uninstall', [pkg.name]);
    });
    tbody.appendChild(tr);
  });

  $('uninstall-selected').textContent = `Copot terpilih (${state.selectedPkgs.size})`;
  $('uninstall-keep-selected').textContent = `Copot (simpan data) (${state.selectedPkgs.size})`;
}

async function showDetail(name) {
  const el = $('detail');
  el.classList.remove('hidden');
  el.textContent = 'Memuat detail...';
  try {
    const info = await api('/api/packages/detail', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ package: name }),
    });
    el.innerHTML = `<h3>${info.package}</h3>
      <dl>
        <dt>Versi</dt><dd>${info.versionName || '-'} (kode ${info.versionCode || '-'})</dd>
        <dt>Ukuran data</dt><dd>${humanSize(info.sizeBytes)}</dd>
        <dt>Terpasang</dt><dd>${info.installTime || '-'}</dd>
        <dt>Diperbarui</dt><dd>${info.updateTime || '-'}</dd>
        <dt>APK</dt><dd>${info.apkPath || '-'}</dd>
        <dt>Izin</dt><dd>${(info.permissions || []).join('<br>') || '-'}</dd>
      </dl>`;
  } catch (err) {
    el.textContent = 'Gagal memuat detail: ' + err.message;
  }
}

function renderJobs() {
  const list = $('queue-list');
  list.innerHTML = '';
  const jobs = Array.from(state.jobs.values()).sort((a, b) => (a.createdAt < b.createdAt ? 1 : -1));
  if (jobs.length === 0) {
    list.innerHTML = '<li class="muted">Belum ada pekerjaan.</li>';
    return;
  }
  jobs.forEach((job) => {
    const li = document.createElement('li');
    const running = job.status === 'running' || job.status === 'queued';
    li.innerHTML = `
      <div><strong>${job.label || job.target}</strong>
        <span class="status-${job.status}">${job.status}</span></div>
      <div class="meta">${job.message || ''} ${job.error ? '· ' + job.error : ''} ${job.result ? '· ' + job.result : ''}</div>
      <div class="progress"><span style="width:${job.status === 'success' ? 100 : job.progress || 0}%"></span></div>
      ${running ? '<button class="secondary" data-cancel>Batalkan</button>' : ''}`;
    if (running) {
      li.querySelector('[data-cancel]').addEventListener('click', () => {
        api('/api/jobs/cancel', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id: job.id }),
        }).catch((e) => toast(e.message, true));
      });
    }
    list.appendChild(li);
  });
}

async function createJobs(kind, targets) {
  if (!targets.length) { toast('Pilih dulu apa yang mau diproses', true); return; }
  try {
    const created = await api('/api/jobs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        kind,
        targets,
        replace: $('replace').checked,
        allowDowngrade: $('downgrade').checked,
      }),
    });
    created.forEach((job) => state.jobs.set(job.id, job));
    renderJobs();
    switchTab(null, 'queue');
  } catch (err) {
    toast(err.message, true);
  }
}

async function loadHistory() {
  try {
    const entries = await api('/api/history?limit=200');
    const tbody = $('history-table').querySelector('tbody');
    tbody.innerHTML = '';
    entries.forEach((e) => {
      const tr = document.createElement('tr');
      tr.innerHTML = `<td>${new Date(e.time).toLocaleString('id-ID')}</td>
        <td>${e.action}</td><td>${e.package || '-'}</td>
        <td class="${e.success ? 'status-success' : 'status-failed'}">${e.success ? 'sukses' : 'gagal'}</td>
        <td>${e.detail || ''}</td>`;
      tbody.appendChild(tr);
    });
  } catch (err) {
    toast(err.message, true);
  }
}

async function loadPackages() {
  try {
    const system = $('show-system').checked ? '1' : '0';
    state.packages = await api(`/api/packages?system=${system}`);
    renderPackages();
  } catch (err) {
    toast('Gagal memuat daftar aplikasi: ' + err.message, true);
  }
}

function connectEvents() {
  const source = new EventSource('/api/events');
  source.addEventListener('state', (ev) => renderDevice(JSON.parse(ev.data).device));
  source.addEventListener('job', (ev) => {
    const job = JSON.parse(ev.data);
    state.jobs.set(job.id, job);
    renderJobs();
    if (job.status === 'success' || job.status === 'failed') {
      loadHistory();
      if (job.kind !== 'install') loadPackages();
    }
  });
  source.onerror = () => setTimeout(connectEvents, 3000);
}

function setupDropZone() {
  const zone = $('drop');
  ['dragenter', 'dragover'].forEach((ev) =>
    zone.addEventListener(ev, (e) => { e.preventDefault(); zone.classList.add('over'); }));
  ['dragleave', 'drop'].forEach((ev) =>
    zone.addEventListener(ev, (e) => { e.preventDefault(); zone.classList.remove('over'); }));

  zone.addEventListener('drop', async (e) => {
    const files = Array.from(e.dataTransfer.files || []).filter((f) => f.name.toLowerCase().endsWith('.apk'));
    if (!files.length) { toast('Hanya berkas .apk yang bisa dipasang', true); return; }
    for (const file of files) await uploadFile(file);
  });

  $('pick-file').addEventListener('click', () => $('file-input').click());
  $('file-input').addEventListener('change', async (e) => {
    for (const file of Array.from(e.target.files)) await uploadFile(file);
    e.target.value = '';
  });
}

async function uploadFile(file) {
  const form = new FormData();
  form.append('file', file);
  try {
    addApk(await api('/api/apks/upload', { method: 'POST', body: form }));
    toast(`${file.name} siap dipasang`);
  } catch (err) {
    toast('Gagal mengunggah: ' + err.message, true);
  }
}

function bind() {
  $('refresh').addEventListener('click', () =>
    api('/api/device/refresh', { method: 'POST' }).catch((e) => toast(e.message, true)));
  $('load-folder').addEventListener('click', async () => {
    const folder = $('folder').value.trim();
    if (!folder) { toast('Isi dulu folder koleksi', true); return; }
    try {
      state.apks = await api(`/api/apks?folder=${encodeURIComponent(folder)}`);
      renderApks();
      await api('/api/config', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ apkFolder: folder }),
      });
    } catch (err) { toast(err.message, true); }
  });
  $('add-url').addEventListener('click', async () => {
    const url = $('url').value.trim();
    if (!url) { toast('Isi dulu URL-nya', true); return; }
    try {
      addApk(await api('/api/apks/url', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      }));
      $('url').value = '';
    } catch (err) { toast(err.message, true); }
  });
  $('install-selected').addEventListener('click', () =>
    createJobs('install', Array.from(state.selectedApks)));
  $('uninstall-selected').addEventListener('click', () => {
    const targets = Array.from(state.selectedPkgs);
    if (targets.length && confirm(`Copot ${targets.length} aplikasi? Tindakan ini menghapus aplikasi dari HP.`)) {
      createJobs('uninstall', targets);
    }
  });
  $('uninstall-keep-selected').addEventListener('click', () => {
    const targets = Array.from(state.selectedPkgs);
    if (targets.length && confirm(`Copot ${targets.length} aplikasi tapi simpan datanya?`)) {
      createJobs('uninstall_keep', targets);
    }
  });
  $('reload-packages').addEventListener('click', loadPackages);
  $('show-system').addEventListener('change', loadPackages);
  $('search').addEventListener('input', renderPackages);
  $('sort').addEventListener('change', renderPackages);
  $('export-csv').addEventListener('click', () => { window.location = '/api/history/export?format=csv'; });
  $('export-json').addEventListener('click', () => { window.location = '/api/history/export?format=json'; });
}

async function boot() {
  bind();
  setupDropZone();
  connectEvents();
  try {
    const snapshot = await api('/api/state');
    renderDevice(snapshot.device);
    (snapshot.jobs || []).forEach((job) => state.jobs.set(job.id, job));
    renderJobs();
    if (snapshot.config?.apkFolder) $('folder').value = snapshot.config.apkFolder;
  } catch (err) {
    toast('Gagal memuat status: ' + err.message, true);
  }
  renderApks();
  renderPackages();
  loadHistory();
}

boot();
