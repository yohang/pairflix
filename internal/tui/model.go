package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yohang/pairflix/internal/cast"
	"github.com/yohang/pairflix/internal/engine"
)

// refreshInterval is the dashboard update cadence.
const refreshInterval = 2 * time.Second

// maxLogLines is how many log entries the log pane keeps.
const maxLogLines = 6

type (
	tickMsg time.Time

	snapshotMsg struct {
		snap     engine.Snapshot
		progress int64
	}

	castMsg cast.MediaStatus

	logMsg struct {
		at   time.Time
		line string
	}

	ctlErrMsg struct{ err error }
)

// model is the Bubble Tea model for the dashboard.
type model struct {
	cfg Config

	width  int
	height int

	snap     engine.Snapshot
	prev     engine.Snapshot
	progress int64

	downRate float64
	upRate   float64

	castStatus cast.MediaStatus
	lastState  string

	logs []string
}

func newModel(cfg Config) model {
	return model{cfg: cfg}
}

// Init arms the refresh tick and takes an immediate first snapshot.
func (m model) Init() tea.Cmd {
	return tea.Batch(m.snapshotCmd(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// snapshotCmd fetches transfer data off the UI goroutine.
func (m model) snapshotCmd() tea.Cmd {
	snapshot := m.cfg.Snapshot
	fileProgress := m.cfg.FileProgress

	return func() tea.Msg {
		msg := snapshotMsg{}

		if snapshot != nil {
			msg.snap = snapshot()
		}

		if fileProgress != nil {
			msg.progress = fileProgress()
		}

		return msg
	}
}

// Update handles messages.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		return m, nil

	case tickMsg:
		return m, tea.Batch(m.snapshotCmd(), tickCmd())

	case snapshotMsg:
		m.prev = m.snap
		m.snap = msg.snap
		m.progress = msg.progress
		m.downRate, m.upRate = rates(m.prev, m.snap)

		return m, nil

	case castMsg:
		return m.updateCast(cast.MediaStatus(msg)), nil

	case logMsg:
		return m.appendLog(msg.at, msg.line), nil

	case ctlErrMsg:
		if msg.err != nil {
			return m.appendLog(time.Now(), "control: "+msg.err.Error()), nil
		}

		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)
	}

	return m, nil
}

// updateCast stores the playback status and logs state transitions.
func (m model) updateCast(status cast.MediaStatus) model {
	m.castStatus = status

	if status.State != "" && status.State != m.lastState {
		m.lastState = status.State

		line := strings.ToLower(status.State)
		if status.Reason != "" {
			line += " (" + strings.ToLower(status.Reason) + ")"
		}

		m = m.appendLog(time.Now(), "chromecast: "+line)
	}

	return m
}

func (m model) appendLog(at time.Time, line string) model {
	entry := at.Format("15:04:05") + " " + line

	m.logs = append(m.logs, entry)
	if len(m.logs) > maxLogLines {
		m.logs = m.logs[len(m.logs)-maxLogLines:]
	}

	return m
}

// updateKey handles key bindings.
func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case " ":
		if m.cfg.Controls == nil {
			return m, nil
		}

		controls := m.cfg.Controls
		paused := m.castStatus.State == "PAUSED"

		return m, func() tea.Msg {
			if paused {
				return ctlErrMsg{err: controls.Unpause()}
			}

			return ctlErrMsg{err: controls.Pause()}
		}

	case "s":
		if m.cfg.Controls == nil {
			return m, nil
		}

		controls := m.cfg.Controls

		return m, func() tea.Msg { return ctlErrMsg{err: controls.Stop()} }
	}

	return m, nil
}

// rates derives byte rates from two consecutive snapshots.
func rates(prev, cur engine.Snapshot) (down, up float64) {
	if prev.Taken.IsZero() {
		return 0, 0
	}

	dt := cur.Taken.Sub(prev.Taken).Seconds()
	if dt <= 0 {
		return 0, 0
	}

	down = float64(cur.BytesReadData-prev.BytesReadData) / dt
	up = float64(cur.BytesWrittenData-prev.BytesWrittenData) / dt

	if down < 0 {
		down = 0
	}

	if up < 0 {
		up = 0
	}

	return down, up
}

// hint returns the key hint bar text.
func (m model) hint() string {
	if m.cfg.Controls != nil {
		return "space pause/resume · s stop · q quit"
	}

	return "q quit"
}

// stateIcon maps a player state to its display icon.
func stateIcon(state string) string {
	switch state {
	case "PLAYING":
		return "▶"
	case "PAUSED":
		return "⏸"
	case "BUFFERING":
		return "◌"
	case cast.StateConnected:
		return "·"
	case cast.StateDisconnected:
		return "✗"
	default:
		return "∙"
	}
}
