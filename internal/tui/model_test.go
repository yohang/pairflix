package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yohang/pairflix/internal/cast"
	"github.com/yohang/pairflix/internal/engine"
)

// fakeControls records playback control calls.
type fakeControls struct {
	paused, unpaused, stopped int
	err                       error
}

func (f *fakeControls) Pause() error { f.paused++; return f.err }

func (f *fakeControls) Unpause() error { f.unpaused++; return f.err }

func (f *fakeControls) Stop() error { f.stopped++; return f.err }

func snapAt(t time.Time, read, written int64) engine.Snapshot {
	return engine.Snapshot{BytesReadData: read, BytesWrittenData: written, Taken: t}
}

func apply(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(msg)

	nm, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", next)
	}

	return nm, cmd
}

func TestRateComputation(t *testing.T) {
	t.Parallel()

	m := newModel(Config{})
	t0 := time.Now()

	m, _ = apply(t, m, snapshotMsg{snap: snapAt(t0, 1000, 100)})
	m, _ = apply(t, m, snapshotMsg{snap: snapAt(t0.Add(2*time.Second), 5000, 300)})

	if m.downRate != 2000 {
		t.Errorf("downRate = %v, want 2000 B/s", m.downRate)
	}

	if m.upRate != 100 {
		t.Errorf("upRate = %v, want 100 B/s", m.upRate)
	}
}

func TestQuitKeys(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"q", "ctrl+c"} {
		m := newModel(Config{})

		var msg tea.KeyMsg
		if key == "q" {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
		} else {
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		}

		_, cmd := apply(t, m, msg)
		if cmd == nil {
			t.Fatalf("key %q: no command, want tea.Quit", key)
		}

		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("key %q: cmd() = %T, want tea.QuitMsg", key, cmd())
		}
	}
}

func TestSpaceTogglesPause(t *testing.T) {
	t.Parallel()

	controls := &fakeControls{}
	m := newModel(Config{Controls: controls})

	space := tea.KeyMsg{Type: tea.KeySpace}

	// PLAYING → pause.
	m, _ = apply(t, m, castMsg(cast.MediaStatus{State: "PLAYING"}))

	_, cmd := apply(t, m, space)
	if cmd == nil {
		t.Fatal("space: no command")
	}

	cmd()

	// PAUSED → unpause.
	m, _ = apply(t, m, castMsg(cast.MediaStatus{State: "PAUSED"}))

	_, cmd = apply(t, m, space)
	cmd()

	if controls.paused != 1 || controls.unpaused != 1 {
		t.Errorf("paused=%d unpaused=%d, want 1 each", controls.paused, controls.unpaused)
	}
}

func TestStopKey(t *testing.T) {
	t.Parallel()

	controls := &fakeControls{}
	m := newModel(Config{Controls: controls})

	_, cmd := apply(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("s: no command")
	}

	cmd()

	if controls.stopped != 1 {
		t.Errorf("stopped = %d, want 1", controls.stopped)
	}
}

func TestKeysNoopWithoutControls(t *testing.T) {
	t.Parallel()

	m := newModel(Config{})

	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeySpace},
		{Type: tea.KeyRunes, Runes: []rune{'s'}},
	} {
		if _, cmd := apply(t, m, msg); cmd != nil {
			t.Errorf("key %v: expected no command without controls", msg)
		}
	}
}

func TestControlErrorLogged(t *testing.T) {
	t.Parallel()

	m := newModel(Config{})

	m, _ = apply(t, m, ctlErrMsg{err: errors.New("boom")})

	if len(m.logs) != 1 {
		t.Fatalf("logs = %v, want one entry", m.logs)
	}
}

func TestCastTransitionsLoggedDeduped(t *testing.T) {
	t.Parallel()

	m := newModel(Config{})

	m, _ = apply(t, m, castMsg(cast.MediaStatus{State: "PLAYING"}))
	m, _ = apply(t, m, castMsg(cast.MediaStatus{State: "PLAYING"}))
	m, _ = apply(t, m, castMsg(cast.MediaStatus{State: "IDLE", Reason: "ERROR"}))

	if len(m.logs) != 2 {
		t.Fatalf("logs = %v, want 2 entries (deduped)", m.logs)
	}

	if want := "chromecast: idle (error)"; !strings.Contains(m.logs[1], want) {
		t.Errorf("log = %q, want containing %q", m.logs[1], want)
	}
}

func TestLogCapped(t *testing.T) {
	t.Parallel()

	m := newModel(Config{})
	for i := range maxLogLines + 3 {
		m, _ = apply(t, m, logMsg{at: time.Now(), line: string(rune('a' + i))})
	}

	if len(m.logs) != maxLogLines {
		t.Errorf("logs len = %d, want %d", len(m.logs), maxLogLines)
	}
}
