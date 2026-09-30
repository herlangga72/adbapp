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
}
