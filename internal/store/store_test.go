package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	e := Entry{
		Time:    time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC),
		Action:  "install",
		Package: "com.foo",
		Device:  "S1",
		Success: true,
	}
	if err := s.Append(e); err != nil {
		t.Fatalf("Append gagal: %v", err)
	}
	got, err := s.Read(10)
	if err != nil {
		t.Fatalf("Read gagal: %v", err)
	}
	if len(got) != 1 || got[0].Package != "com.foo" || !got[0].Success {
		t.Fatalf("isi riwayat salah: %+v", got)
	}
}

func TestReadReturnsNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	base := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := s.Append(Entry{Time: base.Add(time.Duration(i) * time.Minute), Package: "pkg"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("harus 3 baris, dapat %d", len(got))
	}
	if !got[0].Time.After(got[2].Time) {
		t.Fatalf("urutan harus terbaru dulu: %+v", got)
	}
}

func TestReadHonorsLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	for i := 0; i < 5; i++ {
		if err := s.Append(Entry{Package: "pkg"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Read(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("limit tidak dihormati: %d", len(got))
	}
}

func TestReadIgnoresBrokenLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	if err := s.Append(Entry{Package: "ok"}); err != nil {
		t.Fatal(err)
	}
	f, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendRaw(f, "{ini bukan json}\n"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(10)
	if err != nil {
		t.Fatalf("baris rusak seharusnya dilewati: %v", err)
	}
	if len(got) != 1 || got[0].Package != "ok" {
		t.Fatalf("hasil salah: %+v", got)
	}
}

func TestExportCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	if err := s.Append(Entry{
		Time:    time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC),
		Action:  "install",
		Package: "com.foo",
		Device:  "S1",
		Success: true,
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := s.ExportCSV(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "time,action,package,device,success,detail\n") {
		t.Fatalf("header CSV salah:\n%s", out)
	}
	if !strings.Contains(out, "com.foo") {
		t.Fatalf("baris CSV tidak memuat paket:\n%s", out)
	}
}

func TestExportJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	if err := s.Append(Entry{Package: "com.foo", Success: true}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := s.ExportJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var decoded []Entry
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON tidak sah: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Package != "com.foo" {
		t.Fatalf("isi JSON salah: %+v", decoded)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Config{ApkFolder: "/home/user/apk"}
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ApkFolder != cfg.ApkFolder {
		t.Fatalf("got %+v want %+v", got, cfg)
	}
}

func TestLoadConfigMissingFileReturnsDefault(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "tidak-ada.json"))
	if err != nil {
		t.Fatalf("seharusnya tidak error: %v", err)
	}
	if cfg.ApkFolder != "" {
		t.Fatalf("default harus kosong: %+v", cfg)
	}
}

func TestLoadConfigCorruptReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{ini bukan json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("config.json rusak seharusnya mengembalikan error")
	}
}

func TestSaveConfigReplacesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveConfig(path, Config{ApkFolder: "/lama"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(path, Config{ApkFolder: "/baru"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ApkFolder != "/baru" {
		t.Fatalf("nilai baru tidak menimpa: %+v", got)
	}
	// Tidak boleh ada berkas sementara yang tertinggal.
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "config.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("berkas sementara tertinggal: %v", matches)
	}
}

func TestAppendDefaultsZeroTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	before := time.Now()
	if err := s.Append(Entry{Package: "pkg"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("harus 1 entri, dapat %d", len(got))
	}
	if got[0].Time.IsZero() {
		t.Fatal("Time nol seharusnya diisi waktu sekarang")
	}
	if got[0].Time.Before(before.Add(-time.Second)) || got[0].Time.After(time.Now().Add(time.Second)) {
		t.Fatalf("Time tidak wajar: %v (sekarang %v)", got[0].Time, time.Now())
	}
}

func TestExportJSONEmptyIsArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	var buf bytes.Buffer
	if err := s.ExportJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Fatalf("ekspor kosong harus [] bukan null: %q", buf.String())
	}
}

func TestReadMissingFileReturnsNil(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "tidak-ada.jsonl"))
	got, err := s.Read(5)
	if err != nil {
		t.Fatalf("berkas hilang seharusnya tidak error: %v", err)
	}
	if got != nil {
		t.Fatalf("berkas hilang harus nil: %+v", got)
	}
}

func TestReadTailEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := New(path).Read(5)
	if err != nil {
		t.Fatalf("berkas kosong tidak boleh error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("berkas kosong harus kosong: %+v", got)
	}
}

func TestReadTailLimitOnSmallFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	base := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := s.Append(Entry{
			Time:    base.Add(time.Duration(i) * time.Minute),
			Package: fmt.Sprintf("pkg-%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		limit int
		want  []string
	}{
		{1, []string{"pkg-4"}},
		{2, []string{"pkg-4", "pkg-3"}},
		{5, []string{"pkg-4", "pkg-3", "pkg-2", "pkg-1", "pkg-0"}},
		{9, []string{"pkg-4", "pkg-3", "pkg-2", "pkg-1", "pkg-0"}},
	}
	for _, tc := range cases {
		got, err := s.Read(tc.limit)
		if err != nil {
			t.Fatalf("Read(%d) gagal: %v", tc.limit, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("Read(%d) dapat %d entri, mau %d: %+v", tc.limit, len(got), len(tc.want), got)
		}
		for i := range tc.want {
			if got[i].Package != tc.want[i] {
				t.Fatalf("Read(%d) urutan salah di %d: got %q mau %q", tc.limit, i, got[i].Package, tc.want[i])
			}
		}
	}
}

func TestReadTailLastThreeOfLargeHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	for i := 0; i < 2000; i++ {
		if err := s.Append(Entry{Package: fmt.Sprintf("pkg-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Read(3)
	if err != nil {
		t.Fatalf("Read gagal: %v", err)
	}
	want := []string{"pkg-1999", "pkg-1998", "pkg-1997"}
	if len(got) != len(want) {
		t.Fatalf("harus 3 entri, dapat %d: %+v", len(got), got)
	}
	for i := range want {
		if got[i].Package != want[i] {
			t.Fatalf("urutan salah di %d: got %q mau %q", i, got[i].Package, want[i])
		}
	}
}

func TestReadTailNoTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	e1, err := json.Marshal(Entry{Package: "a"})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := json.Marshal(Entry{Package: "b"})
	if err != nil {
		t.Fatal(err)
	}
	// Baris terakhir sengaja tanpa newline penutup.
	if err := appendRaw(path, string(e1)+"\n"+string(e2)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Package != "b" {
		t.Fatalf("baris terakhir tanpa newline salah: %+v", got)
	}
}

func TestReadTailLineLongerThanChunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	big := strings.Repeat("x", 200*1024)
	if err := s.Append(Entry{Package: "big", Detail: big}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Entry{Package: "small"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Package != "small" {
		t.Fatalf("entri terakhir salah: %+v", got)
	}
	got, err = s.Read(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Package != "big" || got[1].Detail != big {
		t.Fatalf("baris panjang tidak terbaca utuh: len=%d", len(got))
	}
}

func TestReadTailRefillsPastCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	for i := 0; i < 200; i++ {
		if err := s.Append(Entry{Package: fmt.Sprintf("pkg-%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	// Baris rusak di paling ekor tidak boleh membuat Read(limit) kekurangan
	// entri: pembacaan harus mundur sampai limit entri sah terkumpul.
	if err := appendRaw(path, "{ini bukan json}\n"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(5)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pkg-199", "pkg-198", "pkg-197", "pkg-196", "pkg-195"}
	if len(got) != len(want) {
		t.Fatalf("harus %d entri sah, dapat %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i].Package != want[i] {
			t.Fatalf("urutan salah di %d: got %q mau %q", i, got[i].Package, want[i])
		}
	}
}

func TestReadHugeLimitNoOverflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	for i := 0; i < 3; i++ {
		if err := s.Append(Entry{Package: fmt.Sprintf("pkg-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	// limit == math.MaxInt tidak boleh meluap atau panik; artinya baca semua.
	got, err := s.Read(math.MaxInt)
	if err != nil {
		t.Fatalf("Read(math.MaxInt) gagal: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("harus 3 entri, dapat %d: %+v", len(got), got)
	}
	if got[0].Package != "pkg-2" {
		t.Fatalf("terbaru harus pkg-2, dapat %q", got[0].Package)
	}
}

func TestReadFullHandlesLongDetailAndExport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	// ~2 MB, jauh di atas buffer scanner lama (1 MB) tetapi di bawah batas
	// baru (8 MB).
	big := strings.Repeat("d", 2*1024*1024)
	if err := s.Append(Entry{Package: "big", Detail: big, Success: true}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(0)
	if err != nil {
		t.Fatalf("Read(0) gagal untuk Detail panjang: %v", err)
	}
	if len(got) != 1 || got[0].Detail != big {
		t.Fatalf("Detail panjang tidak bulat: len=%d", len(got))
	}

	var csvBuf bytes.Buffer
	if err := s.ExportCSV(&csvBuf); err != nil {
		t.Fatalf("ExportCSV gagal: %v", err)
	}
	if !strings.Contains(csvBuf.String(), big) {
		t.Fatal("CSV tidak memuat Detail panjang")
	}
	var jsonBuf bytes.Buffer
	if err := s.ExportJSON(&jsonBuf); err != nil {
		t.Fatalf("ExportJSON gagal: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), big) {
		t.Fatal("JSON tidak memuat Detail panjang")
	}
}
