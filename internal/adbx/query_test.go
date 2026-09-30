package adbx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const devicesSample = `List of devices attached
R58M12ABCDE            device product:beyond1lte model:SM_G973F device:beyond1 transport_id:1
0123456789ABCDEF       unauthorized transport_id:2
192.168.1.9:5555       offline transport_id:3

`

func TestDevicesParsesLines(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: devicesSample}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("harus 3 perangkat, dapat %d: %+v", len(got), got)
	}
	if got[0].Serial != "R58M12ABCDE" || got[0].State != "device" || got[0].Model != "SM_G973F" {
		t.Fatalf("baris pertama salah: %+v", got[0])
	}
	if got[0].Product != "beyond1lte" || got[0].DeviceName != "beyond1" {
		t.Fatalf("product/device baris pertama salah: %+v", got[0])
	}
	if got[1].Serial != "0123456789ABCDEF" {
		t.Fatalf("serial baris kedua salah: %+v", got[1])
	}
	if got[1].State != "unauthorized" {
		t.Fatalf("baris kedua salah: %+v", got[1])
	}
	if got[2].Serial != "192.168.1.9:5555" || got[2].State != "offline" {
		t.Fatalf("baris ketiga salah: %+v", got[2])
	}
}

const packagesSample = `package:/data/app/~~Ab==/com.foo-abc==/base.apk=com.foo versionCode:42
package:/data/app/~~Cd==/com.bar-xyz==/base.apk=com.bar versionCode:7
`

func TestPackagesParsesNameAndVersion(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: packagesSample}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	got, err := r.Packages(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("harus 2 paket, dapat %d", len(got))
	}
	if got[0].Name != "com.foo" || got[0].VersionCode != 42 {
		t.Fatalf("paket pertama salah: %+v", got[0])
	}
	if got[0].ApkPath != "/data/app/~~Ab==/com.foo-abc==/base.apk" {
		t.Fatalf("path salah: %q", got[0].ApkPath)
	}
	if got[1].System {
		t.Fatal("paket pihak ketiga tidak boleh ditandai sistem")
	}
}

func TestPackagesFallsBackWhenVersionCodeUnsupported(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stderr: "Error: Unknown option: --show-versioncode", ExitCode: 1},
		{Stdout: "package:/data/app/com.foo/base.apk=com.foo"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.Packages(context.Background(), false)
	if err != nil {
		t.Fatalf("harus jatuh ke perintah tanpa flag: %v", err)
	}
	if len(got) != 1 || got[0].Name != "com.foo" {
		t.Fatalf("hasil fallback salah: %+v", got)
	}
	if !strings.Contains(strings.Join(callsOf(fe), " "), "pm list packages") {
		t.Fatal("perintah pm list packages tidak dijalankan")
	}
	calls := callsOf(fe)
	if len(calls) < 2 {
		t.Fatalf("harus 2 panggilan: %v", calls)
	}
	if strings.Contains(calls[1], "--show-versioncode") {
		t.Fatalf("panggilan kedua tidak boleh memakai --show-versioncode: %q", calls[1])
	}
}

func TestPackagesRetriesWhenFirstCallParsesEmpty(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: "Unknown option: --show-versioncode"},
		{Stdout: packagesSample},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	got, err := r.Packages(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("harus 2 paket, dapat %d: %+v", len(got), got)
	}
	calls := callsOf(fe)
	if len(calls) != 2 {
		t.Fatalf("harus 2 panggilan: %v", calls)
	}
	if strings.Contains(calls[1], "--show-versioncode") {
		t.Fatalf("panggilan kedua tidak boleh memakai --show-versioncode: %q", calls[1])
	}
}

func TestPackagesSystemUsesSFlag(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: packagesSample}}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	got, err := r.Packages(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(callsOf(fe), " "), "list packages -s") {
		t.Fatalf("flag -s tidak dipakai: %v", callsOf(fe))
	}
	if len(got) != 2 || !got[0].System {
		t.Fatalf("paket sistem tidak ditandai: %+v", got)
	}
}

func TestParseDevicesLineTable(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []Device
	}{
		{
			name: "baris daemon * dilewati",
			line: "* daemon not running; starting now at tcp:5037",
			want: nil,
		},
		{
			name: "no permissions dipetakan ke offline",
			line: "????????????\tno permissions (user in plugdev group; are your udev rules wrong?)",
			want: []Device{{Serial: "????????????", State: "offline"}},
		},
		{
			name: "status tak dikenal dipetakan ke offline",
			line: "ABC123\tfrobnicating transport_id:9",
			want: []Device{{Serial: "ABC123", State: "offline"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDevices(tc.line)
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
			for i := range tc.want {
				if got[i].Serial != tc.want[i].Serial || got[i].State != tc.want[i].State {
					t.Fatalf("got %+v want %+v", got, tc.want)
				}
			}
		})
	}
}

