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
