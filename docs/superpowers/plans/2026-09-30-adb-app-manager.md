# ADB App Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Membangun `adbapp`, satu biner Go lintas platform yang menyajikan UI web lokal untuk memasang dan mencopot aplikasi Android lewat ADB dengan USB debugging.

**Architecture:** Satu proses Go menyalakan server HTTP di `127.0.0.1`, menyajikan UI yang ditanam (`go:embed`), dan memanggil `adb` (juga ditanam di dalam biner) lewat paket `adbx`. Perintah dijalankan berurutan melalui `queue`, hasilnya dialirkan ke browser lewat SSE.

**Tech Stack:** Go 1.24, `github.com/avast/apkparser` untuk membaca metadata APK, `net/http` + `embed`, pengujian `testing` + `httptest`, CI GitHub Actions.

**Referensi:** `docs/superpowers/specs/2026-09-30-adb-app-manager-design.md`.

**Catatan penyimpangan kecil dari spec:** spec menyebut "adb palsu berupa skrip kecil". Di plan ini penggantinya adalah `Execer` yang bisa disuntik (fake di dalam proses), karena skrip shell tidak jalan di runner Windows saat pengujian CI. Fungsinya sama: seluruh perilaku `adb` bisa disimulasikan.

**Catatan konvensi:** module path dipakai `github.com/herlangga72/adbapp`. Ganti bila nama repo GitHub berbeda, dengan `go mod edit -module github.com/herlangga72/adbapp` diikuti `go mod tidy`.

---

## Struktur file

| File | Tanggung jawab |
|---|---|
| `go.mod`, `go.sum` | Modul dan dependensi |
| `main.go` | Merakit semua unit, menyalakan server, membuka browser |
| `internal/paths/paths.go` | Menentukan folder data per OS dan membuatnya |
| `internal/bundle/embed.go` | Menanam berkas `bin/` (biner adb) ke dalam biner |
| `internal/bundle/bundle.go` | Mengekstrak `adb` ke folder data |
| `internal/bundle/bin/README.md` | Penanda agar `go:embed` selalu bisa dikompilasi |
| `internal/adbx/adbx.go` | Menjalankan perintah `adb`, mengurai keluaran |
| `internal/adbx/errors.go` | Menerjemahkan keluaran `adb` menjadi error yang jelas |
| `internal/device/device.go` | Memantau perangkat yang tersambung |
| `internal/apkmeta/apkmeta.go` | Membaca package/versi/minSdk dari berkas APK |
| `internal/store/store.go` | Riwayat `history.jsonl` + konfigurasi + ekspor |
| `internal/queue/queue.go` | Antrean job berurutan, progress, pembatalan |
| `internal/httpapi/httpapi.go` | REST + SSE + penyajian UI |
| `internal/webui/embed.go` | Menanam berkas `static/` |
| `internal/webui/static/index.html` | Halaman UI |
| `internal/webui/static/app.js` | Logika UI |
| `internal/webui/static/styles.css` | Gaya UI |
| `Makefile` | `fetch-adb`, `build`, `test`, `run` |
| `.github/workflows/release.yml` | Build + rilis otomatis 3 OS |

---

## Task 0: Kerangka proyek

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `internal/bundle/bin/README.md`

- [ ] **Step 1: Inisialisasi modul dan tambahkan dependensi**

```bash
cd /home/server/autoinstall-and-uninstall-using-adb
go mod init github.com/herlangga72/adbapp
go get github.com/avast/apkparser@latest
go mod edit -go=1.24
```

Expected: `go.mod` terbentuk, `apkparser` tercatat di `require`, dan direktif `go`
di pin ke `1.24` (bukan versi toolchain lokal).

- [ ] **Step 2: Tulis `.gitignore`**

```gitignore
/adbapp
/adbapp.exe
/internal/bundle/bin/*/
dist/
*.apk
!internal/apkmeta/testdata/*.apk
```

Baris terakhir memastikan APK contoh untuk pengujian tetap ikut ter-commit.

- [ ] **Step 3: Tulis penanda embed**

Create `internal/bundle/bin/README.md`:

```markdown
# Folder biner adb

Folder ini diisi otomatis oleh `make fetch-adb` (dan oleh CI) dengan struktur:

    bin/<os>-<arch>/adb      (Linux/macOS)
    bin/<os>-<arch>/adb.exe  (Windows)

Berkas `adb` tidak ikut masuk git karena besar dan berbeda per platform.
Berkas README ini sengaja disimpan supaya `go:embed bin` selalu punya isi
dan proyek tetap bisa dikompilasi walau biner adb belum diunduh.
```

- [ ] **Step 4: Verifikasi kompilasi dasar**

Run: `go build ./...`
Expected: sukses tanpa keluaran (belum ada berkas .go, tapi modul valid).

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum .gitignore internal/bundle/bin/README.md
git commit -m "chore: kerangka modul Go untuk adbapp"
```

---

## Task 1: Folder data per OS (`internal/paths`)

**Files:**
- Create: `internal/paths/paths.go`
- Test: `internal/paths/paths_test.go`
- Test: `internal/paths/base_linux_test.go` (`//go:build linux`)
- Test: `internal/paths/base_windows_test.go` (`//go:build windows`)
- Test: `internal/paths/base_darwin_test.go` (`//go:build darwin`)

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/paths/paths_test.go`:

```go
package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFromBuildsLayout(t *testing.T) {
	p := ResolveFrom("/base")
	want := Paths{
		DataDir:     filepath.Join("/base", "adbapp"),
		AdbDir:      filepath.Join("/base", "adbapp", "adb"),
		UploadsDir:  filepath.Join("/base", "adbapp", "uploads"),
		PulledDir:   filepath.Join("/base", "adbapp", "pulled"),
		HistoryFile: filepath.Join("/base", "adbapp", "history.jsonl"),
		ConfigFile:  filepath.Join("/base", "adbapp", "config.json"),
	}
	if p != want {
		t.Fatalf("layout salah:\n got %+v\nwant %+v", p, want)
	}
}

func TestEnsureCreatesDirs(t *testing.T) {
	p := ResolveFrom(t.TempDir())
	if err := p.Ensure(); err != nil {
		t.Fatalf("Ensure gagal: %v", err)
	}
	for _, dir := range []string{p.DataDir, p.AdbDir, p.UploadsDir, p.PulledDir} {
		if !isDir(dir) {
			t.Fatalf("folder %s tidak dibuat", dir)
		}
	}
}

func TestResolveUsesAdbappDirectory(t *testing.T) {
	p, err := Resolve()
	if err != nil {
		t.Skipf("Resolve error: %v", err)
	}
	if got := filepath.Base(p.DataDir); got != "adbapp" {
		t.Fatalf("got %q, want adbapp", got)
	}
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
```

Create `internal/paths/base_linux_test.go` (`//go:build linux`):

```go
//go:build linux

package paths

import "testing"

func TestBaseDirUsesXDGOnLinux(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg")
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	if got != "/xdg" {
		t.Fatalf("got %q, want /xdg", got)
	}
}

func TestBaseDirIgnoresRelativeXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative/data")
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	if got == "relative/data" {
		t.Fatalf("baseDir memakai XDG relatif: %q", got)
	}
}
```

Create `internal/paths/base_windows_test.go` (`//go:build windows`):

```go
//go:build windows

package paths

import "testing"

func TestBaseDirUsesLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\tester\AppData\Local`)
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	if got != `C:\Users\tester\AppData\Local` {
		t.Fatalf("got %q, want LOCALAPPDATA", got)
	}
}

func TestBaseDirFallsBackWhenLocalAppDataEmpty(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	if got == "" {
		t.Fatal("baseDir mengembalikan path kosong")
	}
}
```

Create `internal/paths/base_darwin_test.go` (`//go:build darwin`):

```go
//go:build darwin

package paths

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBaseDirDarwin(t *testing.T) {
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	want := filepath.Join("Library", "Application Support")
	if !strings.HasSuffix(got, want) {
		t.Fatalf("got %q, want suffix %q", got, want)
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/paths/`
Expected: FAIL, `undefined: ResolveFrom`.

- [ ] **Step 3: Implementasi**

Create `internal/paths/paths.go`:

```go
// Package paths menentukan lokasi folder data aplikasi sesuai konvensi tiap OS.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Paths adalah lokasi folder data aplikasi dan berkas-berkas yang dikelolanya.
type Paths struct {
	DataDir     string
	AdbDir      string
	UploadsDir  string
	PulledDir   string
	HistoryFile string
	ConfigFile  string
}

// ResolveFrom menyusun tata letak folder dengan basis yang bisa ditentukan,
// sehingga mudah diuji tanpa menyentuh folder asli pengguna.
func ResolveFrom(base string) Paths {
	data := filepath.Join(base, "adbapp")
	return Paths{
		DataDir:     data,
		AdbDir:      filepath.Join(data, "adb"),
		UploadsDir:  filepath.Join(data, "uploads"),
		PulledDir:   filepath.Join(data, "pulled"),
		HistoryFile: filepath.Join(data, "history.jsonl"),
		ConfigFile:  filepath.Join(data, "config.json"),
	}
}

// Resolve memakai folder data standar OS yang sedang berjalan.
func Resolve() (Paths, error) {
	base, err := baseDir()
	if err != nil {
		return Paths{}, err
	}
	return ResolveFrom(base), nil
}

func baseDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return v, nil
		}
		if v, err := os.UserConfigDir(); err == nil && v != "" {
			return v, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Local"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if v := os.Getenv("XDG_DATA_HOME"); v != "" && filepath.IsAbs(v) {
			return v, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share"), nil
	}
}

// Ensure membuat seluruh folder yang dibutuhkan.
func (p Paths) Ensure() error {
	for _, dir := range []string{p.DataDir, p.AdbDir, p.UploadsDir, p.PulledDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("gagal membuat folder %s: %w", dir, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Jalankan tes, pastikan lulus**

Run: `go test ./internal/paths/ -v`
Expected: PASS untuk ketiga tes.

- [ ] **Step 5: Commit**

```bash
git add internal/paths
git commit -m "feat(paths): tentukan folder data per OS"
```

---

## Task 2: Ekstraksi `adb` tertanam (`internal/bundle`)

**Files:**
- Create: `internal/bundle/embed.go`
- Create: `internal/bundle/bundle.go`
- Test: `internal/bundle/bundle_test.go`

Catatan: unit yang ditanam adalah direktori `bin/<goos>-<goarch>/`. Di Windows
direktori itu berisi `adb.exe` beserta `AdbWinApi.dll` dan `AdbWinUsbApi.dll`
yang wajib ikut terekstrak, karena `adb.exe` memuat `AdbWinApi.dll` secara
statis.

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/bundle/bundle_test.go`:

```go
package bundle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp-*"))
	if err != nil {
		t.Fatalf("glob gagal: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("berkas sementara tersisa: %v", matches)
	}
}

func TestEnsureFromExtractsBinary(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("#!/bin/sh\necho hi\n"), Mode: 0o644},
	}
	dir := t.TempDir()
	got, err := ensureFrom(fsys, dir, "linux", "amd64")
	if err != nil {
		t.Fatalf("ensureFrom gagal: %v", err)
	}
	want := filepath.Join(dir, "adb")
	if got != want {
		t.Fatalf("path salah: got %q want %q", got, want)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("adb tidak tertulis: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("adb kosong")
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(want)
		if st.Mode().Perm()&0o111 == 0 {
			t.Fatalf("adb tidak bisa dieksekusi: %v", st.Mode())
		}
	}
}

func TestEnsureFromSkipsWhenVersionMatches(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("v1"), Mode: 0o644},
	}
	dir := t.TempDir()
	first, err := ensureFrom(fsys, dir, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("diubah manual"), 0o755); err != nil {
		t.Fatal(err)
	}
	second, err := ensureFrom(fsys, dir, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(second)
	if string(data) != "diubah manual" {
		t.Fatal("adb seharusnya tidak diekstrak ulang saat versi sama")
	}
}

func TestEnsureFromErrorsWhenPlatformMissing(t *testing.T) {
	fsys := fstest.MapFS{"bin/README.md": &fstest.MapFile{Data: []byte("x")}}
	if _, err := ensureFrom(fsys, t.TempDir(), "plan9", "mips"); err == nil {
		t.Fatal("seharusnya error saat biner platform tidak ada")
	}
}

func TestEnsureFromReextractsWhenVersionChanges(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("baru"), Mode: 0o644},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "adb"), []byte("lama"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".version"), []byte("0:linux-amd64"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureFrom(fsys, dir, "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "adb"))
	if string(data) != "baru" {
		t.Fatalf("adb seharusnya diekstrak ulang saat versi berubah, isi: %q", data)
	}
}

func TestEnsureFromReextractsWhenBinaryMissing(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("isi adb"), Mode: 0o644},
	}
	dir := t.TempDir()
	first, err := ensureFrom(fsys, dir, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	second, err := ensureFrom(fsys, dir, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(second)
	if err != nil || len(data) == 0 {
		t.Fatalf("adb seharusnya diekstrak ulang saat biner hilang: %v", err)
	}
}

func TestEnsureFromReextractsWhenStampCorrupt(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("baru"), Mode: 0o644},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "adb"), []byte("lama"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".version"), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureFrom(fsys, dir, "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "adb"))
	if string(data) != "baru" {
		t.Fatalf("adb seharusnya diekstrak ulang saat stempel rusak, isi: %q", data)
	}
}

func TestEnsureFromExtractsWindowsDLLs(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/windows-amd64/adb.exe":           &fstest.MapFile{Data: []byte("exe"), Mode: 0o644},
		"bin/windows-amd64/AdbWinApi.dll":     &fstest.MapFile{Data: []byte("api"), Mode: 0o644},
		"bin/windows-amd64/AdbWinUsbApi.dll":  &fstest.MapFile{Data: []byte("usb"), Mode: 0o644},
		"bin/windows-amd64/source.properties": &fstest.MapFile{Data: []byte("prop"), Mode: 0o644},
	}
	dir := t.TempDir()
	got, err := ensureFrom(fsys, dir, "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "adb.exe") {
		t.Fatalf("path adb salah: %q", got)
	}
	for _, n := range []string{"adb.exe", "AdbWinApi.dll", "AdbWinUsbApi.dll", "source.properties"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("%s tidak diekstrak: %v", n, err)
		}
	}
}

func TestEnsureFromLeavesNoTempAfterSuccess(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("isi"), Mode: 0o644},
	}
	dir := t.TempDir()
	if _, err := ensureFrom(fsys, dir, "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	assertNoTemps(t, dir)
}

func TestEnsureFromLeavesNoTempAfterFailure(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("isi"), Mode: 0o644},
	}
	dir := t.TempDir()
	// Direktori tujuan yang tidak kosong membuat operasi rename gagal.
	if err := os.MkdirAll(filepath.Join(dir, "adb", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureFrom(fsys, dir, "linux", "amd64"); err == nil {
		t.Fatal("seharusnya error saat tujuan tidak dapat ditulis")
	}
	assertNoTemps(t, dir)
}

func TestEnsureFromConcurrent(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb":        &fstest.MapFile{Data: []byte("#!/bin/sh\necho hi\n"), Mode: 0o644},
		"bin/linux-amd64/NOTICE.txt": &fstest.MapFile{Data: []byte("notice"), Mode: 0o644},
	}
	dir := t.TempDir()
	const n = 8
	var wg sync.WaitGroup
	paths := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			paths[i], errs[i] = ensureFrom(fsys, dir, "linux", "amd64")
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d gagal: %v", i, errs[i])
		}
		if paths[i] == "" {
			t.Fatalf("goroutine %d mengembalikan path kosong", i)
		}
	}
	assertNoTemps(t, dir)
}

func TestEnsureFromRestoresMissingDLL(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/windows-amd64/adb.exe":          &fstest.MapFile{Data: []byte("exe"), Mode: 0o644},
		"bin/windows-amd64/AdbWinApi.dll":    &fstest.MapFile{Data: []byte("api"), Mode: 0o644},
		"bin/windows-amd64/AdbWinUsbApi.dll": &fstest.MapFile{Data: []byte("usb"), Mode: 0o644},
	}
	dir := t.TempDir()
	if _, err := ensureFrom(fsys, dir, "windows", "amd64"); err != nil {
		t.Fatal(err)
	}
	dll := filepath.Join(dir, "AdbWinApi.dll")
	if err := os.Remove(dll); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureFrom(fsys, dir, "windows", "amd64"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dll); err != nil {
		t.Fatalf("AdbWinApi.dll seharusnya dipulihkan: %v", err)
	}
}

func TestEnsureFromCleansStaleTempOnFastPath(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("isi"), Mode: 0o644},
	}
	dir := t.TempDir()
	if _, err := ensureFrom(fsys, dir, "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "adb.tmp-stale")
	if err := os.WriteFile(stale, []byte("sisa"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureFrom(fsys, dir, "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("berkas sementara basi seharusnya dihapus, err: %v", err)
	}
	assertNoTemps(t, dir)
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/bundle/`
Expected: FAIL, `undefined: ensureFrom`.

- [ ] **Step 3: Implementasi**

Create `internal/bundle/embed.go`:

```go
// Package bundle menanam biner adb ke dalam biner aplikasi dan mengekstraknya
// ke folder data saat aplikasi dijalankan.
package bundle

import "embed"

// Version dinaikkan setiap kali biner adb di folder bin/ diperbarui, supaya
// aplikasi tahu kapan harus mengekstrak ulang.
const Version = "1"

//go:embed bin
var binFS embed.FS
```

Create `internal/bundle/bundle.go`:

```go
package bundle

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"
)

// Ensure mengekstrak adb untuk OS saat ini dan mengembalikan path-nya.
func Ensure(adbDir string) (string, error) {
	return ensureFrom(binFS, adbDir, runtime.GOOS, runtime.GOARCH)
}

func adbName(goos string) string {
	if goos == "windows" {
		return "adb.exe"
	}
	return "adb"
}

// ensureFrom mengekstrak seluruh isi folder bin/<goos>-<goarch>/ ke adbDir.
// Di Windows, adb.exe memuat AdbWinApi.dll secara statis sehingga DLL tersebut
// harus ikut diekstrak berdampingan; karena itu unit yang disalin adalah
// direktori, bukan satu berkas.
func ensureFrom(fsys fs.FS, adbDir, goos, goarch string) (string, error) {
	name := adbName(goos)
	srcDir := path.Join("bin", goos+"-"+goarch)

	entries, err := fs.ReadDir(fsys, srcDir)
	if err != nil {
		return "", notBundledError(goos, goarch)
	}

	var files []string
	hasAdb := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files = append(files, e.Name())
		if e.Name() == name {
			hasAdb = true
		}
	}
	if !hasAdb {
		return "", notBundledError(goos, goarch)
	}

	dst := filepath.Join(adbDir, name)
	stamp := filepath.Join(adbDir, ".version")
	want := Version + ":" + goos + "-" + goarch

	// Bersihkan sisa berkas sementara dari proses yang gagal sebelumnya, bahkan
	// ketika ekstraksi ulang tidak diperlukan.
	removeStaleTemps(adbDir)

	if got, err := os.ReadFile(stamp); err == nil && string(got) == want {
		if bundledFilesPresent(adbDir, files) {
			return dst, nil
		}
	}

	if err := os.MkdirAll(adbDir, 0o755); err != nil {
		return "", fmt.Errorf("membuat folder %s: %w", adbDir, err)
	}

	for _, f := range files {
		if err := copyFile(fsys, path.Join(srcDir, f), filepath.Join(adbDir, f)); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(stamp, []byte(want), 0o644); err != nil {
		return "", fmt.Errorf("menulis %s: %w", stamp, err)
	}
	return dst, nil
}

func notBundledError(goos, goarch string) error {
	return fmt.Errorf(
		"biner adb untuk %s-%s belum dibundel; jalankan `make fetch-adb` sebelum build",
		goos, goarch)
}

// bundledFilesPresent memastikan semua berkas yang seharusnya diekstrak sudah
// ada sebagai berkas biasa di adbDir. Jika satu saja hilang, ekstraksi ulang
// perlu dijalankan.
func bundledFilesPresent(adbDir string, files []string) bool {
	for _, f := range files {
		st, err := os.Stat(filepath.Join(adbDir, f))
		if err != nil || !st.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// removeStaleTemps membersihkan sisa berkas sementara yang sudah lama
// tertinggal. Hanya berkas yang lebih tua dari satu jam yang dihapus supaya
// ekstraksi paralel yang sedang berjalan tidak terganggu.
func removeStaleTemps(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp-*"))
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Hour)
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && st.ModTime().Before(cutoff) {
			os.Remove(m)
		}
	}
}

func copyFile(fsys fs.FS, src, dst string) error {
	info, err := fs.Stat(fsys, src)
	if err != nil {
		return fmt.Errorf("membaca %s: %w", src, err)
	}
	in, err := fsys.Open(src)
	if err != nil {
		return fmt.Errorf("membuka %s: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("membuat berkas sementara untuk %s: %w", dst, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return fmt.Errorf("menyalin %s: %w", src, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("menyinkronkan %s: %w", dst, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("menutup berkas sementara %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("mengatur mode %s: %w", dst, err)
	}

	if err := os.Rename(tmpName, dst); err != nil {
		if rmErr := os.Remove(dst); rmErr == nil {
			if err := os.Rename(tmpName, dst); err == nil {
				return nil
			}
		}
		if st, statErr := os.Stat(dst); statErr == nil && st.Mode().IsRegular() && st.Size() == info.Size() {
			// Berkas tujuan sudah ditulis oleh ekstraksi lain yang berjalan
			// bersamaan; isinya identik, jadi anggap berhasil.
			return nil
		}
		return fmt.Errorf("memindahkan %s ke %s: %w", tmpName, dst, err)
	}
	return nil
}
```

- [ ] **Step 4: Jalankan tes, pastikan lulus**

Run: `go test ./internal/bundle/ -v`
Expected: PASS untuk seluruh tes (ekstraksi, ekstraksi ulang, pemulihan berkas
bundel yang hilang, DLL Windows, tidak ada berkas sementara tertinggal, temp
basi dibersihkan, dan ekstraksi paralel).

- [ ] **Step 5: Commit**

```bash
git add internal/bundle
git commit -m "feat(bundle): ekstrak biner adb tertanam ke folder data"
```

---

## Task 3: Menjalankan `adb` (`internal/adbx`, bagian 1)

**Files:**
- Create: `internal/adbx/adbx.go`
- Create: `internal/adbx/errors.go` (sementara, isi lengkap di Task 4)
- Test: `internal/adbx/adbx_test.go`

Catatan semantik penting: bila konteks dibatalkan, `osExec.Run` mengembalikan
`ctx.Err()` (bukan `*exec.ExitError`) sehingga `errors.Is(err, context.Canceled)`
dan `errors.Is(err, context.DeadlineExceeded)` bekerja; Task 10 bergantung pada ini
untuk menandai job yang dibatalkan. `New` juga memasang batas waktu pengaman bawaan
15 menit untuk setiap perintah agar adb yang menggantung tidak memblokir selamanya;
batas itu bukan timeout UI, dan pembatalan eksplisit dari antrean tetap berlaku
seketika.

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/adbx/adbx_test.go`:

```go
package adbx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeExec struct {
	results []Result
	calls   [][]string
}

func (f *fakeExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if len(f.results) == 0 {
		return Result{}, nil
	}
	r := f.results[0]
	f.results = f.results[1:]
	return r, nil
}

