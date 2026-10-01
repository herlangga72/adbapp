package store

import "os"

// appendRaw menambahkan teks mentah ke berkas riwayat, dipakai untuk
// mensimulasikan baris yang rusak.
func appendRaw(path, text string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return err
}
