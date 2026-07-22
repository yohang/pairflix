package cast

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func fakeDiscoverer(entries ...Entry) *Discoverer {
	return &Discoverer{
		Timeout: 100 * time.Millisecond,
		DiscoverFn: func(ctx context.Context) (<-chan Entry, error) {
			ch := make(chan Entry, len(entries))

			for _, e := range entries {
				ch <- e
			}

			close(ch)

			return ch, nil
		},
	}
}

func TestDevicesFiltersAndSorts(t *testing.T) {
	t.Parallel()

	d := fakeDiscoverer(
		Entry{UUID: "b", Name: "Bedroom TV", Model: "Chromecast", CA: "5", Addr: "192.168.1.11", Port: 8009},
		Entry{UUID: "a", Name: "Kitchen speaker", Model: "Google Home Mini", CA: "4", Addr: "192.168.1.12", Port: 8009},
		Entry{UUID: "c", Name: "Living Room TV", Model: "Google TV", CA: "199691", Addr: "192.168.1.13", Port: 8009},
	)

	devices, err := d.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}

	if len(devices) != 2 {
		t.Fatalf("len = %d, want 2 (audio device filtered)", len(devices))
	}

	if devices[0].Name != "Bedroom TV" || devices[1].Name != "Living Room TV" {
		t.Errorf("order = %q, %q; want sorted by name", devices[0].Name, devices[1].Name)
	}
}

func TestDevicesDedupesByUUID(t *testing.T) {
	t.Parallel()

	entry := Entry{UUID: "same", Name: "TV", Model: "Chromecast", CA: "5", Addr: "192.168.1.11", Port: 8009}
	d := fakeDiscoverer(entry, entry, entry)

	devices, err := d.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}

	if len(devices) != 1 {
		t.Errorf("len = %d, want 1 after dedupe", len(devices))
	}
}

func TestDevicesNoneFound(t *testing.T) {
	t.Parallel()

	d := fakeDiscoverer(
		Entry{UUID: "a", Name: "Speaker", Model: "Chromecast Audio", CA: "4"},
	)

	if _, err := d.Devices(context.Background()); !errors.Is(err, ErrNoDevices) {
		t.Errorf("error = %v, want ErrNoDevices", err)
	}
}

func TestDevicesDiscoveryError(t *testing.T) {
	t.Parallel()

	d := &Discoverer{
		Timeout: time.Second,
		DiscoverFn: func(context.Context) (<-chan Entry, error) {
			return nil, errors.New("mdns broken")
		},
	}

	if _, err := d.Devices(context.Background()); err == nil {
		t.Error("expected error when discovery fails")
	}
}

func TestByName(t *testing.T) {
	t.Parallel()

	d := fakeDiscoverer(
		Entry{UUID: "a", Name: "Living Room TV", Model: "Chromecast", CA: "5", Addr: "192.168.1.11", Port: 8009},
		Entry{UUID: "b", Name: "Bedroom TV", Model: "Chromecast", CA: "5", Addr: "192.168.1.12", Port: 8009},
	)

	dev, err := d.ByName(context.Background(), "living room tv")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}

	if dev.Addr != "192.168.1.11" {
		t.Errorf("Addr = %q, want 192.168.1.11", dev.Addr)
	}
}

func TestByNameNotFound(t *testing.T) {
	t.Parallel()

	d := fakeDiscoverer(
		Entry{UUID: "a", Name: "Bedroom TV", Model: "Chromecast", CA: "5"},
	)

	_, err := d.ByName(context.Background(), "Garage TV")
	if err == nil {
		t.Fatal("expected error for unknown device")
	}

	if !strings.Contains(err.Error(), "Bedroom TV") {
		t.Errorf("error should list discovered devices, got %q", err)
	}
}