func TestRunPrependsSerial(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "ok"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("ABC123"))
	if _, err := r.Run(context.Background(), "shell", "id"); err != nil {
		t.Fatal(err)
	}
	got := fe.calls[0]
	want := []string{"/usr/bin/adb", "-s", "ABC123", "shell", "id"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestRunWithoutSerialOmitsFlag(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "ok"}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	if _, err := r.Run(context.Background(), "devices"); err != nil {
		t.Fatal(err)
	}
	if fe.calls[0][1] != "devices" {
		t.Fatalf("flag -s seharusnya tidak ada: %v", fe.calls[0])
	}
}

// blockingExec menunggu konteks selesai lalu mengembalikan ctx.Err(), meniru
// proses yang dibunuh saat konteks dibatalkan.
type blockingExec struct{}

func (blockingExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	<-ctx.Done()
	return Result{}, ctx.Err()
}

func TestRunPropagatesContextCancellation(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(blockingExec{}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Run(ctx, "shell", "id")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

type deadlineExec struct{}

func (deadlineExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return Result{}, context.DeadlineExceeded
}

func TestRunPropagatesDeadlineExceeded(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(deadlineExec{}))
	_, err := r.Run(context.Background(), "shell", "id")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
}

func TestRunClassifiesNonZeroExit(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stderr: "error: device offline", ExitCode: 1}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	_, err := r.Run(context.Background(), "shell", "id")
	if !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("got %v, want ErrDeviceNotFound", err)
	}
}

type errorExec struct{ err error }

func (e *errorExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return Result{}, e.err
}

func TestRunWrapsNonExitError(t *testing.T) {
	perr := &fs.PathError{Op: "fork/exec", Path: "/usr/bin/adb", Err: errors.New("no such file")}
	r := New("/usr/bin/adb", WithExecer(&errorExec{err: perr}))
	_, err := r.Run(context.Background(), "devices")
	if err == nil {
		t.Fatal("ingin error, dapat nil")
	}
	if !strings.Contains(err.Error(), "/usr/bin/adb") {
		t.Fatalf("error harus menyebut path adb: %v", err)
	}
	if !strings.Contains(err.Error(), "menjalankan ") {
		t.Fatalf("error harus memakai pesan yang menyebut subjek: %v", err)
	}
	var got *fs.PathError
	if !errors.As(err, &got) {
		t.Fatalf("error harus membungkus PathError: %v", err)
	}
}

func TestOutputTrimsWhitespace(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: " hello\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.Output(context.Background(), "shell", "echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("got %q want %q", got, "hello")
	}
}

func TestWithTimeoutZeroOrNegativeMeansNoTimeout(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "ok"}, {Stdout: "ok"}}}
	for _, d := range []time.Duration{0, -time.Second} {
		r := New("/usr/bin/adb", WithExecer(fe), WithTimeout(d))
		if _, err := r.Run(context.Background(), "devices"); err != nil {
			t.Fatalf("timeout %v: %v", d, err)
		}
	}
}

func TestWithExecerNilKeepsDefault(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(nil))
	if r.exec == nil {
		t.Fatal("exec tidak boleh nil")
	}
}

func TestNewDefaultTimeout(t *testing.T) {
	r := New("/usr/bin/adb")
	if r.timeout != 15*time.Minute {
		t.Fatalf("timeout bawaan = %v, want 15m", r.timeout)
	}
}

// safeExec aman dipakai bersamaan dan merekam seluruh panggilan.
type safeExec struct {
	mu    sync.Mutex
	calls [][]string
}

func (s *safeExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	s.mu.Lock()
	s.calls = append(s.calls, append([]string{name}, args...))
	s.mu.Unlock()
	return Result{Stdout: "ok"}, nil
}

func TestRunConcurrentSameRunner(t *testing.T) {
	const n = 8
	se := &safeExec{}
	r := New("/usr/bin/adb", WithExecer(se), WithSerial("SER"))

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			arg := fmt.Sprintf("arg-%d", i)
			if _, err := r.Run(context.Background(), "shell", arg); err != nil {
				t.Errorf("goroutine %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	se.mu.Lock()
	defer se.mu.Unlock()
	if len(se.calls) != n {
		t.Fatalf("harus %d panggilan, dapat %d", n, len(se.calls))
	}
	wantPrefix := []string{"/usr/bin/adb", "-s", "SER", "shell"}
	seen := make(map[string]bool, n)
	for _, c := range se.calls {
		if len(c) != 5 {
			t.Fatalf("jumlah args tak terduga: %v", c)
		}
		for i := range wantPrefix {
			if c[i] != wantPrefix[i] {
				t.Fatalf("prefix args salah: %v", c)
			}
		}
		seen[c[4]] = true
	}
	for i := 0; i < n; i++ {
		if !seen[fmt.Sprintf("arg-%d", i)] {
			t.Fatalf("arg-%d hilang (cross-talk): %v", i, se.calls)
		}
	}
}

// TestHelperProcess bukan tes biasa: ia hanya tidur ketika dijalankan sebagai
// proses anak oleh tes lain.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("ADBX_HELPER_PROCESS") != "1" {
		return
	}
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

func TestOSExecPropagatesCancellation(t *testing.T) {
	t.Setenv("ADBX_HELPER_PROCESS", "1")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	defer cancel()
	_, err := (osExec{}).Run(ctx, os.Args[0], "-test.run=TestHelperProcess")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestRunnerTimeoutFires(t *testing.T) {
	t.Setenv("ADBX_HELPER_PROCESS", "1")
	r := New(os.Args[0], WithTimeout(300*time.Millisecond))
	_, err := r.Run(context.Background(), "-test.run=TestHelperProcess")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
}

func TestWithSerialClonesRunner(t *testing.T) {
	r := New("/usr/bin/adb")
	r2 := r.WithSerial("S9")
	if got := r.args("devices"); len(got) != 1 || got[0] != "devices" {
		t.Fatalf("runner asal terubah: %v", got)
	}
	got := r2.args("devices")
	want := []string{"-s", "S9", "devices"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
```

Dua tes memakai pola helper-process (menjalankan ulang biner tes sebagai proses anak) untuk menguji `osExec` asli: `TestOSExecPropagatesCancellation` (pembatalan nyata) dan `TestRunnerTimeoutFires` (timeout nyata).

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/adbx/`
Expected: FAIL, `undefined: New`.

- [ ] **Step 3: Implementasi**

Create `internal/adbx/adbx.go`:

```go
// Package adbx menjalankan perintah adb dan mengurai keluarannya.
package adbx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Result adalah keluaran satu perintah adb.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Execer menjalankan sebuah program. Implementasi asli memakai os/exec;
// pengujian menyuntikkan versi tiruan. Implementasi harus aman dipakai
// bersamaan (concurrent-safe) karena satu Runner dapat dipakai banyak goroutine.
type Execer interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

type osExec struct{}

func (osExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errBuf.String()}
	if err != nil {
		// Bila konteks dibatalkan, exec.CommandContext membunuh proses dan
		// mengembalikan *exec.ExitError. Kembalikan ctx.Err() agar pemanggil
		// dapat mengenali context.Canceled / context.DeadlineExceeded.
		if cerr := ctx.Err(); cerr != nil {
			return res, cerr
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Runner menjalankan perintah adb untuk satu perangkat (bila serial diisi).
type Runner struct {
	adbPath string
	exec    Execer
	serial  string
	timeout time.Duration
}

type Option func(*Runner)

// WithExecer mengganti pelaksana perintah. Nilai nil diabaikan sehingga
// Runner tetap memakai pelaksana bawaan dan tidak panik saat dipanggil.
func WithExecer(e Execer) Option {
	return func(r *Runner) {
		if e != nil {
			r.exec = e
		}
	}
}

func WithSerial(serial string) Option {
	return func(r *Runner) { r.serial = serial }
}

// WithTimeout memberi batas waktu pengaman untuk setiap perintah adb. Nilai
// nol atau negatif berarti tanpa batas waktu.
func WithTimeout(d time.Duration) Option {
	return func(r *Runner) { r.timeout = d }
}

// New membuat Runner. Batas waktu bawaan 15 menit dipasang sebagai jaring
// pengaman agar adb yang menggantung tidak memblokir selamanya.
func New(adbPath string, opts ...Option) *Runner {
	r := &Runner{adbPath: adbPath, exec: osExec{}, timeout: 15 * time.Minute}
	for _, o := range opts {
		o(r)
	}
	return r
}

// WithSerial mengembalikan salinan Runner yang menargetkan serial tertentu.
// Ini salinan dangkal (shallow): aman hanya selama Runner memegang field
// skalar/interface saja.
func (r *Runner) WithSerial(serial string) *Runner {
	clone := *r
	clone.serial = serial
	return &clone
}

func (r *Runner) args(rest ...string) []string {
	args := make([]string, 0, len(rest)+2)
	if r.serial != "" {
		args = append(args, "-s", r.serial)
	}
	return append(args, rest...)
}

// Run menjalankan perintah adb. Error yang dikembalikan sudah diterjemahkan
// bila keluarannya cocok dengan pola kegagalan yang dikenal.
func (r *Runner) Run(ctx context.Context, rest ...string) (Result, error) {
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}
	res, err := r.exec.Run(ctx, r.adbPath, r.args(rest...)...)
	if err != nil {
		return res, fmt.Errorf("menjalankan %s %s: %w", r.adbPath, strings.Join(rest, " "), err)
	}
	if res.ExitCode != 0 {
		return res, Classify(res)
	}
	return res, nil
}

// Output menjalankan perintah dan mengembalikan stdout yang sudah dipangkas.
func (r *Runner) Output(ctx context.Context, rest ...string) (string, error) {
	res, err := r.Run(ctx, rest...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

- [ ] **Step 4: Tambahkan `Classify` agar tes bisa dikompilasi**

Create `internal/adbx/errors.go` (sementara, isi lengkap di Task 4):

```go
package adbx

import (
	"errors"
	"strings"
)

// ErrDeviceNotFound dikembalikan bila adb melaporkan perangkat tidak ada atau
// sedang offline. Sentinel lain serta daftar pola lengkap ditambahkan pada Task 4.
var ErrDeviceNotFound = errors.New("perangkat tidak ditemukan")

// Classify menerjemahkan keluaran adb yang gagal menjadi error yang jelas.
// Daftar lengkap pola ditambahkan pada Task 4.
func Classify(res Result) error {
	lower := strings.ToLower(res.Stderr + "\n" + res.Stdout)
	switch {
	case strings.Contains(lower, "device not found"),
		strings.Contains(lower, "device offline"),
		strings.Contains(lower, "no devices/emulators found"):
		return ErrDeviceNotFound
	default:
		return &CommandError{Result: res}
	}
}

// CommandError adalah kegagalan adb yang belum dikenali polanya.
type CommandError struct {
	Result Result
}

func (e *CommandError) Error() string {
	msg := firstLine(e.Result.Stderr)
	if msg == "" {
		msg = firstLine(e.Result.Stdout)
	}
	if msg == "" {
		msg = "perintah adb gagal"
	}
	return msg
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}
```

Catatan: tes `TestRunClassifiesNonZeroExit` memakai `ErrDeviceNotFound` supaya
jalur "exit code != 0 diproses `Classify`" bisa diverifikasi sejak Task 3; Task 4
mengganti seluruh isi `errors.go` dengan daftar sentinel dan pola yang lengkap.
Catatan: pola `strings.Contains(lower, "device not found")` di sini belum
menangkap nomor seri (`device 'SERIAL' not found`); perbaikan lengkapnya ada di
Task 4 lewat `looksLikeDeviceNotFound`.

- [ ] **Step 5: Jalankan tes, pastikan lulus**

Run: `go test ./internal/adbx/ -v`
Expected: PASS untuk seluruh tes.

- [ ] **Step 6: Commit**

```bash
git add internal/adbx
git commit -m "feat(adbx): runner adb dengan exec yang bisa disuntik"
```

---

## Task 4: Menerjemahkan kegagalan `adb` (`internal/adbx/errors.go`)

**Files:**
- Modify: `internal/adbx/errors.go`
- Test: `internal/adbx/errors_test.go`

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/adbx/errors_test.go`:

```go
package adbx

import (
	"errors"
	"testing"
)

func TestClassifyKnownFailures(t *testing.T) {
	cases := []struct {
		name    string
		res     Result
		wantErr error
	}{
		{
			name:    "device not found",
			res:     Result{Stderr: "error: device 'ABC' not found", ExitCode: 1},
			wantErr: ErrDeviceNotFound,
		},
		{
			name:    "unauthorized",
			res:     Result{Stderr: "error: device unauthorized", ExitCode: 1},
			wantErr: ErrUnauthorized,
		},
		{
			name:    "no space",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_INSUFFICIENT_STORAGE]", ExitCode: 1},
			wantErr: ErrInsufficientStorage,
		},
		{
			name:    "already exists",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_ALREADY_EXISTS]", ExitCode: 1},
			wantErr: ErrAlreadyExists,
		},
		{
			name:    "downgrade",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_VERSION_DOWNGRADE]", ExitCode: 1},
			wantErr: ErrDowngrade,
		},
		{
			name:    "signature mismatch",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE: Package com.foo signatures do not match previously installed version]", ExitCode: 1},
			wantErr: ErrSignatureMismatch,
		},
		{
			name:    "invalid apk",
			res:     Result{Stderr: "Failure [INSTALL_PARSE_FAILED_NOT_APK]", ExitCode: 1},
			wantErr: ErrInvalidApk,
		},
		{
			name:    "older sdk",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_OLDER_SDK]", ExitCode: 1},
			wantErr: ErrNeedsNewerAndroid,
		},
		{
			name:    "shell permission denied",
			res:     Result{Stderr: "shell: permission denied", ExitCode: 1},
			wantErr: ErrShellPermission,
		},
		{
			name:    "package not found",
			res:     Result{Stderr: "Failure [DELETE_FAILED_INTERNAL_ERROR]\nFailure [not installed for 0]", ExitCode: 1},
			wantErr: ErrPackageNotFound,
		},
		{
			name:    "system app",
			res:     Result{Stderr: "Failure [DELETE_FAILED_INTERNAL_ERROR]", ExitCode: 1},
			wantErr: ErrSystemApp,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Classify(tc.res)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestClassifyUnknownFallsBackToCommandError(t *testing.T) {
	err := Classify(Result{Stderr: "sesuatu yang aneh", ExitCode: 1})
	var ce *CommandError
	if !errors.As(err, &ce) {
		t.Fatalf("seharusnya CommandError, dapat %T", err)
	}
	if got, want := err.Error(), "perintah adb gagal: sesuatu yang aneh"; got != want {
		t.Fatalf("pesan salah: got %q, want %q", got, want)
	}
}

func TestClassifyNewSentinelMessages(t *testing.T) {
	cases := []struct {
		name string
		res  Result
		want error
		msg  string
	}{
		{
			name: "signature mismatch",
			res:  Result{Stderr: "Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE]"},
			want: ErrSignatureMismatch,
			msg:  "aplikasi dengan nama paket sama sudah terpasang dengan tanda tangan berbeda; copot dulu yang lama",
		},
		{
			name: "invalid apk",
			res:  Result{Stderr: "Failure [INSTALL_PARSE_FAILED_NOT_APK]"},
			want: ErrInvalidApk,
			msg:  "berkas APK tidak sah atau tidak ditandatangani",
		},
		{
			name: "older sdk",
			res:  Result{Stderr: "Failure [INSTALL_FAILED_OLDER_SDK]"},
			want: ErrNeedsNewerAndroid,
			msg:  "APK ini butuh versi Android yang lebih baru",
		},
		{
			name: "shell permission",
			res:  Result{Stderr: "Security exception: insufficient permissions"},
			want: ErrShellPermission,
			msg:  "perangkat menolak perintah ini (izin shell kurang)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Classify(tc.res)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if got := err.Error(); got != tc.msg {
				t.Fatalf("pesan salah: got %q, want %q", got, tc.msg)
			}
		})
	}
}

func TestClassifyDoesNotBridgeLines(t *testing.T) {
	// "not found" tanpa penanda "device" bukan device-not-found.
	err := Classify(Result{Stderr: "package com.foo not found", ExitCode: 1})
	if errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("tidak boleh ErrDeviceNotFound: %v", err)
	}
	var ce *CommandError
	if !errors.As(err, &ce) {
		t.Fatalf("seharusnya CommandError, dapat %T", err)
	}

	// "device ..." di satu baris tidak boleh dijembatani dengan "not found" di
	// baris lain.
	err = Classify(Result{
		Stderr:   "Failure [INSTALL_FAILED_INSUFFICIENT_STORAGE]",
		Stdout:   "device status: ok\nremote object not found",
		ExitCode: 1,
	})
	if !errors.Is(err, ErrInsufficientStorage) {
		t.Fatalf("got %v, want ErrInsufficientStorage", err)
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/adbx/ -run TestClassify`
Expected: FAIL, `undefined: ErrDeviceNotFound`.

- [ ] **Step 3: Implementasi**

Replace seluruh isi `internal/adbx/errors.go`:

```go
package adbx

import (
	"errors"
	"strings"
)

var (
	ErrDeviceNotFound      = errors.New("perangkat tidak ditemukan")
	ErrUnauthorized        = errors.New("perangkat belum diizinkan")
	ErrInsufficientStorage = errors.New("penyimpanan perangkat penuh")
	ErrAlreadyExists       = errors.New("aplikasi sudah terpasang")
	ErrDowngrade           = errors.New("versi lebih rendah dari yang terpasang")
	ErrPackageNotFound     = errors.New("aplikasi tidak terpasang")
	ErrSystemApp           = errors.New("aplikasi sistem tidak boleh dicopot")
	ErrSignatureMismatch   = errors.New("aplikasi dengan nama paket sama sudah terpasang dengan tanda tangan berbeda; copot dulu yang lama")
	ErrInvalidApk          = errors.New("berkas APK tidak sah atau tidak ditandatangani")
	ErrNeedsNewerAndroid   = errors.New("APK ini butuh versi Android yang lebih baru")
	ErrShellPermission     = errors.New("perangkat menolak perintah ini (izin shell kurang)")
)

// Classify menerjemahkan keluaran adb yang gagal menjadi error yang bisa
// ditindaklanjuti. Urutan pemeriksaan penting: pola yang lebih spesifik lebih
// dulu.
func Classify(res Result) error {
	text := res.Stderr + "\n" + res.Stdout
	lower := strings.ToLower(text)

	switch {
	case strings.Contains(lower, "not installed for"):
		return ErrPackageNotFound
	case strings.Contains(lower, "update_incompatible"),
		strings.Contains(lower, "signatures do not match"):
		return ErrSignatureMismatch
	case strings.Contains(lower, "parse_failed"),
		strings.Contains(lower, "invalid_apk"):
		return ErrInvalidApk
	case strings.Contains(lower, "older_sdk"),
		strings.Contains(lower, "requires newer sdk"):
		return ErrNeedsNewerAndroid
	case strings.Contains(lower, "device unauthorized"),
		strings.Contains(lower, "unauthorized"):
		return ErrUnauthorized
	case strings.Contains(lower, "insufficient permissions"),
		strings.Contains(lower, "permission denied"):
		return ErrShellPermission
	case looksLikeDeviceNotFound(lower),
		strings.Contains(lower, "device offline"),
		strings.Contains(lower, "no devices/emulators found"):
		return ErrDeviceNotFound
	case strings.Contains(lower, "insufficient_storage"):
		return ErrInsufficientStorage
	case strings.Contains(lower, "already_exists"):
		return ErrAlreadyExists
	case strings.Contains(lower, "version_downgrade"):
		return ErrDowngrade
	case strings.Contains(lower, "delete_failed"):
		// Heuristik: DELETE_FAILED_INTERNAL_ERROR biasanya berarti paket
		// sistem tidak boleh dicopot, tetapi kadang hanya berarti paketnya
		// sudah tidak ada. Arm "not installed for" di atas lebih dulu,
		// sehingga kasus paket hilang tetap terklasifikasi benar bila kedua
		// penanda muncul bersamaan.
		return ErrSystemApp
	default:
		return &CommandError{Result: res}
	}
}

// CommandError adalah kegagalan adb yang belum dikenali polanya.
type CommandError struct {
	Result Result
}

func (e *CommandError) Error() string {
	msg := firstLine(e.Result.Stderr)
	if msg == "" {
		msg = firstLine(e.Result.Stdout)
	}
	if msg == "" {
		msg = "tanpa keluaran"
	}
	return "perintah adb gagal: " + msg
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

// looksLikeDeviceNotFound cocok untuk pola "device not found" maupun
// "device 'SERIAL' not found". strings.Contains(lower, "device not found")
// tidak cukup karena adb menyisipkan nomor seri di antara "device" dan
// "not found". Pemeriksaan dilakukan per baris supaya keluaran stderr dan
// stdout tidak saling menjembatani pola.
func looksLikeDeviceNotFound(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		i := strings.Index(line, "device")
		if i >= 0 && strings.Contains(line[i:], "not found") {
			return true
		}
	}
	return false
}
```

Catatan: urutan `case` di `Classify` bersifat penting dan tidak boleh diubah
sembarangan. `not installed for` harus diperiksa sebelum `delete_failed`, dan
pola `unauthorized` sebelum device-not-found, karena satu keluaran adb dapat
memuat beberapa penanda sekaligus.

- [ ] **Step 4: Pastikan tidak ada kode sisa**

Pastikan `errors` masih dipakai (untuk `errors.New` pada blok `var`). Cek dengan:

Run: `go vet ./internal/adbx/`
Expected: tidak ada keluaran. Bila muncul `"errors" imported and not used`, hapus
import tersebut.

- [ ] **Step 5: Jalankan tes, pastikan lulus**

Run: `go test ./internal/adbx/ -v`
Expected: PASS untuk seluruh tes `Classify` dan `CommandError`.

- [ ] **Step 6: Commit**

```bash
git add internal/adbx
git commit -m "feat(adbx): terjemahkan kegagalan adb menjadi error yang jelas"
```

---

## Task 5: Membaca daftar perangkat dan paket (`internal/adbx/query.go`)

**Files:**
- Create: `internal/adbx/query.go`
- Test: `internal/adbx/query_test.go`

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/adbx/query_test.go`:

```go
package adbx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const devicesSample = `List of devices attached
R58M12ABCDE            device product:beyond1lte model:SM_G973F device:beyond1 transport_id:1
0123456789ABCDEF       unauthorized transport_id:2
192.168.1.9:5555       offline transport_id:3

`

func TestDevicesParsesLines(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: devicesSample}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("harus 3 perangkat, dapat %d: %+v", len(got), got)
	}
	if got[0].Serial != "R58M12ABCDE" || got[0].State != "device" || got[0].Model != "SM_G973F" {
		t.Fatalf("baris pertama salah: %+v", got[0])
	}
	if got[0].Product != "beyond1lte" || got[0].DeviceName != "beyond1" {
		t.Fatalf("product/device baris pertama salah: %+v", got[0])
	}
	if got[1].Serial != "0123456789ABCDEF" {
		t.Fatalf("serial baris kedua salah: %+v", got[1])
	}
	if got[1].State != "unauthorized" {
		t.Fatalf("baris kedua salah: %+v", got[1])
	}
	if got[2].Serial != "192.168.1.9:5555" || got[2].State != "offline" {
		t.Fatalf("baris ketiga salah: %+v", got[2])
	}
}

const packagesSample = `package:/data/app/~~Ab==/com.foo-abc==/base.apk=com.foo versionCode:42
package:/data/app/~~Cd==/com.bar-xyz==/base.apk=com.bar versionCode:7
`

func TestPackagesParsesNameAndVersion(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: packagesSample}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	got, err := r.Packages(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("harus 2 paket, dapat %d", len(got))
	}
	if got[0].Name != "com.foo" || got[0].VersionCode != 42 {
		t.Fatalf("paket pertama salah: %+v", got[0])
	}
	if got[0].ApkPath != "/data/app/~~Ab==/com.foo-abc==/base.apk" {
		t.Fatalf("path salah: %q", got[0].ApkPath)
	}
	if got[1].System {
		t.Fatal("paket pihak ketiga tidak boleh ditandai sistem")
	}
}

func TestPackagesFallsBackWhenVersionCodeUnsupported(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stderr: "Error: Unknown option: --show-versioncode", ExitCode: 1},
		{Stdout: "package:/data/app/com.foo/base.apk=com.foo"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.Packages(context.Background(), false)
	if err != nil {
		t.Fatalf("harus jatuh ke perintah tanpa flag: %v", err)
	}
	if len(got) != 1 || got[0].Name != "com.foo" {
		t.Fatalf("hasil fallback salah: %+v", got)
	}
	if !strings.Contains(strings.Join(callsOf(fe), " "), "pm list packages") {
		t.Fatal("perintah pm list packages tidak dijalankan")
	}
	calls := callsOf(fe)
	if len(calls) < 2 {
		t.Fatalf("harus 2 panggilan: %v", calls)
	}
	if strings.Contains(calls[1], "--show-versioncode") {
		t.Fatalf("panggilan kedua tidak boleh memakai --show-versioncode: %q", calls[1])
	}
}

func TestPackagesRetriesWhenFirstCallParsesEmpty(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "Unknown option: --show-versioncode"},
		{Stdout: packagesSample},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	got, err := r.Packages(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("harus 2 paket, dapat %d: %+v", len(got), got)
	}
	calls := callsOf(fe)
	if len(calls) != 2 {
		t.Fatalf("harus 2 panggilan: %v", calls)
	}
	if strings.Contains(calls[1], "--show-versioncode") {
		t.Fatalf("panggilan kedua tidak boleh memakai --show-versioncode: %q", calls[1])
	}
}

