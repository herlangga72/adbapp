// Package store menyimpan riwayat audit dan konfigurasi pengguna.
package store

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// newline adalah pemisah satu entri JSON Lines.
var newline = []byte{'\n'}

// Entry adalah satu baris riwayat audit.
type Entry struct {
	Time    time.Time `json:"time"`
	Action  string    `json:"action"`
	Package string    `json:"package"`
	Device  string    `json:"device"`
	Success bool      `json:"success"`
	Detail  string    `json:"detail,omitempty"`
}

// Store menulis dan membaca riwayat dalam format JSON Lines.
type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store {
	return &Store{path: path}
}

// Append menambahkan satu entri ke akhir berkas riwayat.
func (s *Store) Append(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// Read mengembalikan paling banyak limit entri, terbaru lebih dulu.
//
// Bila limit > 0, hanya ekor berkas yang dibaca sehingga biaya baca tidak
// bergantung pada panjang riwayat. Limit <= 0 membaca seluruh berkas, dipakai
// oleh ekspor CSV/JSON.
func (s *Store) Read(limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if limit > 0 {
		return readTail(f, limit)
	}

	var all []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // baris rusak dilewati, bukan menggagalkan seluruh riwayat
		}
		all = append(all, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Balik urutan: terbaru lebih dulu.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all, nil
}

// tailChunkSize adalah ukuran tiap potongan saat membaca mundur dari ekor.
const tailChunkSize = 64 * 1024

// readTail membaca paling banyak limit baris terakhir berkas dan
// mengembalikannya terbaru lebih dulu. Baris yang lebih panjang dari
// tailChunkSize tetap utuh karena potongan terus dibaca sampai baris lengkap
// terkumpul.
func readTail(f *os.File, limit int) ([]Entry, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	// Baca mundur dalam potongan sampai cukup banyak pemisah baris terkumpul
	// (atau sampai awal berkas). Satu pemisah ekstra memastikan baris terdepan
	// yang dipertahankan berada tepat setelah newline, jadi utuh meskipun
	// potongan pertama dimulai di tengah baris. Potongan disimpan lalu
	// disatukan sekali agar tidak menyalin buffer berulang kali.
	var chunks [][]byte
	newlines := 0
	for pos := size; pos > 0 && newlines < limit+1; {
		n := int64(tailChunkSize)
		if pos < n {
			n = pos
		}
		pos -= n
		chunk := make([]byte, n)
		if _, err := f.ReadAt(chunk, pos); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
		newlines += bytes.Count(chunk, newline)
	}

	total := 0
	for _, c := range chunks {
		total += len(c)
	}
	buf := make([]byte, 0, total)
	for i := len(chunks) - 1; i >= 0; i-- {
		buf = append(buf, chunks[i]...)
	}

	lines := bytes.Split(buf, newline)
	// Buang segmen kosong setelah newline terakhir, bila ada.
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	// Potongan pertama bisa dimulai di tengah baris; ambil hanya limit baris
	// terakhir yang pasti utuh.
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}

	var entries []Entry
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	// Balik urutan: terbaru lebih dulu.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// ExportCSV menulis seluruh riwayat sebagai CSV.
func (s *Store) ExportCSV(w io.Writer) error {
	entries, err := s.readAllOldestFirst()
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"time", "action", "package", "device", "success", "detail"}); err != nil {
		return err
	}
	for _, e := range entries {
		if err := cw.Write([]string{
			e.Time.Format(time.RFC3339),
			e.Action,
			e.Package,
			e.Device,
			strconv.FormatBool(e.Success),
			e.Detail,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ExportJSON menulis seluruh riwayat sebagai satu larik JSON.
func (s *Store) ExportJSON(w io.Writer) error {
	entries, err := s.readAllOldestFirst()
	if err != nil {
		return err
	}
	if entries == nil {
		entries = []Entry{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

func (s *Store) readAllOldestFirst() ([]Entry, error) {
	entries, err := s.Read(0)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// Config adalah pengaturan kecil yang bertahan antar sesi.
type Config struct {
	ApkFolder string `json:"apkFolder"`
}

// LoadConfig membaca konfigurasi; berkas yang belum ada dianggap default.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config.json rusak: %w", err)
	}
	return cfg, nil
}

// SaveConfig menulis konfigurasi ke disk secara atomik: tulis ke berkas
// sementara di direktori yang sama, sync, lalu rename menimpa target. Dengan
// begitu crash atau disk penuh tidak meninggalkan config.json yang rusak.
func SaveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("membuat berkas sementara untuk %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("menulis %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("menyinkronkan %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("menutup berkas sementara %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("mengatur mode %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("memindahkan %s ke %s: %w", tmpName, path, err)
	}
	return nil
}
