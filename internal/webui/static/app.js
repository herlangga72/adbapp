'use strict';

const $ = (id) => document.getElementById(id);

// escapeHtml mengubah karakter khusus HTML menjadi entitas. Semua nilai yang
// berasal dari perangkat atau berkas pengguna (nama APK, nama paket, pesan
// error, riwayat) harus melewatinya sebelum masuk ke innerHTML, supaya nama
// berkas yang jahat tidak bisa menyuntikkan skrip (stored XSS).
function escapeHtml(value) {
  return String(value == null ? '' : value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

const state = {
  apks: [],
  packages: [],
  pkgDetail: {},
  pkgFilter: 'all',
  history: [],
  selectedApks: new Set(),
  selectedPkgs: new Set(),
  jobs: new Map(),
  deviceReady: false,
  selectedDevice: '',
};

// searchTimer menunda render daftar saat pengguna mengetik, sehingga tabel
// tidak dibangun ulang pada setiap ketukan tombol.
let searchTimer = null;
// eventSource menyimpan koneksi SSE aktif agar tidak menumpuk koneksi baru saat
// terjadi galat koneksi.
let eventSource = null;

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

function deviceReady() {
  return state.deviceReady;
}

// updateActionButtons menonaktifkan aksi yang memerlukan perangkat siap
// (spec §7) dan yang belum punya pilihan.
function updateActionButtons() {
  const ready = deviceReady();
  $('load-folder').disabled = !ready;
  $('install-selected').disabled = !ready || state.selectedApks.size === 0;
  $('uninstall-selected').disabled = !ready || state.selectedPkgs.size === 0;
  $('uninstall-keep-selected').disabled = !ready || state.selectedPkgs.size === 0;
}

function renderDevice(status) {
  const wasReady = state.deviceReady;
  state.deviceReady = status.state === 'ready';
  const dot = $('dot');
  dot.className = 'dot ' + status.state;
  const label = $('device-label');
  const hint = $('hint');
  if (status.state === 'ready') {
    const name = status.model || status.serial || 'Perangkat';
    const version = status.androidVersion ? ` · Android ${status.androidVersion}` : '';
    label.textContent = `${name}${version} · siap`;
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
  updateActionButtons();
  renderDevicePicker(status);
  // Tabel hanya dibangun ulang saat kesiapan perangkat berubah, sebab baris
  // tabel memakai deviceReady() untuk mengaktifkan tombolnya. Peristiwa SSE
  // dikirim setiap beberapa detik; membangun ulang ratusan baris tiap kali
  // hanya membuang CPU dan membuat gulir/ketikan terasa tersendat.
  if (wasReady !== state.deviceReady) {
    renderApks();
    renderPackages();
  }
}

// renderDevicePicker menampilkan pemilih perangkat kecil saat lebih dari satu
// perangkat tersambung (atau saat pengguna sudah memilih salah satunya).
function renderDevicePicker(status) {
  const sel = $('device-select');
  const serials = [status.serial, ...(status.others || [])].filter(Boolean);
  const show = (status.others && status.others.length > 0) || !!state.selectedDevice;
  if (!show || serials.length === 0) {
    sel.classList.add('hidden');
    sel.innerHTML = '';
    return;
  }
  if (state.selectedDevice && !serials.includes(state.selectedDevice)) {
    state.selectedDevice = '';
  }
  sel.classList.remove('hidden');
  sel.innerHTML = '';
  serials.forEach((serial) => {
    const opt = document.createElement('option');
    opt.value = serial;
    opt.textContent = serial;
    if (serial === (state.selectedDevice || status.serial)) opt.selected = true;
    sel.appendChild(opt);
  });
}

function renderApks() {
  const tbody = $('apk-table').querySelector('tbody');
  tbody.innerHTML = '';
  state.apks.forEach((apk) => {
    const tr = document.createElement('tr');
    if (state.selectedApks.has(apk.path)) tr.classList.add('selected');
    tr.innerHTML = `
      <td><input type="checkbox" ${state.selectedApks.has(apk.path) ? 'checked' : ''}></td>
      <td>${escapeHtml(apk.name)}</td>
      <td>${apk.package ? escapeHtml(apk.package) : '<span class="muted">tidak terbaca</span>'}</td>
      <td>${escapeHtml(apk.versionName || '-')}</td>
      <td>${apk.minSdk || '-'}</td>
      <td>${humanSize(apk.size)}</td>`;
    if (apk.error) tr.title = apk.error;
    tr.querySelector('input').addEventListener('change', (e) => {
      if (e.target.checked) state.selectedApks.add(apk.path); else state.selectedApks.delete(apk.path);
      // Cukup perbarui baris dan hitungan; tidak perlu membangun ulang tabel.
      tr.classList.toggle('selected', e.target.checked);
      $('install-selected').textContent = `Pasang terpilih (${state.selectedApks.size})`;
      updateActionButtons();
    });
    tbody.appendChild(tr);
  });
  $('install-selected').textContent = `Pasang terpilih (${state.selectedApks.size})`;
  updateActionButtons();
}

function addApk(entry) {
  const existing = state.apks.findIndex((a) => a.path === entry.path);
  if (existing >= 0) state.apks[existing] = entry; else state.apks.push(entry);
  renderApks();
}

// filteredPackages mengembalikan daftar paket yang cocok dengan pencarian.
function filteredPackages() {
  const term = $('search').value.toLowerCase();
  return state.packages.filter((p) => p.name.toLowerCase().includes(term));
}

function detailFor(name) {
  return state.pkgDetail[name] || null;
}

function updateSortOptions() {
  const has = Object.keys(state.pkgDetail).length > 0;
  $('sort').querySelectorAll('option[value="size"], option[value="date"]')
    .forEach((o) => { o.disabled = !has; });
  $('sort-hint').classList.toggle('hidden', has);
}

function renderPackages() {
  const sortMode = $('sort').value;
  const hasDetail = Object.keys(state.pkgDetail).length > 0;
  // Urut ukuran/tanggal butuh detail; sebelum itu jatuh kembali ke urut nama.
  const mode = (sortMode === 'size' || sortMode === 'date') && !hasDetail ? 'name' : sortMode;

  const tbody = $('pkg-table').querySelector('tbody');
  tbody.innerHTML = '';

  const rows = filteredPackages();
  if (mode === 'name') {
    rows.sort((a, b) => a.name.localeCompare(b.name));
  } else if (mode === 'size') {
    rows.sort((a, b) => (detailFor(a.name)?.sizeBytes || 0) - (detailFor(b.name)?.sizeBytes || 0));
  } else if (mode === 'date') {
    rows.sort((a, b) => (detailFor(a.name)?.installTime || '').localeCompare(detailFor(b.name)?.installTime || ''));
  }

  rows.forEach((pkg) => {
    const tr = document.createElement('tr');
    const locked = pkg.system;
    const ready = deviceReady();
    const detail = detailFor(pkg.name);
    tr.innerHTML = `
      <td><input type="checkbox" ${state.selectedPkgs.has(pkg.name) ? 'checked' : ''} ${locked ? 'disabled' : ''}></td>
      <td>${escapeHtml(pkg.name)} ${locked ? '<span class="badge">sistem</span>' : ''}</td>
      <td>${escapeHtml(pkg.versionCode || '-')}</td>
      <td>${detail ? humanSize(detail.sizeBytes) : '-'}</td>
      <td>${escapeHtml(detail?.installTime || '-')}</td>
      <td class="row-actions">
        <button class="secondary" data-act="detail">Detail</button>
        <button class="danger" data-act="uninstall" ${locked || !ready ? 'disabled' : ''}>Copot</button>
        <button class="secondary" data-act="uninstall_keep" ${locked || !ready ? 'disabled' : ''}>Copot (simpan data)</button>
        <button class="secondary" data-act="clear_data" ${!ready ? 'disabled' : ''}>Hapus data</button>
        <button class="secondary" data-act="pull" ${!ready ? 'disabled' : ''}>Tarik APK</button>
      </td>`;
    tr.querySelector('input').addEventListener('change', (e) => {
      if (e.target.checked) state.selectedPkgs.add(pkg.name); else state.selectedPkgs.delete(pkg.name);
      // Perbarui hitungan tombol saja; membangun ulang tabel akan membuang
      // posisi gulir dan membuat daftar panjang tersendat.
      $('uninstall-selected').textContent = `Copot terpilih (${state.selectedPkgs.size})`;
      $('uninstall-keep-selected').textContent = `Copot (simpan data) (${state.selectedPkgs.size})`;
      updateActionButtons();
    });
    tr.querySelector('[data-act=detail]').addEventListener('click', () => showDetail(pkg.name));
    tr.querySelector('[data-act=uninstall]').addEventListener('click', () => {
      if (confirm(`Copot ${pkg.name}? Tindakan ini menghapus aplikasi dari HP.`)) createJobs('uninstall', [pkg.name]);
    });
    tr.querySelector('[data-act=uninstall_keep]').addEventListener('click', () => {
      if (confirm(`Copot ${pkg.name} tapi simpan datanya?`)) createJobs('uninstall_keep', [pkg.name]);
    });
    tr.querySelector('[data-act=clear_data]').addEventListener('click', () => {
      if (confirm(`Hapus data ${pkg.name}? Aplikasi tetap terpasang.`)) createJobs('clear_data', [pkg.name]);
    });
    tr.querySelector('[data-act=pull]').addEventListener('click', () => {
      if (confirm(`Tarik APK ${pkg.name} ke folder hasil?`)) createJobs('pull', [pkg.name]);
    });
    tbody.appendChild(tr);
  });

  $('uninstall-selected').textContent = `Copot terpilih (${state.selectedPkgs.size})`;
  $('uninstall-keep-selected').textContent = `Copot (simpan data) (${state.selectedPkgs.size})`;
  updateActionButtons();
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
    el.innerHTML = `<h3>${escapeHtml(info.package)}</h3>
      <dl>
        <dt>Versi</dt><dd>${escapeHtml(info.versionName || '-')} (kode ${escapeHtml(info.versionCode || '-')})</dd>
        <dt>Ukuran data</dt><dd>${humanSize(info.sizeBytes)}</dd>
        <dt>Terpasang</dt><dd>${escapeHtml(info.installTime || '-')}</dd>
        <dt>Diperbarui</dt><dd>${escapeHtml(info.updateTime || '-')}</dd>
        <dt>APK</dt><dd>${escapeHtml(info.apkPath || '-')}</dd>
        <dt>Izin</dt><dd>${(info.permissions || []).map(escapeHtml).join('<br>') || '-'}</dd>
      </dl>`;
  } catch (err) {
    el.textContent = 'Gagal memuat detail: ' + err.message;
  }
}

// loadDetails memperkaya baris yang terlihat dengan ukuran dan tanggal
// terpasang, dengan concurrency terbatas (4 sekaligus).
async function loadDetails() {
  const targets = filteredPackages().map((p) => p.name).filter((n) => !state.pkgDetail[n]);
  const btn = $('load-details');
  if (!targets.length) {
    toast('Detail sudah dimuat untuk baris yang terlihat');
    updateSortOptions();
    return;
  }
  btn.disabled = true;
  const total = targets.length;
  let done = 0;
  let next = 0;
  const worker = async () => {
    while (next < targets.length) {
      const name = targets[next++];
      try {
        state.pkgDetail[name] = await api('/api/packages/detail', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ package: name }),
        });
      } catch (e) { /* baris ini tetap tanpa detail */ }
      done++;
      btn.textContent = `Muat detail (${done}/${total})`;
    }
  };
  await Promise.all(Array.from({ length: Math.min(4, targets.length) }, worker));
  btn.disabled = false;
  btn.textContent = 'Muat detail';
  updateSortOptions();
  renderPackages();
  toast(`Detail dimuat untuk ${done} aplikasi`);
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
      <div><strong>${escapeHtml(job.label || job.target)}</strong>
        <span class="status-${escapeHtml(job.status)}">${escapeHtml(job.status)}</span></div>
      <div class="meta">${escapeHtml(job.message || '')} ${job.error ? '· ' + escapeHtml(job.error) : ''} ${job.result ? '· ' + escapeHtml(job.result) : ''}</div>
      <div class="progress"><span style="width:${job.status === 'success' ? 100 : (Number(job.progress) || 0)}%"></span></div>
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
    const raw = await api('/api/history?limit=200');
    state.history = Array.isArray(raw) ? raw : [];
    renderHistory();
  } catch (err) {
    toast(err.message, true);
  }
}

