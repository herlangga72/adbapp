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
	ErrSignatureMismatch   = errors.New("aplikasi dengan nama paket sama sudah terpasang dengan tanda tangan berbeda; copot dulu yang lama")
	ErrInvalidApk          = errors.New("berkas APK tidak sah atau tidak ditandatangani")
	ErrNeedsNewerAndroid   = errors.New("APK ini butuh versi Android yang lebih baru")
	ErrShellPermission     = errors.New("perangkat menolak perintah ini (izin shell kurang)")
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
	case strings.Contains(lower, "update_incompatible"),
		strings.Contains(lower, "signatures do not match"):
		return ErrSignatureMismatch
	case strings.Contains(lower, "parse_failed"),
		strings.Contains(lower, "invalid_apk"):
		return ErrInvalidApk
	case strings.Contains(lower, "older_sdk"),
		strings.Contains(lower, "requires newer sdk"):
		return ErrNeedsNewerAndroid
	case strings.Contains(lower, "device unauthorized"),
		strings.Contains(lower, "unauthorized"):
		return ErrUnauthorized
	case strings.Contains(lower, "insufficient permissions"),
		strings.Contains(lower, "permission denied"):
		return ErrShellPermission
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
		// Heuristik: DELETE_FAILED_INTERNAL_ERROR biasanya berarti paket
		// sistem tidak boleh dicopot, tetapi kadang hanya berarti paketnya
		// sudah tidak ada. Arm "not installed for" di atas lebih dulu,
		// sehingga kasus paket hilang tetap terklasifikasi benar bila kedua
		// penanda muncul bersamaan.
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
		msg = "tanpa keluaran"
	}
	return "perintah adb gagal: " + msg
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
// "device 'SERIAL' not found". strings.Contains(lower, "device not found")
// tidak cukup karena adb menyisipkan nomor seri di antara "device" dan
// "not found". Pemeriksaan dilakukan per baris supaya keluaran stderr dan
// stdout tidak saling menjembatani pola.
func looksLikeDeviceNotFound(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		i := strings.Index(line, "device")
		if i >= 0 && strings.Contains(line[i:], "not found") {
			return true
		}
	}
	return false
}
