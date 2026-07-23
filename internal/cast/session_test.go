package cast

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeApp records calls; playback lifecycle is driven through its status.
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
	idleAfter   int // report IDLE/FINISHED after this many MediaStatus calls
	paused      int
	unpaused    int
	stopped     int
}

func newFakeApp() *fakeApp {
	return &fakeApp{}
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

	if f.idleAfter > 0 && f.statusCalls > f.idleAfter {
		return MediaStatus{State: "IDLE", Reason: "FINISHED"}, true
	}

	return f.status, true
}

func (f *fakeApp) setStatus(status MediaStatus) {
	f.mu.Lock()
	f.status = status
	f.mu.Unlock()
}

func (f *fakeApp) Pause() error {
	f.mu.Lock()
	f.paused++
	f.mu.Unlock()

	return nil
}

func (f *fakeApp) Unpause() error {
	f.mu.Lock()
	f.unpaused++
	f.mu.Unlock()

	return nil
}

func (f *fakeApp) StopMedia() error {
	f.mu.Lock()
	f.stopped++
	f.mu.Unlock()

	return nil
}

func (f *fakeApp) Close(stopMedia bool) error {
	f.mu.Lock()
	f.closed = true
	f.stopMedia = stopMedia
	f.mu.Unlock()

	return nil
}

func casterFor(app App) *Caster {
	return &Caster{
		NewApp:         func() App { return app },
		RetryDelay:     time.Millisecond,
		StatusInterval: 2 * time.Millisecond,
	}
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

	// First two launches time out (slow Google TV), third succeeds and
	// playback finishes immediately.
	app := newFakeApp()
	app.loadFails = 2
	app.status = MediaStatus{State: "IDLE", Reason: "FINISHED"}

	if err := casterFor(app).Run(context.Background(), testDevice, "http://x/s.mp4", "video/mp4"); err != nil {
		t.Fatalf("Run: %v, want nil after retries", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	if app.loadCalls != 3 {
		t.Errorf("loadCalls = %d, want 3", app.loadCalls)
	}
}

func TestRunStatusCallbacks(t *testing.T) {
	t.Parallel()

	// Plays for two polls, then finishes.
	app := newFakeApp()
	app.status = MediaStatus{State: "PLAYING", Position: 5 * time.Second, Duration: time.Minute}
	app.idleAfter = 2

	var (
		mu     sync.Mutex
		states []string
	)

	caster := casterFor(app)
	caster.OnStatus = func(s MediaStatus) {
		mu.Lock()
		defer mu.Unlock()

		states = append(states, s.State)
	}

	if err := caster.Run(context.Background(), testDevice, "http://x/s.mp4", "video/mp4"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(states) == 0 || states[0] != StateConnected {
		t.Fatalf("states = %v, want CONNECTED first", states)
	}

	joined := strings.Join(states, ",")
	if !strings.Contains(joined, "PLAYING") || !strings.Contains(joined, "IDLE") {
		t.Errorf("states = %v, want PLAYING then IDLE", states)
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
	app.status = MediaStatus{State: "IDLE", Reason: "FINISHED"}

	if err := casterFor(app).Run(context.Background(), testDevice, "http://x/s.mp4", "video/mp4"); err != nil {
		t.Fatalf("Run: %v, want nil on normal playback end", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	if app.loadCalls != 1 || !app.closed {
		t.Errorf("loadCalls=%d closed=%v, want 1 and true", app.loadCalls, app.closed)
	}
}

func TestControlsRequireSession(t *testing.T) {
	t.Parallel()

	caster := casterFor(newFakeApp())

	for name, f := range map[string]func() error{
		"Pause": caster.Pause, "Unpause": caster.Unpause, "Stop": caster.Stop,
	} {
		if err := f(); !errors.Is(err, ErrNoSession) {
			t.Errorf("%s without session: error = %v, want ErrNoSession", name, err)
		}
	}
}

func TestControlsDuringSession(t *testing.T) {
	t.Parallel()

	app := newFakeApp()
	app.status = MediaStatus{State: "PLAYING"}

	var once sync.Once

	caster := casterFor(app)
	caster.OnStatus = func(s MediaStatus) {
		if s.State != "PLAYING" {
			return
		}

		// Exercise the controls once while the session is live, then let
		// playback finish.
		once.Do(func() {
			if err := caster.Pause(); err != nil {
				t.Errorf("Pause: %v", err)
			}

			if err := caster.Unpause(); err != nil {
				t.Errorf("Unpause: %v", err)
			}

			if err := caster.Stop(); err != nil {
				t.Errorf("Stop: %v", err)
			}

			app.setStatus(MediaStatus{State: "IDLE", Reason: "FINISHED"})
		})
	}

	if err := caster.Run(context.Background(), testDevice, "http://x/s.mp4", "video/mp4"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	app.mu.Lock()

	if app.paused != 1 || app.unpaused != 1 || app.stopped != 1 {
		t.Errorf("paused=%d unpaused=%d stopped=%d, want 1 each",
			app.paused, app.unpaused, app.stopped)
	}

	app.mu.Unlock()

	if err := caster.Pause(); !errors.Is(err, ErrNoSession) {
		t.Errorf("Pause after Run: error = %v, want ErrNoSession", err)
	}
}

// hangingCloseApp never returns from Close, like an unresponsive device.
type hangingCloseApp struct {
	*fakeApp
}

func (h hangingCloseApp) Close(bool) error {
	select {} // block forever
}

func TestRunCancelWithHangingClose(t *testing.T) {
	t.Parallel()

	app := newFakeApp()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()

	err := casterFor(hangingCloseApp{app}).Run(ctx, testDevice, "http://x/s.mp4", "video/mp4")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}

	if elapsed := time.Since(start); elapsed > closeBoundedGrace+2*time.Second {
		t.Errorf("Run took %v; Close hang not bounded", elapsed)
	}
}

func TestRunContextCancel(t *testing.T) {
	t.Parallel()

	app := newFakeApp()
	app.status = MediaStatus{State: "PLAYING"}

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
