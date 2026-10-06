package device

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
)

type fakeLister struct {
	devices []adbx.Device
	err     error
}

func (f fakeLister) Devices(ctx context.Context) ([]adbx.Device, error) {
	return f.devices, f.err
}

// scriptedLister mengembalikan daftar perangkat yang berbeda pada tiap
// pemanggilan, sehingga dapat menguji perilaku lintas-refresh.
type scriptedLister struct {
	lists [][]adbx.Device
	calls int
	err   error
}

func (s *scriptedLister) Devices(ctx context.Context) ([]adbx.Device, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.lists) == 0 {
		return nil, nil
	}
	if s.calls >= len(s.lists) {
		return s.lists[len(s.lists)-1], nil
	}
	list := s.lists[s.calls]
	s.calls++
	return list, nil
}

func TestStatusReadyWhenOneAuthorized(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device", Model: "Pixel"}}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := m.Current()
	if got.State != StateReady {
		t.Fatalf("got %v, want ready", got.State)
	}
	if got.Serial != "S1" {
		t.Fatalf("serial salah: %q", got.Serial)
	}
}

func TestStatusUnauthorizedWhenNoReadyDevice(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "unauthorized"}}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Current().State != StateUnauthorized {
		t.Fatalf("got %v, want unauthorized", m.Current().State)
	}
}

func TestStatusReadyTakesPriorityOverUnauthorized(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{
		{Serial: "S1", State: "unauthorized"},
		{Serial: "S2", State: "device"},
	}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := m.Current()
	if got.State != StateReady || got.Serial != "S2" {
		t.Fatalf("got %v/%q, want ready/S2", got.State, got.Serial)
	}
}

func TestStatusNoneWhenEmpty(t *testing.T) {
	m := New(fakeLister{})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Current().State != StateNone {
		t.Fatalf("got %v, want none", m.Current().State)
	}
}

func TestStatusOfflineWhenOnlyOffline(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "offline"}}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Current().State != StateOffline {
		t.Fatalf("got %v, want offline", m.Current().State)
	}
}

func TestRepeatedRefreshKeepsSameSerial(t *testing.T) {
	l := &scriptedLister{lists: [][]adbx.Device{
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
		// Urutan dibalik: S1 harus tetap terpilih (lengket, bukan first-wins).
		{{Serial: "S2", State: "device"}, {Serial: "S1", State: "device"}},
		// S1 dicabut: sekarang S2 yang dipakai.
		{{Serial: "S2", State: "device"}},
	}}
	m := New(l)

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("refresh 1: got %q, want S1", got)
	}

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("refresh 2 (urutan dibalik): got %q, want tetap S1", got)
	}

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S2" {
		t.Fatalf("refresh 3 (S1 dicabut): got %q, want S2", got)
	}
}

func TestRefreshMovesAwayWhenPreferredOffline(t *testing.T) {
	l := &scriptedLister{lists: [][]adbx.Device{
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
		// S1 masih terdaftar tetapi offline; cabang mengingat mensyaratkan
		// state == "device", jadi pilihan harus pindah ke S2.
		{{Serial: "S1", State: "offline"}, {Serial: "S2", State: "device"}},
	}}
	m := New(l)

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("refresh 1: got %q, want S1", got)
	}

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := m.Current()
	if got.Serial != "S2" || got.State != StateReady {
		t.Fatalf("refresh 2: got %q/%v, want S2/ready", got.Serial, got.State)
	}
}

func TestModelFallsBackToProduct(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{
		{Serial: "S1", State: "device", Product: "PixelProduct"},
	}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Model; got != "PixelProduct" {
		t.Fatalf("Model = %q, want PixelProduct", got)
	}
}

func TestOthersListsOtherSerials(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{
		{Serial: "S1", State: "device"},
		{Serial: "S2", State: "offline"},
		{Serial: "S3", State: "unauthorized"},
	}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	others := m.Current().Others
	if len(others) != 2 || others[0] != "S2" || others[1] != "S3" {
		t.Fatalf("Others = %v, want [S2 S3]", others)
	}
}

func TestOthersEmptyForSingleDevice(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Others; len(got) != 0 {
		t.Fatalf("Others = %v, want kosong", got)
	}
}

func TestRefreshErrorLeavesCurrentUnchanged(t *testing.T) {
	l := &fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}}
	m := New(l)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	l.err = errors.New("adb gagal")
	if err := m.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh: ingin error, dapat nil")
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("Current().Serial = %q, want tetap S1", got)
	}
}

