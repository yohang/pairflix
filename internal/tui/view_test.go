package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/yohang/pairflix/internal/cast"
	"github.com/yohang/pairflix/internal/engine"
)

func testConfig() Config {
	return Config{
		FileName: "dir/Movie.mkv",
		FileSize: 100 << 20,
		Backend:  "chromecast",
		Device:   "TV (Google TV)",
		Trackers: []string{"udp://a.example:1337/announce", "http://b.example/announce"},
		Controls: &fakeControls{},
	}
}

func sizedModel(t *testing.T, cfg Config, width int) model {
	t.Helper()

	m := newModel(cfg)
	m, _ = apply(t, m, tea.WindowSizeMsg{Width: width, Height: 40})

	return m
}

func TestViewContainsPanes(t *testing.T) {
	t.Parallel()

	m := sizedModel(t, testConfig(), 80)

	snap := engine.Snapshot{
		BytesReadData: 10 << 20,
		ActivePeers:   3,
		TotalPeers:    9,
		Peers: []engine.PeerInfo{
			{Addr: "192.168.1.5:51413", Client: "qBittorrent", DownloadRate: 1 << 20},
		},
		Pieces: []engine.PieceRun{
			{State: engine.PieceComplete, Length: 5},
			{State: engine.PiecePending, Length: 5},
		},
		Taken: time.Now(),
	}

	m, _ = apply(t, m, snapshotMsg{snap: snap, progress: 50 << 20})
	m, _ = apply(t, m, castMsg(cast.MediaStatus{
		State: "PLAYING", Position: 83 * time.Second, Duration: 14 * time.Minute,
	}))

	view := m.render()

	for _, want := range []string{
		"Movie.mkv", "mkv", "Transfer", "Playback", "Trackers", "Log",
		"udp://a.example:1337/announce",
		"192.168.1.5:51413", "qBittorrent",
		"backend: chromecast", "TV (Google TV)",
		"playing", "01:23", "14:00",
		"space pause/resume · s stop · q quit",
		"50%",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestViewPeerRowsCapped(t *testing.T) {
	t.Parallel()

	m := sizedModel(t, testConfig(), 100)

	snap := engine.Snapshot{Taken: time.Now()}
	for i := range 12 {
		snap.Peers = append(snap.Peers, engine.PeerInfo{
			Addr: strings.Repeat("x", 3) + string(rune('a'+i)), DownloadRate: float64(i),
		})
	}

	m, _ = apply(t, m, snapshotMsg{snap: snap})

	view := m.render()

	rows := 0

	for _, p := range snap.Peers {
		if strings.Contains(view, p.Addr) {
			rows++
		}
	}

	if rows != maxPeerRows {
		t.Errorf("peer rows = %d, want %d", rows, maxPeerRows)
	}
}

func TestViewHintWithoutControls(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Controls = nil
	cfg.Backend = "vlc"
	cfg.Device = ""

	m := sizedModel(t, cfg, 80)

	view := m.render()

	if strings.Contains(view, "space pause") {
		t.Error("hint should not offer cast keys without controls")
	}

	if !strings.Contains(view, "q quit") {
		t.Error("hint should offer q quit")
	}

	if !strings.Contains(view, "backend: vlc") {
		t.Error("backend pane should show vlc")
	}
}

func TestViewNarrowNoPanic(t *testing.T) {
	t.Parallel()

	m := sizedModel(t, testConfig(), 20)

	view := m.render()
	if !strings.Contains(view, "too narrow") {
		t.Errorf("narrow view should say so, got %q", view)
	}
}

func TestPieceStrip(t *testing.T) {
	t.Parallel()

	runs := []engine.PieceRun{
		{State: engine.PieceComplete, Length: 10},
		{State: engine.PiecePending, Length: 10},
	}

	strip := pieceStrip(runs, 10)

	if got := strings.Count(strip, "█"); got != 5 {
		t.Errorf("complete cols = %d, want 5 in %q", got, strip)
	}

	if got := strings.Count(strip, "·"); got != 5 {
		t.Errorf("pending cols = %d, want 5 in %q", got, strip)
	}
}

func TestProgressBarBounds(t *testing.T) {
	t.Parallel()

	if bar := progressBar(10, 0); strings.Contains(bar, "█") {
		t.Errorf("0%% bar should be empty, got %q", bar)
	}

	if bar := progressBar(10, 100); strings.Contains(bar, "░") {
		t.Errorf("100%% bar should be full, got %q", bar)
	}

	if bar := progressBar(10, 150); strings.Count(bar, "█") != 10 {
		t.Errorf("overflow bar should clamp, got %q", bar)
	}
}
