package store

import (
	"bytes"
	"encoding/json"
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
