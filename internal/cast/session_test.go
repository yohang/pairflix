package cast

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeApp records calls and lets tests control MediaWait and Load failures.
type fakeApp struct {
	mu          sync.Mutex
	startErr    error
	loadFails   int // fail this many Load calls before succeeding
	loadCalls   int
	updates     int
	closed      bool
	stopMedia   bool
	status      MediaStatus
	statusCalls int
	vanishAfter int // report no media after this many MediaStatus calls
	waitCh      chan struct{}
}

func newFakeApp() *fakeApp {
	return &fakeApp{waitCh: make(chan struct{})}
}

func (f *fakeApp) Start(string, int) error { return f.startErr }

func (f *fakeApp) Load(string, int, string, bool, bool, bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.loadCalls++
	if f.loadCalls <= f.loadFails {
		return errors.New("unable to change to appID: context deadline exceeded")
	}

	return nil
}

func (f *fakeApp) Update() error {
	f.mu.Lock()
	f.updates++
	f.mu.Unlock()

	return nil
}

func (f *fakeApp) MediaStatus() (MediaStatus, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.statusCalls++

	if f.status.State == "" || (f.vanishAfter > 0 && f.statusCalls > f.vanishAfter) {
		return MediaStatus{}, false
	}

	return f.status, true
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
	return &Caster{NewApp: func() App { return app }, RetryDelay: 1}
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
	app.loadFails = loadAttempts // every attempt fails

	err := casterFor(app).Run(context.Background(), testDevice, "http://x/s.mkv", "video/x-matroska")
	if err == nil {
		t.Fatal("expected load error")
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	if app.loadCalls != loadAttempts {
		t.Errorf("loadCalls = %d, want %d", app.loadCalls, loadAttempts)
	}

	if !app.closed || !app.stopMedia {
		t.Errorf("closed=%v stopMedia=%v, want Close(true) after load failure", app.closed, app.stopMedia)
	}
}

func TestRunLoadRetriesSlowReceiver(t *testing.T) {
	t.Parallel()

	// First two launches time out (slow Google TV), third succeeds.
	app := newFakeApp()
	app.loadFails = 2

	close(app.waitCh) // media finishes immediately after the successful load

	if err := casterFor(app).Run(context.Background(), testDevice, "http://x/s.mp4", "video/mp4"); err != nil {
		t.Fatalf("Run: %v, want nil after retries", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	if app.loadCalls != 3 {
		t.Errorf("loadCalls = %d, want 3", app.loadCalls)
	}

	if app.updates != 2 {
		t.Errorf("updates = %d, want 2 (state refresh between attempts)", app.updates)
	}
}

func TestRunStatusCallbacks(t *testing.T) {
	t.Parallel()

	app := newFakeApp()
	app.status = MediaStatus{State: "PLAYING", Position: 5 * time.Second, Duration: time.Minute}

	var (
		mu     sync.Mutex
		states []string
	)

	caster := casterFor(app)
	caster.StatusInterval = 5 * time.Millisecond
	caster.OnStatus = func(s MediaStatus) {
		mu.Lock()

		states = append(states, s.State)

		mu.Unlock()

		// End the session once playback status has been observed.
		if s.State == "PLAYING" {
			app.Close(false) //nolint:errcheck // fake close never fails
		}
	}

	if err := caster.Run(context.Background(), testDevice, "http://x/s.mp4", "video/mp4"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(states) == 0 || states[0] != StateConnected {
		t.Fatalf("states = %v, want CONNECTED first", states)
	}

	found := false

	for _, s := range states {
		if s == "PLAYING" {
			found = true
		}
	}

	if !found {
		t.Errorf("states = %v, want PLAYING reported", states)
	}
}

func TestRunEndsWhenReceiverDies(t *testing.T) {
	t.Parallel()

	// Media plays, then the receiver app vanishes (closed on the TV or
	// gave up on the stream); Run must notice and end the session.
	app := newFakeApp()
	app.status = MediaStatus{State: "PLAYING"}
	app.vanishAfter = 2

	var (
		mu   sync.Mutex
		last string
	)

	caster := casterFor(app)
	caster.StatusInterval = 2 * time.Millisecond
	caster.OnStatus = func(s MediaStatus) {
		mu.Lock()
		defer mu.Unlock()

		last = s.State
	}

	if err := caster.Run(context.Background(), testDevice, "http://x/s.mp4", "video/mp4"); err != nil {
		t.Fatalf("Run: %v, want nil when receiver disappears", err)
	}

	app.mu.Lock()

	if !app.closed || !app.stopMedia {
		t.Errorf("closed=%v stopMedia=%v, want Close(true)", app.closed, app.stopMedia)
	}

	app.mu.Unlock()

	mu.Lock()
	defer mu.Unlock()

	if last != StateDisconnected {
		t.Errorf("last state = %q, want DISCONNECTED", last)
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

	if app.loadCalls != 1 || !app.closed {
		t.Errorf("loadCalls=%d closed=%v, want 1 and true", app.loadCalls, app.closed)
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
