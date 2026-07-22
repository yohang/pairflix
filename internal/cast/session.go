package cast

import (
	"context"
	"fmt"
)

// App is the subset of go-chromecast's application API pairflix uses.
type App interface {
	Start(addr string, port int) error
	Load(url string, startTime int, contentType string, transcode, detach, forceDetach bool) error
	MediaWait()
	Close(stopMedia bool) error
}

// Caster plays a stream URL on a Chromecast device.
type Caster struct {
	// NewApp builds a cast session. Injectable for tests; defaults to the
	// real go-chromecast application.
	NewApp func() App
}

// NewCaster returns a Caster backed by go-chromecast.
func NewCaster() *Caster {
	return &Caster{NewApp: newRealApp}
}

// Run casts streamURL to dev and blocks until playback ends or ctx is
// canceled. Playback ending on the device side (finished, stopped, app
// closed) is a normal return. Cancellation stops the receiver app.
func (c *Caster) Run(ctx context.Context, dev Device, streamURL, contentType string) error {
	app := c.NewApp()

	if err := app.Start(dev.Addr, dev.Port); err != nil {
		return fmt.Errorf("cast: connect to %s (%s): %w", dev.Name, dev.Addr, err)
	}

	if err := app.Load(streamURL, 0, contentType, false, false, false); err != nil {
		_ = app.Close(true)

		return fmt.Errorf("cast: load media on %s: %w", dev.Name, err)
	}

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
	case <-ctx.Done():
		_ = app.Close(true)

		return fmt.Errorf("cast: session canceled: %w", ctx.Err())
	}
}
