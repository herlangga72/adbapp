// Package adbx menjalankan perintah adb dan mengurai keluarannya.
package adbx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Result adalah keluaran satu perintah adb.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Execer menjalankan sebuah program. Implementasi asli memakai os/exec;
// pengujian menyuntikkan versi tiruan. Implementasi harus aman dipakai
// bersamaan (concurrent-safe) karena satu Runner dapat dipakai banyak goroutine.
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
		// Bila konteks dibatalkan, exec.CommandContext membunuh proses dan
		// mengembalikan *exec.ExitError. Kembalikan ctx.Err() agar pemanggil
		// dapat mengenali context.Canceled / context.DeadlineExceeded.
		if cerr := ctx.Err(); cerr != nil {
			return res, cerr
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
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
	timeout time.Duration
}

type Option func(*Runner)

// WithExecer mengganti pelaksana perintah. Nilai nil diabaikan sehingga
// Runner tetap memakai pelaksana bawaan dan tidak panik saat dipanggil.
func WithExecer(e Execer) Option {
	return func(r *Runner) {
		if e != nil {
			r.exec = e
		}
	}
}

func WithSerial(serial string) Option {
	return func(r *Runner) { r.serial = serial }
}

// WithTimeout memberi batas waktu pengaman untuk setiap perintah adb. Nilai
// nol atau negatif berarti tanpa batas waktu.
func WithTimeout(d time.Duration) Option {
	return func(r *Runner) { r.timeout = d }
}

// New membuat Runner. Batas waktu bawaan 15 menit dipasang sebagai jaring
// pengaman agar adb yang menggantung tidak memblokir selamanya.
func New(adbPath string, opts ...Option) *Runner {
	r := &Runner{adbPath: adbPath, exec: osExec{}, timeout: 15 * time.Minute}
	for _, o := range opts {
		o(r)
	}
	return r
}

// WithSerial mengembalikan salinan Runner yang menargetkan serial tertentu.
// Ini salinan dangkal (shallow): aman hanya selama Runner memegang field
// skalar/interface saja.
func (r *Runner) WithSerial(serial string) *Runner {
	clone := *r
	clone.serial = serial
	return &clone
}

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
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}
	res, err := r.exec.Run(ctx, r.adbPath, r.args(rest...)...)
	if err != nil {
		return res, fmt.Errorf("menjalankan %s %s: %w", r.adbPath, strings.Join(rest, " "), err)
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
