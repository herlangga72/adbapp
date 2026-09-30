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
