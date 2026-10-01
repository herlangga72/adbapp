package adbx

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestInstallFlagsAndStageSequence(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "Success\n"}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	var stages []int
	err := r.Install(context.Background(), "/tmp/app.apk",
		InstallOptions{Replace: true, AllowDowngrade: true, GrantAll: true},
		func(percent int, msg string) { stages = append(stages, percent) })
	if err != nil {
		t.Fatalf("install gagal: %v", err)
	}
	joined := strings.Join(fe.calls[0], " ")
	for _, flag := range []string{"-r", "-d", "-g"} {
		if !strings.Contains(joined, flag) {
			t.Fatalf("flag %s tidak ada: %s", flag, joined)
		}
	}
	want := []int{5, 20, 100}
	if len(stages) != len(want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("stages = %v, want %v", stages, want)
		}
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

// TestInstallExitZeroFailureClassified memastikan "Failure [..]" dengan exit 0
// tetap diklasifikasikan sebagai kegagalan.
func TestInstallExitZeroFailureClassified(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "Failure [INSTALL_FAILED_ALREADY_EXISTS]\n"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("got %v, want ErrAlreadyExists", err)
	}
}

// TestInstallFalseSuccessFromPathNotSwallowed meniru adb asli yang menggemakan
// path APK di stderr: path yang memuat kata "Success" tidak boleh menyamarkan
// kegagalan.
func TestInstallFalseSuccessFromPathNotSwallowed(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{
			Stdout:   "Failure [INSTALL_PARSE_FAILED_NOT_APK]\n",
			Stderr:   "adb: failed to install /tmp/Success/app.apk: Failure [INSTALL_PARSE_FAILED_NOT_APK]\n",
			ExitCode: 1,
		},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if err == nil {
		t.Fatal("kegagalan palsu: install seharusnya error")
	}
	if !errors.Is(err, ErrInvalidApk) {
		t.Fatalf("got %v, want ErrInvalidApk", err)
	}
}

// TestResultOfRejectsSuccessSubstring memastikan helper bersama hanya menerima
// baris utuh "Success" dengan exit 0, bukan kemunculan kata di mana pun.
func TestResultOfRejectsSuccessSubstring(t *testing.T) {
	res := Result{Stdout: "Failure [...NOT_APK] /tmp/Success/app.apk\n", ExitCode: 0}
	if err := resultOf(res); err == nil {
		t.Fatal("path yang memuat \"Success\" tidak boleh dianggap sukses")
	}
	if err := resultOf(Result{Stdout: "Success\n", ExitCode: 0}); err != nil {
		t.Fatalf("baris utuh Success harus sukses, dapat %v", err)
	}
}

func TestInstallHonoursRunnerTimeout(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(blockingExec{}), WithSerial("S1"),
		WithTimeout(50*time.Millisecond))
	start := time.Now()
	err := r.Install(context.Background(), "/tmp/app.apk", InstallOptions{}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("install tidak menghormati timeout: %v", elapsed)
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

// TestUninstallExitZeroFailureIsError membunuh mutan `if res.ExitCode == 0 {
// return nil }`: "Failure [..]" dengan exit 0 harus tetap error.
func TestUninstallExitZeroFailureIsError(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "Failure [DELETE_FAILED_INTERNAL_ERROR]\n"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	err := r.Uninstall(context.Background(), "com.foo", false)
	if !errors.Is(err, ErrSystemApp) {
		t.Fatalf("got %v, want ErrSystemApp", err)
	}
}

func TestUninstallHonoursRunnerTimeout(t *testing.T) {
	r := New("/usr/bin/adb", WithExecer(blockingExec{}), WithSerial("S1"),
		WithTimeout(50*time.Millisecond))
	start := time.Now()
	err := r.Uninstall(context.Background(), "com.foo", false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("uninstall tidak menghormati timeout: %v", elapsed)
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
	err := r.ClearData(context.Background(), "com.foo")
	if err == nil {
		t.Fatal("seharusnya error")
	}
	// Pesan harus kontekstual, bukan sekadar "Failed".
	if !strings.Contains(err.Error(), "com.foo") {
		t.Fatalf("error harus menyebut nama paket: %v", err)
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

// TestPullApkSanitizesPackageName memastikan nama paket bermusuhan tidak bisa
// membawa berkas keluar dari direktori tujuan.
func TestPullApkSanitizesPackageName(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "package:/data/app/base.apk\n"},
		{Stdout: "1 file pulled\n"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	dest := t.TempDir()
	got, err := r.PullApk(context.Background(), `../../etc/passwd`, dest)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Clean(got)) != dest {
		t.Fatalf("berkas keluar dari destDir: %q (dest %q)", got, dest)
	}
	rel, err := filepath.Rel(dest, got)
	if err != nil || rel == ".." || strings.ContainsRune(rel, filepath.Separator) {
		t.Fatalf("path hasil tidak aman: %q", got)
	}
}
