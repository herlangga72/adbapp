package adbx

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Device adalah satu baris dari `adb devices -l`.
type Device struct {
	Serial     string
	State      string // device, unauthorized, offline, ...
	Model      string
	Product    string
	DeviceName string
}

// Package adalah satu aplikasi yang terpasang di perangkat.
type Package struct {
	Name        string
	ApkPath     string
	VersionCode int64
	System      bool
}

// PackageInfo adalah detail satu aplikasi, untuk panel detail di UI.
type PackageInfo struct {
	Package     string
	VersionName string
	VersionCode int64
	InstallTime string
	UpdateTime  string
	ApkPath     string
	DataDir     string
	SizeBytes   int64
	Permissions []string
	System      bool
}

func (r *Runner) Devices(ctx context.Context) ([]Device, error) {
	out, err := r.Output(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	return parseDevices(out), nil
}

func parseDevices(out string) []Device {
	var devs []Device
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" ||
			strings.HasPrefix(line, "List of devices") ||
			strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := Device{Serial: fields[0], State: normalizeState(fields[1])}
		for _, f := range fields[2:] {
			k, v, ok := strings.Cut(f, ":")
			if !ok {
				continue
			}
			switch k {
			case "model":
				d.Model = v
			case "product":
				d.Product = v
			case "device":
				d.DeviceName = v
			}
		}
		devs = append(devs, d)
	}
	return devs
}

// normalizeState memetakan status yang tidak dikenal (misalnya baris
// "no permissions (user in plugdev group; are your udev rules wrong?)") ke
// "offline" supaya UI tidak menampilkan string yang kacau.
func normalizeState(s string) string {
	switch s {
	case "device", "unauthorized", "offline", "bootloader", "recovery", "sideload", "rescue", "authorizing":
		return s
	default:
		return "offline"
	}
}

// Packages mengembalikan daftar aplikasi. system=true meminta aplikasi sistem.
func (r *Runner) Packages(ctx context.Context, system bool) ([]Package, error) {
	flag := "-3"
	if system {
		flag = "-s"
	}
	out, err := r.Output(ctx, "shell", "pm", "list", "packages", flag, "-f", "--show-versioncode")
	retried := false
	if err != nil {
		// Perangkat lama belum mendukung --show-versioncode.
		out, err = r.Output(ctx, "shell", "pm", "list", "packages", flag, "-f")
		if err != nil {
			return nil, err
		}
		retried = true
	}
	pkgs := parsePackages(out, system)
	if len(pkgs) == 0 && !retried && looksLikeUnsupportedFlag(out) {
		// Sebagian build adb mencetak "Unknown option" ke stdout tetapi tetap
		// keluar dengan status 0; ulangi tanpa --show-versioncode.
		if out2, err2 := r.Output(ctx, "shell", "pm", "list", "packages", flag, "-f"); err2 == nil {
			pkgs = parsePackages(out2, system)
		}
	}
	return pkgs, nil
}

// looksLikeUnsupportedFlag mendeteksi keluaran yang menandakan flag tidak
// dikenali, supaya daftar kosong yang wajar tidak memicu panggilan ulang.
func looksLikeUnsupportedFlag(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "unknown option") ||
		strings.Contains(lower, "error")
}

func parsePackages(out string, system bool) []Package {
	var pkgs []Package
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		rest := strings.TrimPrefix(line, "package:")
		p := Package{System: system}
		if i := strings.Index(rest, " versionCode:"); i >= 0 {
			code, _ := strconv.ParseInt(strings.TrimSpace(rest[i+len(" versionCode:"):]), 10, 64)
			p.VersionCode = code
			rest = rest[:i]
		}
		if i := strings.LastIndex(rest, "="); i >= 0 {
			p.ApkPath = rest[:i]
			p.Name = rest[i+1:]
		} else {
			p.Name = rest
		}
		if p.Name == "" {
			continue
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// PackageInfo mengambil detail satu aplikasi: versi, ukuran, izin, dan
// apakah ia bagian dari sistem.
func (r *Runner) PackageInfo(ctx context.Context, pkg string) (PackageInfo, error) {
	out, err := r.Output(ctx, "shell", "dumpsys", "package", pkg)
	if err != nil {
		return PackageInfo{}, err
	}
	info := parseDumpsys(pkg, out)
	if info.Package == "" {
		return PackageInfo{}, fmt.Errorf("%w: %s", ErrPackageNotFound, pkg)
	}
	if info.DataDir != "" {
		// Bila `du` gagal, SizeBytes tetap 0 yang berarti "ukuran tidak
		// diketahui".
		if size, err := r.Output(ctx, "shell", "du", "-sk", info.DataDir); err == nil {
			info.SizeBytes = parseDU(size)
		}
	}
	return info, nil
}

func parseDumpsys(pkg, out string) PackageInfo {
	info := PackageInfo{}
	found := false
	inPermissions := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !found && strings.HasPrefix(line, "Package [") {
			// Kehadiran paket dideteksi dari header "Package [<nama>]", bukan
			// dari kelengkapan field versi.
			found = true
			info.Package = pkg
		}
		if line == "requested permissions:" {
			inPermissions = true
			continue
		}
		if inPermissions {
			if strings.Contains(line, ".permission.") {
				info.Permissions = append(info.Permissions, line)
				continue
			}
			inPermissions = false
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "codePath":
			info.ApkPath = value
		case "versionName":
			info.VersionName = value
		case "versionCode":
			fields := strings.Fields(value)
			if len(fields) > 0 {
				code, _ := strconv.ParseInt(fields[0], 10, 64)
				info.VersionCode = code
			}
		case "firstInstallTime":
			info.InstallTime = value
		case "lastUpdateTime":
			info.UpdateTime = value
		case "dataDir":
			info.DataDir = value
		}
	}
	if !found {
		return PackageInfo{}
	}
	// Paket sistem biasanya berada di /system, /product, /vendor, atau /apex.
	// Ini heuristik: aplikasi sistem yang diperbarui dapat berpindah ke
	// /data/app sehingga tidak lagi terdeteksi sebagai sistem.
	info.System = strings.HasPrefix(info.ApkPath, "/system") ||
		strings.HasPrefix(info.ApkPath, "/product") ||
		strings.HasPrefix(info.ApkPath, "/vendor") ||
		strings.HasPrefix(info.ApkPath, "/apex")
	return info
}

func parseDU(out string) int64 {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}
