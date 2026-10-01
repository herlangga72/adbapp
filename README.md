# adbapp

Aplikasi desktop untuk memasang dan mencopot aplikasi Android lewat ADB
(USB debugging), dibuat untuk mempermudah pekerjaan audit aplikasi di satu
perangkat fisik.

Satu biner, tanpa perlu memasang Go/Node/Python di komputer pengguna. `adb`
sudah ditanam di dalam aplikasi.

## Menjalankan

Tiga langkah di HP: nyalakan **Opsi Pengembang**, aktifkan **USB debugging**,
lalu sambungkan kabel dan pilih mode **File Transfer**. Saat muncul dialog
"Allow USB debugging?", tekan **Allow**.

Jalankan `adbapp`, lalu browser terbuka otomatis di halaman aplikasi.

## Pemasangan

**Linux**

```bash
curl -fsSL https://github.com/herlangga72/adbapp/releases/latest/download/adbapp-linux-amd64.tar.gz | tar xz -C ~/bin
~/bin/adbapp
```

**macOS**

```bash
curl -fsSL https://github.com/herlangga72/adbapp/releases/latest/download/adbapp-darwin-arm64.tar.gz | tar xz -C ~/bin
~/bin/adbapp
```

Berkas yang diunduh lewat `curl` tidak diberi label quarantine, jadi Gatekeeper
tidak menghalangi. Bila tetap diblokir (misalnya karena diunduh lewat browser),
jalankan `xattr -dr com.apple.quarantine ~/bin/adbapp` atau klik kanan berkas
lalu pilih **Open**.

**Windows**

Unduh `adbapp-windows-amd64.zip`, ekstrak, lalu klik dua kali `adbapp.exe`.

## Yang bisa dilakukan

- Menyeret & melepas berkas APK, memuat folder koleksi APK, atau menambah dari URL.
- Melihat identitas APK (nama paket, versi) sebelum memasangnya.
- Memasang beberapa APK berurutan lewat antrean, dengan progress dan tombol batalkan.
- Melihat aplikasi yang terpasang, mencari, dan menyaring aplikasi sistem.
- Mencopot aplikasi, mencopot dengan menyimpan data, menghapus data, dan menarik
  APK yang terpasang untuk diarsipkan.
- Mencopot beberapa aplikasi sekaligus.
- Riwayat audit permanen dengan ekspor CSV/JSON.

## Sulit berhasil?

- **Perangkat tidak terdeteksi.** Pastikan kabel mendukung data (bukan kabel
  yang hanya mengisi daya), mode USB disetel ke File Transfer, dan USB debugging
  menyala. Tekan **Pindai ulang**.
- **"Perangkat belum diizinkan".** Lihat layar HP dan tekan **Allow**.
- **Linux: perangkat tetap tidak terlihat.** Tambahkan aturan udev sesuai
  panduan Android, lalu cabut dan sambungkan ulang kabel.
- **Windows: perangkat tidak dikenali.** Pasang driver USB pabrikan HP.
- **adb bentrok.** Tutup aplikasi lain yang memakai adb (misalnya Android Studio).

## Folder data

Tersimpan di luar biner, berisi `adb/`, `history.jsonl`, `config.json`,
`uploads/`, dan `pulled/`:

- Linux: `~/.local/share/adbapp`
- macOS: `~/Library/Application Support/adbapp`
- Windows: `%LOCALAPPDATA%\adbapp`

## Pengembangan

```bash
make fetch-adb   # unduh platform-tools untuk OS ini
make test        # seluruh tes
make build       # biner adbapp
make run         # bangun lalu jalankan
```

Rilis otomatis: dorong tag `v*` (misalnya `git tag v1.0.0 && git push --tags`),
dan GitHub Actions membangun paket untuk Linux, Windows, dan macOS.

## Belum termasuk

Beberapa perangkat sekaligus, ADB nirkabel, split APK (`.apks`/`.xapk`), akses
root, dan penandatanganan Apple.
