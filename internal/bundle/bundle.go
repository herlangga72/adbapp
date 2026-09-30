package bundle

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

func ensureFrom(fsys fs.FS, adbDir, goos, goarch string) (string, error) {
	name := adbName(goos)
	src := fmt.Sprintf("bin/%s-%s/%s", goos, goarch, name)
	if _, err := fs.Stat(fsys, src); err != nil {
		return "", fmt.Errorf(
			"biner adb untuk %s-%s belum dibundel; jalankan `make fetch-adb` sebelum build",
			goos, goarch)
	}

	dst := filepath.Join(adbDir, name)
	stamp := filepath.Join(adbDir, ".version")
	want := Version + ":" + goos + "-" + goarch

	if got, err := os.ReadFile(stamp); err == nil && string(got) == want {
		if _, err := os.Stat(dst); err == nil {
			return dst, nil
		}
	}

	if err := os.MkdirAll(adbDir, 0o755); err != nil {
		return "", err
	}
	if err := copyFile(fsys, src, dst); err != nil {
		return "", err
	}
	if err := os.WriteFile(stamp, []byte(want), 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

func copyFile(fsys fs.FS, src, dst string) error {
	in, err := fsys.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
