// Package device memantau perangkat Android yang tersambung dan memilih satu
// perangkat aktif.
package device

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
)

type State string

const (
	StateReady        State = "ready"
	StateUnauthorized State = "unauthorized"
	StateOffline      State = "offline"
	StateNone         State = "none"
)

// Status adalah kondisi perangkat aktif saat ini.
type Status struct {
	State          State    `json:"state"`
	Serial         string   `json:"serial"`
	Model          string   `json:"model"`
	AndroidVersion string   `json:"androidVersion"`
	Others         []string `json:"others,omitempty"`
}

type lister interface {
	Devices(ctx context.Context) ([]adbx.Device, error)
}

// VersionGetter mengambil versi Android (mis. dari getprop) untuk satu serial.
type VersionGetter func(ctx context.Context, serial string) (string, error)

// Option mengubah perilaku Monitor.
type Option func(*Monitor)

// WithVersionGetter memasang pengambil versi Android. Getter dipanggil sekali
// per serial (hasilnya di-cache); kegagalan diabaikan dan versi dibiarkan
// kosong supaya pemantauan tidak pernah terhenti karenanya. Getter nil diabaikan.
func WithVersionGetter(g VersionGetter) Option {
	return func(m *Monitor) {
		if g != nil {
			m.versionGetter = g
		}
	}
}

// Monitor memilih satu perangkat aktif dan mengingat pilihannya selama
// perangkat itu masih tersambung.
type Monitor struct {
	lister        lister
	versionGetter VersionGetter
	mu            sync.RWMutex
	current       Status
	selected      string
	versions      map[string]string
}

func New(l lister, opts ...Option) *Monitor {
	m := &Monitor{
		lister:   l,
		current:  Status{State: StateNone},
		versions: map[string]string{},
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Refresh meminta daftar perangkat terbaru dan memperbarui status.
func (m *Monitor) Refresh(ctx context.Context) error {
	devices, err := m.lister.Devices(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	preferred := m.current.Serial
	if m.selected != "" {
		if isReadySerial(devices, m.selected) {
			preferred = m.selected
		} else {
			// Pin mengarah ke perangkat yang sudah hilang atau tidak siap:
			// lepaskan supaya pilihan kembali lengket ke perangkat aktif.
			m.selected = ""
		}
	}
	st := pick(devices, preferred)
	m.current = st
	var fetchSerial string
	needFetch := false
	if st.State == StateReady {
		if v, ok := m.versions[st.Serial]; ok {
			m.current.AndroidVersion = v
		} else {
			fetchSerial = st.Serial
			needFetch = true
		}
	}
	m.mu.Unlock()

	if needFetch && m.versionGetter != nil {
		version, verr := m.versionGetter(ctx, fetchSerial)
		if verr != nil {
			version = ""
		}
		m.mu.Lock()
		m.versions[fetchSerial] = version
		if m.current.Serial == fetchSerial {
			m.current.AndroidVersion = version
		}
		m.mu.Unlock()
	}
	return nil
}

// isReadySerial melaporkan apakah serial ada di daftar dan berstatus "device".
func isReadySerial(devices []adbx.Device, serial string) bool {
	for _, d := range devices {
		if d.Serial == serial && d.State == "device" {
			return true
		}
	}
	return false
}

// Select memilih perangkat aktif secara eksplisit. Serial harus dikenal pada
// status terakhir (perangkat aktif atau salah satu pada Others); selain itu
// dikembalikan error. Pin dihormati oleh Refresh selama perangkat itu hadir dan
// siap, lalu dilepas otomatis bila menghilang.
func (m *Monitor) Select(serial string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if serial == "" {
		return errors.New("serial kosong")
	}
	if serial == m.current.Serial {
		m.selected = serial
		return nil
	}
	for _, s := range m.current.Others {
		if s == serial {
			m.selected = serial
			return nil
		}
	}
	return fmt.Errorf("perangkat %s tidak dikenal", serial)
}

func pick(devices []adbx.Device, preferred string) Status {
	if len(devices) == 0 {
		return Status{State: StateNone}
	}

	// Pertahankan perangkat yang sedang dipakai bila masih ada.
	if preferred != "" {
		for _, d := range devices {
			if d.Serial == preferred && d.State == "device" {
				return build(d, devices)
			}
		}
	}

	for _, d := range devices {
		if d.State == "device" {
			return build(d, devices)
		}
	}
	for _, d := range devices {
		if d.State == "unauthorized" {
			return build(d, devices)
		}
	}
	return build(devices[0], devices)
}

func build(chosen adbx.Device, all []adbx.Device) Status {
	st := Status{Serial: chosen.Serial, Model: chosen.Model}
	switch chosen.State {
	case "device":
		st.State = StateReady
	case "unauthorized":
		st.State = StateUnauthorized
	default:
		st.State = StateOffline
	}
	if st.Model == "" {
		st.Model = chosen.Product
	}
	for _, d := range all {
		if d.Serial != chosen.Serial {
			st.Others = append(st.Others, d.Serial)
		}
	}
	return st
}

// Current mengembalikan status terakhir yang diketahui.
func (m *Monitor) Current() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// defaultLoopInterval dipakai bila Loop diberikan interval tidak positif,
// sebab time.NewTicker panik untuk durasi <= 0.
const defaultLoopInterval = 2 * time.Second

// loopInterval mengembalikan interval yang aman untuk time.NewTicker.
func loopInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return defaultLoopInterval
	}
	return interval
}

// Loop memantau perangkat secara berkala sampai ctx dibatalkan.
func (m *Monitor) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(loopInterval(interval))
	defer ticker.Stop()
	for {
		_ = m.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
