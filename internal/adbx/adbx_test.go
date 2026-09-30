package adbx

import (
	"context"
	"testing"
)

type fakeExec struct {
	results []Result
	calls   [][]string
}

func (f *fakeExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if len(f.results) == 0 {
		return Result{}, nil
	}
	r := f.results[0]
	f.results = f.results[1:]
	return r, nil
}

func TestRunPrependsSerial(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "ok"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("ABC123"))
	if _, err := r.Run(context.Background(), "shell", "id"); err != nil {
		t.Fatal(err)
	}
	got := fe.calls[0]
	want := []string{"/usr/bin/adb", "-s", "ABC123", "shell", "id"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestRunWithoutSerialOmitsFlag(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "ok"}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	if _, err := r.Run(context.Background(), "devices"); err != nil {
		t.Fatal(err)
	}
	if fe.calls[0][1] != "devices" {
		t.Fatalf("flag -s seharusnya tidak ada: %v", fe.calls[0])
	}
}
