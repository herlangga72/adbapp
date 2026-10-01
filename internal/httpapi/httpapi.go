// Package httpapi menyajikan API untuk UI dan mengalirkan pembaruan lewat SSE.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
	"github.com/herlangga72/adbapp/internal/device"
	"github.com/herlangga72/adbapp/internal/paths"
	"github.com/herlangga72/adbapp/internal/queue"
	"github.com/herlangga72/adbapp/internal/store"
)

type DeviceSource interface {
	Current() device.Status
	Refresh(ctx context.Context) error
	Select(serial string) error
}

type JobQueue interface {
	Enqueue(queue.Job) queue.Job
	Jobs() []queue.Job
	Cancel(id string) error
	Subscribe() (<-chan queue.Job, func())
}

type History interface {
	Read(limit int) ([]store.Entry, error)
	ExportCSV(w io.Writer) error
	ExportJSON(w io.Writer) error
}

type Adb interface {
	Packages(ctx context.Context, system bool) ([]adbx.Package, error)
	PackageInfo(ctx context.Context, pkg string) (adbx.PackageInfo, error)
}

type Server struct {
	Device  DeviceSource
	Queue   JobQueue
	History History
	Adb     Adb
	Paths   paths.Paths
	Static  fs.FS
	LoadCfg func() (store.Config, error)
	SaveCfg func(store.Config) error
}

// Handler merakit seluruh rute.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/device/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/device/select", s.handleSelectDevice)
	mux.HandleFunc("GET /api/apks", s.handleListAPKs)
	mux.HandleFunc("POST /api/apks/upload", s.handleUpload)
	mux.HandleFunc("POST /api/apks/url", s.handleFromURL)
	mux.HandleFunc("GET /api/packages", s.handlePackages)
	mux.HandleFunc("POST /api/packages/detail", s.handlePackageDetail)
	mux.HandleFunc("POST /api/jobs", s.handleCreateJobs)
	mux.HandleFunc("POST /api/jobs/cancel", s.handleCancelJob)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/history/export", s.handleExport)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("POST /api/config", s.handleConfig)
	mux.Handle("GET /", http.FileServerFS(s.Static))
	return localOnly(mux)
}

// loopbackHost mengembalikan true bila host (tanpa skema, boleh dengan port)
// menunjuk ke komputer ini. Dipakai untuk memeriksa Host maupun Origin.
func loopbackHost(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	host = strings.TrimSuffix(host, ".")
	return strings.EqualFold(host, "127.0.0.1") ||
		strings.EqualFold(host, "localhost") ||
		strings.EqualFold(host, "::1")
}

// loopbackOrigin memeriksa header Origin: harus skema http dan host lokal.
func loopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	return loopbackHost(u.Host)
}

// localOnly menjaga server yang hanya mendengarkan 127.0.0.1 supaya tidak bisa
// dipakai halaman web lain. Selain memeriksa Host, ia menolak permintaan lintas
// situs dari peramban lewat header Origin dan Sec-Fetch-Site.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "hanya bisa diakses dari komputer ini", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !loopbackOrigin(origin) {
			http.Error(w, "permintaan lintas situs ditolak", http.StatusForbidden)
			return
		}
		switch strings.ToLower(r.Header.Get("Sec-Fetch-Site")) {
		case "", "same-origin", "none":
			// permintaan tanpa header ini (curl, skrip) tetap dilayani.
		default:
			http.Error(w, "permintaan lintas situs ditolak", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// errUnsupportedMediaType dipakai saat badan permintaan bukan application/json.
var errUnsupportedMediaType = errors.New("badan permintaan harus application/json")

// isJSONContentType memeriksa header Content-Type. Header wajib ada dan harus
// application/json; akhiran parameter seperti "; charset=utf-8" diterima.
// Ini menutup celah CSRF lewat content type yang boleh dikirim lintas situs
// tanpa preflight (text/plain, application/x-www-form-urlencoded).
func isJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return strings.EqualFold(mediaType, "application/json")
}

// decodeJSON membaca badan JSON. Bila Content-Type bukan application/json ia
// mengembalikan errUnsupportedMediaType supaya pemanggil membalas 415.
func decodeJSON(r *http.Request, v any) error {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		return errUnsupportedMediaType
	}
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	return dec.Decode(v)
}