func TestVersionGetterCalledOncePerSerial(t *testing.T) {
	calls := 0
	m := New(
		fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}},
		WithVersionGetter(func(ctx context.Context, serial string) (string, error) {
			calls++
			if serial != "S1" {
				t.Fatalf("serial = %q, mau S1", serial)
			}
			return "13", nil
		}),
	)
	for i := 0; i < 3; i++ {
		if err := m.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("getter dipanggil %d kali, mau 1", calls)
	}
	if got := m.Current().AndroidVersion; got != "13" {
		t.Fatalf("AndroidVersion = %q, mau 13", got)
	}
}

func TestVersionGetterErrorIsTolerated(t *testing.T) {
	calls := 0
	m := New(
		fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}},
		WithVersionGetter(func(ctx context.Context, serial string) (string, error) {
			calls++
			return "", errors.New("getprop gagal")
		}),
	)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh kedua: %v", err)
	}
	if calls != 1 {
		t.Fatalf("getter dipanggil %d kali, mau 1", calls)
	}
	got := m.Current()
	if got.AndroidVersion != "" {
		t.Fatalf("AndroidVersion = %q, mau kosong", got.AndroidVersion)
	}
	if got.State != StateReady {
		t.Fatalf("State = %v, mau ready", got.State)
	}
}

func TestVersionGetterNotCalledWhenNotReady(t *testing.T) {
	calls := 0
	m := New(
		fakeLister{devices: []adbx.Device{{Serial: "S1", State: "unauthorized"}}},
		WithVersionGetter(func(ctx context.Context, serial string) (string, error) {
			calls++
			return "13", nil
		}),
	)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("getter dipanggil %d kali, mau 0", calls)
	}
}

func TestSelectPinsChosenDevice(t *testing.T) {
	l := &scriptedLister{lists: [][]adbx.Device{
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
	}}
	m := New(l)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("refresh 1: got %q, mau S1", got)
	}
	if err := m.Select("S2"); err != nil {
		t.Fatalf("Select gagal: %v", err)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S2" {
		t.Fatalf("setelah pilih: got %q, mau S2", got)
	}
}

func TestSelectClearedWhenDeviceDisappears(t *testing.T) {
	l := &scriptedLister{lists: [][]adbx.Device{
		{{Serial: "S1", State: "device"}, {Serial: "S2", State: "device"}},
		// S2 dicabut: pin harus dilepas dan pilihan jatuh ke S1.
		{{Serial: "S1", State: "device"}},
	}}
	m := New(l)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Select("S2"); err != nil {
		t.Fatalf("Select gagal: %v", err)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Current().Serial; got != "S1" {
		t.Fatalf("got %q, mau S1", got)
	}
	// Pin sudah dilepas, jadi serial lama sekarang tidak dikenal.
	if err := m.Select("S2"); err == nil {
		t.Fatal("Select S2 seharusnya error setelah pin dilepas")
	}
}

func TestSelectUnknownSerialErrors(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}})
	if err := m.Select("hantu"); err == nil {
		t.Fatal("Select serial tak dikenal seharusnya error")
	}
}

func TestLoopZeroIntervalDoesNotPanic(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "device"}}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // batalkan segera; Loop cukup tidak boleh panik.

	done := make(chan struct{})
	go func() {
		m.Loop(ctx, 0)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Loop tidak berhenti setelah ctx dibatalkan")
	}
}

// TestMonitorConcurrentRefreshAndRead menjalankan Loop (Refresh berkala) dan
// pembacaan status dari banyak goroutine sekaligus. Pembacaan meniru handler
// SSE yang melakukan Marshal dan pembandingan DeepEqual atas Current(), supaya
// pola pemakaian nyata ikut teruji di bawah -race.
func TestMonitorConcurrentRefreshAndRead(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{
		{Serial: "S1", State: "device", Model: "Pixel"},
		{Serial: "S2", State: "device", Model: "Nexus"},
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Loop(ctx, time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				st := m.Current()
				if _, err := json.Marshal(st); err != nil {
					t.Errorf("marshal status: %v", err)
					return
				}
				if !reflect.DeepEqual(st, st) {
					t.Errorf("DeepEqual tidak refleksif: %+v", st)
					return
				}
			}
		}()
	}
	wg.Wait()
}