func TestPackagesEmptyLegitDoesNotRetry(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: ""}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	got, err := r.Packages(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("harus 0 paket, dapat %d", len(got))
	}
	if calls := callsOf(fe); len(calls) != 1 {
		t.Fatalf("daftar kosong yang wajar tidak boleh memicu panggilan ulang: %v", calls)
	}
}

func TestPackagesSystemUsesSFlag(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: packagesSample}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	got, err := r.Packages(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(callsOf(fe), " "), "list packages -s") {
		t.Fatalf("flag -s tidak dipakai: %v", callsOf(fe))
	}
	if len(got) != 2 || !got[0].System {
		t.Fatalf("paket sistem tidak ditandai: %+v", got)
	}
}

func TestParseDevicesLineTable(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []Device
	}{
		{
			name: "baris daemon * dilewati",
			line: "* daemon not running; starting now at tcp:5037",
			want: nil,
		},
		{
			name: "no permissions dipetakan ke offline",
			line: "????????????\tno permissions (user in plugdev group; are your udev rules wrong?)",
			want: []Device{{Serial: "????????????", State: "offline"}},
		},
		{
			name: "status tak dikenal dipetakan ke offline",
			line: "ABC123\tfrobnicating transport_id:9",
			want: []Device{{Serial: "ABC123", State: "offline"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDevices(tc.line)
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
			for i := range tc.want {
				if got[i].Serial != tc.want[i].Serial || got[i].State != tc.want[i].State {
					t.Fatalf("got %+v want %+v", got, tc.want)
				}
			}
		})
	}
}

func callsOf(fe *fakeExec) []string {
	out := make([]string, 0, len(fe.calls))
	for _, c := range fe.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

const dumpsysSample = `Packages:
  Package [com.example.app] (a1b2c3):
    userId=10123
    codePath=/data/app/~~Ab==/com.example.app-x==/base.apk
    versionName=1.2.3
    versionCode=42 minSdk=21 targetSdk=33
    firstInstallTime=2024-01-01 10:00:00
    lastUpdateTime=2024-02-02 11:00:00
    dataDir=/data/user/0/com.example.app
    requested permissions:
      android.permission.INTERNET
      android.permission.CAMERA
    flags=[ HAS_CODE ALLOW_CLEAR_USER_DATA ALLOW_BACKUP ]
`

func TestPackageInfoParsesDumpsys(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: dumpsysSample},
		{Stdout: "1234\t/data/user/0/com.example.app"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if got.VersionName != "1.2.3" || got.VersionCode != 42 {
		t.Fatalf("versi salah: %+v", got)
	}
	if got.ApkPath != "/data/app/~~Ab==/com.example.app-x==/base.apk" {
		t.Fatalf("apkPath salah: %q", got.ApkPath)
	}
	if got.InstallTime != "2024-01-01 10:00:00" {
		t.Fatalf("installTime salah: %q", got.InstallTime)
	}
	if got.UpdateTime != "2024-02-02 11:00:00" {
		t.Fatalf("updateTime salah: %q", got.UpdateTime)
	}
	if got.DataDir != "/data/user/0/com.example.app" {
		t.Fatalf("dataDir salah: %q", got.DataDir)
	}
	if len(got.Permissions) != 2 {
		t.Fatalf("izin salah: %+v", got.Permissions)
	}
	if got.Permissions[0] != "android.permission.INTERNET" ||
		got.Permissions[1] != "android.permission.CAMERA" {
		t.Fatalf("nilai izin salah: %+v", got.Permissions)
	}
	if got.System {
		t.Fatal("paket di /data/app bukan aplikasi sistem")
	}
	if got.SizeBytes != 1234*1024 {
		t.Fatalf("ukuran salah: %d", got.SizeBytes)
	}
}

func TestPackageInfoParsesNamespacedPermissions(t *testing.T) {
	sample := strings.Replace(dumpsysSample,
		"      android.permission.CAMERA",
		"      android.permission.CAMERA\n      org.example.permission.FOO\n      com.vendor.permission.BAR", 1)
	fe := &fakeExec{results: []Result{
		{Stdout: sample},
		{Stdout: "10\t/data/user/0/com.example.app"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Permissions) != 4 {
		t.Fatalf("izin salah: %+v", got.Permissions)
	}
	if got.Permissions[2] != "org.example.permission.FOO" ||
		got.Permissions[3] != "com.vendor.permission.BAR" {
		t.Fatalf("izin namespace salah: %+v", got.Permissions)
	}
}

func TestPackageInfoGarbageReturnsNotFound(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "bukan keluaran dumpsys"}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	_, err := r.PackageInfo(context.Background(), "com.example.app")
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("got %v, want ErrPackageNotFound", err)
	}
}

func TestPackageInfoWithoutVersionName(t *testing.T) {
	sample := strings.Replace(dumpsysSample, "    versionName=1.2.3\n", "", 1)
	fe := &fakeExec{results: []Result{
		{Stdout: sample},
		{Stdout: "10\t/data/user/0/com.example.app"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if got.VersionName != "" || got.DataDir != "/data/user/0/com.example.app" {
		t.Fatalf("hasil salah: %+v", got)
	}
}

func TestPackageInfoMarksApexAsSystem(t *testing.T) {
	sample := strings.Replace(dumpsysSample,
		"codePath=/data/app/~~Ab==/com.example.app-x==/base.apk",
		"codePath=/apex/com.android.foo/foo.apk", 1)
	fe := &fakeExec{results: []Result{
		{Stdout: sample},
		{Stdout: "10\t/apex/com.android.foo"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if !got.System {
		t.Fatal("paket di /apex harus ditandai sistem")
	}
}

func TestPackageInfoMarksSystemApp(t *testing.T) {
	sample := strings.Replace(dumpsysSample,
		"codePath=/data/app/~~Ab==/com.example.app-x==/base.apk",
		"codePath=/system/priv-app/Foo/Foo.apk", 1)
	fe := &fakeExec{results: []Result{
		{Stdout: sample},
		{Stdout: "10\t/system/priv-app/Foo"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if !got.System {
		t.Fatal("paket di /system harus ditandai sistem")
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/adbx/ -run 'TestDevices|TestPackages|TestPackageInfo'`
Expected: FAIL, `undefined: ...Devices`.

- [ ] **Step 3: Implementasi**

Create `internal/adbx/query.go`:

```go
package adbx

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Device adalah satu baris dari `adb devices -l`.
type Device struct {
	Serial     string
	State      string // device, unauthorized, offline, ...
	Model      string
	Product    string
	DeviceName string
}

// Package adalah satu aplikasi yang terpasang di perangkat.
type Package struct {
	Name        string `json:"name"`
	ApkPath     string `json:"apkPath,omitempty"`
	VersionCode int64  `json:"versionCode,omitempty"`
	System      bool   `json:"system,omitempty"`
}

// PackageInfo adalah detail satu aplikasi, untuk panel detail di UI.
type PackageInfo struct {
	Package     string   `json:"package"`
	VersionName string   `json:"versionName,omitempty"`
	VersionCode int64    `json:"versionCode,omitempty"`
	InstallTime string   `json:"installTime,omitempty"`
	UpdateTime  string   `json:"updateTime,omitempty"`
	ApkPath     string   `json:"apkPath,omitempty"`
	DataDir     string   `json:"dataDir,omitempty"`
	SizeBytes   int64    `json:"sizeBytes,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	System      bool     `json:"system,omitempty"`
}

func (r *Runner) Devices(ctx context.Context) ([]Device, error) {
	out, err := r.Output(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	return parseDevices(out), nil
}

func parseDevices(out string) []Device {
	var devs []Device
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" ||
			strings.HasPrefix(line, "List of devices") ||
			strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := Device{Serial: fields[0], State: normalizeState(fields[1])}
		for _, f := range fields[2:] {
			k, v, ok := strings.Cut(f, ":")
			if !ok {
				continue
			}
			switch k {
			case "model":
				d.Model = v
			case "product":
				d.Product = v
			case "device":
				d.DeviceName = v
			}
		}
		devs = append(devs, d)
	}
	return devs
}

// normalizeState memetakan status yang tidak dikenal (misalnya baris
// "no permissions (user in plugdev group; are your udev rules wrong?)") ke
// "offline" supaya UI tidak menampilkan string yang kacau.
func normalizeState(s string) string {
	switch s {
	case "device", "unauthorized", "offline", "bootloader", "recovery", "sideload", "rescue", "authorizing":
		return s
	default:
		return "offline"
	}
}

// Packages mengembalikan daftar aplikasi. system=true meminta aplikasi sistem.
func (r *Runner) Packages(ctx context.Context, system bool) ([]Package, error) {
	flag := "-3"
	if system {
		flag = "-s"
	}
	out, err := r.Output(ctx, "shell", "pm", "list", "packages", flag, "-f", "--show-versioncode")
	retried := false
	if err != nil {
		// Perangkat lama belum mendukung --show-versioncode.
		out, err = r.Output(ctx, "shell", "pm", "list", "packages", flag, "-f")
		if err != nil {
			return nil, err
		}
		retried = true
	}
	pkgs := parsePackages(out, system)
	if len(pkgs) == 0 && !retried && looksLikeUnsupportedFlag(out) {
		// Sebagian build adb mencetak "Unknown option" ke stdout tetapi tetap
		// keluar dengan status 0; ulangi tanpa --show-versioncode.
		if out2, err2 := r.Output(ctx, "shell", "pm", "list", "packages", flag, "-f"); err2 == nil {
			pkgs = parsePackages(out2, system)
		}
	}
	return pkgs, nil
}

// looksLikeUnsupportedFlag mendeteksi keluaran yang menandakan flag tidak
// dikenali, supaya daftar kosong yang wajar tidak memicu panggilan ulang.
func looksLikeUnsupportedFlag(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "unknown option") ||
		strings.Contains(lower, "error")
}

func parsePackages(out string, system bool) []Package {
	var pkgs []Package
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		rest := strings.TrimPrefix(line, "package:")
		p := Package{System: system}
		if i := strings.Index(rest, " versionCode:"); i >= 0 {
			code, _ := strconv.ParseInt(strings.TrimSpace(rest[i+len(" versionCode:"):]), 10, 64)
			p.VersionCode = code
			rest = rest[:i]
		}
		if i := strings.LastIndex(rest, "="); i >= 0 {
			p.ApkPath = rest[:i]
			p.Name = rest[i+1:]
		} else {
			p.Name = rest
		}
		if p.Name == "" {
			continue
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// PackageInfo mengambil detail satu aplikasi: versi, ukuran, izin, dan
// apakah ia bagian dari sistem.
func (r *Runner) PackageInfo(ctx context.Context, pkg string) (PackageInfo, error) {
	out, err := r.Output(ctx, "shell", "dumpsys", "package", pkg)
	if err != nil {
		return PackageInfo{}, err
	}
	info := parseDumpsys(pkg, out)
	if info.Package == "" {
		return PackageInfo{}, fmt.Errorf("%w: %s", ErrPackageNotFound, pkg)
	}
	if info.DataDir != "" {
		// Bila `du` gagal, SizeBytes tetap 0 yang berarti "ukuran tidak
		// diketahui".
		if size, err := r.Output(ctx, "shell", "du", "-sk", info.DataDir); err == nil {
			info.SizeBytes = parseDU(size)
		}
	}
	return info, nil
}

func parseDumpsys(pkg, out string) PackageInfo {
	info := PackageInfo{}
	found := false
	inPermissions := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !found && strings.HasPrefix(line, "Package [") {
			// Kehadiran paket dideteksi dari header "Package [<nama>]", bukan
			// dari kelengkapan field versi.
			found = true
			info.Package = pkg
		}
		if line == "requested permissions:" {
			inPermissions = true
			continue
		}
		if inPermissions {
			if strings.Contains(line, ".permission.") {
				info.Permissions = append(info.Permissions, line)
				continue
			}
			inPermissions = false
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "codePath":
			info.ApkPath = value
		case "versionName":
			info.VersionName = value
		case "versionCode":
			fields := strings.Fields(value)
			if len(fields) > 0 {
				code, _ := strconv.ParseInt(fields[0], 10, 64)
				info.VersionCode = code
			}
		case "firstInstallTime":
			info.InstallTime = value
		case "lastUpdateTime":
			info.UpdateTime = value
		case "dataDir":
			info.DataDir = value
		}
	}
	if !found {
		return PackageInfo{}
	}
	// Paket sistem biasanya berada di /system, /product, /vendor, atau /apex.
	// Ini heuristik: aplikasi sistem yang diperbarui dapat berpindah ke
	// /data/app sehingga tidak lagi terdeteksi sebagai sistem.
	info.System = strings.HasPrefix(info.ApkPath, "/system") ||
		strings.HasPrefix(info.ApkPath, "/product") ||
		strings.HasPrefix(info.ApkPath, "/vendor") ||
		strings.HasPrefix(info.ApkPath, "/apex")
	return info
}

func parseDU(out string) int64 {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}
```

- [ ] **Step 4: Jalankan tes, pastikan lulus**

Run: `go test ./internal/adbx/ -v`
Expected: PASS untuk seluruh tes.

- [ ] **Step 5: Commit**

```bash
git add internal/adbx
git commit -m "feat(adbx): baca daftar perangkat, daftar paket, dan detail paket"
```

---

## Task 6: Operasi yang mengubah perangkat (`internal/adbx/ops.go`)

**Files:**
- Create: `internal/adbx/ops.go`
- Test: `internal/adbx/ops_test.go`

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/adbx/ops_test.go`:

```go
package adbx

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallSuccessReportsStages(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Performing Streamed Install\nSuccess\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	var stages []int
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{Replace: true},
		func(percent int, msg string) { stages = append(stages, percent) })
	if err != nil {
		t.Fatalf("install gagal: %v", err)
	}
	if len(stages) < 2 {
		t.Fatalf("stages terlalu sedikit: %v", stages)
	}
	joined := strings.Join(fe.calls[0], " ")
	if !strings.Contains(joined, "-r") || !strings.Contains(joined, "/tmp/app.apk") {
		t.Fatalf("argumen install salah: %s", joined)
	}
}

func TestInstallFlagsAndStageSequence(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Success\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	var stages []int
	err := r.Install(context.Background(), "/tmp/app.apk",
		InstallOptions{Replace: true, AllowDowngrade: true, GrantAll: true},
		func(percent int, msg string) { stages = append(stages, percent) })
	if err != nil {
		t.Fatalf("install gagal: %v", err)
	}
	joined := strings.Join(fe.calls[0], " ")
	for _, flag := range []string{"-r", "-d", "-g"} {
		if !strings.Contains(joined, flag) {
			t.Fatalf("flag %s tidak ada: %s", flag, joined)
		}
	}
	want := []int{5, 20, 100}
	if len(stages) != len(want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("stages = %v, want %v", stages, want)
		}
	}
}

func TestInstallFailureUsesClassify(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Failure [INSTALL_FAILED_INSUFFICIENT_STORAGE]\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if !errors.Is(err, ErrInsufficientStorage) {
		t.Fatalf("got %v, want ErrInsufficientStorage", err)
	}
}

func TestInstallNonZeroExitIsClassified(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stderr: "adb: device offline", ExitCode: 1},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("got %v, want ErrDeviceNotFound", err)
	}
}

// TestInstallExitZeroFailureClassified memastikan "Failure [..]" dengan exit 0
// tetap diklasifikasikan sebagai kegagalan.
func TestInstallExitZeroFailureClassified(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "Failure [INSTALL_FAILED_ALREADY_EXISTS]\n"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("got %v, want ErrAlreadyExists", err)
	}
}

// TestInstallFalseSuccessFromPathNotSwallowed meniru adb asli yang menggemakan
// path APK di stderr: path yang memuat kata "Success" tidak boleh menyamarkan
// kegagalan.
func TestInstallFalseSuccessFromPathNotSwallowed(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{
			Stdout:   "Failure [INSTALL_PARSE_FAILED_NOT_APK]\n",
			Stderr:   "adb: failed to install /tmp/Success/app.apk: Failure [INSTALL_PARSE_FAILED_NOT_APK]\n",
			ExitCode: 1,
		},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if err == nil {
		t.Fatal("kegagalan palsu: install seharusnya error")
	}
	if !errors.Is(err, ErrInvalidApk) {
		t.Fatalf("got %v, want ErrInvalidApk", err)
	}
}

// TestResultOfRejectsSuccessSubstring memastikan helper bersama hanya menerima
// baris utuh "Success" dengan exit 0, bukan kemunculan kata di mana pun.
func TestResultOfRejectsSuccessSubstring(t *testing.T) {
	res := Result{Stdout: "Failure [...NOT_APK] /tmp/Success/app.apk\n", ExitCode: 0}
	if err := resultOf(res); err == nil {
		t.Fatal("path yang memuat \"Success\" tidak boleh dianggap sukses")
	}
	if err := resultOf(Result{Stdout: "Success\n", ExitCode: 0}); err != nil {
		t.Fatalf("baris utuh Success harus sukses, dapat %v", err)
	}
}

func TestInstallHonoursRunnerTimeout(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(blockingExec{}), WithSerial("S1"),
		WithTimeout(50*time.Millisecond))
	start := time.Now()
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("install tidak menghormati timeout: %v", elapsed)
	}
}

func TestUninstallKeepDataAddsFlag(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Success\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	if err := r.Uninstall(context.Background(), "com.foo", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fe.calls[0], " "), "-k") {
		t.Fatalf("flag -k tidak ada: %v", fe.calls[0])
	}
}

func TestUninstallFailureIsClassified(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "Failure [DELETE_FAILED_INTERNAL_ERROR]\n", ExitCode: 1},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Uninstall(context.Background(), "com.foo", false)
	if !errors.Is(err, ErrSystemApp) {
		t.Fatalf("got %v, want ErrSystemApp", err)
	}
}

// TestUninstallExitZeroFailureIsError membunuh mutan `if res.ExitCode == 0 {
// return nil }`: "Failure [..]" dengan exit 0 harus tetap error.
func TestUninstallExitZeroFailureIsError(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "Failure [DELETE_FAILED_INTERNAL_ERROR]\n"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Uninstall(context.Background(), "com.foo", false)
	if !errors.Is(err, ErrSystemApp) {
		t.Fatalf("got %v, want ErrSystemApp", err)
	}
}

func TestUninstallHonoursRunnerTimeout(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(blockingExec{}), WithSerial("S1"),
		WithTimeout(50*time.Millisecond))
	start := time.Now()
	err := r.Uninstall(context.Background(), "com.foo", false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("uninstall tidak menghormati timeout: %v", elapsed)
	}
}

func TestClearData(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Success\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	if err := r.ClearData(context.Background(), "com.foo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fe.calls[0], " "), "pm clear com.foo") {
		t.Fatalf("perintah salah: %v", fe.calls[0])
	}
}

func TestClearDataFailure(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Failed\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.ClearData(context.Background(), "com.foo")
	if err == nil {
		t.Fatal("seharusnya error")
	}
	// Pesan harus kontekstual, bukan sekadar "Failed".
	if !strings.Contains(err.Error(), "com.foo") {
		t.Fatalf("error harus menyebut nama paket: %v", err)
	}
}

func TestPullApkUsesPathFromDevice(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "package:/data/app/~~Ab==/com.foo-x==/base.apk\n"},
		{Stdout: "1 file pulled\n"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	dest := t.TempDir()
	got, err := r.PullApk(context.Background(), "com.foo", dest)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dest, "com.foo-base.apk")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	pull := strings.Join(fe.calls[1], " ")
	if !strings.Contains(pull, "/data/app/~~Ab==/com.foo-x==/base.apk") {
		t.Fatalf("perintah pull salah: %s", pull)
	}
}

func TestPullApkFailsWhenPackageMissing(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	if _, err := r.PullApk(context.Background(), "com.foo", t.TempDir()); !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("got %v, want ErrPackageNotFound", err)
	}
}

// TestPullApkSanitizesPackageName memastikan nama paket bermusuhan tidak bisa
// membawa berkas keluar dari direktori tujuan.
func TestPullApkSanitizesPackageName(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "package:/data/app/base.apk\n"},
		{Stdout: "1 file pulled\n"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	dest := t.TempDir()
	got, err := r.PullApk(context.Background(), `../../etc/passwd`, dest)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Clean(got)) != dest {
		t.Fatalf("berkas keluar dari destDir: %q (dest %q)", got, dest)
	}
	rel, err := filepath.Rel(dest, got)
	if err != nil || rel == ".." || strings.ContainsRune(rel, filepath.Separator) {
		t.Fatalf("path hasil tidak aman: %q", got)
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/adbx/ -run 'TestInstall|TestUninstall|TestClearData|TestPullApk'`
Expected: FAIL, `undefined: InstallOptions`.

- [ ] **Step 3: Implementasi**

Create `internal/adbx/ops.go`:

```go
package adbx

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// InstallOptions mengatur flag yang dikirim ke `adb install`.
type InstallOptions struct {
	Replace        bool // -r, timpa bila sudah terpasang
	AllowDowngrade bool // -d, izinkan versi lebih rendah
	GrantAll       bool // -g, berikan semua izin runtime
}

// StageFunc melaporkan kemajuan kasar sebuah operasi (0-100).
type StageFunc func(percent int, message string)

func report(fn StageFunc, percent int, message string) {
	if fn != nil {
		fn(percent, message)
	}
}

// Install memasang satu berkas APK.
func (r *Runner) Install(ctx context.Context, apkPath string, opts InstallOptions, stage StageFunc) error {
	args := []string{"install"}
	if opts.Replace {
		args = append(args, "-r")
	}
	if opts.AllowDowngrade {
		args = append(args, "-d")
	}
	if opts.GrantAll {
		args = append(args, "-g")
	}
	args = append(args, apkPath)

	report(stage, 5, "Menyiapkan APK...")
	report(stage, 20, "Mengirim dan memasang APK...")
	res, err := r.Run(ctx, args...)
	if err != nil {
		return err
	}
	if err := resultOf(res); err != nil {
		return err
	}
	report(stage, 100, "Selesai")
	return nil
}

// resultOf menafsirkan keluaran perintah adb yang keluar dengan status 0.
// Sebagian build adb (mis. `install`/`uninstall`) melaporkan kegagalan lewat
// teks "Failure [..]" meski exit code-nya 0, jadi sukses hanya diakui bila
// exit code 0 DAN stdout memuat satu baris utuh berisi "Success". Semua kasus
// lain diklasifikasikan sebagai kegagalan.
func resultOf(res Result) error {
	if res.ExitCode == 0 && hasSuccessLine(res.Stdout) {
		return nil
	}
	return Classify(res)
}

// hasSuccessLine true bila stdout memuat baris yang setelah dipangkas persis
// sama dengan "Success". Pencocokan baris utuh mencegah jalur APK yang
// kebetulan mengandung kata "Success" menyamarkan kegagalan.
func hasSuccessLine(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "Success" {
			return true
		}
	}
	return false
}

// Uninstall mencopot aplikasi. keepData=true menyisakan data aplikasi (-k).
func (r *Runner) Uninstall(ctx context.Context, pkg string, keepData bool) error {
	args := []string{"uninstall"}
	if keepData {
		args = append(args, "-k")
	}
	args = append(args, pkg)

	res, err := r.Run(ctx, args...)
	if err != nil {
		return err
	}
	return resultOf(res)
}

// ClearData menghapus data dan cache aplikasi tanpa mencopotnya.
func (r *Runner) ClearData(ctx context.Context, pkg string) error {
	res, err := r.Run(ctx, "shell", "pm", "clear", pkg)
	if err != nil {
		// r.Run sudah mengklasifikasikan exit code non-nol.
		return err
	}
	out := strings.TrimSpace(res.Stdout)
	if hasSuccessLine(res.Stdout) {
		return nil
	}
	if out == "" {
		out = "perangkat menolak menghapus data"
	}
	return fmt.Errorf("gagal menghapus data %s: %s", pkg, out)
}

// PullApk menyalin berkas APK yang terpasang di perangkat ke destDir dan
// mengembalikan path lokalnya. Hanya base APK yang diambil; APK terpisah
// (split APKs) di luar cakupan v1. Menarik ulang paket yang sama akan
// menimpa berkas tujuan sebelumnya (adb pull menimpa tanpa bertanya).
func (r *Runner) PullApk(ctx context.Context, pkg string, destDir string) (string, error) {
	out, err := r.Output(ctx, "shell", "pm", "path", pkg)
	if err != nil {
		return "", err
	}
	remote := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package:") {
			remote = strings.TrimPrefix(line, "package:")
			break
		}
	}
	if remote == "" {
		return "", fmt.Errorf("%w: %s", ErrPackageNotFound, pkg)
	}
	local := filepath.Join(destDir, sanitizeName(pkg)+"-"+path.Base(remote))
	if _, err := r.Run(ctx, "pull", remote, local); err != nil {
		return "", err
	}
	return local, nil
}

