package device

import (
	"context"
	"testing"

	"github.com/herlangga72/adbapp/internal/adbx"
)

type fakeLister struct {
	devices []adbx.Device
	err     error
}

func (f fakeLister) Devices(ctx context.Context) ([]adbx.Device, error) {
	return f.devices, f.err
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

func TestStatusUnauthorizedTakesPriority(t *testing.T) {
	m := New(fakeLister{devices: []adbx.Device{{Serial: "S1", State: "unauthorized"}}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Current().State != StateUnauthorized {
		t.Fatalf("got %v, want unauthorized", m.Current().State)
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
	m := New(fakeLister{devices: []adbx.Device{
		{Serial: "S1", State: "device"},
		{Serial: "S2", State: "device"},
	}})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := m.Current().Serial
	for i := 0; i < 3; i++ {
		if err := m.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if m.Current().Serial != first {
		t.Fatalf("serial berubah dari %q ke %q padahal masih tersambung",
			first, m.Current().Serial)
	}
}
