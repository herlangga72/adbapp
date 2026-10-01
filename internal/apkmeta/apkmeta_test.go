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