function renderHistory() {
  const term = ($('history-filter').value || '').toLowerCase();
  const tbody = $('history-table').querySelector('tbody');
  tbody.innerHTML = '';
  state.history
    .filter((e) => !term || [e.action, e.package, e.detail]
      .some((f) => (f || '').toLowerCase().includes(term)))
    .forEach((e) => {
      const tr = document.createElement('tr');
      tr.innerHTML = `<td>${escapeHtml(new Date(e.time).toLocaleString('id-ID'))}</td>
        <td>${escapeHtml(e.action)}</td><td>${escapeHtml(e.package || '-')}</td>
        <td class="${e.success ? 'status-success' : 'status-failed'}">${e.success ? 'sukses' : 'gagal'}</td>
        <td>${escapeHtml(e.detail || '')}</td>`;
      tbody.appendChild(tr);
    });
}

async function loadPackages() {
  try {
    if (state.pkgFilter === 'all') {
      // "Semua" menggabungkan pihak ketiga dan sistem, dedup per nama.
      const [third, system] = await Promise.all([
        api('/api/packages?system=0'),
        api('/api/packages?system=1'),
      ]);
      const byName = new Map();
      (Array.isArray(third) ? third : []).forEach((p) => byName.set(p.name, p));
      (Array.isArray(system) ? system : []).forEach((p) => byName.set(p.name, { ...p, system: true }));
      state.packages = Array.from(byName.values());
    } else {
      const list = await api(`/api/packages?system=${state.pkgFilter === 'system' ? '1' : '0'}`);
      state.packages = (Array.isArray(list) ? list : []).map((p) => ({
        ...p,
        system: state.pkgFilter === 'system',
      }));
    }
    renderPackages();
  } catch (err) {
    toast('Gagal memuat daftar aplikasi: ' + err.message, true);
  }
}

