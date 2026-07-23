package stream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/yohang/pairflix/internal/engine"
	"github.com/yohang/pairflix/internal/server"
)

// writeTestTorrent builds a metadata-only .torrent with the given files.
func writeTestTorrent(t *testing.T, name string, files map[string]int64) string {
	t.Helper()

	const pieceLength = 16384

	info := metainfo.Info{Name: name, PieceLength: pieceLength}

	var total int64

	for path, size := range files {
		info.Files = append(info.Files, metainfo.FileInfo{
			Path:   []string{path},
			Length: size,
		})
		total += size
	}

	pieces := (total + pieceLength - 1) / pieceLength
	info.Pieces = make([]byte, pieces*20)

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}

	path := filepath.Join(t.TempDir(), name+".torrent")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	defer f.Close() //nolint:errcheck // test file

	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	if err := mi.Write(f); err != nil {
		t.Fatalf("write metainfo: %v", err)
	}

	return path
}

// newTestManager returns a manager whose backend finishes when its context
// is canceled, and a channel closed when the backend starts.
func newTestManager(t *testing.T) (*Manager, chan struct{}) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	m := New(ctx, Config{BaseDir: t.TempDir()})

	started := make(chan struct{})
	m.runBackend = func(ctx context.Context, url, ct string) error {
		close(started)
		<-ctx.Done()

		return ctx.Err()
	}

	return m, started
}

func waitState(t *testing.T, m *Manager, want State) Status {
	t.Helper()

	deadline := time.After(10 * time.Second)

	for {
		st := m.Status()
		if st.State == want {
			return st
		}

		select {
		case <-deadline:
			t.Fatalf("state = %s, wanted %s (err=%q)", st.State, want, st.Err)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSingleVideoAutoStreams(t *testing.T) {
	t.Parallel()

	m, started := newTestManager(t)

	src := writeTestTorrent(t, "single", map[string]int64{"movie.mkv": 1 << 20, "notes.txt": 100})

	if err := m.Start(src, StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	st := waitState(t, m, StateStreaming)

	if st.FileName != "movie.mkv" {
		t.Errorf("FileName = %q, want movie.mkv", st.FileName)
	}

	if st.StreamURL == "" {
		t.Error("StreamURL empty while streaming")
	}

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("backend never started")
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	waitState(t, m, StateIdle)
}

func TestMultiVideoSelecting(t *testing.T) {
	t.Parallel()

	m, _ := newTestManager(t)

	src := writeTestTorrent(t, "multi", map[string]int64{
		"movie.mkv": 4 << 20, "bonus.mp4": 1 << 20,
	})

	if err := m.Start(src, StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	st := waitState(t, m, StateSelecting)

	if len(st.Files) != 2 {
		t.Fatalf("Files = %v, want 2 candidates", st.Files)
	}

	if err := m.Select(999); err == nil {
		t.Error("Select(999): expected invalid index error")
	}

	if err := m.Select(st.Files[0].Index); err != nil {
		t.Fatalf("Select: %v", err)
	}

	waitState(t, m, StateStreaming)

	_ = m.Stop()
	waitState(t, m, StateIdle)
}

func TestSelectOutsideSelecting(t *testing.T) {
	t.Parallel()

	m, _ := newTestManager(t)

	if err := m.Select(0); !errors.Is(err, ErrNotSelecting) {
		t.Errorf("Select on idle = %v, want ErrNotSelecting", err)
	}
}

func TestStartWhileBusy(t *testing.T) {
	t.Parallel()

	m, _ := newTestManager(t)

	src := writeTestTorrent(t, "busy", map[string]int64{"movie.mkv": 1 << 20})

	if err := m.Start(src, StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitState(t, m, StateStreaming)

	if err := m.Start(src, StartOptions{}); !errors.Is(err, ErrBusy) {
		t.Errorf("second Start = %v, want ErrBusy", err)
	}

	_ = m.Stop()
}

func TestNoVideoIsError(t *testing.T) {
	t.Parallel()

	m, _ := newTestManager(t)

	src := writeTestTorrent(t, "novideo", map[string]int64{"readme.txt": 100})

	if err := m.Start(src, StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	st := waitState(t, m, StateError)

	if st.Err == "" {
		t.Error("error state should carry a message")
	}

	// Error state allows a fresh start.
	src2 := writeTestTorrent(t, "retry", map[string]int64{"movie.mp4": 1 << 20})
	if err := m.Start(src2, StartOptions{}); err != nil {
		t.Fatalf("restart after error: %v", err)
	}

	waitState(t, m, StateStreaming)
	_ = m.Stop()
}

// blockingEngine hangs in Open until its context dies.
type blockingEngine struct{}

func (blockingEngine) Open(ctx context.Context, _ string) error {
	<-ctx.Done()

	return ctx.Err()
}

func (blockingEngine) Name() string                      { return "" }
func (blockingEngine) VideoFiles() []engine.FileInfo     { return nil }
func (blockingEngine) Select(int) (server.Source, error) { return nil, errors.New("no") }
func (blockingEngine) Close() error                      { return nil }

func TestStopDuringPreparing(t *testing.T) {
	t.Parallel()

	m, _ := newTestManager(t)
	m.newEngine = func(engine.Config) (torrentEngine, error) { return blockingEngine{}, nil }

	if err := m.Start("magnet:?xt=urn:btih:dead", StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitState(t, m, StatePreparing)

	if err := m.Stop(); err != nil {
		t.Fatalf("Stop during preparing: %v", err)
	}

	waitState(t, m, StateIdle)
}

func TestUploadedSourceRemoved(t *testing.T) {
	t.Parallel()

	m, _ := newTestManager(t)

	src := writeTestTorrent(t, "uploaded", map[string]int64{"movie.mkv": 1 << 20})

	if err := m.Start(src, StartOptions{RemoveSource: true}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitState(t, m, StateStreaming)

	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("uploaded source still exists: %v", err)
	}

	_ = m.Stop()
}

func TestBackendEndReturnsToIdle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	m := New(ctx, Config{BaseDir: t.TempDir()})
	m.runBackend = func(context.Context, string, string) error {
		return nil // player closed immediately
	}

	src := writeTestTorrent(t, "ends", map[string]int64{"movie.mkv": 1 << 20})

	if err := m.Start(src, StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitState(t, m, StateIdle)
}
