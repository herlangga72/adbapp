package paths

import (
	"path/filepath"
	"runtime"
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

func TestBaseDirUsesXDGOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("khusus Linux")
	}
	t.Setenv("XDG_DATA_HOME", "/xdg")
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	if got != "/xdg" {
		t.Fatalf("got %q, want /xdg", got)
	}
}
