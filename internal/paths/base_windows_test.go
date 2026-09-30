//go:build windows

package paths

import "testing"

func TestBaseDirUsesLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\tester\AppData\Local`)
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	if got != `C:\Users\tester\AppData\Local` {
		t.Fatalf("got %q, want LOCALAPPDATA", got)
	}
}

func TestBaseDirFallsBackWhenLocalAppDataEmpty(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	got, err := baseDir()
	if err != nil {
		t.Fatalf("baseDir error: %v", err)
	}
	if got == "" {
		t.Fatal("baseDir mengembalikan path kosong")
	}
}
