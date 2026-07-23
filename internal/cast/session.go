package cast

import (
	"context"
	"fmt"
	"time"
)

// MediaStatus is a snapshot of playback state on the device.
type MediaStatus struct {
	// State is the receiver player state: CONNECTED (synthesized on
	// session start), PLAYING, PAUSED, BUFFERING, IDLE, DISCONNECTED
	// (synthesized when the receiver app disappears).
	State string
	// Reason qualifies IDLE: FINISHED, CANCELLED, INTERRUPTED, ERROR.
	Reason   string
	Position time.Duration
	Duration time.Duration
}

// Synthetic states emitted around the receiver's own player states.
const (
	// StateConnected is emitted once the control connection to the
	// device is established.
	StateConnected = "CONNECTED"
	// StateDisconnected is emitted when the receiver app disappears
	// (closed on the TV, crashed, or gave up on the stream).
	StateDisconnected = "DISCONNECTED"
)

// goneThreshold is how many consecutive polls without media status (after
// media was seen at least once) declare the receiver session dead.
const goneThreshold = 3

// noMediaTimeout is how long after Load the receiver may show no media
// status at all before the session is declared dead (receiver wedged or
// silently dropped the load).
const noMediaTimeout = 90 * time.Second

// App is the subset of go-chromecast's application API pairflix uses.
type App interface {
	Start(addr string, port int) error
	Load(url string, startTime int, contentType string, transcode, detach, forceDetach bool) error
	Update() error
	MediaStatus() (MediaStatus, bool)
	MediaWait()
	Close(stopMedia bool) error
}

// loadAttempts is how many times Load is tried. go-chromecast waits only 5s
// for the receiver app to launch, but a Chromecast with Google TV routinely
// needs longer to start it; the launch continues device-side after the
// client timeout, so a retry attaches to the now-running app.
const loadAttempts = 3

// Caster plays a stream URL on a Chromecast device.
type Caster struct {
	// NewApp builds a cast session. Injectable for tests; defaults to the
	// real go-chromecast application.
	NewApp func() App
	// RetryDelay is the pause between Load attempts.
	RetryDelay time.Duration
	// StatusInterval is how often playback status is polled when
	// OnStatus is set.
	StatusInterval time.Duration
	// OnStatus, when non-nil, receives a synthetic CONNECTED status after
	// the connection is established and a playback status snapshot every
	// StatusInterval. Called from a single goroutine.
	OnStatus func(MediaStatus)
}

// NewCaster returns a Caster backed by go-chromecast.
func NewCaster() *Caster {
	return &Caster{
		NewApp:         newRealApp,
		RetryDelay:     2 * time.Second,
		StatusInterval: 2 * time.Second,
	}
}

// Run casts streamURL to dev and blocks until playback ends or ctx is
// canceled. Playback ending on the device side (finished, stopped, app
// closed) is a normal return. Cancellation stops the receiver app.
func (c *Caster) Run(ctx context.Context, dev Device, streamURL, contentType string) error {
	app := c.NewApp()

	if err := app.Start(dev.Addr, dev.Port); err != nil {
		return fmt.Errorf("cast: connect to %s (%s): %w", dev.Name, dev.Addr, err)
	}

	if c.OnStatus != nil {
		c.OnStatus(MediaStatus{State: StateConnected})
	}

	if err := c.load(ctx, app, streamURL, contentType); err != nil {
		_ = app.Close(true)

		return fmt.Errorf("cast: load media on %s: %w", dev.Name, err)
	}

	// gone is closed by the status poller when the receiver app
	// disappears — MediaWait alone does not notice a closed app.
	gone := make(chan struct{})

	pollCtx, stopPoll := context.WithCancel(ctx)
	defer stopPoll()

	go c.pollStatus(pollCtx, app, gone)

	// MediaWait has no context support; Close below unblocks it by
	// tearing down the connection.
	done := make(chan struct{})

	go func() {
		app.MediaWait()
		close(done)
	}()

	select {
	case <-done:
		_ = app.Close(true)

		return nil
	case <-gone:
		if c.OnStatus != nil {
			c.OnStatus(MediaStatus{State: StateDisconnected})
		}

		_ = app.Close(true)

		return nil
	case <-ctx.Done():
		_ = app.Close(true)

		return fmt.Errorf("cast: session canceled: %w", ctx.Err())
	}
}

// pollStatus refreshes device state every StatusInterval, reports playback
// snapshots to OnStatus, and closes gone when the receiver app vanishes
// after having shown media at least once.
func (c *Caster) pollStatus(ctx context.Context, app App, gone chan<- struct{}) {
	interval := c.StatusInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	deadline := time.NewTimer(noMediaTimeout)
	defer deadline.Stop()

	var (
		sawMedia bool
		misses   int
	)

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			if !sawMedia {
				close(gone)

				return
			}
		case <-ticker.C:
			_ = app.Update()

			status, ok := app.MediaStatus()
			if ok {
				sawMedia = true
				misses = 0

				if c.OnStatus != nil {
					c.OnStatus(status)
				}

				continue
			}

			if !sawMedia {
				continue
			}

			misses++
			if misses >= goneThreshold {
				close(gone)

				return
			}
		}
	}
}

// load tries Load up to loadAttempts times, refreshing the application
// state between attempts so a slowly-launching receiver is picked up.
func (c *Caster) load(ctx context.Context, app App, streamURL, contentType string) error {
	var lastErr error

	for attempt := 1; attempt <= loadAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-time.After(c.RetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}

			_ = app.Update()
		}

		lastErr = app.Load(streamURL, 0, contentType, false, false, false)
		if lastErr == nil {
			return nil
		}
	}

	return fmt.Errorf("after %d attempts: %w", loadAttempts, lastErr)
}
