package bundle

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
)

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
