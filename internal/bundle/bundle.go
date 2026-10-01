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
