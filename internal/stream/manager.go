// Package stream runs torrent streaming sessions programmatically, for
// frontends (like the web UI) that start and stop streams repeatedly
// within one process.
package stream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/yohang/pairflix/internal/cast"
	"github.com/yohang/pairflix/internal/engine"
	"github.com/yohang/pairflix/internal/server"
	"github.com/yohang/pairflix/internal/vlc"
)

// State is the lifecycle phase of the current session.
type State string

// Session states.
const (
	StateIdle      State = "idle"
	StatePreparing State = "preparing"
	StateSelecting State = "selecting"
	StateStreaming State = "streaming"
	StateError     State = "error"
)

// Sentinel errors for the API layer.
var (
	// ErrBusy is returned by Start while a session is active.
	ErrBusy = errors.New("stream: a session is already active")
	// ErrNotSelecting is returned by Select outside the selecting state.
	ErrNotSelecting = errors.New("stream: no file selection pending")
)

// stopGrace bounds how long Stop waits for a session to tear down.
const stopGrace = 15 * time.Second

// Backend is the playback target, fixed for the manager's lifetime.
type Backend struct {
	// CastDev, when non-nil, casts every stream to this device.
	CastDev *cast.Device
	// VLCBin, when non-empty, plays every stream in this local VLC.
	VLCBin string
}

// Config configures a Manager.
type Config struct {
	Backend   Backend
	BaseDir   string // parent for per-stream temp dirs; "" = os default
	NoUpload  bool
	Readahead int64 // bytes
	Logf      func(format string, args ...any)
}

