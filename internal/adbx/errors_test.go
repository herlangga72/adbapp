package adbx

import (
	"errors"
	"testing"
)

func TestClassifyKnownFailures(t *testing.T) {
	cases := []struct {
		name    string
		res     Result
		wantErr error
	}{
		{
			name:    "device not found",
			res:     Result{Stderr: "error: device 'ABC' not found", ExitCode: 1},
			wantErr: ErrDeviceNotFound,
		},
		{
			name:    "unauthorized",
			res:     Result{Stderr: "error: device unauthorized", ExitCode: 1},
			wantErr: ErrUnauthorized,
		},
		{
			name:    "no space",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_INSUFFICIENT_STORAGE]", ExitCode: 1},
			wantErr: ErrInsufficientStorage,
		},
		{
			name:    "already exists",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_ALREADY_EXISTS]", ExitCode: 1},
			wantErr: ErrAlreadyExists,
		},
		{
			name:    "downgrade",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_VERSION_DOWNGRADE]", ExitCode: 1},
			wantErr: ErrDowngrade,
		},
		{
			name:    "signature mismatch",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE: Package com.foo signatures do not match previously installed version]", ExitCode: 1},
			wantErr: ErrSignatureMismatch,
		},
		{
			name:    "invalid apk",
			res:     Result{Stderr: "Failure [INSTALL_PARSE_FAILED_NOT_APK]", ExitCode: 1},
			wantErr: ErrInvalidApk,
		},
		{
			name:    "older sdk",
			res:     Result{Stderr: "Failure [INSTALL_FAILED_OLDER_SDK]", ExitCode: 1},
			wantErr: ErrNeedsNewerAndroid,
		},
		{
			name:    "shell permission denied",
			res:     Result{Stderr: "shell: permission denied", ExitCode: 1},
			wantErr: ErrShellPermission,
		},
		{
			name:    "package not found",
			res:     Result{Stderr: "Failure [DELETE_FAILED_INTERNAL_ERROR]\nFailure [not installed for 0]", ExitCode: 1},
			wantErr: ErrPackageNotFound,
		},
		{
			name:    "system app",
			res:     Result{Stderr: "Failure [DELETE_FAILED_INTERNAL_ERROR]", ExitCode: 1},
			wantErr: ErrSystemApp,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Classify(tc.res)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestClassifyUnknownFallsBackToCommandError(t *testing.T) {
	err := Classify(Result{Stderr: "sesuatu yang aneh", ExitCode: 1})
	var ce *CommandError
	if !errors.As(err, &ce) {
		t.Fatalf("seharusnya CommandError, dapat %T", err)
	}
	if got, want := err.Error(), "perintah adb gagal: sesuatu yang aneh"; got != want {
		t.Fatalf("pesan salah: got %q, want %q", got, want)
	}
}

func TestClassifyNewSentinelMessages(t *testing.T) {
	cases := []struct {
		name string
		res  Result
		want error
		msg  string
	}{
		{
			name: "signature mismatch",
			res:  Result{Stderr: "Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE]"},
			want: ErrSignatureMismatch,
			msg:  "aplikasi dengan nama paket sama sudah terpasang dengan tanda tangan berbeda; copot dulu yang lama",
		},
		{
			name: "invalid apk",
			res:  Result{Stderr: "Failure [INSTALL_PARSE_FAILED_NOT_APK]"},
			want: ErrInvalidApk,
			msg:  "berkas APK tidak sah atau tidak ditandatangani",
		},
		{
			name: "older sdk",
			res:  Result{Stderr: "Failure [INSTALL_FAILED_OLDER_SDK]"},
			want: ErrNeedsNewerAndroid,
			msg:  "APK ini butuh versi Android yang lebih baru",
		},
		{
			name: "shell permission",
			res:  Result{Stderr: "Security exception: insufficient permissions"},
			want: ErrShellPermission,
			msg:  "perangkat menolak perintah ini (izin shell kurang)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Classify(tc.res)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if got := err.Error(); got != tc.msg {
				t.Fatalf("pesan salah: got %q, want %q", got, tc.msg)
			}
		})
	}
}

func TestClassifyDoesNotBridgeLines(t *testing.T) {
	// "not found" tanpa penanda "device" bukan device-not-found.
	err := Classify(Result{Stderr: "package com.foo not found", ExitCode: 1})
	if errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("tidak boleh ErrDeviceNotFound: %v", err)
	}
	var ce *CommandError
	if !errors.As(err, &ce) {
		t.Fatalf("seharusnya CommandError, dapat %T", err)
	}

	// "device ..." di satu baris tidak boleh dijembatani dengan "not found" di
	// baris lain.
	err = Classify(Result{
		Stderr:   "Failure [INSTALL_FAILED_INSUFFICIENT_STORAGE]",
		Stdout:   "device status: ok\nremote object not found",
		ExitCode: 1,
	})
	if !errors.Is(err, ErrInsufficientStorage) {
		t.Fatalf("got %v, want ErrInsufficientStorage", err)
	}
}
