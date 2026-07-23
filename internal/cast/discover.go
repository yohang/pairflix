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
	// Timeout is the enumeration window for Devices. The full window is
	// always waited: zeroconf can miss the first query, and returning
	// early would break multi-device selection.
	Timeout time.Duration
	// NameTimeout caps ByName's retry loop. Some devices answer only a
	// fraction of mDNS queries (observed: 2/10), so a name search keeps
	// querying until it hits or the cap expires.
	NameTimeout time.Duration
	// DiscoverFn streams raw mDNS entries. Injectable for tests; defaults
	// to go-chromecast's dns package over all interfaces.
	DiscoverFn func(ctx context.Context) (<-chan Entry, error)
}

// NewDiscoverer returns a Discoverer using real mDNS with a 5s enumeration
// window and a 30s name-search cap.
func NewDiscoverer() *Discoverer {
	return &Discoverer{
		Timeout:     5 * time.Second,
		NameTimeout: 30 * time.Second,
		DiscoverFn:  discoverDNS,
	}
}

// discoveryRounds is how many fresh mDNS queries a discovery window runs:
// a single resolver misses announcements often (observed: identical queries
// returning different subsets of the same network), so the window is split
// into rounds and the results merged.
const discoveryRounds = 2

// round runs one mDNS query of the given duration and appends previously
// unseen video devices to devices, deduping by UUID via seen.
func (d *Discoverer) round(
	ctx context.Context, duration time.Duration, seen map[string]struct{}, devices []Device,
) ([]Device, error) {
	roundCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	entries, err := d.DiscoverFn(roundCtx)
	if err != nil {
		return devices, err
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

	return devices, nil
}

// Devices runs discoveryRounds mDNS queries across the timeout window,
// keeps video-capable devices, dedupes by UUID and returns them sorted by
// name. Individual round failures are tolerated as long as one succeeds.
func (d *Discoverer) Devices(ctx context.Context) ([]Device, error) {
	seen := make(map[string]struct{})

	var (
		devices []Device
		errs    []error
	)

	for range discoveryRounds {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cast: discovery: %w", err)
		}

		var err error

		devices, err = d.round(ctx, d.Timeout/discoveryRounds, seen, devices)
		if err != nil {
			errs = append(errs, err)
		}
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

// ByName searches for the device whose friendly name matches name
// (case-insensitive), re-querying until it is found or NameTimeout expires:
// flaky devices answer only a fraction of queries, so one enumeration
// window is not enough. The error lists the devices that were seen.
func (d *Discoverer) ByName(ctx context.Context, name string) (Device, error) {
	ctx, cancel := context.WithTimeout(ctx, d.NameTimeout)
	defer cancel()

	seen := make(map[string]struct{})
	perRound := d.Timeout / discoveryRounds

	var devices []Device

	for ctx.Err() == nil {
		var err error

		devices, err = d.round(ctx, perRound, seen, devices)
		if err != nil && ctx.Err() != nil {
			break
		}

		for _, dev := range devices {
			if strings.EqualFold(dev.Name, name) {
				return dev, nil
			}
		}
	}

	found := make([]string, 0, len(devices))
	for _, dev := range devices {
		found = append(found, dev.Name)
	}

	if len(found) == 0 {
		return Device{}, fmt.Errorf("cast: device %q not found: %w", name, ErrNoDevices)
	}

	return Device{}, fmt.Errorf("cast: device %q not found; discovered: %s",
		name, strings.Join(found, ", "))
}
