package adbx

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// InstallOptions mengatur flag yang dikirim ke `adb install`.
type InstallOptions struct {
	Replace        bool // -r, timpa bila sudah terpasang
	AllowDowngrade bool // -d, izinkan versi lebih rendah
	GrantAll       bool // -g, berikan semua izin runtime
}

// StageFunc melaporkan kemajuan kasar sebuah operasi (0-100).
type StageFunc func(percent int, message string)

func report(fn StageFunc, percent int, message string) {
	if fn != nil {
		fn(percent, message)
	}
}

// Install memasang satu berkas APK.
func (r *Runner) Install(ctx context.Context, apkPath string, opts InstallOptions, stage StageFunc) error {
	args := []string{"install"}
	if opts.Replace {
		args = append(args, "-r")
	}
	if opts.AllowDowngrade {
		args = append(args, "-d")
	}
	if opts.GrantAll {
		args = append(args, "-g")
	}
	args = append(args, apkPath)

	report(stage, 5, "Menyiapkan APK...")
	report(stage, 20, "Mengirim dan memasang APK...")
	res, err := r.Run(ctx, args...)
	if err != nil {
		return err
	}
	if err := resultOf(res); err != nil {
		return err
	}
	report(stage, 100, "Selesai")
	return nil
}

// resultOf menafsirkan keluaran perintah adb yang keluar dengan status 0.
// Sebagian build adb (mis. `install`/`uninstall`) melaporkan kegagalan lewat
// teks "Failure [..]" meski exit code-nya 0, jadi sukses hanya diakui bila
// exit code 0 DAN stdout memuat satu baris utuh berisi "Success". Semua kasus
// lain diklasifikasikan sebagai kegagalan.
func resultOf(res Result) error {
	if res.ExitCode == 0 && hasSuccessLine(res.Stdout) {
		return nil
	}
	return Classify(res)
}

// hasSuccessLine true bila stdout memuat baris yang setelah dipangkas persis
// sama dengan "Success". Pencocokan baris utuh mencegah jalur APK yang
// kebetulan mengandung kata "Success" menyamarkan kegagalan.
func hasSuccessLine(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "Success" {
			return true
		}
	}
	return false
}

// Uninstall mencopot aplikasi. keepData=true menyisakan data aplikasi (-k).
func (r *Runner) Uninstall(ctx context.Context, pkg string, keepData bool) error {
	args := []string{"uninstall"}
	if keepData {
		args = append(args, "-k")
	}
	args = append(args, pkg)

	res, err := r.Run(ctx, args...)
	if err != nil {
		return err
	}
	return resultOf(res)
}

// ClearData menghapus data dan cache aplikasi tanpa mencopotnya.
func (r *Runner) ClearData(ctx context.Context, pkg string) error {
	res, err := r.Run(ctx, "shell", "pm", "clear", pkg)
	if err != nil {
		// r.Run sudah mengklasifikasikan exit code non-nol.
		return err
	}
	out := strings.TrimSpace(res.Stdout)
	if hasSuccessLine(res.Stdout) {
		return nil
	}
	if out == "" {
		out = "perangkat menolak menghapus data"
	}
	return fmt.Errorf("gagal menghapus data %s: %s", pkg, out)
}

// PullApk menyalin berkas APK yang terpasang di perangkat ke destDir dan
// mengembalikan path lokalnya. Hanya base APK yang diambil; APK terpisah
// (split APKs) di luar cakupan v1. Menarik ulang paket yang sama akan
// menimpa berkas tujuan sebelumnya (adb pull menimpa tanpa bertanya).
func (r *Runner) PullApk(ctx context.Context, pkg string, destDir string) (string, error) {
	out, err := r.Output(ctx, "shell", "pm", "path", pkg)
	if err != nil {
		return "", err
	}
	remote := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package:") {
			remote = strings.TrimPrefix(line, "package:")
			break
		}
	}
	if remote == "" {
		return "", fmt.Errorf("%w: %s", ErrPackageNotFound, pkg)
	}
	local := filepath.Join(destDir, sanitizeName(pkg)+"-"+path.Base(remote))
	if _, err := r.Run(ctx, "pull", remote, local); err != nil {
		return "", err
	}
	return local, nil
}

// sanitizeName mengganti karakter yang tidak sah dalam nama berkas dengan
// garis bawah. Ini mencakup pemisah path (`\` dan `/`) serta karakter yang
// terlarang di Windows (`: * ? " < > |`), sehingga nama paket yang bermusuhan
// tidak bisa dipakai untuk menulis keluar dari destDir.
func sanitizeName(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, s)
}
