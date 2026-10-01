// Package store menyimpan riwayat audit dan konfigurasi pengguna.
package store

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
)

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
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
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

// SaveConfig menulis konfigurasi ke disk.
func SaveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
