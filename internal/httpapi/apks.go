package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/herlangga72/adbapp/internal/apkmeta"
)

// maxAPKSize adalah batas atas ukuran berkas APK yang diterima (2 GiB).
const maxAPKSize = 2 << 30

type apkEntry struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Package     string `json:"package,omitempty"`
	VersionName string `json:"versionName,omitempty"`
	VersionCode int64  `json:"versionCode,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (s *Server) entryFor(path string) apkEntry {
	e := apkEntry{Path: path, Name: filepath.Base(path)}
	st, err := os.Stat(path)
	if err != nil {
		e.Error = err.Error()
		return e
	}
	e.Size = st.Size()

	meta, err := apkmeta.ReadCached(path, st.Size(), st.ModTime().UnixNano())
	if err != nil {
		e.Error = err.Error()
		return e
	}
	e.Package = meta.Package
	e.VersionName = meta.VersionName
	e.VersionCode = meta.VersionCode
	return e
}

// handleListAPKs mendaftar berkas .apk di folder koleksi.
func (s *Server) handleListAPKs(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		if s.LoadCfg != nil {
			if cfg, err := s.LoadCfg(); err == nil {
				folder = cfg.ApkFolder
			}
		}
	}
	if folder == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("folder belum ditentukan"))
		return
	}

	dirents, err := os.ReadDir(folder)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("folder tidak bisa dibaca: %w", err))
		return
	}

	entries := make([]apkEntry, 0, len(dirents))
	for _, de := range dirents {
		if de.IsDir() || !strings.EqualFold(filepath.Ext(de.Name()), ".apk") {
			continue
		}
		entries = append(entries, s.entryFor(filepath.Join(folder, de.Name())))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAPKSize)
	if err := r.ParseMultipartForm(1 << 30); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("berkas terlalu besar"))
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Errorf("gagal membaca berkas: %w", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("berkas tidak ditemukan pada permintaan"))
		return
	}
	defer file.Close()

	name := filepath.Base(header.Filename)
	if !strings.EqualFold(filepath.Ext(name), ".apk") {
		writeError(w, http.StatusBadRequest, fmt.Errorf("hanya berkas .apk yang diterima"))
		return
	}

	dest := filepath.Join(s.Paths.UploadsDir, name)
	if err := os.MkdirAll(s.Paths.UploadsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out, err := os.Create(dest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := out.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	entry := s.entryFor(dest)
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleFromURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("url wajib diisi"))
		return
	}

	name := filepath.Base(req.Name)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = filepath.Base(req.URL)
	}
	name = filepath.Base(name)
	if name == "" || name == "." || name == string(filepath.Separator) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("nama berkas tidak sah"))
		return
	}
	if !strings.EqualFold(filepath.Ext(name), ".apk") {
		name += ".apk"
	}

	if err := os.MkdirAll(s.Paths.UploadsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	dest := filepath.Join(s.Paths.UploadsDir, name)
	part := dest + ".part"
	// Bersihkan sisa unduhan sebelumnya agar tidak menumpuk.
	_ = os.Remove(part)

	if err := s.downloadAPK(r.Context(), req.URL, part); err != nil {
		_ = os.Remove(part)
		writeError(w, http.StatusBadGateway, err)
		return
	}

	entry := s.entryFor(part)
	if entry.Error != "" {
		_ = os.Remove(part)
		writeError(w, http.StatusBadRequest, fmt.Errorf("unduhan bukan APK yang sah: %s", entry.Error))
		return
	}
	// Baru setelah tervalidasi, ganti berkas tujuan supaya APK lama dengan nama
	// sama tidak rusak ketika unduhan gagal.
	if err := os.Rename(part, dest); err != nil {
		_ = os.Remove(part)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.entryFor(dest))
}

// downloadAPK mengunduh url ke path dengan batas waktu, batas ukuran, dan
// pembatasan pengalihan. Badan yang melebihi maxAPKSize ditolak, bukan dipotong.
func (s *Server) downloadAPK(ctx context.Context, url, path string) error {
	client := &http.Client{
		Timeout: 15 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("terlalu banyak pengalihan")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("gagal mengunduh: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gagal mengunduh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unduhan gagal, kode %d", resp.StatusCode)
	}

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, maxAPKSize+1))
	if err != nil {
		out.Close()
		return fmt.Errorf("gagal menyimpan unduhan: %w", err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	if n > maxAPKSize {
		return fmt.Errorf("unduhan terlalu besar")
	}
	return nil
}
