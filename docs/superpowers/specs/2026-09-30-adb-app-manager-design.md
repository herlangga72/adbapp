# Desain: ADB App Manager — pasang & copot aplikasi Android untuk audit

Tanggal: 2026-09-30
Status: disetujui (menunggu tinjauan akhir pengguna)

## 1. Ringkasan

Aplikasi desktop lintas platform (Windows, Linux, macOS) untuk memasang dan
mencopot aplikasi Android lewat ADB dengan USB debugging. Tujuannya memberi
kemudahan "pasang lalu copot" berulang kali saat mengaudit aplikasi di satu
perangkat fisik.

Bentuknya: satu biner Go yang menyalakan server lokal di laptop, lalu membuka
halaman web di browser sebagai antarmuka. `adb` (platform-tools) ditanam di
dalam biner, jadi pengguna tidak perlu memasang apa pun.

## 2. Keputusan yang sudah disepakati

| Topik | Keputusan |
|---|---|
| Skenario utama | Satu HP, bergantian banyak APK |
| Sistem operasi | Windows + Linux + macOS (dari satu basis kode) |
| Bentuk antar muka | UI di browser, server lokal di `127.0.0.1`, tanpa login |
| Lingkup fitur | Install; uninstall; uninstall simpan data (`-k`); hapus data; detail paket; pull APK terpasang |
| Cara kerja | Antrean install berurutan + bulk uninstall |
| Sumber APK | Drag & drop, folder koleksi, dan URL |
| Riwayat | Tersimpan permanen + ekspor CSV/JSON |
| Distribusi | Paket jadi per OS, build otomatis di GitHub Actions |
| `adb` | Dibundel di dalam paket |
| Teknologi | Go, UI web tertanam (`go:embed`), tanpa alat build frontend |
| Pemasangan di macOS | Installer satu baris lewat `curl` (menghindari Gatekeeper) |

## 3. Lingkup

**Termasuk v1**

- Deteksi perangkat dan statusnya (siap / belum diizinkan / offline).
- Memasukkan APK lewat drag & drop, folder koleksi, dan unduh dari URL.
- Membaca metadata APK sebelum dipasang (nama package, versi, minSdk).
- Antrean install berurutan dengan progress live dan tombol batalkan.
- Daftar aplikasi terpasang: cari, filter, urutkan.
- Detail paket: versi, ukuran, tanggal terpasang, dan daftar izin yang diminta.
- Aksi per aplikasi: copot, copot simpan data, hapus data, tarik APK.
- Bulk uninstall dengan konfirmasi.
- Riwayat audit permanen + ekspor CSV/JSON.
- Paket rilis per OS dengan `adb` tertanam, dibangun otomatis di GitHub Actions.

**Tidak termasuk v1** (disiapkan agar mudah ditambah nanti)

- Beberapa perangkat sekaligus.
- ADB nirkabel (TCP/IP).
- Split APK (`.apks` / `.xapk`).
- Akses root / aplikasi sistem yang dilindungi.
- Penandatanganan dan notarisasi Apple.

## 4. Arsitektur dan komponen

Aplikasi adalah satu biner Go. Saat dijalankan:

1. Menyalakan server HTTP di `127.0.0.1` pada port acak yang bebas.
2. Membuka browser otomatis ke alamat itu.
3. Menyiapkan `adb`: biner `adb` (tertanam) diekstrak ke folder data aplikasi
   dan diberi izin eksekusi bila belum ada atau versinya berbeda.

UI disajikan dari berkas yang ditanam di biner lewat `go:embed`, sehingga tidak
ada berkas statis yang bisa hilang atau salah path.

| Unit | Tugas | Dependensi |
|---|---|---|
| `adbx` | Menjalankan perintah `adb`, mengurai keluaran sukses/gagal | lokasi biner adb |
| `device` | Memantau perangkat: terdeteksi, hilang, unauthorized; memilih satu device aktif | `adbx` |
| `queue` | Menjalankan job install/uninstall satu per satu, melaporkan progress, mendukung pembatalan | `adbx`, `device`, `store` |
| `store` | Menyimpan riwayat (`history.jsonl`), konfigurasi, dan ekspor CSV/JSON | berkas di folder data |
| `apkmeta` | Membaca package, versi, dan minSdk dari berkas APK | berkas APK |
| `httpapi` | Endpoint REST + aliran event live (SSE) ke browser | `queue`, `device`, `store` |
| `web` | Berkas UI (HTML/CSS/JS ringan) yang ditanam | - |

Batasan antarmuka tiap unit: `adbx` tidak tahu soal UI maupun riwayat; `queue`
tidak tahu cara `adb` bekerja (hanya memanggil `adbx`); `web` tidak tahu apa pun
selain endpoint HTTP.

Alur data: **browser → httpapi → queue → adbx → adb → perangkat**, hasilnya
dikirim balik ke browser lewat SSE tanpa perlu refresh.

## 5. Model data dan alur

**Objek utama**

- **Device**: serial, merek/model, versi Android, status (`ready`,
  `unauthorized`, `offline`).
