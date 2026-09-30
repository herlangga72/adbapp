//go:build darwin

package paths

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBaseDirDarwin(t *testing.T) {
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	want := filepath.Join("Library", "Application Support")
	if !strings.HasSuffix(got, want) {
		t.Fatalf("got %q, want suffix %q", got, want)
	}
}