// sanitizeName mengganti karakter yang tidak sah dalam nama berkas dengan
// garis bawah. Ini mencakup pemisah path (`\` dan `/`) serta karakter yang
// terlarang di Windows (`: * ? " < > |`), sehingga nama paket yang bermusuhan
// tidak bisa dipakai untuk menulis keluar dari destDir.
func sanitizeName(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, s)
}
```

- [ ] **Step 4: Jalankan tes, pastikan lulus**

Run: `go test ./internal/adbx/ -v`
Expected: PASS untuk seluruh tes.

- [ ] **Step 5: Commit**

```bash
git add internal/adbx
git commit -m "feat(adbx): install, uninstall, clear data, dan pull APK"
```

---

## Task 7: Memantau perangkat (`internal/device`)

**Files:**
- Create: `internal/device/device.go`
- Test: `internal/device/device_test.go`

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/device/device_test.go`:

```go
package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
)

type fakeLister struct {
	devices []adbx.Device
	err     error
}

func (f fakeLister) Devices(ctx context.Context) ([]adbx.Device, error) {
	return f.devices, f.err
}

// scriptedLister mengembalikan daftar perangkat yang berbeda pada tiap
// pemanggilan, sehingga dapat menguji perilaku lintas-refresh.
type scriptedLister struct {
	lists [][]adbx.Device
	calls int
	err   error
}

func (s *scriptedLister) Devices(ctx context.Context) ([]adbx.Device, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.lists) == 0 {
		return nil, nil
	}
	if s.calls >= len(s.lists) {
		return s.lists[len(s.lists)-1], nil
	}
	list := s.lists[s.calls]
	s.calls++
	return list, nil
}

func TestStatusReadyWhenOneAuthorized(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device", Model: "Pixel"}}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := m.Current()
	if got.State != StateReady {
		t.Fatalf("got %v, want ready", got.State)
	}
	if got.Serial != "S1" {
		t.Fatalf("serial salah: %q", got.Serial)
	}
}

func TestStatusUnauthorizedWhenNoReadyDevice(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "unauthorized"}}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Current().State != StateUnauthorized {
		t.Fatalf("got %v, want unauthorized", m.Current().State)
	}
}

func TestStatusReadyTakesPriorityOverUnauthorized(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{
		{Serial: "S1", State: "unauthorized"},
		{Serial: "S2", State: "device"},
	}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := m.Current()
	if got.State != StateReady || got.Serial != "S2" {
		t.Fatalf("got %v/%q, want ready/S2", got.State, got.Serial)
	}
}

func TestStatusNoneWhenEmpty(t *testing.T) {
	m := New(fakeLister{})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Current().State != StateNone {
		t.Fatalf("got %v, want none", m.Current().State)
	}
}

func TestStatusOfflineWhenOnlyOffline(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "offline"}}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Current().State != StateOffline {
		t.Fatalf("got %v, want offline", m.Current().State)
	}
}

func TestRepeatedRefreshKeepsSameSerial(t *testing.T) {
	l := &scriptedLister{lists: [][]adbx.Device{
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
		// Urutan dibalik: S1 harus tetap terpilih (lengket, bukan first-wins).
		{{Serial: "S2", State: "device"}, {Serial: "S1", State: "device"}},
		// S1 dicabut: sekarang S2 yang dipakai.
		{{Serial: "S2", State: "device"}},
	}}
	m := New(l)

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("refresh 1: got %q, want S1", got)
	}

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("refresh 2 (urutan dibalik): got %q, want tetap S1", got)
	}

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S2" {
		t.Fatalf("refresh 3 (S1 dicabut): got %q, want S2", got)
	}
}

func TestRefreshMovesAwayWhenPreferredOffline(t *testing.T) {
	l := &scriptedLister{lists: [][]adbx.Device{
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
		// S1 masih terdaftar tetapi offline; cabang mengingat mensyaratkan
		// state == "device", jadi pilihan harus pindah ke S2.
		{{Serial: "S1", State: "offline"}, {Serial: "S2", State: "device"}},
	}}
	m := New(l)

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("refresh 1: got %q, want S1", got)
	}

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := m.Current()
	if got.Serial != "S2" || got.State != StateReady {
		t.Fatalf("refresh 2: got %q/%v, want S2/ready", got.Serial, got.State)
	}
}

func TestModelFallsBackToProduct(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{
		{Serial: "S1", State: "device", Product: "PixelProduct"},
	}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Model; got != "PixelProduct" {
		t.Fatalf("Model = %q, want PixelProduct", got)
	}
}

func TestOthersListsOtherSerials(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{
		{Serial: "S1", State: "device"},
		{Serial: "S2", State: "offline"},
		{Serial: "S3", State: "unauthorized"},
	}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	others := m.Current().Others
	if len(others) != 2 || others[0] != "S2" || others[1] != "S3" {
		t.Fatalf("Others = %v, want [S2 S3]", others)
	}
}

func TestOthersEmptyForSingleDevice(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Others; len(got) != 0 {
		t.Fatalf("Others = %v, want kosong", got)
	}
}

func TestSelectPinsChosenDevice(t *testing.T) {
	l := &scriptedLister{lists: [][]adbx.Device{
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
	}}
	m := New(l)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("refresh 1: got %q, mau S1", got)
	}
	if err := m.Select("S2"); err != nil {
		t.Fatalf("Select gagal: %v", err)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S2" {
		t.Fatalf("setelah pilih: got %q, mau S2", got)
	}
}

func TestSelectClearedWhenDeviceDisappears(t *testing.T) {
	l := &scriptedLister{lists: [][]adbx.Device{
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
		// S2 dicabut: pin harus dilepas dan pilihan jatuh ke S1.
		{{Serial: "S1", State: "device"}},
	}}
	m := New(l)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Select("S2"); err != nil {
		t.Fatalf("Select gagal: %v", err)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("got %q, mau S1", got)
	}
	// Pin sudah dilepas, jadi serial lama sekarang tidak dikenal.
	if err := m.Select("S2"); err == nil {
		t.Fatal("Select S2 seharusnya error setelah pin dilepas")
	}
}

func TestSelectUnknownSerialErrors(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}})
	if err := m.Select("hantu"); err == nil {
		t.Fatal("Select serial tak dikenal seharusnya error")
	}
}

func TestRefreshErrorLeavesCurrentUnchanged(t *testing.T) {
	l := &fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}}
	m := New(l)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	l.err = errors.New("adb gagal")
	if err := m.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh: ingin error, dapat nil")
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("Current().Serial = %q, want tetap S1", got)
	}
}

func TestVersionGetterCalledOncePerSerial(t *testing.T) {
	calls := 0
	m := New(
		fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}},
		WithVersionGetter(func(ctx context.Context, serial string) (string, error) {
			calls++
			if serial != "S1" {
				t.Fatalf("serial = %q, mau S1", serial)
			}
			return "13", nil
		}),
	)
	for i := 0; i < 3; i++ {
		if err := m.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("getter dipanggil %d kali, mau 1", calls)
	}
	if got := m.Current().AndroidVersion; got != "13" {
		t.Fatalf("AndroidVersion = %q, mau 13", got)
	}
}

func TestVersionGetterErrorIsTolerated(t *testing.T) {
	calls := 0
	m := New(
		fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}},
		WithVersionGetter(func(ctx context.Context, serial string) (string, error) {
			calls++
			return "", errors.New("getprop gagal")
		}),
	)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh kedua: %v", err)
	}
	if calls != 1 {
		t.Fatalf("getter dipanggil %d kali, mau 1", calls)
	}
	got := m.Current()
	if got.AndroidVersion != "" {
		t.Fatalf("AndroidVersion = %q, mau kosong", got.AndroidVersion)
	}
	if got.State != StateReady {
		t.Fatalf("State = %v, mau ready", got.State)
	}
}

func TestVersionGetterNotCalledWhenNotReady(t *testing.T) {
	calls := 0
	m := New(
		fakeLister{devices: []adbx.Device{{Serial: "S1", State: "unauthorized"}}},
		WithVersionGetter(func(ctx context.Context, serial string) (string, error) {
			calls++
			return "13", nil
		}),
	)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("getter dipanggil %d kali, mau 0", calls)
	}
}

func TestLoopZeroIntervalDoesNotPanic(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // batalkan segera; Loop cukup tidak boleh panik.

	done := make(chan struct{})
	go func() {
		m.Loop(ctx, 0)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Loop tidak berhenti setelah ctx dibatalkan")
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/device/`
Expected: FAIL, `undefined: New`.

- [ ] **Step 3: Implementasi**

Create `internal/device/device.go`:

```go
// Package device memantau perangkat Android yang tersambung dan memilih satu
// perangkat aktif.
package device

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
)

type State string

const (
	StateReady        State = "ready"
	StateUnauthorized State = "unauthorized"
	StateOffline      State = "offline"
	StateNone         State = "none"
)

// Status adalah kondisi perangkat aktif saat ini.
type Status struct {
	State          State    `json:"state"`
	Serial         string   `json:"serial"`
	Model          string   `json:"model"`
	AndroidVersion string   `json:"androidVersion"`
	Others         []string `json:"others,omitempty"`
}

type lister interface {
	Devices(ctx context.Context) ([]adbx.Device, error)
}

// VersionGetter mengambil versi Android (mis. dari getprop) untuk satu serial.
type VersionGetter func(ctx context.Context, serial string) (string, error)

// Option mengubah perilaku Monitor.
type Option func(*Monitor)

// WithVersionGetter memasang pengambil versi Android. Getter dipanggil sekali
// per serial (hasilnya di-cache); kegagalan diabaikan dan versi dibiarkan
// kosong supaya pemantauan tidak pernah terhenti karenanya. Getter nil diabaikan.
func WithVersionGetter(g VersionGetter) Option {
	return func(m *Monitor) {
		if g != nil {
			m.versionGetter = g
		}
	}
}

// Monitor memilih satu perangkat aktif dan mengingat pilihannya selama
// perangkat itu masih tersambung.
type Monitor struct {
	lister        lister
	versionGetter VersionGetter
	mu            sync.RWMutex
	current       Status
	selected      string
	versions      map[string]string
}

func New(l lister, opts ...Option) *Monitor {
	m := &Monitor{
		lister:   l,
		current:  Status{State: StateNone},
		versions: map[string]string{},
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Refresh meminta daftar perangkat terbaru dan memperbarui status.
func (m *Monitor) Refresh(ctx context.Context) error {
	devices, err := m.lister.Devices(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	preferred := m.current.Serial
	if m.selected != "" {
		if isReadySerial(devices, m.selected) {
			preferred = m.selected
		} else {
			// Pin mengarah ke perangkat yang sudah hilang atau tidak siap:
			// lepaskan supaya pilihan kembali lengket ke perangkat aktif.
			m.selected = ""
		}
	}
	st := pick(devices, preferred)
	m.current = st
	var fetchSerial string
	needFetch := false
	if st.State == StateReady {
		if v, ok := m.versions[st.Serial]; ok {
			m.current.AndroidVersion = v
		} else {
			fetchSerial = st.Serial
			needFetch = true
		}
	}
	m.mu.Unlock()

	if needFetch && m.versionGetter != nil {
		version, verr := m.versionGetter(ctx, fetchSerial)
		if verr != nil {
			version = ""
		}
		m.mu.Lock()
		m.versions[fetchSerial] = version
		if m.current.Serial == fetchSerial {
			m.current.AndroidVersion = version
		}
		m.mu.Unlock()
	}
	return nil
}

// isReadySerial melaporkan apakah serial ada di daftar dan berstatus "device".
func isReadySerial(devices []adbx.Device, serial string) bool {
	for _, d := range devices {
		if d.Serial == serial && d.State == "device" {
			return true
		}
	}
	return false
}

// Select memilih perangkat aktif secara eksplisit. Serial harus dikenal pada
// status terakhir (perangkat aktif atau salah satu pada Others); selain itu
// dikembalikan error. Pin dihormati oleh Refresh selama perangkat itu hadir dan
// siap, lalu dilepas otomatis bila menghilang.
func (m *Monitor) Select(serial string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if serial == "" {
		return errors.New("serial kosong")
	}
	if serial == m.current.Serial {
		m.selected = serial
		return nil
	}
	for _, s := range m.current.Others {
		if s == serial {
			m.selected = serial
			return nil
		}
	}
	return fmt.Errorf("perangkat %s tidak dikenal", serial)
}

func pick(devices []adbx.Device, preferred string) Status {
	if len(devices) == 0 {
		return Status{State: StateNone}
	}

	// Pertahankan perangkat yang sedang dipakai bila masih ada.
	if preferred != "" {
		for _, d := range devices {
			if d.Serial == preferred && d.State == "device" {
				return build(d, devices)
			}
		}
	}

	for _, d := range devices {
		if d.State == "device" {
			return build(d, devices)
		}
	}
	for _, d := range devices {
		if d.State == "unauthorized" {
			return build(d, devices)
		}
	}
	return build(devices[0], devices)
}

func build(chosen adbx.Device, all []adbx.Device) Status {
	st := Status{Serial: chosen.Serial, Model: chosen.Model}
	switch chosen.State {
	case "device":
		st.State = StateReady
	case "unauthorized":
		st.State = StateUnauthorized
	default:
		st.State = StateOffline
	}
	if st.Model == "" {
		st.Model = chosen.Product
	}
	for _, d := range all {
		if d.Serial != chosen.Serial {
			st.Others = append(st.Others, d.Serial)
		}
	}
	return st
}

// Current mengembalikan status terakhir yang diketahui.
func (m *Monitor) Current() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// defaultLoopInterval dipakai bila Loop diberikan interval tidak positif,
// sebab time.NewTicker panik untuk durasi <= 0.
const defaultLoopInterval = 2 * time.Second

// loopInterval mengembalikan interval yang aman untuk time.NewTicker.
func loopInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return defaultLoopInterval
	}
	return interval
}

// Loop memantau perangkat secara berkala sampai ctx dibatalkan.
func (m *Monitor) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(loopInterval(interval))
	defer ticker.Stop()
	for {
		_ = m.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
```

- [ ] **Step 4: Jalankan tes, pastikan lulus**

Run: `go test ./internal/device/ -v`
Expected: PASS untuk seluruh tes device.

- [ ] **Step 5: Commit**

```bash
git add internal/device
git commit -m "feat(device): pantau perangkat tersambung dan pilih satu yang aktif"
```

---

## Task 8: Membaca metadata APK (`internal/apkmeta`)

**Files:**
- Create: `internal/apkmeta/apkmeta.go`
- Test: `internal/apkmeta/apkmeta_test.go`
- Test data: `internal/apkmeta/testdata/mini.apk`

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/apkmeta/apkmeta_test.go`:

```go
package apkmeta

import (
	"archive/zip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// moduleManifestBin adalah AndroidManifest.xml biner bawaan modul apkparser.
// Berkas ini tidak disalin ke repo; tes membangun APK sementara saat berjalan.
const moduleManifestBin = "98d2e837b8f3ac41e74b86b2d532972955e5352197a893206ecd9650f678ae31.bin"

// buildModuleAPK membangun berkas APK sementara dari testdata biner modul
// apkparser. Tes di-skip bila modul atau testdata-nya tidak tersedia.
func buildModuleAPK(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/avast/apkparser").Output()
	if err != nil {
		t.Skipf("modul apkparser tidak tersedia: %v", err)
	}
	binPath := filepath.Join(strings.TrimSpace(string(out)), "testdata", moduleManifestBin)
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Skipf("testdata modul apkparser tidak tersedia: %v", err)
	}

	apkPath := filepath.Join(t.TempDir(), "mini.apk")
	f, err := os.Create(apkPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return apkPath
}

func resetCache() {
	cacheMu.Lock()
	cache = map[string]cachedMeta{}
	cacheMu.Unlock()
}

// TestReadFixture membaca fixture testdata/mini.apk bila tersedia dan
// memeriksa package beserta versionName-nya. Tes di-skip bila fixture belum
// disediakan.
func TestReadFixture(t *testing.T) {
	path := filepath.Join("testdata", "mini.apk")
	if _, err := os.Stat(path); err != nil {
		t.Skip("testdata/mini.apk belum ada")
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read gagal: %v", err)
	}
	if got.Package != "com.example.mini" {
		t.Fatalf("package salah: %q", got.Package)
	}
	if got.VersionName == "" {
		t.Fatal("versionName kosong")
	}
}

func TestReadRejectsNonAPK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bukan.apk")
	if err := os.WriteFile(path, []byte("bukan zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("seharusnya error untuk berkas bukan APK")
	}
}

// TestReadRealManifestFromModule menjalankan jalur parse APK sungguhan di CI
// tanpa mengomit APK milik siapa pun: APK dibangun saat tes dari manifest
// biner bawaan modul apkparser.
func TestReadRealManifestFromModule(t *testing.T) {
	apk := buildModuleAPK(t)
	got, err := Read(apk)
	if err != nil {
		t.Fatalf("Read APK asli gagal: %v", err)
	}
	if got.Package != "name.tbx.erndy" {
		t.Fatalf("package salah: %q", got.Package)
	}
	if got.VersionName != "1.3" {
		t.Fatalf("versionName salah: %q", got.VersionName)
	}
	if got.VersionCode != 4 {
		t.Fatalf("versionCode salah: %d", got.VersionCode)
	}
	if got.MinSDK != 4 {
		t.Fatalf("minSdk salah: %d", got.MinSDK)
	}
}

// TestReadCachedStaleness memastikan cache tidak dipakai saat size/mtime
// berubah: pemanggilan kedua harus membaca ulang, sehingga berkas yang bukan
// APK memunculkan error alih-alih mengembalikan entri basi.
func TestReadCachedStaleness(t *testing.T) {
	t.Cleanup(resetCache)
	path := filepath.Join(t.TempDir(), "hilang.apk")

	// Isi cache secara putih untuk path ini, meniru hasil baca sebelumnya.
	cacheMu.Lock()
	cache[path] = cachedMeta{size: 1, mod: 1, meta: Meta{Package: "basi"}}
	cacheMu.Unlock()

	got, err := ReadCached(path, 2, 2)
	if err == nil {
		t.Fatalf("stat berubah seharusnya memicu baca ulang: %+v", got)
	}
}

// TestReadCachedBounds memastikan cache tetap terbatas saat terus diisi.
func TestReadCachedBounds(t *testing.T) {
	t.Cleanup(resetCache)
	apk := buildModuleAPK(t)

	cacheMu.Lock()
	for i := 0; i < cacheMaxEntries; i++ {
		cache[fmt.Sprintf("/dummy/%d.apk", i)] = cachedMeta{size: 1, mod: 1}
	}
	cacheMu.Unlock()

	m, err := ReadCached(apk, 10, 20)
	if err != nil {
		t.Fatalf("ReadCached gagal: %v", err)
	}
	cacheMu.Lock()
	size := len(cache)
	cacheMu.Unlock()
	if size > cacheMaxEntries {
		t.Fatalf("cache melebihi batas: %d > %d", size, cacheMaxEntries)
	}

	// Lookup ulang untuk stat yang sama harus tetap bekerja.
	got, err := ReadCached(apk, 10, 20)
	if err != nil {
		t.Fatalf("ReadCached ulang gagal: %v", err)
	}
	if got != m {
		t.Fatalf("hasil cache tidak konsisten: %+v vs %+v", got, m)
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/apkmeta/`
Expected: FAIL, `undefined: Read`.

- [ ] **Step 3: Implementasi**

Create `internal/apkmeta/apkmeta.go`:

```go
// Package apkmeta membaca identitas paket dari berkas APK tanpa memasangnya.
package apkmeta

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/avast/apkparser"
)

var ErrNoManifest = errors.New("AndroidManifest.xml tidak ditemukan di dalam APK")

// Meta adalah identitas singkat sebuah APK.
type Meta struct {
	Package     string `json:"package"`
	VersionName string `json:"versionName"`
	Label       string `json:"label"`
	VersionCode int64  `json:"versionCode"`
	MinSDK      int    `json:"minSdk"`
}

// Read membuka APK dan mengembalikan identitasnya.
func Read(path string) (Meta, error) {
	var m Meta
	enc := &metaEncoder{meta: &m}
	zipErr, _, manifestErr := apkparser.ParseApk(path, enc)
	if manifestErr != nil {
		return Meta{}, fmt.Errorf("gagal membaca AndroidManifest.xml: %w", manifestErr)
	}
	if zipErr != nil {
		return Meta{}, fmt.Errorf("berkas bukan APK yang sah: %w", zipErr)
	}
	if m.Package == "" {
		return Meta{}, ErrNoManifest
	}
	return m, nil
}

// metaEncoder menangkap elemen yang kita butuhkan dari aliran token XML.
type metaEncoder struct {
	meta *Meta
}

func (e *metaEncoder) EncodeToken(t xml.Token) error {
	start, ok := t.(xml.StartElement)
	if !ok {
		return nil
	}
	switch start.Name.Local {
	case "manifest":
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "package":
				e.meta.Package = a.Value
			case "versionName":
				e.meta.VersionName = a.Value
			case "versionCode":
				code, err := strconv.ParseInt(strings.TrimSpace(a.Value), 10, 64)
				if err == nil {
					e.meta.VersionCode = code
				}
			}
		}
	case "uses-sdk":
		for _, a := range start.Attr {
			if a.Name.Local == "minSdkVersion" {
				sdk, err := strconv.Atoi(strings.TrimSpace(a.Value))
				if err == nil {
					e.meta.MinSDK = sdk
				}
			}
		}
	case "application":
		for _, a := range start.Attr {
			if a.Name.Local == "label" {
				e.meta.Label = a.Value
			}
		}
	}
	return nil
}

