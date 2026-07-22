package cast

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeApp records calls and lets tests control MediaWait.
type fakeApp struct {
	mu        sync.Mutex
	startErr  error
	loadErr   error
	loaded    bool
	closed    bool
	stopMedia bool
	waitCh    chan struct{}
}

func newFakeApp() *fakeApp {
	return &fakeApp{waitCh: make(chan struct{})}
}

func (f *fakeApp) Start(string, int) error { return f.startErr }

func (f *fakeApp) Load(string, int, string, bool, bool, bool) error {
	f.mu.Lock()
	f.loaded = true
	f.mu.Unlock()

	return f.loadErr
}

func (f *fakeApp) MediaWait() { <-f.waitCh }

func (f *fakeApp) Close(stopMedia bool) error {
	f.mu.Lock()
	f.closed = true
	f.stopMedia = stopMedia
	f.mu.Unlock()

	// Closing the connection unblocks MediaWait, like the real client.
	select {
	case <-f.waitCh:
	default:
		close(f.waitCh)
	}

	return nil
}

func casterFor(app App) *Caster {
	return &Caster{NewApp: func() App { return app }}
}

var testDevice = Device{Name: "TV", Model: "Chromecast", Addr: "192.168.1.20", Port: 8009}

func TestRunStartError(t *testing.T) {
	t.Parallel()

	app := newFakeApp()
	app.startErr = errors.New("connection refused")

	err := casterFor(app).Run(context.Background(), testDevice, "http://x/s.mp4", "video/mp4")
	if err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("error = %v, want connect error", err)
	}
}

func TestRunLoadErrorStillCloses(t *testing.T) {
	t.Parallel()

	app := newFakeApp()
	app.loadErr = errors.New("LOAD_FAILED")

	err := casterFor(app).Run(context.Background(), testDevice, "http://x/s.mkv", "video/x-matroska")
	if err == nil {
		t.Fatal("expected load error")
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	if !app.closed || !app.stopMedia {
		t.Errorf("closed=%v stopMedia=%v, want Close(true) after load failure", app.closed, app.stopMedia)
	}
}

func TestRunPlaybackFinished(t *testing.T) {
	t.Parallel()

	app := newFakeApp()
	close(app.waitCh) // media finishes immediately

	if err := casterFor(app).Run(context.Background(), testDevice, "http://x/s.mp4", "video/mp4"); err != nil {
		t.Fatalf("Run: %v, want nil on normal playback end", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	if !app.loaded || !app.closed {
		t.Errorf("loaded=%v closed=%v, want both true", app.loaded, app.closed)
	}
}

func TestRunContextCancel(t *testing.T) {
	t.Parallel()

	app := newFakeApp()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := casterFor(app).Run(ctx, testDevice, "http://x/s.mp4", "video/mp4")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	if !app.closed || !app.stopMedia {
		t.Errorf("closed=%v stopMedia=%v, want Close(true) on cancel", app.closed, app.stopMedia)
	}
}
