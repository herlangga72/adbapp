package adbx

import (
	"errors"
	"strings"
)

// ErrDeviceNotFound dikembalikan bila adb melaporkan perangkat tidak ada atau
// sedang offline. Sentinel lain serta daftar pola lengkap ditambahkan pada Task 4.
var ErrDeviceNotFound = errors.New("perangkat tidak ditemukan")

// Classify menerjemahkan keluaran adb yang gagal menjadi error yang jelas.
// Daftar lengkap pola ditambahkan pada Task 4.
func Classify(res Result) error {
	lower := strings.ToLower(res.Stderr + "\n" + res.Stdout)
	switch {
	case strings.Contains(lower, "device not found"),
		strings.Contains(lower, "device offline"),
		strings.Contains(lower, "no devices/emulators found"):
		return ErrDeviceNotFound
	default:
		return &CommandError{Result: res}
	}
}

// CommandError adalah kegagalan adb yang belum dikenali polanya.
type CommandError struct {
	Result Result
}

func (e *CommandError) Error() string {
	msg := firstLine(e.Result.Stderr)
	if msg == "" {
		msg = firstLine(e.Result.Stdout)
	}
	if msg == "" {
		msg = "perintah adb gagal"
	}
	return msg
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}