// FileChoice is a video file candidate presented for selection.
type FileChoice struct {
	Index int    `json:"index"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
}

// Status is a snapshot of the manager state for display.
type Status struct {
	State     State
	Torrent   string
	FileName  string
	StreamURL string
	Files     []FileChoice
	Err       string
	DataDir   string
}

// StartOptions tunes one session start.
type StartOptions struct {
	// AdvertiseHost overrides the host in the stream URL when no cast
	// device dictates one (e.g. the web UI's Host header).
	AdvertiseHost string
	// RemoveSource deletes src (an uploaded .torrent) once consumed.
	RemoveSource bool
}

// torrentEngine is the session's view of the torrent engine, split out so
// tests can fake it.
type torrentEngine interface {
	Open(ctx context.Context, src string) error
	Name() string
	VideoFiles() []engine.FileInfo
	Select(index int) (server.Source, error)
	Close() error
}

// realEngine adapts *engine.Engine to torrentEngine.
type realEngine struct {
	*engine.Engine
}

// Select adapts the concrete *engine.File to the server.Source interface.
func (r realEngine) Select(index int) (server.Source, error) {
	return r.Engine.Select(index)
}

// session is the state of one stream attempt.
type session struct {
	gen     int
	eng     torrentEngine
	cancel  context.CancelFunc
	done    chan struct{}
	dataDir string
	opts    StartOptions

	torrent   string
	fileName  string
	streamURL string
	files     []FileChoice

	selectCh chan int
}

// Manager runs at most one streaming session at a time.
type Manager struct {
	cfg Config
	ctx context.Context

	// Injectable seams for tests.
	newEngine  func(cfg engine.Config) (torrentEngine, error)
	runBackend func(ctx context.Context, streamURL, contentType string) error

	mu    sync.Mutex
	state State
	err   string
	sess  *session
	gen   int
}

// New creates a Manager. ctx is the process lifetime: cancellation stops
// any active session.
func New(ctx context.Context, cfg Config) *Manager {
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}

	m := &Manager{
		cfg:   cfg,
		ctx:   ctx,
		state: StateIdle,
	}

	m.newEngine = func(ecfg engine.Config) (torrentEngine, error) {
		eng, err := engine.New(ecfg)
		if err != nil {
			return nil, err
		}

		return realEngine{eng}, nil
	}

	m.runBackend = m.defaultRunBackend

	return m
}

// defaultRunBackend plays streamURL on the configured backend, blocking
// until playback ends. With no backend it blocks until ctx is done.
func (m *Manager) defaultRunBackend(ctx context.Context, streamURL, contentType string) error {
	switch {
	case m.cfg.Backend.CastDev != nil:
		return cast.NewCaster().Run(ctx, *m.cfg.Backend.CastDev, streamURL, contentType)
	case m.cfg.Backend.VLCBin != "":
		return vlc.NewLauncher().Run(ctx, m.cfg.Backend.VLCBin, streamURL)
	default:
		<-ctx.Done()

		return ctx.Err()
	}
}

// Status returns a display snapshot.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := Status{State: m.state, Err: m.err}

	if m.sess != nil {
		st.Torrent = m.sess.torrent
		st.FileName = m.sess.fileName
		st.StreamURL = m.sess.streamURL
		st.Files = m.sess.files
		st.DataDir = m.sess.dataDir
	}

	return st
}

// Start begins a session for src (magnet link or .torrent path). It
// returns ErrBusy unless the manager is idle or in error state.
func (m *Manager) Start(src string, opts StartOptions) error {
	m.mu.Lock()

	if m.state != StateIdle && m.state != StateError {
		m.mu.Unlock()

		return ErrBusy
	}

	dataDir, err := os.MkdirTemp(m.cfg.BaseDir, "pairflix-")
	if err != nil {
		m.mu.Unlock()

		return fmt.Errorf("stream: create data dir: %w", err)
	}

	sessCtx, cancel := context.WithCancel(m.ctx)

	m.gen++
	sess := &session{
		gen:      m.gen,
		cancel:   cancel,
		done:     make(chan struct{}),
		dataDir:  dataDir,
		opts:     opts,
		selectCh: make(chan int, 1),
	}
	m.sess = sess
	m.state = StatePreparing
	m.err = ""
	m.mu.Unlock()

	go m.runSession(sessCtx, sess, src)

	return nil
}

// Select picks the video file to stream while in the selecting state.
func (m *Manager) Select(index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != StateSelecting || m.sess == nil {
		return ErrNotSelecting
	}

	valid := false

	for _, f := range m.sess.files {
		if f.Index == index {
			valid = true

			break
		}
	}

	if !valid {
		return fmt.Errorf("stream: invalid file index %d", index)
	}

	select {
	case m.sess.selectCh <- index:
		return nil
	default:
		return ErrNotSelecting
	}
}

// Stop ends the active session, if any. It is idempotent and waits up to
// stopGrace for teardown.
func (m *Manager) Stop() error {
	m.mu.Lock()
	sess := m.sess
	m.mu.Unlock()

	if sess == nil {
		return nil
	}

	sess.cancel()

	select {
	case <-sess.done:
		return nil
	case <-time.After(stopGrace):
		m.cfg.Logf("stream: session teardown timed out; abandoning")

		return errors.New("stream: teardown timed out")
	}
}

// Close stops any session; the manager must not be used afterwards.
func (m *Manager) Close() {
	_ = m.Stop()
}

// setState transitions state if sess is still the current generation.
func (m *Manager) setState(sess *session, state State, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sess == nil || m.sess.gen != sess.gen {
		return
	}

	m.state = state
	m.err = errMsg

	if state == StateIdle || state == StateError {
		m.sess = nil
	}
}

// runSession drives one session from open to teardown.
func (m *Manager) runSession(ctx context.Context, sess *session, src string) {
	defer close(sess.done)
	defer sess.cancel()

	err := m.prepareAndStream(ctx, sess, src)

	switch {
	case err == nil || errors.Is(err, context.Canceled):
		m.cfg.Logf("stream: session ended; data kept at %s", sess.dataDir)
		m.setState(sess, StateIdle, "")
	default:
		m.cfg.Logf("stream: session failed: %v", err)
		m.setState(sess, StateError, err.Error())
	}
}

// prepareAndStream opens the torrent, resolves the file and streams it.
func (m *Manager) prepareAndStream(ctx context.Context, sess *session, src string) error {
	eng, err := m.newEngine(engine.Config{
		DataDir:   sess.dataDir,
		NoUpload:  m.cfg.NoUpload,
		Readahead: m.cfg.Readahead,
	})
	if err != nil {
		return err
	}

	sess.eng = eng
	defer func() { _ = eng.Close() }()

	m.cfg.Logf("stream: fetching metadata for %s", src)

	if err := eng.Open(ctx, src); err != nil {
		m.removeSource(sess, src)

		return err
	}

	m.removeSource(sess, src)

	m.mu.Lock()
	sess.torrent = eng.Name()
	m.mu.Unlock()

	index, err := m.resolveFile(ctx, sess, eng)
	if err != nil {
		return err
	}

	source, err := eng.Select(index)
	if err != nil {
		return err
	}

	return m.streamFile(ctx, sess, source)
}

// removeSource deletes an uploaded torrent file once consumed.
func (m *Manager) removeSource(sess *session, src string) {
	if sess.opts.RemoveSource {
		_ = os.Remove(src)
	}
}

// resolveFile returns the torrent file index to stream: error when no
// video, automatic for one candidate, user selection otherwise.
func (m *Manager) resolveFile(ctx context.Context, sess *session, eng torrentEngine) (int, error) {
	videos := eng.VideoFiles()
	if len(videos) == 0 {
		return 0, errors.New("no video files found in torrent")
	}

	if len(videos) == 1 {
		m.mu.Lock()
		sess.fileName = videos[0].Path
		m.mu.Unlock()

		return videos[0].Index, nil
	}

	choices := make([]FileChoice, len(videos))
	for i, v := range videos {
		choices[i] = FileChoice{Index: v.Index, Path: v.Path, Size: v.Length}
	}

	m.mu.Lock()
	sess.files = choices
	m.mu.Unlock()

	m.setState(sess, StateSelecting, "")

	select {
	case index := <-sess.selectCh:
		for _, v := range videos {
			if v.Index == index {
				m.mu.Lock()
				sess.fileName = v.Path
				sess.files = nil
				m.mu.Unlock()
			}
		}

		m.setState(sess, StatePreparing, "")

		return index, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// streamFile serves source over HTTP and runs the backend until either
// ends or the session is stopped.
func (m *Manager) streamFile(ctx context.Context, sess *session, source server.Source) error {
	srv := server.New(source)

	bind, advertise := m.bindAddr(sess)

	streamURL, err := srv.Start(bind, advertise)
	if err != nil {
		return err
	}

	m.mu.Lock()
	sess.streamURL = streamURL
	m.mu.Unlock()

	if m.cfg.Backend.CastDev != nil && !cast.ProbablySupported(source.Name()) {
		m.cfg.Logf("stream: %s may not play on the default Chromecast receiver", source.Name())
	}

	m.setState(sess, StateStreaming, "")
	m.cfg.Logf("stream: serving %s at %s", source.Name(), streamURL)

	group, gctx := errgroup.WithContext(ctx)

	group.Go(srv.Serve)

	group.Go(func() error {
		<-gctx.Done()

		return srv.Close()
	})

	group.Go(func() error {
		err := m.runBackend(gctx, streamURL, server.ContentType(source.Name()))

		// Backend ending (player closed, cast stopped) ends the session.
		sess.cancel()

		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}

		return nil
	})

	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

// bindAddr returns the media server bind address and advertised host for
// the configured backend.
func (m *Manager) bindAddr(sess *session) (string, string) {
	switch {
	case m.cfg.Backend.CastDev != nil:
		dev := m.cfg.Backend.CastDev

		lanIP, err := cast.LocalIPFor(fmt.Sprintf("%s:%d", dev.Addr, dev.Port))
		if err != nil {
			m.cfg.Logf("stream: LAN IP detection failed: %v", err)

			return "0.0.0.0:0", ""
		}

		return "0.0.0.0:0", lanIP
	case m.cfg.Backend.VLCBin != "":
		return "", "" // 127.0.0.1 random port
	default:
		return "0.0.0.0:0", sess.opts.AdvertiseHost
	}
}