- **APK kandidat**: sumber (dragdrop/folder/url), nama berkas, ukuran, dan
  metadata dari isi APK (package, versionName, minSdk).
- **InstalledApp**: package, versionName, ukuran, tanggal terpasang, penanda
  aplikasi sistem, dan daftar izin yang diminta.
- **Job**: id, jenis (`install`, `uninstall`, `uninstall_keep`, `clear_data`,
  `pull`), sasaran, status (`queued`, `running`, `success`, `failed`,
  `cancelled`), pesan, stempel waktu.

**Berkas di folder data aplikasi** (bukan di dalam biner)

- `adb/` — biner `adb` hasil ekstraksi
- `history.jsonl` — satu baris per aksi: waktu, aksi, package, serial device,
  hasil, detail error
- `config.json` — folder koleksi terakhir, preferensi kecil
- `uploads/` — APK dari drag & drop atau URL yang belum dipasang
- `pulled/` — hasil tarik APK dari perangkat

**Alur contoh**

1. *Install berurutan*: pilih beberapa APK dari folder → dibuat sekumpulan job
   `install` → `queue` memproses satu per satu → progress tiap job live → tiap
   job tercatat di `history.jsonl`.
2. *Copot massal*: tandai beberapa aplikasi terpasang → job `uninstall`
   berurutan → dicatat.
3. *Perangkat tercabut saat proses*: `device` mendeteksi saat pemantauan
   berkala → job yang sedang jalan ditandai `failed` dengan pesan "perangkat
   terputus"; sisa antrean berhenti di `queued`, bukan ditandai sukses.

Karena pemakaiannya satu HP bergantian, sistem memakai satu device aktif. Bila
lebih dari satu perangkat tersambung, UI menampilkan pemilih kecil di bar atas
untuk memilih perangkat mana yang aktif (endpoint `POST /api/device/select`);
pin dilepas otomatis bila perangkat itu tercabut. Setiap aksi dicatat, termasuk
pembatalan job yang masih `queued` (`Cancel` memanggil callback riwayat).

## 6. Antarmuka pengguna

Satu halaman dengan bar atas dan dua tab utama, plus panel bawah yang bisa
dilipat.

**Bar atas (selalu terlihat)** — indikator perangkat: titik warna (hijau siap,
kuning belum diizinkan, merah tidak ada), merek/model, versi Android, tombol
"Pindai ulang". Bila perangkat belum diizinkan, instruksi singkat muncul di
sini. Versi Android dibaca dari `getprop ro.build.version.release` ketika
perangkat menjadi siap, lalu di-cache per serial agar tidak ditanyakan ulang
tiap siklus pemantauan; bila pembacaan gagal, bagian versi dibiarkan kosong.

**Tab "Pasang"** — kotak seret & lepas besar, dua tombol (Folder koleksi,
Tambah dari URL), daftar APK yang masuk dengan kotak centang, nama berkas,
package, versi, ukuran, dan tombol "Pasang terpilih (N)".

**Tab "Terpasang"** — kotak pencarian, filter (Semua / Pihak ketiga / Sistem),
dan pengurutan (nama, ukuran, tanggal). Tiap baris punya aksi cepat: Copot,
Copot (simpan data), Hapus data, Tarik APK. Aplikasi sistem ditandai dan tombol
copotnya nonaktif. Ukuran dan tanggal hanya tersedia lewat pemuatan detail per
paket (tombol "Muat detail"), jadi pengurutan ukuran/tanggal baru aktif setelah
detail dimuat; sebelum itu urutan jatuh kembali ke nama.

**Panel bawah "Antrean & Riwayat"** — dua sub-tab: antrean job berjalan dengan
progress bar dan tombol batalkan, serta riwayat audit dengan filter dan tombol
Ekspor CSV/JSON.

**Gaya visual** — bersih dan terang, tanpa hiasan berlebihan. Tombol berbahaya
(copot massal) berwarna merah dan selalu meminta konfirmasi.

## 7. Penanganan error

Prinsip: tidak ada kegagalan yang diam-diam. Setiap masalah memunculkan pesan
singkat berbahasa manusia beserta langkah perbaikannya.

**Perangkat dan koneksi**

- Perangkat tidak tersambung → status merah, tombol dinonaktifkan, petunjuk:
  cek kabel, pilih mode *File Transfer*, pastikan USB debugging menyala.
- Belum diizinkan (*unauthorized*) → instruksi menekan *Allow* di layar HP.
- Versi Android dibaca dari `getprop`; kegagalan pembacaan tidak muncul sebagai
  error, bagian versi hanya dikosongkan (pemantauan tetap berjalan).
- Perangkat tercabut saat job jalan → job ditandai gagal dengan pesan
  "perangkat terputus"; sisa antrean berhenti di `queued`.

**`adb` di komputer**

- `adb` gagal jalan (versi bentrok, port 5037 terpakai) → pesan dengan saran
  tindakan.