func (e *metaEncoder) Flush() error { return nil }

// cache menghindari pembacaan APK berulang kali untuk berkas yang sama.
var (
	cacheMu sync.Mutex
	cache   = map[string]cachedMeta{}
)

// cacheMaxEntries membatasi jumlah entri cache agar tidak tumbuh tanpa batas
// saat daftar APK terus berubah.
const cacheMaxEntries = 512

type cachedMeta struct {
	size int64
	mod  int64
	meta Meta
}

// ReadCached seperti Read, tetapi menyimpan hasil untuk berkas yang tidak
// berubah. Dipakai oleh lapisan HTTP yang sering meminta daftar APK.
func ReadCached(path string, size, modUnixNano int64) (Meta, error) {
	cacheMu.Lock()
	if c, ok := cache[path]; ok && c.size == size && c.mod == modUnixNano {
		cacheMu.Unlock()
		return c.meta, nil
	}
	cacheMu.Unlock()

	m, err := Read(path)
	if err != nil {
		return Meta{}, err
	}

	cacheMu.Lock()
	if len(cache) >= cacheMaxEntries {
		cache = map[string]cachedMeta{}
	}
	cache[path] = cachedMeta{size: size, mod: modUnixNano, meta: m}
	cacheMu.Unlock()
	return m, nil
}
```

- [ ] **Step 4: Unduh APK contoh untuk testdata**

APK kecil apa pun bisa dipakai, asalkan package-nya diketahui. Karena APK contoh
harus dimiliki sendiri, langkah ini membuat berkas fixture di mesin pengembang:

```bash
cd /home/server/autoinstall-and-uninstall-using-adb
mkdir -p internal/apkmeta/testdata
# Letakkan berkas APK kecil milikmu di sini dengan nama mini.apk, misalnya
# APK contoh yang kamu pakai untuk audit. Tes otomatis di-skip bila tidak ada.
```

Tes `TestReadFixture` memakai `t.Skip` saat berkas tidak ada, sehingga CI tetap
hijau bila fixture belum disediakan. `TestReadRejectsNonAPK` selalu berjalan.

- [ ] **Step 5: Jalankan tes, pastikan lulus**

Run: `go test ./internal/apkmeta/ -v`
Expected: PASS (satu tes boleh SKIP bila fixture belum ada).

- [ ] **Step 6: Commit**

```bash
git add internal/apkmeta
git commit -m "feat(apkmeta): baca package dan versi dari berkas APK"
```

---

## Task 9: Riwayat dan konfigurasi (`internal/store`)

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/store/store_test.go`:

```go
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	e := Entry{
		Time:    time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC),
		Action:  "install",
		Package: "com.foo",
		Device:  "S1",
		Success: true,
	}
	if err := s.Append(e); err != nil {
		t.Fatalf("Append gagal: %v", err)
	}
	got, err := s.Read(10)
	if err != nil {
		t.Fatalf("Read gagal: %v", err)
	}
	if len(got) != 1 || got[0].Package != "com.foo" || !got[0].Success {
		t.Fatalf("isi riwayat salah: %+v", got)
	}
}

func TestReadReturnsNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	base := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := s.Append(Entry{Time: base.Add(time.Duration(i) * time.Minute), Package: "pkg"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("harus 3 baris, dapat %d", len(got))
	}
	if !got[0].Time.After(got[2].Time) {
		t.Fatalf("urutan harus terbaru dulu: %+v", got)
	}
}

func TestReadHonorsLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	for i := 0; i < 5; i++ {
		if err := s.Append(Entry{Package: "pkg"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Read(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("limit tidak dihormati: %d", len(got))
	}
}

func TestReadIgnoresBrokenLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	if err := s.Append(Entry{Package: "ok"}); err != nil {
		t.Fatal(err)
	}
	f, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendRaw(f, "{ini bukan json}\n"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(10)
	if err != nil {
		t.Fatalf("baris rusak seharusnya dilewati: %v", err)
	}
	if len(got) != 1 || got[0].Package != "ok" {
		t.Fatalf("hasil salah: %+v", got)
	}
}

func TestExportCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	if err := s.Append(Entry{
		Time:    time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC),
		Action:  "install",
		Package: "com.foo",
		Device:  "S1",
		Success: true,
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := s.ExportCSV(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "time,action,package,device,success,detail\n") {
		t.Fatalf("header CSV salah:\n%s", out)
	}
	if !strings.Contains(out, "com.foo") {
		t.Fatalf("baris CSV tidak memuat paket:\n%s", out)
	}
}

func TestExportJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	if err := s.Append(Entry{Package: "com.foo", Success: true}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := s.ExportJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var decoded []Entry
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON tidak sah: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Package != "com.foo" {
		t.Fatalf("isi JSON salah: %+v", decoded)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Config{ApkFolder: "/home/user/apk"}
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ApkFolder != cfg.ApkFolder {
		t.Fatalf("got %+v want %+v", got, cfg)
	}
}

func TestLoadConfigMissingFileReturnsDefault(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "tidak-ada.json"))
	if err != nil {
		t.Fatalf("seharusnya tidak error: %v", err)
	}
	if cfg.ApkFolder != "" {
		t.Fatalf("default harus kosong: %+v", cfg)
	}
}

func TestLoadConfigCorruptReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{ini bukan json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("config.json rusak seharusnya mengembalikan error")
	}
}

func TestSaveConfigReplacesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveConfig(path, Config{ApkFolder: "/lama"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(path, Config{ApkFolder: "/baru"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ApkFolder != "/baru" {
		t.Fatalf("nilai baru tidak menimpa: %+v", got)
	}
	// Tidak boleh ada berkas sementara yang tertinggal.
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "config.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("berkas sementara tertinggal: %v", matches)
	}
}

func TestAppendDefaultsZeroTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	before := time.Now()
	if err := s.Append(Entry{Package: "pkg"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("harus 1 entri, dapat %d", len(got))
	}
	if got[0].Time.IsZero() {
		t.Fatal("Time nol seharusnya diisi waktu sekarang")
	}
	if got[0].Time.Before(before.Add(-time.Second)) || got[0].Time.After(time.Now().Add(time.Second)) {
		t.Fatalf("Time tidak wajar: %v (sekarang %v)", got[0].Time, time.Now())
	}
}

func TestExportJSONEmptyIsArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	var buf bytes.Buffer
	if err := s.ExportJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Fatalf("ekspor kosong harus [] bukan null: %q", buf.String())
	}
}

func TestReadMissingFileReturnsNil(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "tidak-ada.jsonl"))
	got, err := s.Read(5)
	if err != nil {
		t.Fatalf("berkas hilang seharusnya tidak error: %v", err)
	}
	if got != nil {
		t.Fatalf("berkas hilang harus nil: %+v", got)
	}
}

func TestReadTailEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := New(path).Read(5)
	if err != nil {
		t.Fatalf("berkas kosong tidak boleh error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("berkas kosong harus kosong: %+v", got)
	}
}

func TestReadTailLimitOnSmallFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	base := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := s.Append(Entry{
			Time:    base.Add(time.Duration(i) * time.Minute),
			Package: fmt.Sprintf("pkg-%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		limit int
		want  []string
	}{
		{1, []string{"pkg-4"}},
		{2, []string{"pkg-4", "pkg-3"}},
		{5, []string{"pkg-4", "pkg-3", "pkg-2", "pkg-1", "pkg-0"}},
		{9, []string{"pkg-4", "pkg-3", "pkg-2", "pkg-1", "pkg-0"}},
	}
	for _, tc := range cases {
		got, err := s.Read(tc.limit)
		if err != nil {
			t.Fatalf("Read(%d) gagal: %v", tc.limit, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("Read(%d) dapat %d entri, mau %d: %+v", tc.limit, len(got), len(tc.want), got)
		}
		for i := range tc.want {
			if got[i].Package != tc.want[i] {
				t.Fatalf("Read(%d) urutan salah di %d: got %q mau %q", tc.limit, i, got[i].Package, tc.want[i])
			}
		}
	}
}

func TestReadTailLastThreeOfLargeHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	for i := 0; i < 2000; i++ {
		if err := s.Append(Entry{Package: fmt.Sprintf("pkg-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Read(3)
	if err != nil {
		t.Fatalf("Read gagal: %v", err)
	}
	want := []string{"pkg-1999", "pkg-1998", "pkg-1997"}
	if len(got) != len(want) {
		t.Fatalf("harus 3 entri, dapat %d: %+v", len(got), got)
	}
	for i := range want {
		if got[i].Package != want[i] {
			t.Fatalf("urutan salah di %d: got %q mau %q", i, got[i].Package, want[i])
		}
	}
}

func TestReadTailNoTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	e1, err := json.Marshal(Entry{Package: "a"})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := json.Marshal(Entry{Package: "b"})
	if err != nil {
		t.Fatal(err)
	}
	// Baris terakhir sengaja tanpa newline penutup.
	if err := appendRaw(path, string(e1)+"\n"+string(e2)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Package != "b" {
		t.Fatalf("baris terakhir tanpa newline salah: %+v", got)
	}
}

func TestReadTailLineLongerThanChunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	big := strings.Repeat("x", 200*1024)
	if err := s.Append(Entry{Package: "big", Detail: big}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Entry{Package: "small"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Package != "small" {
		t.Fatalf("entri terakhir salah: %+v", got)
	}
	got, err = s.Read(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Package != "big" || got[1].Detail != big {
		t.Fatalf("baris panjang tidak terbaca utuh: len=%d", len(got))
	}
}

func TestReadTailRefillsPastCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	for i := 0; i < 200; i++ {
		if err := s.Append(Entry{Package: fmt.Sprintf("pkg-%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	// Baris rusak di paling ekor tidak boleh membuat Read(limit) kekurangan
	// entri: pembacaan harus mundur sampai limit entri sah terkumpul.
	if err := appendRaw(path, "{ini bukan json}\n"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(5)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pkg-199", "pkg-198", "pkg-197", "pkg-196", "pkg-195"}
	if len(got) != len(want) {
		t.Fatalf("harus %d entri sah, dapat %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i].Package != want[i] {
			t.Fatalf("urutan salah di %d: got %q mau %q", i, got[i].Package, want[i])
		}
	}
}

func TestReadHugeLimitNoOverflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	for i := 0; i < 3; i++ {
		if err := s.Append(Entry{Package: fmt.Sprintf("pkg-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	// limit == math.MaxInt tidak boleh meluap atau panik; artinya baca semua.
	got, err := s.Read(math.MaxInt)
	if err != nil {
		t.Fatalf("Read(math.MaxInt) gagal: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("harus 3 entri, dapat %d: %+v", len(got), got)
	}
	if got[0].Package != "pkg-2" {
		t.Fatalf("terbaru harus pkg-2, dapat %q", got[0].Package)
	}
}

func TestReadFullHandlesLongDetailAndExport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	// ~2 MB, jauh di atas buffer scanner lama (1 MB) tetapi di bawah batas
	// baru (8 MB).
	big := strings.Repeat("d", 2*1024*1024)
	if err := s.Append(Entry{Package: "big", Detail: big, Success: true}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(0)
	if err != nil {
		t.Fatalf("Read(0) gagal untuk Detail panjang: %v", err)
	}
	if len(got) != 1 || got[0].Detail != big {
		t.Fatalf("Detail panjang tidak bulat: len=%d", len(got))
	}

	var csvBuf bytes.Buffer
	if err := s.ExportCSV(&csvBuf); err != nil {
		t.Fatalf("ExportCSV gagal: %v", err)
	}
	if !strings.Contains(csvBuf.String(), big) {
		t.Fatal("CSV tidak memuat Detail panjang")
	}
	var jsonBuf bytes.Buffer
	if err := s.ExportJSON(&jsonBuf); err != nil {
		t.Fatalf("ExportJSON gagal: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), big) {
		t.Fatal("JSON tidak memuat Detail panjang")
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/store/`
Expected: FAIL, `undefined: New`.

- [ ] **Step 3: Implementasi**

Create `internal/store/store.go`:

```go
// Package store menyimpan riwayat audit dan konfigurasi pengguna.
package store

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// newline adalah pemisah satu entri JSON Lines.
var newline = []byte{'\n'}

// Entry adalah satu baris riwayat audit.
type Entry struct {
	Time    time.Time `json:"time"`
	Action  string    `json:"action"`
	Package string    `json:"package"`
	Device  string    `json:"device"`
	Success bool      `json:"success"`
	Detail  string    `json:"detail,omitempty"`
}

// Store menulis dan membaca riwayat dalam format JSON Lines.
type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store {
	return &Store{path: path}
}

// Append menambahkan satu entri ke akhir berkas riwayat.
func (s *Store) Append(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// Read mengembalikan paling banyak limit entri, terbaru lebih dulu.
//
// Bila limit > 0, hanya ekor berkas yang dibaca sehingga biaya baca tidak
// bergantung pada panjang riwayat. Limit <= 0 membaca seluruh berkas, dipakai
// oleh ekspor CSV/JSON.
func (s *Store) Read(limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if limit > 0 {
		return readTail(f, limit)
	}

	var all []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // baris rusak dilewati, bukan menggagalkan seluruh riwayat
		}
		all = append(all, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Balik urutan: terbaru lebih dulu.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all, nil
}

// tailChunkSize adalah ukuran tiap potongan saat membaca mundur dari ekor.
const tailChunkSize = 64 * 1024

// maxLineSize adalah batas panjang satu baris pada jalur baca penuh. Nilai ini
// cukup besar untuk menampung Detail yang panjang (mis. dump adb) tanpa
// menggagalkan pemindaian.
const maxLineSize = 8 * 1024 * 1024

// readTail mengembalikan paling banyak limit entri terakhir berkas, terbaru
// lebih dulu. Baris yang lebih panjang dari tailChunkSize tetap utuh karena
// potongan terus dibaca sampai baris lengkap terkumpul. Pembacaan mundur
// berlanjut melewati baris rusak sampai terkumpul limit entri yang benar-benar
// dapat di-parse, atau sampai awal berkas.
func readTail(f *os.File, limit int) ([]Entry, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	// Kumpulkan potongan dari ekor sampai cukup banyak entri sah. Jumlah
	// baris baru dipakai sebagai batas atas murah: selama newline belum
	// mencapai limit, mustahil ada limit entri sah, jadi tidak perlu parse.
	// Batas ini juga aman untuk limit == math.MaxInt karena tidak pernah
	// dihitung limit+1 yang bisa meluap.
	var buf []byte
	var newlines int64
	pos := size
	for pos > 0 {
		n := int64(tailChunkSize)
		if pos < n {
			n = pos
		}
		pos -= n
		chunk := make([]byte, n)
		if _, err := f.ReadAt(chunk, pos); err != nil {
			return nil, err
		}
		// chunk mendahului buf karena dibaca lebih dekat ke awal berkas.
		buf = append(chunk, buf...)
		newlines += int64(bytes.Count(chunk, newline))
		if newlines < int64(limit) {
			continue
		}
		if countParsedLines(buf, pos > 0) >= limit {
			break
		}
	}

	lines := bytes.Split(buf, newline)
	// Buang segmen kosong setelah newline terakhir, bila ada.
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	// Bila pembacaan berhenti sebelum awal berkas, segmen pertama bisa
	// dimulai di tengah baris; buang agar sisa segmen pasti utuh.
	if pos > 0 && len(lines) > 0 {
		lines = lines[1:]
	}

	// Telusuri dari ekor agar entri terbaru lebih dulu, melewati baris rusak,
	// sampai limit entri terkumpul.
	var entries []Entry
	for i := len(lines) - 1; i >= 0 && len(entries) < limit; i-- {
		line := lines[i]
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// countParsedLines menghitung baris pada buf yang berhasil di-parse. Bila
// skipFirst benar, segmen pertama diabaikan karena potongan pembacaan bisa
// dimulai di tengah baris sehingga segmen itu tidak utuh.
func countParsedLines(buf []byte, skipFirst bool) int {
	lines := bytes.Split(buf, newline)
	if skipFirst && len(lines) > 0 {
		lines = lines[1:]
	}
	n := 0
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) == nil {
			n++
		}
	}
	return n
}

// ExportCSV menulis seluruh riwayat sebagai CSV.
func (s *Store) ExportCSV(w io.Writer) error {
	entries, err := s.readAllOldestFirst()
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"time", "action", "package", "device", "success", "detail"}); err != nil {
		return err
	}
	for _, e := range entries {
		if err := cw.Write([]string{
			e.Time.Format(time.RFC3339),
			e.Action,
			e.Package,
			e.Device,
			strconv.FormatBool(e.Success),
			e.Detail,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ExportJSON menulis seluruh riwayat sebagai satu larik JSON.
func (s *Store) ExportJSON(w io.Writer) error {
	entries, err := s.readAllOldestFirst()
	if err != nil {
		return err
	}
	if entries == nil {
		entries = []Entry{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

func (s *Store) readAllOldestFirst() ([]Entry, error) {
	entries, err := s.Read(0)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// Config adalah pengaturan kecil yang bertahan antar sesi.
type Config struct {
	ApkFolder string `json:"apkFolder"`
}

// LoadConfig membaca konfigurasi; berkas yang belum ada dianggap default.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config.json rusak: %w", err)
	}
	return cfg, nil
}

// SaveConfig menulis konfigurasi ke disk secara atomik: tulis ke berkas
// sementara di direktori yang sama, sync, lalu rename menimpa target. Dengan
// begitu crash atau disk penuh tidak meninggalkan config.json yang rusak.
func SaveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("membuat berkas sementara untuk %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("menulis %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("menyinkronkan %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("menutup berkas sementara %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("mengatur mode %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("memindahkan %s ke %s: %w", tmpName, path, err)
	}
	return nil
}
```

- [ ] **Step 4: Tambahkan helper `appendRaw` untuk tes**

Create `internal/store/testhelper_test.go`:

```go
package store

import "os"

// appendRaw menambahkan teks mentah ke berkas riwayat, dipakai untuk
// mensimulasikan baris yang rusak.
func appendRaw(path, text string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return err
}
```

- [ ] **Step 5: Jalankan tes, pastikan lulus**

Run: `go test ./internal/store/ -v`
Expected: PASS untuk seluruh tes.

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "feat(store): riwayat audit JSONL, konfigurasi, dan ekspor CSV/JSON"
```

---

## Task 10: Antrean job (`internal/queue`)

**Files:**
- Create: `internal/queue/queue.go`
- Test: `internal/queue/queue_test.go`

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/queue/queue_test.go`:

