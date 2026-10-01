package apkmeta

import (
	"os"
	"path/filepath"
	"testing"
)

// fixtureAPK membangun APK minimal: arsip zip berisi AndroidManifest.xml
// biner. Berkas dibuat sekali di TestMain agar tidak perlu biner besar
// di dalam repo.
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
