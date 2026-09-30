package adbx

import "errors"
import "os/exec"

// asExitError memisahkan pengecekan tipe ini agar mudah dibaca di adbx.go.
func asExitError(err error, target **exec.ExitError) bool {
	return errors.As(err, target)
}
