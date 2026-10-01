package webui

import (
	"io/fs"
	"strings"
	"testing"
)

func TestFSContainsIndex(t *testing.T) {
	data, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("index.html tidak ada di dalam biner: %v", err)
	}
	if !strings.Contains(string(data), "adbapp") {
		t.Fatal("index.html tidak memuat penanda aplikasi")
	}
}

func TestFSContainsAssets(t *testing.T) {
	for _, name := range []string{"app.js", "styles.css"} {
		if _, err := fs.ReadFile(FS(), name); err != nil {
			t.Fatalf("%s tidak ada: %v", name, err)
		}
	}
}
