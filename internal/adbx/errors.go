package adbx

// Classify menerjemahkan keluaran adb yang gagal menjadi error yang jelas.
// Daftar lengkap pola ditambahkan pada Task 4.
func Classify(res Result) error {
	return &CommandError{Result: res}
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
