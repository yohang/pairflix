package cast

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ErrNoDevices is returned when discovery finds no Chromecast video device.
var ErrNoDevices = errors.New("cast: no Chromecast video devices found on the network")

// Discoverer finds Chromecast video devices via mDNS.
type Discoverer struct {
	// Timeout is the discovery window. Discovery always waits the full
	// window: zeroconf can miss the first query, and returning early
	// would break multi-device selection.
	Timeout time.Duration
	// DiscoverFn streams raw mDNS entries. Injectable for tests; defaults
	// to go-chromecast's dns package over all interfaces.
	DiscoverFn func(ctx context.Context) (<-chan Entry, error)
}

// NewDiscoverer returns a Discoverer using real mDNS with a 5s window.
func NewDiscoverer() *Discoverer {
	return &Discoverer{
		Timeout:    5 * time.Second,
		DiscoverFn: discoverDNS,
	}
}

// discoveryRounds is how many fresh mDNS queries a discovery window runs:
// a single resolver misses announcements often (observed: identical queries
// returning different subsets of the same network), so the window is split
// into rounds and the results merged.
const discoveryRounds = 2

// Devices runs discoveryRounds mDNS queries across the timeout window,
// keeps video-capable devices, dedupes by UUID and returns them sorted by
// name. Individual round failures are tolerated as long as one succeeds.
func (d *Discoverer) Devices(ctx context.Context) ([]Device, error) {
	seen := make(map[string]struct{})

	var (
		devices  []Device
		errs     []error
		perRound = d.Timeout / discoveryRounds
	)

	for range discoveryRounds {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cast: discovery: %w", err)
		}

		roundCtx, cancel := context.WithTimeout(ctx, perRound)

		entries, err := d.DiscoverFn(roundCtx)
		if err != nil {
			cancel()

			errs = append(errs, err)

			continue
		}

		for entry := range entries {
			if _, dup := seen[entry.UUID]; dup {
				continue
			}

			seen[entry.UUID] = struct{}{}

			if !isVideoDevice(entry.CA, entry.Model) {
				continue
			}

			devices = append(devices, Device{
				Name:  entry.Name,
				Model: entry.Model,
				Addr:  entry.Addr,
				Port:  entry.Port,
			})
		}

		cancel()
	}

	if len(errs) == discoveryRounds {
		return nil, fmt.Errorf("cast: discovery: %w", errors.Join(errs...))
	}

	if len(devices) == 0 {
		return nil, ErrNoDevices
	}

	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })

	return devices, nil
}

// ByName returns the device whose friendly name matches name
// (case-insensitive). The error lists the devices that were found.
func (d *Discoverer) ByName(ctx context.Context, name string) (Device, error) {
	devices, err := d.Devices(ctx)
	if err != nil {
		return Device{}, err
	}

	var found []string

	for _, dev := range devices {
		if strings.EqualFold(dev.Name, name) {
			return dev, nil
		}

		found = append(found, dev.Name)
	}

	return Device{}, fmt.Errorf("cast: device %q not found; discovered: %s",
		name, strings.Join(found, ", "))
}