- Linux: udev rules belum ada → pesan berisi perintah yang perlu dijalankan.
- Windows: driver USB belum terpasang → tautan bantuan.

**Paket**

- Berkas bukan APK atau korup → ditolak saat dimasukkan, disertai alasan.
- Package sudah terpasang → ditawarkan "Pasang ulang (timpa)" atau batal.
- Versi lebih rendah dari yang terpasang → bisa ditimpa dengan izin penurunan
  versi, atau batal.
- Storage perangkat penuh → pesan khusus, bukan keluaran mentah `adb`.
- Aplikasi sistem / tidak boleh dicopot → tombol nonaktif sejak awal.
- URL gagal diunduh atau hasilnya bukan APK → dibatalkan dengan pesan.

## 8. Distribusi dan pemasangan

Build otomatis menghasilkan paket per OS. Tidak ada penandatanganan di v1.

Nama biner aplikasi adalah `adbapp`. Paket rilis diberi nama
`adbapp-<os>-<arch>.(zip|tar.gz)` dan diunggah sebagai lampiran rilis GitHub.
Folder data aplikasi mengikuti konvensi tiap OS (Windows:
`%LOCALAPPDATA%\adbapp`; Linux: `$XDG_DATA_HOME/adbapp` atau
`~/.local/share/adbapp`; macOS: `~/Library/Application Support/adbapp`).

Server hanya mengikat `127.0.0.1`; binding loopback itu dipasangkan dengan guard
origin peramban (`Origin`/`Sec-Fetch-Site` loopback dan `Content-Type`
`application/json` pada endpoint JSON). Ini pengerasan desain "tanpa login",
bukan perubahan pengalaman pengguna.

**Windows** — unduh `.zip`, ekstrak, klik dua kali `adbapp.exe`.
**Linux** — unduh `.tar.gz`, ekstrak, jalankan `./adbapp`.
**macOS** — installer satu baris lewat Terminal:

```
curl -fsSL https://github.com/herlangga72/adbapp/releases/latest/download/adbapp-darwin-arm64.tar.gz | tar xz -C ~/bin
```

Berkas yang diambil `curl` tidak diberi label quarantine, sehingga Gatekeeper
tidak menghalangi. Sebagai cadangan, README menjelaskan cara menghapus label
(`xattr -dr com.apple.quarantine ~/bin/adbapp`) atau klik kanan → Open.

`adb` yang diekstrak aplikasi tidak terkena masalah quarantine karena ditulis
sendiri oleh aplikasi.

## 9. Pengujian

**Tanpa perangkat sungguhan.** Seluruh interaksi ke perangkat lewat `adb`,
sehingga dibuat `adb` palsu: skrip kecil yang mengeluarkan jawaban tiruan
(daftar device, install sukses, install gagal, perangkat tercabut di tengah
proses). Dengan itu:

- `adbx` diuji mengurai keluaran `adb` nyata, termasuk keluaran error.
- `device` diuji mensimulasikan perangkat muncul, hilang, dan unauthorized.
- `queue` diuji urutan, pembatalan, dan perilaku saat satu job gagal.
- `store` diuji penulisan riwayat dan ekspor.
- `httpapi` diuji lewat `httptest`, termasuk aliran event SSE.
- `apkmeta` diuji dengan APK kecil. Fixture tidak disimpan di repo: tes
  membangunnya saat berjalan dari `AndroidManifest.xml` biner bawaan modul
  `apkparser`. Berkas `testdata/mini.apk` hanya diperiksa bila ada, dan
  di-skip bila tidak.

**Dengan perangkat sungguhan (checklist rilis).** Pasang satu APK contoh,
pastikan muncul di daftar terpasang, tarik APK-nya, lalu copot. Termasuk uji
mencabut kabel di tengah instalasi.

## 10. CI/CD

Workflow GitHub Actions berjalan pada matriks Windows, Linux, macOS. Pada tag
`v*`:

1. Unduh platform-tools untuk OS tersebut, lalu tanam `adb` ke dalam biner.
   Semua biner platform disimpan di bawah `internal/bundle/bin/<os>-<arch>/`
   dan ditanam lewat `go:embed`; direktori platform yang sesuai dipilih saat
   runtime, bukan lewat build tag per OS.
2. Jalankan seluruh pengujian.
3. Hasilkan paket `.zip` (Windows) dan `.tar.gz` (Linux/macOS).
4. Unggah otomatis ke halaman Releases.

MacOS belum ditandatangani; pengguna memakai cara pemasangan `curl` di Bagian 8.

## 11. Kriteria keberhasilan

- Memasang APK ke satu perangkat pada Windows, Linux, dan macOS dari satu basis
  kode, tanpa memasang Go/Node/Python di komputer pengguna.
- Memasang beberapa APK berurutan dan mencopot beberapa aplikasi sekaligus
  dengan progress yang terlihat live.
- Setiap aksi tercatat di riwayat yang bisa diekspor.
- Tidak ada kegagalan senyap: seluruh kondisi error menghasilkan pesan dan
  langkah perbaikan yang jelas.
