// Package adbx menjalankan perintah adb dan mengurai keluarannya.
package adbx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Result adalah keluaran satu perintah adb.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Execer menjalankan sebuah program. Implementasi asli memakai os/exec;
// pengujian menyuntikkan versi tiruan.
type Execer interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

type osExec struct{}

func (osExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errBuf.String()}
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Runner menjalankan perintah adb untuk satu perangkat (bila serial diisi).
type Runner struct {
	adbPath string
	exec    Execer
	serial  string
}

type Option func(*Runner)

func WithExecer(e Execer) Option {
	return func(r *Runner) { r.exec = e }
}

func WithSerial(serial string) Option {
	return func(r *Runner) { r.serial = serial }
}

func New(adbPath string, opts ...Option) *Runner {
	r := &Runner{adbPath: adbPath, exec: osExec{}}
	for _, o := range opts {
		o(r)
	}
	return r
}

// WithSerial mengembalikan salinan Runner yang menargetkan serial tertentu.
func (r *Runner) WithSerial(serial string) *Runner {
	clone := *r
	clone.serial = serial
	return &clone
}

func (r *Runner) Serial() string { return r.serial }

func (r *Runner) args(rest ...string) []string {
	args := make([]string, 0, len(rest)+2)
	if r.serial != "" {
		args = append(args, "-s", r.serial)
	}
	return append(args, rest...)
}

// Run menjalankan perintah adb. Error yang dikembalikan sudah diterjemahkan
// bila keluarannya cocok dengan pola kegagalan yang dikenal.
func (r *Runner) Run(ctx context.Context, rest ...string) (Result, error) {
	res, err := r.exec.Run(ctx, r.adbPath, r.args(rest...)...)
	if err != nil {
		return res, fmt.Errorf("gagal menjalankan adb: %w", err)
	}
	if res.ExitCode != 0 {
		return res, Classify(res)
	}
	return res, nil
}

// Output menjalankan perintah dan mengembalikan stdout yang sudah dipangkas.
func (r *Runner) Output(ctx context.Context, rest ...string) (string, error) {
	res, err := r.Run(ctx, rest...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
