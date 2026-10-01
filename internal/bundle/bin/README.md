# Folder biner adb

Folder ini diisi otomatis oleh `make fetch-adb` (dan oleh CI) dengan struktur:

    bin/<os>-<arch>/adb      (Linux/macOS)
    bin/<os>-<arch>/adb.exe  (Windows)

Berkas `adb` tidak ikut masuk git karena besar dan berbeda per platform.
Berkas README ini sengaja disimpan supaya `go:embed bin` selalu punya isi
dan proyek tetap bisa dikompilasi walau biner adb belum diunduh.
