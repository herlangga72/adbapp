package adbx

import (
	"errors"
	"strings"
)

var (
	ErrDeviceNotFound      = errors.New("perangkat tidak ditemukan")
	ErrUnauthorized        = errors.New("perangkat belum diizinkan")
	ErrInsufficientStorage = errors.New("penyimpanan perangkat penuh")
	ErrAlreadyExists       = errors.New("aplikasi sudah terpasang")
	ErrDowngrade           = errors.New("versi lebih rendah dari yang terpasang")
	ErrPackageNotFound     = errors.New("aplikasi tidak terpasang")
	ErrSystemApp           = errors.New("aplikasi sistem tidak boleh dicopot")
)

// Classify menerjemahkan keluaran adb yang gagal menjadi error yang bisa
// ditindaklanjuti. Urutan pemeriksaan penting: pola yang lebih spesifik lebih
// dulu.
func Classify(res Result) error {
	text := res.Stderr + "\n" + res.Stdout
	lower := strings.ToLower(text)

	switch {
	case strings.Contains(lower, "not installed for"):
		return ErrPackageNotFound
	case strings.Contains(lower, "device unauthorized"),
		strings.Contains(lower, "insufficient permissions"),
		strings.Contains(lower, "unauthorized"):
		return ErrUnauthorized
	case looksLikeDeviceNotFound(lower),
		strings.Contains(lower, "device offline"),
		strings.Contains(lower, "no devices/emulators found"):
		return ErrDeviceNotFound
	case strings.Contains(lower, "insufficient_storage"):
		return ErrInsufficientStorage
	case strings.Contains(lower, "already_exists"):
		return ErrAlreadyExists
	case strings.Contains(lower, "version_downgrade"):
		return ErrDowngrade
	case strings.Contains(lower, "delete_failed"):
		return ErrSystemApp
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
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

// looksLikeDeviceNotFound cocok untuk pola "device not found" maupun
// "device 'SERIAL' not found" (adb menyisipkan nomor seri).
func looksLikeDeviceNotFound(lower string) bool {
	i := strings.Index(lower, "device")
	if i < 0 {
		return false
	}
	return strings.Contains(lower[i:], "not found")
}
