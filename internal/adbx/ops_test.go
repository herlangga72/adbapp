package adbx

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSuccessReportsStages(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Performing Streamed Install\nSuccess\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	var stages []int
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{Replace: true},
		func(percent int, msg string) { stages = append(stages, percent) })
	if err != nil {
		t.Fatalf("install gagal: %v", err)
	}
	if len(stages) < 2 {
		t.Fatalf("stages terlalu sedikit: %v", stages)
	}
	joined := strings.Join(fe.calls[0], " ")
	if !strings.Contains(joined, "-r") || !strings.Contains(joined, "/tmp/app.apk") {
		t.Fatalf("argumen install salah: %s", joined)
	}
}

func TestInstallFailureUsesClassify(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Failure [INSTALL_FAILED_INSUFFICIENT_STORAGE]\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if !errors.Is(err, ErrInsufficientStorage) {
		t.Fatalf("got %v, want ErrInsufficientStorage", err)
	}
}

func TestInstallNonZeroExitIsClassified(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stderr: "adb: device offline", ExitCode: 1},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("got %v, want ErrDeviceNotFound", err)
	}
}

func TestUninstallKeepDataAddsFlag(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Success\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	if err := r.Uninstall(context.Background(), "com.foo", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fe.calls[0], " "), "-k") {
		t.Fatalf("flag -k tidak ada: %v", fe.calls[0])
	}
}

func TestUninstallFailureIsClassified(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "Failure [DELETE_FAILED_INTERNAL_ERROR]\n", ExitCode: 1},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Uninstall(context.Background(), "com.foo", false)
	if !errors.Is(err, ErrSystemApp) {
		t.Fatalf("got %v, want ErrSystemApp", err)
	}
}

func TestClearData(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Success\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	if err := r.ClearData(context.Background(), "com.foo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fe.calls[0], " "), "pm clear com.foo") {
		t.Fatalf("perintah salah: %v", fe.calls[0])
	}
}

func TestClearDataFailure(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Failed\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	if err := r.ClearData(context.Background(), "com.foo"); err == nil {
		t.Fatal("seharusnya error")
	}
}

func TestPullApkUsesPathFromDevice(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "package:/data/app/~~Ab==/com.foo-x==/base.apk\n"},
		{Stdout: "1 file pulled\n"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	dest := t.TempDir()
	got, err := r.PullApk(context.Background(), "com.foo", dest)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dest, "com.foo-base.apk")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	pull := strings.Join(fe.calls[1], " ")
	if !strings.Contains(pull, "/data/app/~~Ab==/com.foo-x==/base.apk") {
		t.Fatalf("perintah pull salah: %s", pull)
	}
}

func TestPullApkFailsWhenPackageMissing(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	if _, err := r.PullApk(context.Background(), "com.foo", t.TempDir()); !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("got %v, want ErrPackageNotFound", err)
	}
}
