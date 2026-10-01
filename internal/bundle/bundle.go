package bundle

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"
)

// renameFile adalah pembungkus tipis di atas os.Rename supaya tes dapat
// menyuntikkan kegagalan rename sementara. Jangan diubah saat runtime.
var renameFile = os.Rename

const (
	renameAttempts = 5
	renameBackoff  = 50 * time.Millisecond
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
	if _, err := fs.Stat(fsys, src); err != nil {
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

	if err := moveIntoPlace(tmpName, dst); err != nil {
		return err
	}
	return nil
}

// moveIntoPlace memindahkan berkas sementara tmpName menjadi dst. Di Windows,
// os.Rename ke atas berkas tujuan yang SUDAH ada dapat gagal dengan
// "Access is denied" ketika ada proses lain (goroutine ekstraksi paralel,
// antivirus, atau pengindeks) memegang handle sesaat. Karena itu rename dicoba
// beberapa kali, berkas tujuan dihapus lebih dulu sebagai upaya antara, dan
// jika ekstraksi lain sudah menghasilkan berkas tujuan yang identik, pemindahan
// dianggap selesai tanpa mengganggu berkas yang sudah baik.
func moveIntoPlace(tmpName, dst string) error {
	var lastErr error
	for attempt := 0; attempt < renameAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(renameBackoff * time.Duration(attempt))
		}
		if err := renameFile(tmpName, dst); err == nil {
			return nil
		} else {
			lastErr = err
		}
		// Berkas tujuan sudah ditulis oleh ekstraksi lain yang berjalan
		// bersamaan dengan isi identik; jangan ganggu, cukup buang tmp.
		// (defer os.Remove(tmpName) di pemanggil yang membersihkannya.)
		if sameContent(tmpName, dst) {
			return nil
		}
		// Windows tidak bisa rename di atas berkas tujuan yang terkunci;
		// hapus dulu supaya percobaan berikutnya bisa berhasil.
		os.Remove(dst)
	}
	return fmt.Errorf("memindahkan %s ke %s: %w", tmpName, dst, lastErr)
}

// sameContent melaporkan apakah berkas a dan b adalah berkas biasa berukuran
// sama dengan isi (hash SHA-256) yang sama.
func sameContent(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil || !sa.Mode().IsRegular() {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil || !sb.Mode().IsRegular() || sa.Size() != sb.Size() {
		return false
	}
	ha, err := hashFile(a)
	if err != nil {
		return false
	}
	hb, err := hashFile(b)
	if err != nil {
		return false
	}
	return string(ha) == string(hb)
}

func hashFile(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
