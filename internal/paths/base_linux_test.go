//go:build linux

package paths

import "testing"

func TestBaseDirUsesXDGOnLinux(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg")
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	if got != "/xdg" {
		t.Fatalf("got %q, want /xdg", got)
	}
}

func TestBaseDirIgnoresRelativeXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative/data")
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	if got == "relative/data" {
		t.Fatalf("baseDir memakai XDG relatif: %q", got)
	}
}
