package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/herlangga72/adbapp/internal/apkmeta"
)

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
	if err := r.ParseMultipartForm(1 << 30); err != nil {
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
	if err := decodeJSON(r, &req); err != nil || req.URL == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("url wajib diisi"))
		return
	}

	name := filepath.Base(req.Name)
	if name == "" || name == "." || name == "/" {
		name = filepath.Base(req.URL)
	}
	if !strings.EqualFold(filepath.Ext(name), ".apk") {
		name += ".apk"
	}

	resp, err := http.Get(req.URL)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("gagal mengunduh: %w", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Errorf("unduhan gagal, kode %d", resp.StatusCode))
		return
	}

	if err := os.MkdirAll(s.Paths.UploadsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	dest := filepath.Join(s.Paths.UploadsDir, name)
	out, err := os.Create(dest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 2<<30)); err != nil {
		out.Close()
		writeError(w, http.StatusBadGateway, fmt.Errorf("gagal menyimpan unduhan: %w", err))
		return
	}
	out.Close()

	entry := s.entryFor(dest)
	if entry.Error != "" {
		if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Errorf("unduhan bukan APK yang sah: %s", entry.Error))
		return
	}
	writeJSON(w, http.StatusOK, entry)
}
