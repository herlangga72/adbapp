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

func swapRename(t *testing.T, fn func(oldpath, newpath string) error) {
	t.Helper()
	orig := renameFile
	renameFile = fn
	t.Cleanup(func() { renameFile = orig })
}

func linkErr(oldpath, newpath string) error {
	return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: os.ErrPermission}
}

// TestCopyFileTwiceIdenticalDestination meniru dua ekstraksi berurutan ke
// tujuan yang sama dengan isi identik: salinan kedua harus sukses dan tidak
// meninggalkan berkas sementara.
func TestCopyFileTwiceIdenticalDestination(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("isi"), Mode: 0o644},
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "adb")
	if err := copyFile(fsys, "bin/linux-amd64/adb", dst); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(fsys, "bin/linux-amd64/adb", dst); err != nil {
		t.Fatalf("salinan kedua seharusnya sukses: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "isi" {
		t.Fatalf("isi tujuan salah: %q err=%v", data, err)
	}
	assertNoTemps(t, dir)
}

// TestMoveIntoPlaceToleratesIdenticalDestination memaksa rename selalu gagal
// (meniru Windows yang menolak rename ke atas berkas tujuan yang sudah ada) dan
// memastikan berkas tujuan identik dianggap berhasil tanpa mengganggu tujuan.
func TestMoveIntoPlaceToleratesIdenticalDestination(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("isi identik"), Mode: 0o644},
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "adb")
	if err := os.WriteFile(dst, []byte("isi identik"), 0o755); err != nil {
		t.Fatal(err)
	}
	swapRename(t, linkErr)
	if err := copyFile(fsys, "bin/linux-amd64/adb", dst); err != nil {
		t.Fatalf("copyFile seharusnya toleran terhadap tujuan identik: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "isi identik" {
		t.Fatalf("berkas tujuan berubah: %q err=%v", got, err)
	}
	assertNoTemps(t, dir)
}

// TestMoveIntoPlaceRejectsDifferentDestination memastikan toleransi isi tidak
// melemahkan pengecekan: tujuan yang isinya berbeda harus tetap dipindahkan.
func TestMoveIntoPlaceRejectsDifferentDestination(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("baru"), Mode: 0o644},
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "adb")
	if err := os.WriteFile(dst, []byte("lama"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(fsys, "bin/linux-amd64/adb", dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "baru" {
		t.Fatalf("tujuan seharusnya ditimpa dengan isi baru: %q err=%v", got, err)
	}
	assertNoTemps(t, dir)
}

// TestCopyFileRetriesTransientRenameFailure menyuntikkan dua kegagalan rename
// lalu sukses, dan memastikan pemindahan dicoba ulang sampai berhasil.
func TestCopyFileRetriesTransientRenameFailure(t *testing.T) {
	fsys := fstest.MapFS{
		"bin/linux-amd64/adb": &fstest.MapFile{Data: []byte("isi"), Mode: 0o644},
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "adb")
	var mu sync.Mutex
	calls := 0
	swapRename(t, func(oldpath, newpath string) error {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n <= 2 {
			return linkErr(oldpath, newpath)
		}
		return os.Rename(oldpath, newpath)
	})
	if err := copyFile(fsys, "bin/linux-amd64/adb", dst); err != nil {
		t.Fatalf("copyFile seharusnya berhasil setelah percobaan ulang: %v", err)
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	if n < 3 {
		t.Fatalf("rename seharusnya dicoba ulang, calls=%d", n)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "isi" {
		t.Fatalf("isi tujuan salah: %q err=%v", data, err)
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