// writeDecodeError memetakan kegagalan decodeJSON ke kode status yang tepat.
func writeDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnsupportedMediaType) {
		writeError(w, http.StatusUnsupportedMediaType, err)
		return
	}
	writeError(w, http.StatusBadRequest, fmt.Errorf("badan permintaan tidak sah: %w", err))
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	cfg := store.Config{}
	if s.LoadCfg != nil {
		if c, err := s.LoadCfg(); err == nil {
			cfg = c
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device": s.Device.Current(),
		"jobs":   s.Queue.Jobs(),
		"config": cfg,
	})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.Device.Refresh(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Device.Current())
}

// handleSelectDevice memilih perangkat aktif ketika lebih dari satu tersambung.
// Serial yang tidak dikenal dibalas 404; serial kosong dibalas 400.
func (s *Server) handleSelectDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Serial string `json:"serial"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if strings.TrimSpace(req.Serial) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("serial wajib diisi"))
		return
	}
	if err := s.Device.Select(req.Serial); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	// Pilih ulang sekarang supaya status yang dikembalikan langsung mencerminkan
	// perangkat yang baru dipilih.
	if err := s.Device.Refresh(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Device.Current())
}

func (s *Server) handlePackages(w http.ResponseWriter, r *http.Request) {
	system := r.URL.Query().Get("system") == "1"
	pkgs, err := s.Adb.Packages(r.Context(), system)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, pkgs)
}

func (s *Server) handlePackageDetail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Package string `json:"package"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.Package == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("package wajib diisi"))
		return
	}
	info, err := s.Adb.PackageInfo(r.Context(), req.Package)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type createJobsRequest struct {
	Kind           queue.Kind `json:"kind"`
	Targets        []string   `json:"targets"`
	Replace        bool       `json:"replace"`
	AllowDowngrade bool       `json:"allowDowngrade"`
}

func (s *Server) handleCreateJobs(w http.ResponseWriter, r *http.Request) {
	var req createJobsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if !knownKind(req.Kind) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("jenis job tidak dikenal: %s", req.Kind))
		return
	}
	if len(req.Targets) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("tidak ada sasaran"))
		return
	}

	created := make([]queue.Job, 0, len(req.Targets))
	for _, target := range req.Targets {
		if strings.TrimSpace(target) == "" {
			continue
		}
		created = append(created, s.Queue.Enqueue(queue.Job{
			Kind:           req.Kind,
			Target:         target,
			Label:          labelFor(req.Kind, target),
			DestDir:        s.Paths.PulledDir,
			Replace:        req.Replace,
			AllowDowngrade: req.AllowDowngrade,
		}))
	}
	writeJSON(w, http.StatusOK, created)
}

func knownKind(k queue.Kind) bool {
	switch k {
	case queue.KindInstall, queue.KindUninstall, queue.KindUninstallKeep,
		queue.KindClearData, queue.KindPull:
		return true
	}
	return false
}

func labelFor(k queue.Kind, target string) string {
	base := target
	if i := strings.LastIndex(target, "/"); i >= 0 {
		base = target[i+1:]
	}
	switch k {
	case queue.KindInstall:
		return "Pasang " + base
	case queue.KindUninstall:
		return "Copot " + base
	case queue.KindUninstallKeep:
		return "Copot (simpan data) " + base
	case queue.KindClearData:
		return "Hapus data " + base
	case queue.KindPull:
		return "Tarik APK " + base
	}
	return base
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("id wajib diisi"))
		return
	}
	if err := s.Queue.Cancel(req.ID); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "dibatalkan"})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := s.History.Read(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if entries == nil {
		entries = []store.Entry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	w.Header().Set("Content-Disposition", "attachment; filename=riwayat-adbapp."+format)
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := s.History.ExportJSON(w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	default:
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		if err := s.History.ExportCSV(w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	var cfg store.Config
	if err := decodeJSON(r, &cfg); err != nil {
		writeDecodeError(w, err)
		return
	}
	if s.SaveCfg != nil {
		if err := s.SaveCfg(cfg); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, cfg)
}

// handleEvents mengalirkan perubahan job dan status perangkat sebagai SSE.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE tidak didukung", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	jobs, unsubscribe := s.Queue.Subscribe()
	defer unsubscribe()

	sendState := func() {
		payload, err := json.Marshal(map[string]any{"device": s.Device.Current()})
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: state\ndata: %s\n\n", payload)
		flusher.Flush()
	}
	sendState()

	deviceTicker := time.NewTicker(2 * time.Second)
	defer deviceTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}
			payload, err := json.Marshal(job)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: job\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-deviceTicker.C:
			sendState()
		}
	}
}