```go
package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
)

type fakeRunner struct {
	mu          sync.Mutex
	calls       []string
	fail        map[string]error
	block       chan struct{}
	interrupted chan struct{}
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{fail: map[string]error{}}
}

func (f *fakeRunner) record(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeRunner) sequence() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeRunner) Install(ctx context.Context, apkPath string, opts adbx.InstallOptions, stage adbx.StageFunc) error {
	if stage != nil {
		stage(10, "mulai")
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			if f.interrupted != nil {
				close(f.interrupted)
			}
			return ctx.Err()
		}
	}
	f.record("install:" + apkPath)
	if err, ok := f.fail["install:"+apkPath]; ok {
		return err
	}
	if stage != nil {
		stage(100, "selesai")
	}
	return nil
}

func (f *fakeRunner) Uninstall(ctx context.Context, pkg string, keepData bool) error {
	key := "uninstall:" + pkg
	if keepData {
		key = "uninstall_keep:" + pkg
	}
	f.record(key)
	return f.fail[key]
}

func (f *fakeRunner) ClearData(ctx context.Context, pkg string) error {
	f.record("clear:" + pkg)
	return f.fail["clear:"+pkg]
}

func (f *fakeRunner) PullApk(ctx context.Context, pkg string, destDir string) (string, error) {
	f.record("pull:" + pkg)
	if err, ok := f.fail["pull:"+pkg]; ok {
		return "", err
	}
	return destDir + "/" + pkg + ".apk", nil
}

// slowSuccessRunner meniru runner yang tetap mengembalikan sukses meski
// konteksnya dibatalkan, untuk menguji cabang finalisasi cancelled.
type slowSuccessRunner struct {
	fakeRunner
	started chan struct{}
	release chan struct{}
}

func (r *slowSuccessRunner) Install(ctx context.Context, apkPath string, opts adbx.InstallOptions, stage adbx.StageFunc) error {
	close(r.started)
	<-r.release
	return nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout menunggu: %s", what)
}

func runQueue(t *testing.T, q *Queue) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go q.Run(ctx)
	t.Cleanup(cancel)
	return cancel
}

func statusOf(t *testing.T, q *Queue, id string) Job {
	t.Helper()
	for _, j := range q.Jobs() {
		if j.ID == id {
			return j
		}
	}
	t.Fatalf("job %s tidak ditemukan", id)
	return Job{}
}

func TestJobsRunInOrder(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	b := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/b.apk"})

	waitFor(t, "kedua job selesai", func() bool {
		return statusOf(t, q, a.ID).Status == StatusSuccess &&
			statusOf(t, q, b.ID).Status == StatusSuccess
	})

	got := fr.sequence()
	want := []string{"install:/tmp/a.apk", "install:/tmp/b.apk"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("urutan salah: got %v want %v", got, want)
	}
}

func TestFailingJobDoesNotStopQueue(t *testing.T) {
	fr := newFakeRunner()
	fr.fail["install:/tmp/a.apk"] = adbx.ErrInsufficientStorage
	q := New(fr)
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	b := q.Enqueue(Job{Kind: KindUninstall, Target: "com.b"})

	waitFor(t, "job kedua selesai", func() bool {
		return statusOf(t, q, b.ID).Status == StatusSuccess
	})
	if got := statusOf(t, q, a.ID); got.Status != StatusFailed {
		t.Fatalf("job pertama harus gagal, dapat %v", got.Status)
	}
	if got := statusOf(t, q, a.ID).Error; got == "" {
		t.Fatal("pesan error job pertama kosong")
	}
}

func TestCancelQueuedJobPreventsRun(t *testing.T) {
	fr := newFakeRunner()
	fr.block = make(chan struct{})
	q := New(fr)
	runQueue(t, q)

	first := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/first.apk"})
	second := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/second.apk"})

	waitFor(t, "job pertama berjalan", func() bool {
		return statusOf(t, q, first.ID).Status == StatusRunning
	})

	if err := q.Cancel(second.ID); err != nil {
		t.Fatalf("Cancel gagal: %v", err)
	}
	close(fr.block)

	waitFor(t, "job pertama selesai", func() bool {
		return statusOf(t, q, first.ID).Status == StatusSuccess
	})
	if got := statusOf(t, q, second.ID); got.Status != StatusCancelled {
		t.Fatalf("job kedua harus cancelled, dapat %v", got.Status)
	}
	for _, c := range fr.sequence() {
		if c == "install:/tmp/second.apk" {
			t.Fatal("job yang dibatalkan tetap dijalankan")
		}
	}
}

func TestWaitsWhileDeviceDisconnected(t *testing.T) {
	fr := newFakeRunner()
	var connected atomic.Bool
	q := New(fr, WithDeviceCheck(func() bool { return connected.Load() }))
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	time.Sleep(50 * time.Millisecond)
	if got := statusOf(t, q, a.ID).Status; got != StatusQueued {
		t.Fatalf("tanpa perangkat job harus tetap queued, dapat %v", got)
	}

	connected.Store(true)
	waitFor(t, "job jalan setelah perangkat tersambung", func() bool {
		return statusOf(t, q, a.ID).Status == StatusSuccess
	})
}

func TestOnDoneReceivesResult(t *testing.T) {
	fr := newFakeRunner()
	var mu sync.Mutex
	var done []Job
	q := New(fr, WithOnDone(func(j Job, err error) {
		mu.Lock()
		done = append(done, j)
		mu.Unlock()
	}))
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindPull, Target: "com.foo", DestDir: "/tmp/pulled"})
	waitFor(t, "job selesai", func() bool {
		return statusOf(t, q, a.ID).Status == StatusSuccess
	})

	waitFor(t, "callback dipanggil", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(done) == 1
	})
	mu.Lock()
	defer mu.Unlock()
	if done[0].Result != "/tmp/pulled/com.foo.apk" {
		t.Fatalf("hasil pull salah: %q", done[0].Result)
	}
}

func TestCancelRunningJobReportsCancelled(t *testing.T) {
	fr := newFakeRunner()
	fr.block = make(chan struct{})
	q := New(fr)
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	waitFor(t, "job berjalan", func() bool {
		return statusOf(t, q, a.ID).Status == StatusRunning
	})
	if err := q.Cancel(a.ID); err != nil {
		t.Fatalf("Cancel gagal: %v", err)
	}
	waitFor(t, "job dibatalkan", func() bool {
		return statusOf(t, q, a.ID).Status == StatusCancelled
	})
}

func TestUnknownKindFails(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	runQueue(t, q)
	a := q.Enqueue(Job{Kind: Kind("ngawur"), Target: "x"})
	waitFor(t, "job gagal", func() bool {
		return statusOf(t, q, a.ID).Status == StatusFailed
	})
	if statusOf(t, q, a.ID).Error == "" {
		t.Fatal("job dengan jenis tak dikenal harus punya pesan error")
	}
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		q.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run tidak kembali setelah konteks dibatalkan")
	}
}

func TestSubscribeDeliversAndUnsubscribeIsIdempotent(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	runQueue(t, q)

	ch, unsubscribe := q.Subscribe()
	q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/sub.apk"})

	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("saluran langganan ditutup sebelum mengirim pembaruan")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tidak menerima pembaruan langganan")
	}

	// Panggilan kedua harus idempoten dan tidak boleh panik.
	unsubscribe()
	unsubscribe()

	// Setelah unsubscribe, saluran ditutup sehingga tidak ada pengiriman baru.
	closed := false
	for !closed {
		select {
		case _, ok := <-ch:
			if !ok {
				closed = true
			}
		case <-time.After(3 * time.Second):
			t.Fatal("saluran langganan tidak ditutup setelah unsubscribe")
		}
	}

	// Pelanggan lambat: saluran penuh tidak boleh menghambat Enqueue.
	slow := New(newFakeRunner())
	slowCh, slowUnsub := slow.Subscribe()
	defer slowUnsub()

	doneEnqueue := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			slow.Enqueue(Job{Kind: KindInstall, Target: "/tmp/slow.apk"})
		}
		close(doneEnqueue)
	}()

	select {
	case <-doneEnqueue:
	case <-time.After(3 * time.Second):
		t.Fatal("Enqueue terhambat oleh pelanggan dengan saluran penuh")
	}
	if len(slowCh) != cap(slowCh) {
		t.Fatalf("saluran pelanggan lambat seharusnya penuh: len=%d cap=%d", len(slowCh), cap(slowCh))
	}
}

func TestRunJobSkipsNonQueuedJob(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	job := &Job{ID: "selesai", Kind: KindInstall, Target: "/tmp/selesai.apk", Status: StatusCancelled}

	q.runJob(context.Background(), job)

	if got := job.Status; got != StatusCancelled {
		t.Fatalf("job non-queued harus tetap cancelled, dapat %v", got)
	}
	if got := fr.sequence(); len(got) != 0 {
		t.Fatalf("runner tidak boleh dipanggil untuk job non-queued, dapat %v", got)
	}
}

func TestCancelledJobStaysCancelledWhenRunnerSucceeds(t *testing.T) {
	fr := &slowSuccessRunner{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan Job, 1)
	q := New(fr, WithOnDone(func(j Job, err error) { done <- j }))
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	select {
	case <-fr.started:
	case <-time.After(3 * time.Second):
		t.Fatal("runner tidak mulai")
	}
	if err := q.Cancel(a.ID); err != nil {
		t.Fatalf("Cancel gagal: %v", err)
	}
	close(fr.release)

	select {
	case final := <-done:
		if final.Status != StatusCancelled {
			t.Fatalf("status akhir harus cancelled, dapat %v", final.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("job tidak selesai setelah runner dilepas")
	}
}

func TestCancelRunningInterruptsRunner(t *testing.T) {
	fr := newFakeRunner()
	fr.block = make(chan struct{})
	fr.interrupted = make(chan struct{})
	q := New(fr)
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	waitFor(t, "job berjalan", func() bool {
		return statusOf(t, q, a.ID).Status == StatusRunning
	})
	if err := q.Cancel(a.ID); err != nil {
		t.Fatalf("Cancel gagal: %v", err)
	}

	select {
	case <-fr.interrupted:
	case <-time.After(3 * time.Second):
		t.Fatal("runner tidak menerima sinyal pembatalan")
	}
	waitFor(t, "job akhir cancelled", func() bool {
		return statusOf(t, q, a.ID).Status == StatusCancelled
	})
}

func TestSubscriberSeesLifecycle(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	runQueue(t, q)

	ch, unsubscribe := q.Subscribe()
	defer unsubscribe()

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/life.apk"})

	sawRunning := false
	sawTerminal := false
	deadline := time.After(3 * time.Second)
	for !sawRunning || !sawTerminal {
		select {
		case j, ok := <-ch:
			if !ok {
				t.Fatal("saluran langganan ditutup sebelum siklus penuh")
			}
			if j.ID != a.ID {
				continue
			}
			switch j.Status {
			case StatusRunning:
				sawRunning = true
			case StatusSuccess, StatusFailed, StatusCancelled:
				sawTerminal = true
			}
		case <-deadline:
			t.Fatalf("siklus tidak lengkap: running=%v terminal=%v", sawRunning, sawTerminal)
		}
	}
}

func TestJobJSONOmitsUnsetTimestamps(t *testing.T) {
	queued, err := json.Marshal(Job{ID: "x", Kind: KindInstall, Status: StatusQueued})
	if err != nil {
		t.Fatalf("marshal job queued: %v", err)
	}
	if bytes.Contains(queued, []byte("startedAt")) || bytes.Contains(queued, []byte("endedAt")) {
		t.Fatalf("job queued tidak boleh memuat timestamp: %s", queued)
	}

	started := time.Now()
	ended := started.Add(time.Second)
	done, err := json.Marshal(Job{ID: "x", Kind: KindInstall, Status: StatusSuccess, StartedAt: &started, EndedAt: &ended})
	if err != nil {
		t.Fatalf("marshal job selesai: %v", err)
	}
	if !bytes.Contains(done, []byte("startedAt")) || !bytes.Contains(done, []byte("endedAt")) {
		t.Fatalf("job selesai harus memuat timestamp: %s", done)
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/queue/`
Expected: FAIL, `undefined: New`.

- [ ] **Step 3: Implementasi**

Create `internal/queue/queue.go`:

```go
// Package queue menjalankan pekerjaan install/uninstall satu per satu.
package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
)

type Kind string

const (
	KindInstall       Kind = "install"
	KindUninstall     Kind = "uninstall"
	KindUninstallKeep Kind = "uninstall_keep"
	KindClearData     Kind = "clear_data"
	KindPull          Kind = "pull"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Job adalah satu pekerjaan di antrean.
type Job struct {
	ID             string     `json:"id"`
	Kind           Kind       `json:"kind"`
	Target         string     `json:"target"`
	Label          string     `json:"label,omitempty"`
	DestDir        string     `json:"destDir,omitempty"`
	Replace        bool       `json:"replace,omitempty"`
	AllowDowngrade bool       `json:"allowDowngrade,omitempty"`
	Status         Status     `json:"status"`
	Progress       int        `json:"progress"`
	Message        string     `json:"message,omitempty"`
	Error          string     `json:"error,omitempty"`
	Result         string     `json:"result,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
}

// Runner adalah kemampuan perangkat yang dibutuhkan antrean.
type Runner interface {
	Install(ctx context.Context, apkPath string, opts adbx.InstallOptions, stage adbx.StageFunc) error
	Uninstall(ctx context.Context, pkg string, keepData bool) error
	ClearData(ctx context.Context, pkg string) error
	PullApk(ctx context.Context, pkg string, destDir string) (string, error)
}

type Option func(*Queue)

// WithDeviceCheck membuat antrean menahan pekerjaan saat perangkat tidak siap.
func WithDeviceCheck(fn func() bool) Option {
	return func(q *Queue) { q.deviceOK = fn }
}

// WithOnDone mendaftarkan callback setelah sebuah job selesai (untuk riwayat).
func WithOnDone(fn func(Job, error)) Option {
	return func(q *Queue) { q.onDone = fn }
}

type Queue struct {
	runner   Runner
	deviceOK func() bool
	onDone   func(Job, error)

	mu      sync.Mutex
	jobs    []*Job
	byID    map[string]*Job
	cancels map[string]context.CancelFunc
	subs    map[int]chan Job
	nextSub int
	nextID  int
	notify  chan struct{}
}

func New(r Runner, opts ...Option) *Queue {
	q := &Queue{
		runner:  r,
		byID:    map[string]*Job{},
		cancels: map[string]context.CancelFunc{},
		subs:    map[int]chan Job{},
		notify:  make(chan struct{}, 1),
	}
	for _, o := range opts {
		o(q)
	}
	return q
}

// Enqueue menambahkan pekerjaan baru ke ujung antrean.
func (q *Queue) Enqueue(spec Job) Job {
	q.mu.Lock()
	q.nextID++
	if spec.ID == "" {
		spec.ID = fmt.Sprintf("job-%d", q.nextID)
	}
	spec.Status = StatusQueued
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = time.Now()
	}
	job := &spec
	q.jobs = append(q.jobs, job)
	q.byID[job.ID] = job
	snapshot := *job
	q.mu.Unlock()

	q.broadcast(snapshot)
	q.wake()
	return snapshot
}

func (q *Queue) wake() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Jobs mengembalikan salinan seluruh pekerjaan.
func (q *Queue) Jobs() []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		out = append(out, *j)
	}
	return out
}

// Cancel membatalkan pekerjaan yang masih menunggu atau sedang berjalan.
func (q *Queue) Cancel(id string) error {
	q.mu.Lock()
	job, ok := q.byID[id]
	if !ok {
		q.mu.Unlock()
		return fmt.Errorf("job %s tidak ditemukan", id)
	}
	switch job.Status {
	case StatusQueued:
		job.Status = StatusCancelled
		job.Message = "dibatalkan sebelum dijalankan"
		ended := time.Now()
		job.EndedAt = &ended
		snap := *job
		q.mu.Unlock()
		q.broadcast(snap)
		// Setiap aksi harus tercatat di riwayat, termasuk pembatalan saat masih
		// menunggu. Bentuk argumen disamakan dengan jalur job berjalan.
		if q.onDone != nil {
			q.onDone(snap, context.Canceled)
		}
		return nil
	case StatusRunning:
		cancel := q.cancels[id]
		job.Status = StatusCancelled
		job.Message = "dibatalkan"
		q.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return nil
	default:
		q.mu.Unlock()
		return fmt.Errorf("job %s sudah selesai", id)
	}
}

// Subscribe menerima pembaruan setiap kali status job berubah.
func (q *Queue) Subscribe() (<-chan Job, func()) {
	ch := make(chan Job, 64)
	q.mu.Lock()
	q.nextSub++
	id := q.nextSub
	q.subs[id] = ch
	q.mu.Unlock()

	return ch, func() {
		q.mu.Lock()
		if c, ok := q.subs[id]; ok {
			delete(q.subs, id)
			close(c)
		}
		q.mu.Unlock()
	}
}

func (q *Queue) broadcast(j Job) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, ch := range q.subs {
		select {
		case ch <- j:
		default: // pelanggan lambat tidak boleh menghambat antrean
		}
	}
}

// Run menjalankan antrean sampai ctx dibatalkan.
func (q *Queue) Run(ctx context.Context) {
	for {
		job := q.nextQueued()
		if job == nil {
			select {
			case <-ctx.Done():
				return
			case <-q.notify:
			case <-time.After(time.Second):
			}
			continue
		}
		if q.deviceOK != nil && !q.deviceOK() {
			// Tahan antrean, jangan tandai gagal: perangkat mungkin kembali.
			select {
			case <-ctx.Done():
				return
			case <-q.notify:
			case <-time.After(time.Second):
			}
			continue
		}
		q.runJob(ctx, job)
	}
}

func (q *Queue) nextQueued() *Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range q.jobs {
		if j.Status == StatusQueued {
			return j
		}
	}
	return nil
}

func (q *Queue) runJob(ctx context.Context, job *Job) {
	jobCtx, cancel := context.WithCancel(ctx)

	q.mu.Lock()
	if job.Status != StatusQueued {
		q.mu.Unlock()
		cancel()
		return
	}
	job.Status = StatusRunning
	started := time.Now()
	job.StartedAt = &started
	job.Message = "Mulai"
	q.cancels[job.ID] = cancel
	snap := *job
	q.mu.Unlock()
	q.broadcast(snap)

	err := q.execute(jobCtx, job)
	cancel()

	q.mu.Lock()
	delete(q.cancels, job.ID)
	ended := time.Now()
	job.EndedAt = &ended
	switch {
	case job.Status == StatusCancelled:
		// sudah ditandai oleh Cancel
	case err != nil && errors.Is(err, context.Canceled):
		job.Status = StatusCancelled
		job.Message = "dibatalkan"
	case err != nil && errors.Is(err, adbx.ErrDeviceNotFound):
		// Perangkat dicabut di tengah proses: beri pesan yang lebih jelas, tetapi
		// error asli tetap disimpan agar bisa ditelusuri.
		job.Status = StatusFailed
		job.Message = "perangkat terputus"
		job.Error = err.Error()
	case err != nil:
		job.Status = StatusFailed
		job.Error = err.Error()
		job.Message = "Gagal"
	default:
		job.Status = StatusSuccess
		job.Progress = 100
		job.Message = "Selesai"
	}
	final := *job
	q.mu.Unlock()

	q.broadcast(final)
	if q.onDone != nil {
		q.onDone(final, err)
	}
}

func (q *Queue) execute(ctx context.Context, job *Job) error {
	switch job.Kind {
	case KindInstall:
		opts := adbx.InstallOptions{
			Replace:        job.Replace,
			AllowDowngrade: job.AllowDowngrade,
		}
		return q.runner.Install(ctx, job.Target, opts, func(percent int, msg string) {
			q.updateProgress(job.ID, percent, msg)
		})
	case KindUninstall:
		return q.runner.Uninstall(ctx, job.Target, false)
	case KindUninstallKeep:
		return q.runner.Uninstall(ctx, job.Target, true)
	case KindClearData:
		return q.runner.ClearData(ctx, job.Target)
	case KindPull:
		dest, err := q.runner.PullApk(ctx, job.Target, job.DestDir)
		if err == nil {
			q.setResult(job.ID, dest)
		}
		return err
	default:
		return fmt.Errorf("jenis job tidak dikenal: %s", job.Kind)
	}
}

func (q *Queue) updateProgress(id string, percent int, msg string) {
	q.mu.Lock()
	j, ok := q.byID[id]
	if !ok {
		q.mu.Unlock()
		return
	}
	j.Progress = percent
	j.Message = msg
	snap := *j
	q.mu.Unlock()
	q.broadcast(snap)
}

func (q *Queue) setResult(id, result string) {
	q.mu.Lock()
	j, ok := q.byID[id]
	if !ok {
		q.mu.Unlock()
		return
	}
	j.Result = result
	q.mu.Unlock()
}
```

**Catatan timestamp:** `StartedAt` dan `EndedAt` kini bertipe `*time.Time`
agar `omitempty` benar-benar menghilangkan kedua bidang saat job masih antre;
field diisi lewat pointer baru (`&started`, `&ended`) dan tidak pernah dimutasi
melalui pointer bersama.

- [ ] **Step 4: Rapikan tes terakhir yang berlebihan**

Tes `TestUnknownKindFails` memuat pemeriksaan `errors.Is` yang tidak berguna.
Ganti isi tes tersebut menjadi:

```go
func TestUnknownKindFails(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	runQueue(t, q)
	a := q.Enqueue(Job{Kind: Kind("ngawur"), Target: "x"})
	waitFor(t, "job gagal", func() bool {
		return statusOf(t, q, a.ID).Status == StatusFailed
	})
	if statusOf(t, q, a.ID).Error == "" {
		t.Fatal("job dengan jenis tak dikenal harus punya pesan error")
	}
}
```

Sesudah itu, import `errors` di berkas tes tidak lagi dipakai. Hapus `"errors"`
dari blok import `queue_test.go`.

- [ ] **Step 5: Jalankan tes, pastikan lulus**

Run: `go test ./internal/queue/ -race -v`
Expected: PASS untuk keempat belas tes, tanpa peringatan race.

- [ ] **Step 6: Commit**

```bash
git add internal/queue
git commit -m "feat(queue): antrean job berurutan dengan progress dan pembatalan"
```

---

## Task 11: API HTTP dan SSE (`internal/httpapi`)

**Files:**
- Create: `internal/httpapi/httpapi.go`
- Create: `internal/httpapi/apks.go`
- Test: `internal/httpapi/httpapi_test.go`

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/httpapi/httpapi_test.go`:

```go
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/herlangga72/adbapp/internal/adbx"
	"github.com/herlangga72/adbapp/internal/device"
	"github.com/herlangga72/adbapp/internal/paths"
	"github.com/herlangga72/adbapp/internal/queue"
	"github.com/herlangga72/adbapp/internal/store"
)

type fakeDevice struct {
	status   device.Status
	selected string
}

func (f *fakeDevice) Current() device.Status            { return f.status }
func (f *fakeDevice) Refresh(ctx context.Context) error { return nil }

// Select meniru Monitor.Select: hanya serial yang dikenal yang diterima, lalu
// perangkat itu dijadikan yang aktif.
func (f *fakeDevice) Select(serial string) error {
	all := append([]string{f.status.Serial}, f.status.Others...)
	found := false
	for _, s := range all {
		if s == serial {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("perangkat %s tidak dikenal", serial)
	}
	f.selected = serial
	f.status.Serial = serial
	others := []string{}
	for _, s := range all {
		if s != serial {
			others = append(others, s)
		}
	}
	f.status.Others = others
	return nil
}

type fakeQueue struct{ enqueued []queue.Job }

func (f *fakeQueue) Enqueue(j queue.Job) queue.Job {
	f.enqueued = append(f.enqueued, j)
	j.ID = "job-1"
	j.Status = queue.StatusQueued
	return j
}
func (f *fakeQueue) Jobs() []queue.Job      { return nil }
func (f *fakeQueue) Cancel(id string) error { return nil }
func (f *fakeQueue) Subscribe() (<-chan queue.Job, func()) {
	ch := make(chan queue.Job)
	return ch, func() { close(ch) }
}

type fakeHistory struct{ entries []store.Entry }

func (f *fakeHistory) Read(limit int) ([]store.Entry, error) { return f.entries, nil }
func (f *fakeHistory) ExportCSV(w io.Writer) error {
	_, err := io.WriteString(w, "time,action,package,device,success,detail\n")
	return err
}
func (f *fakeHistory) ExportJSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(f.entries)
}

type fakeAdb struct{}

func (fakeAdb) Packages(ctx context.Context, system bool) ([]adbx.Package, error) {
	return []adbx.Package{{Name: "com.foo", ApkPath: "/data/app/com.foo/base.apk"}}, nil
}
func (fakeAdb) PackageInfo(ctx context.Context, pkg string) (adbx.PackageInfo, error) {
	return adbx.PackageInfo{Package: pkg, VersionName: "1.0"}, nil
}

func newTestServer(t *testing.T) (*Server, *fakeQueue) {
	t.Helper()
	fq := &fakeQueue{}
	s := &Server{
		Device:  &fakeDevice{status: device.Status{State: device.StateReady, Serial: "S1", Model: "Pixel"}},
		Queue:   fq,
		History: &fakeHistory{entries: []store.Entry{{Action: "install", Package: "com.foo", Success: true}}},
		Adb:     fakeAdb{},
		Paths:   paths.ResolveFrom(t.TempDir()),
		Static:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>adbapp</html>")}},
	}
	return s, fq
}

// localRequest membuat permintaan dengan Host lokal, karena httptest.NewRequest
// memakai "example.com" yang memang ditolak oleh middleware localOnly.
func localRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Host = "127.0.0.1"
	return req
}

func TestStateEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d", rec.Code)
	}
	var body struct {
		Device device.Status `json:"device"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON tidak sah: %v", err)
	}
	if body.Device.Serial != "S1" {
		t.Fatalf("serial salah: %+v", body.Device)
	}
}

func TestHistoryEmptyReturnsArray(t *testing.T) {
	s, _ := newTestServer(t)
	s.History = store.New(filepath.Join(t.TempDir(), "history.jsonl"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Fatalf("body = %q, mau []", body)
	}
}

func TestCreateJobsEnqueuesOnePerTarget(t *testing.T) {
	s, fq := newTestServer(t)
	payload := `{"kind":"uninstall","targets":["com.a","com.b"]}`
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/jobs", strings.NewReader(payload))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d body %s", rec.Code, rec.Body.String())
	}
	if len(fq.enqueued) != 2 {
		t.Fatalf("harus 2 job, dapat %d", len(fq.enqueued))
	}
	if fq.enqueued[0].Kind != queue.KindUninstall || fq.enqueued[1].Target != "com.b" {
		t.Fatalf("job salah: %+v", fq.enqueued)
	}
}

func TestCreateJobsRejectsUnknownKind(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/jobs", strings.NewReader(`{"kind":"ngawur","targets":["x"]}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("harus 400, dapat %d", rec.Code)
	}
}

func TestExportCSVSetsHeaders(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/history/export?format=csv", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("content type salah: %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("harus sebagai unduhan: %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestLocalOnlyRejectsForeignHost(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Host = "evil.example.com"
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("harus 403, dapat %d", rec.Code)
	}
}

func TestUploadRejectsCorruptAPK(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writeMultipart(t, &body, "file", "contoh.apk", []byte("bukan-apk-sungguhan"))
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/apks/upload", &body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=batas")
	s.Handler().ServeHTTP(rec, req)

	// Berkas yang bukan APK sah ditolak dan tidak ditinggalkan di folder.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("harus 400, dapat %d body %s", rec.Code, rec.Body.String())
	}
}

