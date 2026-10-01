package paths

import (
	"os"
	"path/filepath"
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

func TestResolveUsesAdbappDirectory(t *testing.T) {
	p, err := Resolve()
	if err != nil {
		t.Skipf("Resolve error: %v", err)
	}
	if got := filepath.Base(p.DataDir); got != "adbapp" {
		t.Fatalf("got %q, want adbapp", got)
	}
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
