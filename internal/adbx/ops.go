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

	report(stage, 5, "Mengirim APK ke perangkat...")
	res, err := r.exec.Run(ctx, r.adbPath, r.args(args...)...)
	if err != nil {
		return fmt.Errorf("gagal menjalankan adb: %w", err)
	}
	report(stage, 90, "Memasang...")
	if err := installResult(res); err != nil {
		return err
	}
	report(stage, 100, "Selesai")
	return nil
}

// installResult memeriksa keluaran `adb install`, yang bisa melaporkan
// kegagalan lewat teks "Failure [..]" walau exit code-nya 0.
func installResult(res Result) error {
	out := res.Stdout + "\n" + res.Stderr
	if strings.Contains(out, "Success") {
		return nil
	}
	return Classify(res)
}

// Uninstall mencopot aplikasi. keepData=true menyisakan data aplikasi (-k).
func (r *Runner) Uninstall(ctx context.Context, pkg string, keepData bool) error {
	args := []string{"uninstall"}
	if keepData {
		args = append(args, "-k")
	}
	args = append(args, pkg)

	res, err := r.exec.Run(ctx, r.adbPath, r.args(args...)...)
	if err != nil {
		return fmt.Errorf("gagal menjalankan adb: %w", err)
	}
	out := res.Stdout + "\n" + res.Stderr
	if strings.Contains(out, "Success") {
		return nil
	}
	return Classify(res)
}

// ClearData menghapus data dan cache aplikasi tanpa mencopotnya.
func (r *Runner) ClearData(ctx context.Context, pkg string) error {
	res, err := r.exec.Run(ctx, r.adbPath, r.args("shell", "pm", "clear", pkg)...)
	if err != nil {
		return fmt.Errorf("gagal menjalankan adb: %w", err)
	}
	out := strings.TrimSpace(res.Stdout)
	if strings.Contains(out, "Success") {
		return nil
	}
	if res.ExitCode != 0 {
		return Classify(res)
	}
	if out == "" {
		out = "perangkat menolak menghapus data"
	}
	return fmt.Errorf("%s", out)
}

// PullApk menyalin APK yang terpasang di perangkat ke destDir dan
// mengembalikan path lokalnya.
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
	local := filepath.Join(destDir, pkg+"-"+path.Base(remote))
	if _, err := r.Run(ctx, "pull", remote, local); err != nil {
		return "", err
	}
	return local, nil
}
