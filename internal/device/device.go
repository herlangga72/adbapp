// Package device memantau perangkat Android yang tersambung dan memilih satu
// perangkat aktif.
package device

import (
	"context"
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
	State  State    `json:"state"`
	Serial string   `json:"serial"`
	Model  string   `json:"model"`
	Others []string `json:"others,omitempty"`
}

type lister interface {
	Devices(ctx context.Context) ([]adbx.Device, error)
}

// Monitor memilih satu perangkat aktif dan mengingat pilihannya selama
// perangkat itu masih tersambung.
type Monitor struct {
	lister  lister
	mu      sync.RWMutex
	current Status
}

func New(l lister) *Monitor {
	return &Monitor{lister: l, current: Status{State: StateNone}}
}

// Refresh meminta daftar perangkat terbaru dan memperbarui status.
func (m *Monitor) Refresh(ctx context.Context) error {
	devices, err := m.lister.Devices(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = pick(devices, m.current.Serial)
	return nil
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
