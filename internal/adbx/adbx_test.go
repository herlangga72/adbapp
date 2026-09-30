package adbx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"
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

// blockingExec menunggu konteks selesai lalu mengembalikan ctx.Err(), meniru
// proses yang dibunuh saat konteks dibatalkan.
type blockingExec struct{}

func (blockingExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	<-ctx.Done()
	return Result{}, ctx.Err()
}

func TestRunPropagatesContextCancellation(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(blockingExec{}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Run(ctx, "shell", "id")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

type deadlineExec struct{}

func (deadlineExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return Result{}, context.DeadlineExceeded
}

func TestRunPropagatesDeadlineExceeded(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(deadlineExec{}))
	_, err := r.Run(context.Background(), "shell", "id")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
}

func TestRunClassifiesNonZeroExit(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stderr: "error: device offline", ExitCode: 1}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	_, err := r.Run(context.Background(), "shell", "id")
	if !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("got %v, want ErrDeviceNotFound", err)
	}
}

type errorExec struct{ err error }

func (e *errorExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return Result{}, e.err
}

func TestRunWrapsNonExitError(t *testing.T) {
	perr := &fs.PathError{Op: "fork/exec", Path: "/usr/bin/adb", Err: errors.New("no such file")}
	r := New("/usr/bin/adb", WithExecer(&errorExec{err: perr}))
	_, err := r.Run(context.Background(), "devices")
	if err == nil {
		t.Fatal("ingin error, dapat nil")
	}
	if !strings.Contains(err.Error(), "/usr/bin/adb") {
		t.Fatalf("error harus menyebut path adb: %v", err)
	}
	var got *fs.PathError
	if !errors.As(err, &got) {
		t.Fatalf("error harus membungkus PathError: %v", err)
	}
}

func TestOutputTrimsWhitespace(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: " hello\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.Output(context.Background(), "shell", "echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("got %q want %q", got, "hello")
	}
}

func TestWithTimeoutZeroOrNegativeMeansNoTimeout(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "ok"}, {Stdout: "ok"}}}
	for _, d := range []time.Duration{0, -time.Second} {
		r := New("/usr/bin/adb", WithExecer(fe), WithTimeout(d))
		if _, err := r.Run(context.Background(), "devices"); err != nil {
			t.Fatalf("timeout %v: %v", d, err)
		}
	}
}

func TestWithExecerNilKeepsDefault(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(nil))
	if r.exec == nil {
		t.Fatal("exec tidak boleh nil")
	}
}

// safeExec aman dipakai bersamaan dan merekam seluruh panggilan.
type safeExec struct {
	mu    sync.Mutex
	calls [][]string
}

func (s *safeExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	s.mu.Lock()
	s.calls = append(s.calls, append([]string{name}, args...))
	s.mu.Unlock()
	return Result{Stdout: "ok"}, nil
}

func TestRunConcurrentSameRunner(t *testing.T) {
	const n = 8
	se := &safeExec{}
	r := New("/usr/bin/adb", WithExecer(se), WithSerial("SER"))

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			arg := fmt.Sprintf("arg-%d", i)
			if _, err := r.Run(context.Background(), "shell", arg); err != nil {
				t.Errorf("goroutine %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	se.mu.Lock()
	defer se.mu.Unlock()
	if len(se.calls) != n {
		t.Fatalf("harus %d panggilan, dapat %d", n, len(se.calls))
	}
	wantPrefix := []string{"/usr/bin/adb", "-s", "SER", "shell"}
	seen := make(map[string]bool, n)
	for _, c := range se.calls {
		if len(c) != 5 {
			t.Fatalf("jumlah args tak terduga: %v", c)
		}
		for i := range wantPrefix {
			if c[i] != wantPrefix[i] {
				t.Fatalf("prefix args salah: %v", c)
			}
		}
		seen[c[4]] = true
	}
	for i := 0; i < n; i++ {
		if !seen[fmt.Sprintf("arg-%d", i)] {
			t.Fatalf("arg-%d hilang (cross-talk): %v", i, se.calls)
		}
	}
}