func TestSelectDeviceUnknownSerial(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/device/select", strings.NewReader(`{"serial":"tidak-ada"}`))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("harus 404, dapat %d body %s", rec.Code, rec.Body.String())
	}
}

func writeMultipart(t *testing.T, buf *bytes.Buffer, field, filename string, content []byte) {
	t.Helper()
	buf.WriteString("--batas\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"" + field + "\"; filename=\"" + filename + "\"\r\n")
	buf.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	buf.Write(content)
	buf.WriteString("\r\n--batas--\r\n")
}

func TestIndexServedFromEmbeddedFS(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "adbapp") {
		t.Fatalf("halaman tidak disajikan: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/httpapi/`
Expected: FAIL, `undefined: Server`.

- [ ] **Step 3: Implementasi inti**

Create `internal/httpapi/httpapi.go`:

```go
// Package httpapi menyajikan API untuk UI dan mengalirkan pembaruan lewat SSE.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
	"github.com/herlangga72/adbapp/internal/device"
	"github.com/herlangga72/adbapp/internal/paths"
	"github.com/herlangga72/adbapp/internal/queue"
	"github.com/herlangga72/adbapp/internal/store"
)

type DeviceSource interface {
	Current() device.Status
	Refresh(ctx context.Context) error
	Select(serial string) error
}

type JobQueue interface {
	Enqueue(queue.Job) queue.Job
	Jobs() []queue.Job
	Cancel(id string) error
	Subscribe() (<-chan queue.Job, func())
}

type History interface {
	Read(limit int) ([]store.Entry, error)
	ExportCSV(w io.Writer) error
	ExportJSON(w io.Writer) error
}

type Adb interface {
	Packages(ctx context.Context, system bool) ([]adbx.Package, error)
	PackageInfo(ctx context.Context, pkg string) (adbx.PackageInfo, error)
}

type Server struct {
	Device  DeviceSource
	Queue   JobQueue
	History History
	Adb     Adb
	Paths   paths.Paths
	Static  fs.FS
	LoadCfg func() (store.Config, error)
	SaveCfg func(store.Config) error
}

// Handler merakit seluruh rute.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/device/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/device/select", s.handleSelectDevice)
	mux.HandleFunc("GET /api/apks", s.handleListAPKs)
	mux.HandleFunc("POST /api/apks/upload", s.handleUpload)
	mux.HandleFunc("POST /api/apks/url", s.handleFromURL)
	mux.HandleFunc("GET /api/packages", s.handlePackages)
	mux.HandleFunc("POST /api/packages/detail", s.handlePackageDetail)
	mux.HandleFunc("POST /api/jobs", s.handleCreateJobs)
	mux.HandleFunc("POST /api/jobs/cancel", s.handleCancelJob)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/history/export", s.handleExport)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("POST /api/config", s.handleConfig)
	mux.Handle("GET /", http.FileServerFS(s.Static))
	return localOnly(mux)
}

// localOnly menolak permintaan yang datang dengan Host bukan alamat lokal,
// supaya server yang hanya mendengarkan 127.0.0.1 tidak bisa dipakai halaman
// web lain di jaringan.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		host = strings.Trim(host, "[]")
		switch host {
		case "127.0.0.1", "localhost", "::1":
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "hanya bisa diakses dari komputer ini", http.StatusForbidden)
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	return dec.Decode(v)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	cfg := store.Config{}
	if s.LoadCfg != nil {
		if c, err := s.LoadCfg(); err == nil {
			cfg = c
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device": s.Device.Current(),
		"jobs":   s.Queue.Jobs(),
		"config": cfg,
	})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.Device.Refresh(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Device.Current())
}

// handleSelectDevice memilih perangkat aktif ketika lebih dari satu tersambung.
// Serial yang tidak dikenal dibalas 404; serial kosong dibalas 400.
func (s *Server) handleSelectDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Serial string `json:"serial"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if strings.TrimSpace(req.Serial) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("serial wajib diisi"))
		return
	}
	if err := s.Device.Select(req.Serial); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err := s.Device.Refresh(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Device.Current())
}

func (s *Server) handlePackages(w http.ResponseWriter, r *http.Request) {
	system := r.URL.Query().Get("system") == "1"
	pkgs, err := s.Adb.Packages(r.Context(), system)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, pkgs)
}

func (s *Server) handlePackageDetail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Package string `json:"package"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Package == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("package wajib diisi"))
		return
	}
	info, err := s.Adb.PackageInfo(r.Context(), req.Package)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type createJobsRequest struct {
	Kind           queue.Kind `json:"kind"`
	Targets        []string   `json:"targets"`
	Replace        bool       `json:"replace"`
	AllowDowngrade bool       `json:"allowDowngrade"`
}

func (s *Server) handleCreateJobs(w http.ResponseWriter, r *http.Request) {
	var req createJobsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("badan permintaan tidak sah: %w", err))
		return
	}
	if !knownKind(req.Kind) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("jenis job tidak dikenal: %s", req.Kind))
		return
	}
	if len(req.Targets) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("tidak ada sasaran"))
		return
	}

	created := make([]queue.Job, 0, len(req.Targets))
	for _, target := range req.Targets {
		if strings.TrimSpace(target) == "" {
			continue
		}
		created = append(created, s.Queue.Enqueue(queue.Job{
			Kind:           req.Kind,
			Target:         target,
			Label:          labelFor(req.Kind, target),
			DestDir:        s.Paths.PulledDir,
			Replace:        req.Replace,
			AllowDowngrade: req.AllowDowngrade,
		}))
	}
	writeJSON(w, http.StatusOK, created)
}

func knownKind(k queue.Kind) bool {
	switch k {
	case queue.KindInstall, queue.KindUninstall, queue.KindUninstallKeep,
		queue.KindClearData, queue.KindPull:
		return true
	}
	return false
}

func labelFor(k queue.Kind, target string) string {
	base := target
	if i := strings.LastIndex(target, "/"); i >= 0 {
		base = target[i+1:]
	}
	switch k {
	case queue.KindInstall:
		return "Pasang " + base
	case queue.KindUninstall:
		return "Copot " + base
	case queue.KindUninstallKeep:
		return "Copot (simpan data) " + base
	case queue.KindClearData:
		return "Hapus data " + base
	case queue.KindPull:
		return "Tarik APK " + base
	}
	return base
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("id wajib diisi"))
		return
	}
	if err := s.Queue.Cancel(req.ID); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "dibatalkan"})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := s.History.Read(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if entries == nil {
		entries = []store.Entry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	w.Header().Set("Content-Disposition", "attachment; filename=riwayat-adbapp."+format)
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := s.History.ExportJSON(w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	default:
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		if err := s.History.ExportCSV(w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	var cfg store.Config
	if err := decodeJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.SaveCfg != nil {
		if err := s.SaveCfg(cfg); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, cfg)
}

// handleEvents mengalirkan perubahan job dan status perangkat sebagai SSE.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE tidak didukung", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	jobs, unsubscribe := s.Queue.Subscribe()
	defer unsubscribe()

	sendState := func() {
		payload, err := json.Marshal(map[string]any{"device": s.Device.Current()})
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: state\ndata: %s\n\n", payload)
		flusher.Flush()
	}
	sendState()

	deviceTicker := time.NewTicker(2 * time.Second)
	defer deviceTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}
			payload, err := json.Marshal(job)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: job\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-deviceTicker.C:
			sendState()
		}
	}
}
```

- [ ] **Step 4: Implementasi berkas APK**

Create `internal/httpapi/apks.go`:

```go
package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/herlangga72/adbapp/internal/apkmeta"
)

type apkEntry struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Package     string `json:"package,omitempty"`
	VersionName string `json:"versionName,omitempty"`
	VersionCode int64  `json:"versionCode,omitempty"`
	MinSDK      int    `json:"minSdk,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (s *Server) entryFor(path string) apkEntry {
	e := apkEntry{Path: path, Name: filepath.Base(path)}
	st, err := os.Stat(path)
	if err != nil {
		e.Error = err.Error()
		return e
	}
	e.Size = st.Size()

	meta, err := apkmeta.ReadCached(path, st.Size(), st.ModTime().UnixNano())
	if err != nil {
		e.Error = err.Error()
		return e
	}
	e.Package = meta.Package
	e.VersionName = meta.VersionName
	e.VersionCode = meta.VersionCode
	e.MinSDK = meta.MinSDK
	return e
}

// handleListAPKs mendaftar berkas .apk di folder koleksi.
func (s *Server) handleListAPKs(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		if s.LoadCfg != nil {
			if cfg, err := s.LoadCfg(); err == nil {
				folder = cfg.ApkFolder
			}
		}
	}
	if folder == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("folder belum ditentukan"))
		return
	}

	dirents, err := os.ReadDir(folder)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("folder tidak bisa dibaca: %w", err))
		return
	}

	entries := make([]apkEntry, 0, len(dirents))
	for _, de := range dirents {
		if de.IsDir() || !strings.EqualFold(filepath.Ext(de.Name()), ".apk") {
			continue
		}
		entries = append(entries, s.entryFor(filepath.Join(folder, de.Name())))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 30); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("gagal membaca berkas: %w", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("berkas tidak ditemukan pada permintaan"))
		return
	}
	defer file.Close()

	name := filepath.Base(header.Filename)
	if !strings.EqualFold(filepath.Ext(name), ".apk") {
		writeError(w, http.StatusBadRequest, fmt.Errorf("hanya berkas .apk yang diterima"))
		return
	}

	dest := filepath.Join(s.Paths.UploadsDir, name)
	if err := os.MkdirAll(s.Paths.UploadsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out, err := os.Create(dest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := out.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	entry := s.entryFor(dest)
	if entry.Error != "" {
		// Berkas yang bukan APK yang sah ditolak di sini, sama seperti jalur URL,
		// dan tidak ditinggalkan di folder unggahan (spec 7).
		_ = os.Remove(dest)
		writeError(w, http.StatusBadRequest, fmt.Errorf("berkas bukan APK yang sah: %s", entry.Error))
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleFromURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil || req.URL == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("url wajib diisi"))
		return
	}

	name := filepath.Base(req.Name)
	if name == "" || name == "." || name == "/" {
		name = filepath.Base(req.URL)
	}
	if !strings.EqualFold(filepath.Ext(name), ".apk") {
		name += ".apk"
	}

	resp, err := http.Get(req.URL)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("gagal mengunduh: %w", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Errorf("unduhan gagal, kode %d", resp.StatusCode))
		return
	}

	if err := os.MkdirAll(s.Paths.UploadsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	dest := filepath.Join(s.Paths.UploadsDir, name)
	out, err := os.Create(dest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 2<<30)); err != nil {
		out.Close()
		writeError(w, http.StatusBadGateway, fmt.Errorf("gagal menyimpan unduhan: %w", err))
		return
	}
	out.Close()

	entry := s.entryFor(dest)
	if entry.Error != "" {
		if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Errorf("unduhan bukan APK yang sah: %s", entry.Error))
		return
	}
	writeJSON(w, http.StatusOK, entry)
}
```

- [ ] **Step 5: Jalankan tes, pastikan lulus**

Run: `go test ./internal/httpapi/ -v`
Expected: PASS untuk seluruh tes httpapi.

- [ ] **Step 6: Commit**

```bash
git add internal/httpapi
git commit -m "feat(httpapi): API REST, SSE, dan pengelolaan berkas APK"
```

### Hardening pasca-tinjauan (guard lintas situs)

Bind loopback saja tidak cukup: halaman web mana pun bisa mengirim permintaan
"simple request" CORS ke `127.0.0.1` dan server tetap menerimanya. Karena itu,
di `localOnly` (berjalan untuk setiap rute sebelum handler):

- `Host` wajib loopback (`127.0.0.1`, `localhost`, `::1`, dengan/tanpa port,
  dengan/tanpa titik akhir), diperiksa memakai `net.SplitHostPort` dan
  `strings.EqualFold`.
- Bila header `Origin` ada, ia harus origin loopback berskema `http`
  (`http://127.0.0.1[:port]`, `http://localhost[:port]`, `http://[::1][:port]`);
  selain itu `403`.
- Bila header `Sec-Fetch-Site` ada, nilainya harus `same-origin` atau `none`;
  selain itu `403`.
- Permintaan tanpa kedua header (mis. `curl`, skrip) tetap dilayani.

Endpoint JSON (`POST /api/jobs`, `/api/jobs/cancel`, `/api/device/refresh`,
`/api/config`, `/api/apks/url`, `/api/packages/detail`) juga menolak
`Content-Type` yang bukan `application/json` dengan `415` (akhiran
`; charset=...` diterima). Ini memblokir `text/plain` yang boleh dikirim lintas
situs tanpa preflight. Selain itu `handleFromURL` mengunduh ke berkas
`<dest>.part` lalu `os.Rename` setelah tervalidasi (APK lama tidak rusak),
memakai `http.NewRequestWithContext` dengan klien berbatas waktu 15 menit dan
maksimal 5 pengalihan, serta menolak unduhan di atas 2 GiB. `handleUpload`
dibatasi `http.MaxBytesReader` dan membalas `413` bila melewati batas.

---

## Task 12: Tampilan web (`internal/webui`)

**Files:**
- Create: `internal/webui/embed.go`
- Create: `internal/webui/static/index.html`
- Create: `internal/webui/static/styles.css`
- Create: `internal/webui/static/app.js`
- Test: `internal/webui/embed_test.go`

- [ ] **Step 1: Tulis tes yang gagal**

Create `internal/webui/embed_test.go`:

```go
package webui

import (
	"io/fs"
	"strings"
	"testing"
)

func TestFSContainsIndex(t *testing.T) {
	data, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("index.html tidak ada di dalam biner: %v", err)
	}
	if !strings.Contains(string(data), "adbapp") {
		t.Fatal("index.html tidak memuat penanda aplikasi")
	}
}

func TestFSContainsAssets(t *testing.T) {
	for _, name := range []string{"app.js", "styles.css"} {
		if _, err := fs.ReadFile(FS(), name); err != nil {
			t.Fatalf("%s tidak ada: %v", name, err)
		}
	}
}
```

- [ ] **Step 2: Jalankan tes, pastikan gagal**

Run: `go test ./internal/webui/`
Expected: FAIL, `undefined: FS`.

- [ ] **Step 3: Implementasi embed**

Create `internal/webui/embed.go`:

```go
// Package webui menanam berkas tampilan ke dalam biner aplikasi.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticFS embed.FS

// FS mengembalikan berkas tampilan siap disajikan.
func FS() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
```

- [ ] **Step 4: Tulis halaman**

Create `internal/webui/static/index.html`:

