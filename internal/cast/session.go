package cast

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	Pause() error
	Unpause() error
	StopMedia() error
	Close(stopMedia bool) error
}

// loadAttempts is how many times Load is tried. go-chromecast waits only 5s
// for the receiver app to launch, but a Chromecast with Google TV routinely
// needs longer to start it; the launch continues device-side after the
// client timeout, so a retry attaches to the now-running app.
const loadAttempts = 3

// ErrNoSession is returned by playback controls when no cast session is
// active.
var ErrNoSession = errors.New("cast: no active session")

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

	// mu guards app, which is set while a Run session is active so the
	// playback controls below can reach it from other goroutines.
	mu  sync.Mutex
	app App
}

// setApp records (or clears, with nil) the active session app.
func (c *Caster) setApp(app App) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.app = app
}

// control runs f against the active session app, or returns ErrNoSession.
func (c *Caster) control(f func(App) error) error {
	c.mu.Lock()
	app := c.app
	c.mu.Unlock()

	if app == nil {
		return ErrNoSession
	}

	return f(app)
}

// Pause pauses playback on the active session.
func (c *Caster) Pause() error {
	return c.control(App.Pause)
}

// Unpause resumes playback on the active session.
func (c *Caster) Unpause() error {
	return c.control(App.Unpause)
}

// Stop stops the media on the active session; the session then ends the
// usual way (MediaWait returns or the receiver goes idle).
func (c *Caster) Stop() error {
	return c.control(App.StopMedia)
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

	c.setApp(app)
	defer c.setApp(nil)

	if c.OnStatus != nil {
		c.OnStatus(MediaStatus{State: StateConnected})
	}

	if err := c.load(ctx, app, streamURL, contentType); err != nil {
		closeBounded(app)

		return fmt.Errorf("cast: load media on %s: %w", dev.Name, err)
	}

	// The status poller is the session's lifecycle authority: ended is
	// closed when playback reaches a terminal idle state, gone when the
	// receiver app disappears. (go-chromecast's MediaWait is deliberately
	// unused: it can neither be canceled nor unblocked externally.)
	ended := make(chan struct{})
	gone := make(chan struct{})

	pollCtx, stopPoll := context.WithCancel(ctx)
	defer stopPoll()

	go c.pollStatus(pollCtx, app, ended, gone)

	select {
	case <-ended:
		closeBounded(app)

		return nil
	case <-gone:
		if c.OnStatus != nil {
			c.OnStatus(MediaStatus{State: StateDisconnected})
		}

		closeBounded(app)

		return nil
	case <-ctx.Done():
		closeBounded(app)

		return fmt.Errorf("cast: session canceled: %w", ctx.Err())
	}
}

// closeBoundedGrace bounds how long a session teardown may take:
// go-chromecast's Close can block indefinitely against an unresponsive
// device, and callers (like the web mode) must not hang on Stop.
const closeBoundedGrace = 5 * time.Second

// closeBounded closes app, abandoning the attempt (and its goroutine)
// after closeBoundedGrace.
func closeBounded(app App) {
	done := make(chan struct{})

	go func() {
		_ = app.Close(true)

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(closeBoundedGrace):
	}
}

// terminalIdleReasons are the receiver idle reasons that end a session.
var terminalIdleReasons = map[string]bool{
	"FINISHED": true, "CANCELLED": true, "INTERRUPTED": true, "ERROR": true,
}

// pollStatus refreshes device state every StatusInterval and reports
// playback snapshots to OnStatus. It closes ended when playback reaches a
// terminal idle state and gone when the receiver app vanishes after having
// shown media at least once.
func (c *Caster) pollStatus(ctx context.Context, app App, ended, gone chan<- struct{}) {
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

				if status.State == "IDLE" && terminalIdleReasons[status.Reason] {
					close(ended)

					return
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

		// detach=true: for URL media, go-chromecast otherwise blocks
		// inside Load until playback finishes (it calls MediaWait), which
		// would make the session uncancelable.
		lastErr = app.Load(streamURL, 0, contentType, false, true, false)
		if lastErr == nil {
			return nil
		}
	}

	return fmt.Errorf("after %d attempts: %w", loadAttempts, lastErr)
}
