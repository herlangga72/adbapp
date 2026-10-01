// Package browser membuka URL di browser bawaan sistem.
package browser

import (
	"os/exec"
	"runtime"
)

// Open membuka url di browser. Kegagalan tidak fatal: pengguna masih bisa
// membuka alamatnya secara manual.
func Open(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
