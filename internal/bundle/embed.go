// Package bundle menanam biner adb ke dalam biner aplikasi dan mengekstraknya
// ke folder data saat aplikasi dijalankan.
package bundle

import "embed"

// Version dinaikkan setiap kali biner adb di folder bin/ diperbarui, supaya
// aplikasi tahu kapan harus mengekstrak ulang.
const Version = "1"

//go:embed bin
var binFS embed.FS