```html
<!doctype html>
<html lang="id">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>adbapp — pasang &amp; copot aplikasi Android</title>
<link rel="stylesheet" href="styles.css">
</head>
<body>
<header class="topbar">
  <div class="device">
    <span id="dot" class="dot"></span>
    <span id="device-label">Memeriksa perangkat...</span>
    <select id="device-select" class="hidden" aria-label="Pilih perangkat"></select>
  </div>
  <button id="refresh" class="secondary">Pindai ulang</button>
</header>

<div id="hint" class="hint hidden"></div>

<nav class="tabs">
  <button class="tab active" data-tab="install">Pasang</button>
  <button class="tab" data-tab="installed">Terpasang</button>
</nav>

<main>
  <section id="tab-install" class="panel active">
    <div id="drop" class="drop">
      <p><strong>Seret &amp; lepas berkas APK</strong> ke sini</p>
      <p class="muted">atau pakai tombol di bawah</p>
      <div class="row">
        <button id="pick-file" class="secondary">Pilih berkas</button>
        <input id="folder" class="grow" placeholder="Folder koleksi APK, mis. /home/kamu/apk">
        <button id="load-folder" class="secondary">Muat folder</button>
      </div>
      <div class="row">
        <input id="url" class="grow" placeholder="Tambah dari URL, mis. https://contoh/app.apk">
        <button id="add-url" class="secondary">Unduh</button>
      </div>
    </div>
    <input id="file-input" type="file" accept=".apk" multiple class="hidden">

    <div class="toolbar">
      <label><input type="checkbox" id="replace" checked> Timpa bila sudah terpasang</label>
      <label><input type="checkbox" id="downgrade"> Izinkan versi lebih rendah</label>
    </div>

    <table id="apk-table">
      <thead>
        <tr><th></th><th>Berkas</th><th>Paket</th><th>Versi</th><th>minSdk</th><th>Ukuran</th></tr>
      </thead>
      <tbody></tbody>
    </table>
    <div class="actions">
      <button id="install-selected" class="primary">Pasang terpilih</button>
    </div>
  </section>

  <section id="tab-installed" class="panel">
    <div class="toolbar">
      <input id="search" class="grow" placeholder="Cari aplikasi...">
      <div class="filters" role="group" aria-label="Filter aplikasi">
        <button class="chip filter active" data-filter="all">Semua</button>
        <button class="chip filter" data-filter="third">Pihak ketiga</button>
        <button class="chip filter" data-filter="system">Sistem</button>
      </div>
      <select id="sort">
        <option value="name">Urut nama</option>
        <option value="size" disabled>Urut ukuran</option>
        <option value="date" disabled>Urut tanggal</option>
      </select>
      <button id="load-details" class="secondary">Muat detail</button>
      <button id="reload-packages" class="secondary">Muat ulang</button>
    </div>
    <div id="sort-hint" class="muted small">Muat detail dulu untuk mengaktifkan urut ukuran/tanggal.</div>
    <table id="pkg-table">
      <thead>
        <tr><th></th><th>Paket</th><th>Versi</th><th>Ukuran</th><th>Tanggal</th><th>Aksi</th></tr>
      </thead>
      <tbody></tbody>
    </table>
    <div class="actions">
      <button id="uninstall-selected" class="danger">Copot terpilih</button>
      <button id="uninstall-keep-selected" class="danger">Copot (simpan data)</button>
    </div>
    <div id="detail" class="detail hidden"></div>
  </section>
</main>

<section class="bottom">
  <button id="bottom-toggle" class="bottom-header" aria-expanded="true">
    <span>Antrean &amp; Riwayat</span>
    <span class="chevron" aria-hidden="true">▾</span>
  </button>
  <div class="bottom-body">
    <div class="tabs small">
      <button class="tab active" data-tab="queue">Antrean</button>
      <button class="tab" data-tab="history">Riwayat</button>
    </div>
    <div id="tab-queue" class="panel active">
      <ul id="queue-list" class="jobs"></ul>
    </div>
    <div id="tab-history" class="panel">
      <div class="row">
        <input id="history-filter" class="grow" placeholder="Saring riwayat (aksi, paket, catatan)...">
        <button id="export-csv" class="secondary">Ekspor CSV</button>
        <button id="export-json" class="secondary">Ekspor JSON</button>
      </div>
      <table id="history-table">
        <thead><tr><th>Waktu</th><th>Aksi</th><th>Paket</th><th>Hasil</th><th>Catatan</th></tr></thead>
        <tbody></tbody>
      </table>
    </div>
  </div>
</section>

<div id="toast" class="toast hidden"></div>
<script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 5: Tulis gaya**

Create `internal/webui/static/styles.css`:

```css
:root {
  --bg: #f7f8fa;
  --panel: #ffffff;
  --line: #dfe3e8;
  --text: #1c2024;
  --muted: #6b7280;
  --accent: #2563eb;
  --danger: #dc2626;
  --ok: #16a34a;
  --warn: #d97706;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
  background: var(--bg);
  color: var(--text);
  font-size: 14px;
}
.hidden { display: none !important; }
.topbar {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px; padding: 10px 16px; background: var(--panel);
  border-bottom: 1px solid var(--line); position: sticky; top: 0; z-index: 5;
}
.device { display: flex; align-items: center; gap: 8px; font-weight: 600; }
.dot { width: 10px; height: 10px; border-radius: 50%; background: var(--muted); }
.dot.ready { background: var(--ok); }
.dot.unauthorized { background: var(--warn); }
.dot.offline, .dot.none { background: var(--danger); }
.hint {
  margin: 8px 16px; padding: 10px 12px; border-left: 3px solid var(--warn);
  background: #fff7ed; color: #7c2d12; border-radius: 4px;
}
.tabs { display: flex; gap: 4px; padding: 8px 16px 0; }
.tabs.small { padding-top: 0; }
.tab {
  border: 1px solid var(--line); background: var(--panel); padding: 6px 14px;
  border-radius: 6px 6px 0 0; cursor: pointer; color: var(--muted);
}
.tab.active { color: var(--text); font-weight: 600; border-bottom-color: var(--panel); }
main { padding: 0 16px 16px; }
.panel { background: var(--panel); border: 1px solid var(--line); border-radius: 8px; padding: 14px; display: none; }
.panel.active { display: block; }
.bottom { margin: 16px; }
.drop {
  border: 2px dashed var(--line); border-radius: 8px; padding: 16px;
  text-align: center; background: #fbfcfd;
}
.drop.over { border-color: var(--accent); background: #eff6ff; }
.drop .row, .toolbar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.row { margin-top: 8px; }
.toolbar { margin: 12px 0; }
.grow { flex: 1 1 220px; }
input[type=text], input:not([type]), select {
  padding: 6px 8px; border: 1px solid var(--line); border-radius: 6px; background: #fff;
}
table { width: 100%; border-collapse: collapse; }
th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--line); }
th { font-size: 12px; text-transform: uppercase; color: var(--muted); letter-spacing: .03em; }
tr.selected { background: #eff6ff; }
button {
  padding: 6px 12px; border-radius: 6px; border: 1px solid var(--line);
  background: var(--panel); cursor: pointer;
}
button.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
button.danger { background: var(--danger); border-color: var(--danger); color: #fff; }
button.secondary { background: #f1f3f5; }
button:disabled { opacity: .5; cursor: not-allowed; }
.actions { display: flex; gap: 8px; margin-top: 12px; }
.muted { color: var(--muted); }
.badge {
  font-size: 11px; padding: 1px 6px; border-radius: 10px;
  background: #e5e7eb; color: #374151;
}
.jobs { list-style: none; margin: 0; padding: 0; }
.jobs li { padding: 8px 0; border-bottom: 1px solid var(--line); }
.jobs .meta { color: var(--muted); font-size: 12px; }
.progress { height: 6px; background: #e5e7eb; border-radius: 3px; overflow: hidden; margin-top: 6px; }
.progress > span { display: block; height: 100%; background: var(--accent); }
.status-success { color: var(--ok); }
.status-failed { color: var(--danger); }
.status-cancelled { color: var(--muted); }
.detail { margin-top: 12px; padding: 12px; background: #f9fafb; border: 1px solid var(--line); border-radius: 6px; }
.detail dl { display: grid; grid-template-columns: 160px 1fr; gap: 4px 12px; margin: 0; }
.toast {
  position: fixed; right: 16px; bottom: 16px; max-width: 420px;
  background: #111827; color: #fff; padding: 10px 14px; border-radius: 8px;
  box-shadow: 0 6px 20px rgba(0,0,0,.2); z-index: 20;
}
.small { font-size: 12px; }
.filters { display: flex; gap: 4px; }
.chip {
  padding: 6px 12px; border-radius: 999px; border: 1px solid var(--line);
  background: var(--panel); color: var(--muted); cursor: pointer;
}
.chip.active { background: var(--accent); border-color: var(--accent); color: #fff; font-weight: 600; }
.row-actions { display: flex; gap: 6px; flex-wrap: wrap; }
.row-actions button { padding: 4px 8px; font-size: 12px; }
.bottom-header {
  width: 100%; display: flex; align-items: center; justify-content: space-between;
  padding: 8px 12px; border: 1px solid var(--line); border-radius: 8px 8px 0 0;
  background: var(--panel); cursor: pointer; font-weight: 600;
}
.bottom .tabs.small { padding: 8px 8px 0; }
.bottom .panel { border-radius: 0 0 8px 8px; }
.chevron { transition: transform .15s ease; }
.bottom.collapsed .chevron { transform: rotate(-90deg); }
.bottom.collapsed .bottom-body { display: none; }
```

- [ ] **Step 6: Tulis logika UI**

Create `internal/webui/static/app.js`:

```js
'use strict';

const $ = (id) => document.getElementById(id);

// escapeHtml mengubah karakter khusus HTML menjadi entitas. Semua nilai yang
// berasal dari perangkat atau berkas pengguna harus dilewatkan helper ini
// sebelum masuk innerHTML, supaya nama berkas yang jahat tidak menyuntikkan
// skrip (stored XSS).
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
  renderApks();
  renderPackages();
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
      renderApks();
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
      renderPackages();
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
  $('search').addEventListener('input', renderPackages);
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
```

- [ ] **Step 7: Jalankan tes, pastikan lulus**

Run: `go test ./internal/webui/ -v`
Expected: PASS untuk kedua tes.

- [ ] **Step 8: Commit**

```bash
git add internal/webui
git commit -m "feat(webui): tampilan web tertanam untuk pasang dan copot aplikasi"
```

---

## Task 13: Merakit semuanya (`main.go`)

**Files:**
- Create: `main.go`
- Create: `internal/browser/browser.go`

- [ ] **Step 1: Tulis pembuka browser**

Create `internal/browser/browser.go`:

```go
// Package browser membuka URL di browser bawaan sistem.
package browser

import (
	"os/exec"
	"runtime"
)

// Open membuka url di browser. Kegagalan tidak fatal: pengguna masih bisa
// membuka alamatnya secara manual.
func Open(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
```

- [ ] **Step 2: Tulis `main.go`**

Create `main.go`:

```go
// Command adbapp menyajikan UI lokal untuk memasang dan mencopot aplikasi
// Android lewat adb.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
	"github.com/herlangga72/adbapp/internal/browser"
	"github.com/herlangga72/adbapp/internal/bundle"
	"github.com/herlangga72/adbapp/internal/device"
	"github.com/herlangga72/adbapp/internal/httpapi"
	"github.com/herlangga72/adbapp/internal/paths"
	"github.com/herlangga72/adbapp/internal/queue"
	"github.com/herlangga72/adbapp/internal/store"
	"github.com/herlangga72/adbapp/internal/webui"
)

func main() {
	port := flag.Int("port", 0, "port server lokal (0 = pilih otomatis)")
	noOpen := flag.Bool("no-open", false, "jangan buka browser otomatis")
	dataDir := flag.String("data", "", "folder data khusus (untuk pengujian)")
	flag.Parse()

	if err := run(*port, *noOpen, *dataDir); err != nil {
		log.Fatalf("adbapp berhenti: %v", err)
	}
}

func run(port int, noOpen bool, dataDir string) error {
	p, err := resolvePaths(dataDir)
	if err != nil {
		return err
	}
	if err := p.Ensure(); err != nil {
		return fmt.Errorf("tidak bisa menyiapkan folder data: %w", err)
	}

	adbPath, err := bundle.Ensure(p.AdbDir)
	if err != nil {
		return err
	}
	log.Printf("adb siap: %s", adbPath)

	base := adbx.New(adbPath)
	monitor := device.New(base, device.WithVersionGetter(func(ctx context.Context, serial string) (string, error) {
		return base.WithSerial(serial).Output(ctx, "shell", "getprop", "ro.build.version.release")
	}))
	history := store.New(p.HistoryFile)

	targeted := &targetedRunner{base: base, monitor: monitor}
	q := queue.New(targeted,
		queue.WithDeviceCheck(func() bool { return monitor.Current().State == device.StateReady }),
		queue.WithOnDone(func(job queue.Job, jobErr error) {
			entry := store.Entry{
				Action:  string(job.Kind),
				Package: job.Target,
				Device:  monitor.Current().Serial,
				Success: jobErr == nil && job.Status == queue.StatusSuccess,
			}
			if jobErr != nil {
				entry.Detail = jobErr.Error()
			} else if job.Result != "" {
				entry.Detail = job.Result
			}
			if err := history.Append(entry); err != nil {
				log.Printf("gagal menulis riwayat: %v", err)
			}
		}))

	cfgPath := p.ConfigFile
	api := &httpapi.Server{
		Device:  monitor,
		Queue:   q,
		History: history,
		Adb:     targeted,
		Paths:   p,
		Static:  webui.FS(),
		LoadCfg: func() (store.Config, error) { return store.LoadConfig(cfgPath) },
		SaveCfg: func(c store.Config) error { return store.SaveConfig(cfgPath, c) },
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("tidak bisa membuka port lokal: %w", err)
	}
	url := "http://" + listener.Addr().String()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := monitor.Refresh(ctx); err != nil {
		log.Printf("pemindaian perangkat awal gagal: %v", err)
	}
	go monitor.Loop(ctx, 2*time.Second)
	go q.Run(ctx)

	log.Printf("adbapp jalan di %s", url)
	log.Printf("folder data: %s", p.DataDir)
	if !noOpen {
		if err := browser.Open(url); err != nil {
			log.Printf("tidak bisa membuka browser otomatis: %v", err)
		}
	}

	server := &http.Server{Handler: api.Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func resolvePaths(dataDir string) (paths.Paths, error) {
	if dataDir != "" {
		return paths.ResolveFrom(dataDir), nil
	}
	return paths.Resolve()
}

// targetedRunner meneruskan setiap perintah adb ke perangkat yang sedang aktif.
type targetedRunner struct {
	base    *adbx.Runner
	monitor *device.Monitor
}

func (t *targetedRunner) runner() *adbx.Runner {
	return t.base.WithSerial(t.monitor.Current().Serial)
}

func (t *targetedRunner) Install(ctx context.Context, apkPath string, opts adbx.InstallOptions, stage adbx.StageFunc) error {
	return t.runner().Install(ctx, apkPath, opts, stage)
}

func (t *targetedRunner) Uninstall(ctx context.Context, pkg string, keepData bool) error {
	return t.runner().Uninstall(ctx, pkg, keepData)
}

func (t *targetedRunner) ClearData(ctx context.Context, pkg string) error {
	return t.runner().ClearData(ctx, pkg)
}

func (t *targetedRunner) PullApk(ctx context.Context, pkg string, destDir string) (string, error) {
	return t.runner().PullApk(ctx, pkg, destDir)
}

func (t *targetedRunner) Packages(ctx context.Context, system bool) ([]adbx.Package, error) {
	return t.runner().Packages(ctx, system)
}

func (t *targetedRunner) PackageInfo(ctx context.Context, pkg string) (adbx.PackageInfo, error) {
	return t.runner().PackageInfo(ctx, pkg)
}

var _ = strings.TrimSpace // penanda sementara agar import tetap dipakai
```

- [ ] **Step 3: Hapus baris penanda sementara**

Hapus baris terakhir `var _ = strings.TrimSpace ...` dan hapus `"strings"` dari
blok import `main.go`, karena tidak ada lagi yang memakainya.

- [ ] **Step 4: Bangun aplikasi**

Run: `go build -o adbapp .`
Expected: sukses, berkas `adbapp` terbentuk. Bila muncul
`biner adb untuk linux-amd64 belum dibundel`, lanjutkan ke Task 14 Step 1 lalu
ulangi perintah ini.

- [ ] **Step 5: Uji jalan singkat tanpa perangkat**

Run: `./adbapp -no-open -port 7799 -data /tmp/adbapp-uji &` lalu
`curl -s http://127.0.0.1:7799/api/state` dan
`curl -s http://127.0.0.1:7799/ | head -3`, terakhir `kill %1`.
Expected: `/api/state` mengembalikan JSON dengan `"device"`, `"jobs"`, dan
`"config"`; halaman utama mengembalikan HTML.

- [ ] **Step 6: Commit**

```bash
git add main.go internal/browser
git commit -m "feat: rakit server lokal, antrean, dan pembuka browser"
```

---

## Task 14: Makefile dan rilis otomatis

**Files:**
- Create: `Makefile`
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Tulis Makefile dengan pengunduh platform-tools**

Create `Makefile`:

```makefile
BINARY := adbapp
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
PT_V   := 35.0.2

PLATFORM := $(GOOS)-$(GOARCH)

# Arsip platform-tools memakai akhiran -linux.zip / -darwin.zip, sedangkan
# Windows memakai -win.zip (bukan -windows.zip).
PT_OS := $(GOOS)
ifeq ($(GOOS),windows)
PT_OS := win
endif

# adb.exe di Windows memuat AdbWinApi.dll secara statis, jadi DLL bawaan
# platform-tools harus ikut diekstrak berdampingan dengan adb.exe; tanpa itu
# adb tidak bisa dijalankan.
PT_FILES := adb
ifeq ($(GOOS),windows)
PT_FILES := adb.exe AdbWinApi.dll AdbWinUsbApi.dll
endif

ADB_DIR := internal/bundle/bin/$(PLATFORM)

.PHONY: test build run fetch-adb clean fmt vet release

fetch-adb:
	@echo "mengunduh platform-tools $(PT_V) untuk $(GOOS)"
	@tmp=$$(mktemp -d); \
	url="https://dl.google.com/android/repository/platform-tools_r$(PT_V)-$(PT_OS).zip"; \
	echo "$$url"; \
	curl -fsSL -o $$tmp/pt.zip "$$url" || { echo "unduhan gagal"; exit 1; }; \
	unzip -q -o $$tmp/pt.zip -d $$tmp; \
	mkdir -p $(ADB_DIR); \
	for f in $(PT_FILES); do cp "$$tmp/platform-tools/$$f" "$(ADB_DIR)/$$f"; done; \
	chmod +x "$(ADB_DIR)/adb" 2>/dev/null || true; \
	chmod +x "$(ADB_DIR)/adb.exe" 2>/dev/null || true; \
	rm -rf $$tmp; \
	ls -l $(ADB_DIR)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

build:
	go build -o $(BINARY) .

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY) $(BINARY).exe
```

Catatan: nama arsip platform-tools memakai pola
`platform-tools_r<versi>-<os>.zip`. Linux/macOS memakai `-linux.zip` /
`-darwin.zip`, sedangkan Windows memakai `-win.zip` (bukan `-windows.zip`).
Di Windows, `adb.exe` memuat `AdbWinApi.dll` secara statis sehingga
`AdbWinApi.dll` dan `AdbWinUsbApi.dll` harus ikut dikirim di folder
`internal/bundle/bin/<os>-<arch>/`; tanpa itu adb gagal dijalankan. Bila
unduhan gagal karena pola berubah, periksa
`https://developer.android.com/tools/releases/platform-tools` dan sesuaikan
variabel `PT_V`.

- [ ] **Step 2: Uji pengunduhan di mesin pengembang**

Run: `make fetch-adb && ls -l internal/bundle/bin/linux-amd64/adb`
Expected: berkas `adb` ada, dapat dieksekusi.

- [ ] **Step 3: Bangun dan jalankan pengujian penuh**

Run: `make test && make build`
Expected: seluruh tes lulus, biner `adbapp` terbentuk.

- [ ] **Step 4: Tulis workflow rilis**

Create `.github/workflows/release.yml`:

```yaml
name: build-and-release

on:
  push:
    tags: ["v*"]
  workflow_dispatch:

permissions:
  contents: write

jobs:
  build:
    strategy:
      fail-fast: false
      matrix:
        include:
          - runner: ubuntu-latest
            goos: linux
            goarch: amd64
            platform_tools: linux
            archive: tar.gz
          - runner: ubuntu-latest
            goos: windows
            goarch: amd64
            platform_tools: win
            archive: zip
          - runner: macos-latest
            goos: darwin
            goarch: arm64
            platform_tools: darwin
            archive: tar.gz
          - runner: macos-latest
            goos: darwin
            goarch: amd64
            platform_tools: darwin
            archive: tar.gz

    runs-on: ${{ matrix.runner }}

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"

      - name: Siapkan biner adb untuk platform ini
        shell: bash
        run: |
          set -euo pipefail
          PT_VERSION=35.0.2
          PT_OS="${{ matrix.platform_tools }}"
          # adb.exe di Windows memuat AdbWinApi.dll secara statis, jadi DLL itu
          # harus ikut diekstrak berdampingan dengan adb.exe.
          FILES=(adb)
          if [ "${{ matrix.goos }}" = "windows" ]; then
            FILES=(adb.exe AdbWinApi.dll AdbWinUsbApi.dll)
          fi
          DEST="internal/bundle/bin/${{ matrix.goos }}-${{ matrix.goarch }}"
          mkdir -p "$DEST"
          curl -fsSL -o pt.zip \
            "https://dl.google.com/android/repository/platform-tools_r${PT_VERSION}-${PT_OS}.zip"
          unzip -q -o pt.zip
          for f in "${FILES[@]}"; do cp "platform-tools/$f" "$DEST/$f"; done
          chmod +x "$DEST/adb" 2>/dev/null || true
          chmod +x "$DEST/adb.exe" 2>/dev/null || true
          ls -l "$DEST"

      - name: Jalankan pengujian
        shell: bash
        run: go test ./...

      - name: Bangun biner
        shell: bash
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
          CGO_ENABLED: "0"
        run: |
          set -euo pipefail
          NAME=adbapp
          if [ "$GOOS" = "windows" ]; then NAME=adbapp.exe; fi
          go build -trimpath -ldflags "-s -w" -o "dist/$GOOS-$GOARCH/$NAME" .
          ls -l "dist/$GOOS-$GOARCH"

      - name: Bungkus arsip
        shell: bash
        run: |
          set -euo pipefail
          cd "dist/${{ matrix.goos }}-${{ matrix.goarch }}"
          BASE="adbapp-${{ matrix.goos }}-${{ matrix.goarch }}"
          if [ "${{ matrix.archive }}" = "zip" ]; then
            (command -v zip >/dev/null && zip -q "../$BASE.zip" *) || powershell -Command "Compress-Archive -Path * -DestinationPath ../$BASE.zip -Force"
          else
            tar czf "../$BASE.tar.gz" *
          fi
          ls -l ..

      - name: Unggah ke halaman rilis
        if: startsWith(github.ref, 'refs/tags/')
        uses: softprops/action-gh-release@v2
        with:
          files: |
            dist/*.zip
            dist/*.tar.gz
```

- [ ] **Step 5: Periksa sintaks workflow**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml'))" && echo OK`
Expected: `OK`.

- [ ] **Step 6: Commit**

```bash
git add Makefile .github/workflows/release.yml
git commit -m "build: Makefile fetch-adb dan rilis otomatis tiga OS"
```

---

## Task 15: Dokumentasi pemakaian

**Files:**
- Create: `README.md`

- [ ] **Step 1: Tulis README**

Create `README.md`:

```markdown
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
```

- [ ] **Step 2: Periksa seluruh pengujian dan build sekali lagi**

Run: `make test && make build && ./adbapp -h`
Expected: tes lulus, build sukses, dan aplikasi mencetak daftar flag
(`-port`, `-no-open`, `-data`).

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: README pemakaian, pemasangan, dan pemecahan masalah"
```

---

## Task 16: Publikasi ke GitHub `herlangga72`

**Files:**
- Modify: `README.md` (tautan rilis memakai akun nyata, sudah diselaraskan)

Repo tujuan: `github.com/herlangga72/adbapp`. Mesin ini sudah menyiapkan `gh`
yang login sebagai `herlangga72` dengan protokol SSH dan scope `repo`, jadi repo
bisa dibuat dan di-push langsung. Workflow dari Task 14 sudah mengunggah aset ke
halaman Releases, jadi tugas ini hanya menyalakan dan memverifikasinya.

Repo dibuat **publik**, karena rancangan pemasangan yang disetujui memakai satu
baris `curl` tanpa autentikasi; repo privat akan membuat tautan itu gagal dipakai
orang lain. Bisa diubah kapan saja lewat
`gh repo edit herlangga72/adbapp --visibility private` (dengan konsekuensi tautan
`curl` perlu autentikasi).

- [ ] **Step 1: Pastikan identitas rilis sudah benar**

Run: `head -1 go.mod` dan `grep -c "herlangga72/adbapp" README.md`
Expected: `module github.com/herlangga72/adbapp`, dan README memuat 2 tautan
`github.com/herlangga72/adbapp`.

- [ ] **Step 2: Repo GitHub dan kebijakan push berkelanjutan**

Repo sudah dibuat (2026-10-01): `https://github.com/herlangga72/adbapp`, publik,
branch default `master`, remote `origin` menunjuk ke
`git@github.com:herlangga72/adbapp.git`. Sejak itu, **setiap langkah kerja
di-push** supaya progres selalu terlihat di GitHub:

```bash
git push origin feat/adbapp     # setelah setiap task selesai
```

Di akhir seluruh task, gabungkan ke branch utama:

```bash
git checkout master
git merge --no-ff feat/adbapp -m "feat: adbapp v1.0.0 (pasang dan copot aplikasi Android lewat adb)"
git push origin master
```

Expected: `master` memuat seluruh riwayat, dan `origin` tetap menunjuk ke
`git@github.com:herlangga72/adbapp.git`.

- [ ] **Step 3: Jalankan build sekali tanpa tag untuk memastikan matriksnya hijau**

```bash
gh workflow list
gh workflow run build-and-release.yml
gh run list --workflow=build-and-release.yml --limit 1
```

Expected: workflow `build-and-release` terdaftar dan satu run berjalan. Tanpa tag,
langkah unggah ke halaman rilis dilewati (memang begitu rancangannya).

- [ ] **Step 4: Terbitkan versi pertama**

```bash
git tag -a v1.0.0 -m "adbapp v1.0.0"
git push origin v1.0.0
gh run list --workflow=build-and-release.yml --limit 1
```

Expected: satu run baru dipicu oleh tag, keempat kombinasi OS/arsitektur berhasil,
dan langkah "Unggah ke halaman rilis" mengunggah berkas arsip.

- [ ] **Step 5: Verifikasi aset rilis**

Run: `gh release view v1.0.0 --json assets -q '.assets[].name'`
Expected: `adbapp-darwin-amd64.tar.gz`, `adbapp-darwin-arm64.tar.gz`,
`adbapp-linux-amd64.tar.gz`, dan `adbapp-windows-amd64.zip`.

- [ ] **Step 6: Verifikasi tautan installer persis seperti di README**

Run:
`curl -sIL -o /dev/null -w '%{http_code}\n' https://github.com/herlangga72/adbapp/releases/latest/download/adbapp-linux-amd64.tar.gz`
Expected: `200`.

- [ ] **Step 7: Commit penyelarasan tautan (bila ada perubahan tertunda)**

```bash
git add README.md docs/superpowers/plans/2026-09-30-adb-app-manager.md docs/superpowers/specs/2026-09-30-adb-app-manager-design.md
git commit -m "docs: arahkan tautan rilis ke github.com/herlangga72/adbapp"
git push origin master
```

---

## Pemeriksaan akhir sebelum rilis

- [ ] **Jalankan seluruh tes di tiga OS.** `go test ./...` pada Linux, macOS,
      dan Windows (GitHub Actions melakukannya otomatis pada matriks).
- [ ] **Uji dengan perangkat sungguhan** sesuai Bagian 9 spec: pasang satu APK
      contoh, pastikan muncul di daftar terpasang, tarik APK-nya, lalu copot.
- [ ] **Uji cabut kabel di tengah instalasi.** Job harus berstatus gagal dengan
      pesan "perangkat terputus", dan job berikutnya tetap menunggu, bukan
      dianggap sukses.
- [ ] **Uji blokir host asing.** `curl -H 'Host: evil.example.com' http://127.0.0.1:<port>/api/state`
      harus menjawab 403.
- [ ] **Uji ekspor.** Unduh CSV dan JSON dari halaman Riwayat dan pastikan
      isinya sesuai aksi yang baru dijalankan.

---

## Catatan penyimpangan dari spec

1. **"adb palsu" berupa skrip** diganti dengan `Execer` yang bisa disuntik.
   Alasan: skrip shell tidak jalan di runner Windows saat CI. Cakupan
   pengujiannya setara, dan seluruh logika parsing diuji dengan keluaran adb
   yang realistis.
2. **Ukuran dan tanggal terpasang aplikasi** diambil lewat `dumpsys` + `du`
   per aplikasi saat detail dibuka, supaya daftar aplikasi tetap cepat. Daftar
   utama menampilkan nama paket, kode versi, dan penanda aplikasi sistem.
3. **`--show-versioncode`** tidak didukung perangkat lama; aplikasi otomatis
   jatuh ke perintah tanpa flag itu.

---

## Self-review

**Cakupan spec:**

| Butir spec | Task |
|---|---|
| Deteksi perangkat + status | Task 5, Task 7, Task 13 |
| Drag & drop, folder, URL | Task 11 (`handleUpload`, `handleListAPKs`, `handleFromURL`), Task 12 |
| Metadata APK sebelum pasang | Task 8, Task 11 (`entryFor`) |
| Antrean install berurutan + progress + batalkan | Task 10, Task 11, Task 12 |
| Daftar aplikasi terpasang, cari, filter | Task 5, Task 11, Task 12 |
| Copot, copot simpan data, hapus data, tarik APK | Task 6, Task 10, Task 11, Task 12 |
| Detail paket (versi, ukuran, izin) | Task 5, Task 11, Task 12 |
| Bulk uninstall + konfirmasi | Task 11, Task 12 |
| Riwayat permanen + ekspor CSV/JSON | Task 9, Task 11, Task 12 (termasuk pembatalan `queued` lewat `onDone`) |
| Pesan error yang jelas | Task 4, Task 6, Task 12 (panel petunjuk) |
| Satu perangkat aktif + pemilih bila lebih | Task 7 (`Others`, `Select`), Task 11 (`/api/device/select`), Task 12 (pemilih) |
| Paket per OS + adb tertanam + CI | Task 2, Task 14 |
| Pemasangan `curl` di macOS | Task 14 (arsip), Task 15 (README) |
| Pengujian tanpa perangkat | Task 3-11 |
| Checklist pengujian dengan perangkat | "Pemeriksaan akhir sebelum rilis" |

**Konsistensi tipe dan nama:**

- `adbx.InstallOptions{Replace, AllowDowngrade, GrantAll}`, `adbx.StageFunc`
  dipakai konsisten di Task 3, 6, 10, 11, 13.
- `queue.Job` dan `queue.Status*`/`queue.Kind*` konsisten antara Task 10, 11, 13;
  `Cancel` job `queued` juga memanggil `onDone` agar riwayat lengkap.
- `device.Status{State, Serial, Model, AndroidVersion, Others}` dan
  `Monitor.Select(serial)` konsisten di Task 7, 11, 12, 13.
- `store.Entry{Time, Action, Package, Device, Success, Detail}` konsisten di
  Task 9, 11, 13.
- `apkmeta.Meta{Package, VersionName, Label, VersionCode, MinSDK}` dan
  `ReadCached(path, size, modUnixNano)` konsisten di Task 8 dan 11; `minSdk`
  disalurkan ke `httpapi.apkEntry` dan kolom tab Pasang.
- `httpapi.DeviceSource` memuat `Current`, `Refresh`, dan `Select`, cocok dengan
  `device.Monitor` di Task 7 dan 13.
- `paths.Paths` konsisten di Task 1, 11, 13.
- Nama SSE: `event: state` dan `event: job` cocok antara Task 11 (server) dan
  Task 12 (client).

**Tidak ada placeholder:** setiap langkah kode memuat kode lengkap, perintah
yang bisa dijalankan, dan hasil yang diharapkan.