function connectEvents() {
  // Tutup koneksi lama sebelum membuat yang baru supaya koneksi tidak menumpuk
  // ketika handler galat dipanggil berkali-kali.
  if (eventSource) eventSource.close();
  const source = new EventSource('/api/events');
  eventSource = source;
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
  source.onerror = () => {
    // EventSource menyambung ulang sendiri selama koneksi belum tertutup
    // permanen. Panggil ulang hanya bila benar-benar CLOSED, agar tidak ada
    // dua koneksi aktif ke server yang sama.
    if (source.readyState === EventSource.CLOSED && eventSource === source) {
      setTimeout(connectEvents, 3000);
    }
  };
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

function setupBottomToggle() {
  const section = document.querySelector('.bottom');
  const toggle = $('bottom-toggle');
  const collapsed = localStorage.getItem('adbapp.bottomCollapsed') === '1';
  section.classList.toggle('collapsed', collapsed);
  toggle.setAttribute('aria-expanded', String(!collapsed));

  toggle.addEventListener('click', () => {
    const next = !section.classList.contains('collapsed');
    section.classList.toggle('collapsed', next);
    toggle.setAttribute('aria-expanded', String(!next));
    localStorage.setItem('adbapp.bottomCollapsed', next ? '1' : '0');
  });
}

function bind() {
  $('refresh').addEventListener('click', () =>
    api('/api/device/refresh', { method: 'POST' }).catch((e) => toast(e.message, true)));
  $('device-select').addEventListener('change', async (e) => {
    state.selectedDevice = e.target.value;
    try {
      const st = await api('/api/device/select', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serial: state.selectedDevice }),
      });
      renderDevice(st);
      loadPackages();
    } catch (err) {
      toast(err.message, true);
      state.selectedDevice = '';
    }
  });
  $('load-folder').addEventListener('click', async () => {
    const folder = $('folder').value.trim();
    if (!folder) { toast('Isi dulu folder koleksi', true); return; }
    try {
      const list = await api(`/api/apks?folder=${encodeURIComponent(folder)}`);
      state.apks = Array.isArray(list) ? list : [];
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
  $('load-details').addEventListener('click', loadDetails);
  document.querySelectorAll('.filter').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.filter').forEach((b) => b.classList.toggle('active', b === btn));
      state.pkgFilter = btn.dataset.filter;
      loadPackages();
    });
  });
  $('search').addEventListener('input', () => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(renderPackages, 120);
  });
  $('sort').addEventListener('change', renderPackages);
  $('history-filter').addEventListener('input', renderHistory);
  $('export-csv').addEventListener('click', () => { window.location = '/api/history/export?format=csv'; });
  $('export-json').addEventListener('click', () => { window.location = '/api/history/export?format=json'; });
}

async function boot() {
  bind();
  setupDropZone();
  setupBottomToggle();
  connectEvents();
  updateSortOptions();
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