func callsOf(fe *fakeExec) []string {
	out := make([]string, 0, len(fe.calls))
	for _, c := range fe.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

const dumpsysSample = `Packages:
  Package [com.example.app] (a1b2c3):
    userId=10123
    codePath=/data/app/~~Ab==/com.example.app-x==/base.apk
    versionName=1.2.3
    versionCode=42 minSdk=21 targetSdk=33
    firstInstallTime=2024-01-01 10:00:00
    lastUpdateTime=2024-02-02 11:00:00
    dataDir=/data/user/0/com.example.app
    requested permissions:
      android.permission.INTERNET
      android.permission.CAMERA
    flags=[ HAS_CODE ALLOW_CLEAR_USER_DATA ALLOW_BACKUP ]
`

func TestPackageInfoParsesDumpsys(t *testing.T) {
	fe := &fakeExec{results: []Result{
		{Stdout: dumpsysSample},
		{Stdout: "1234\t/data/user/0/com.example.app"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe), WithSerial("S1"))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if got.VersionName != "1.2.3" || got.VersionCode != 42 {
		t.Fatalf("versi salah: %+v", got)
	}
	if got.ApkPath != "/data/app/~~Ab==/com.example.app-x==/base.apk" {
		t.Fatalf("apkPath salah: %q", got.ApkPath)
	}
	if got.InstallTime != "2024-01-01 10:00:00" {
		t.Fatalf("installTime salah: %q", got.InstallTime)
	}
	if got.UpdateTime != "2024-02-02 11:00:00" {
		t.Fatalf("updateTime salah: %q", got.UpdateTime)
	}
	if got.DataDir != "/data/user/0/com.example.app" {
		t.Fatalf("dataDir salah: %q", got.DataDir)
	}
	if len(got.Permissions) != 2 {
		t.Fatalf("izin salah: %+v", got.Permissions)
	}
	if got.Permissions[0] != "android.permission.INTERNET" ||
		got.Permissions[1] != "android.permission.CAMERA" {
		t.Fatalf("nilai izin salah: %+v", got.Permissions)
	}
	if got.System {
		t.Fatal("paket di /data/app bukan aplikasi sistem")
	}
	if got.SizeBytes != 1234*1024 {
		t.Fatalf("ukuran salah: %d", got.SizeBytes)
	}
}

func TestPackageInfoParsesNamespacedPermissions(t *testing.T) {
	sample := strings.Replace(dumpsysSample,
		"      android.permission.CAMERA",
		"      android.permission.CAMERA\n      org.example.permission.FOO\n      com.vendor.permission.BAR", 1)
	fe := &fakeExec{results: []Result{
		{Stdout: sample},
		{Stdout: "10\t/data/user/0/com.example.app"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Permissions) != 4 {
		t.Fatalf("izin salah: %+v", got.Permissions)
	}
	if got.Permissions[2] != "org.example.permission.FOO" ||
		got.Permissions[3] != "com.vendor.permission.BAR" {
		t.Fatalf("izin namespace salah: %+v", got.Permissions)
	}
}

func TestPackageInfoGarbageReturnsNotFound(t *testing.T) {
	fe := &fakeExec{results: []Result{{Stdout: "bukan keluaran dumpsys"}}}
	r := New("/usr/bin/adb", WithExecer(fe))
	_, err := r.PackageInfo(context.Background(), "com.example.app")
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("got %v, want ErrPackageNotFound", err)
	}
}

func TestPackageInfoWithoutVersionName(t *testing.T) {
	sample := strings.Replace(dumpsysSample, "    versionName=1.2.3\n", "", 1)
	fe := &fakeExec{results: []Result{
		{Stdout: sample},
		{Stdout: "10\t/data/user/0/com.example.app"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if got.VersionName != "" || got.DataDir != "/data/user/0/com.example.app" {
		t.Fatalf("hasil salah: %+v", got)
	}
}

func TestPackageInfoMarksApexAsSystem(t *testing.T) {
	sample := strings.Replace(dumpsysSample,
		"codePath=/data/app/~~Ab==/com.example.app-x==/base.apk",
		"codePath=/apex/com.android.foo/foo.apk", 1)
	fe := &fakeExec{results: []Result{
		{Stdout: sample},
		{Stdout: "10\t/apex/com.android.foo"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if !got.System {
		t.Fatal("paket di /apex harus ditandai sistem")
	}
}

func TestPackageInfoMarksSystemApp(t *testing.T) {
	sample := strings.Replace(dumpsysSample,
		"codePath=/data/app/~~Ab==/com.example.app-x==/base.apk",
		"codePath=/system/priv-app/Foo/Foo.apk", 1)
	fe := &fakeExec{results: []Result{
		{Stdout: sample},
		{Stdout: "10\t/system/priv-app/Foo"},
	}}
	r := New("/usr/bin/adb", WithExecer(fe))
	got, err := r.PackageInfo(context.Background(), "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if !got.System {
		t.Fatal("paket di /system harus ditandai sistem")
	}
}
