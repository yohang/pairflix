// Package tui renders the full-screen session dashboard.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yohang/pairflix/internal/cast"
	"github.com/yohang/pairflix/internal/engine"
)

// CastController exposes playback controls for the active cast session.
// Implemented by cast.Caster.
type CastController interface {
	Pause() error
	Unpause() error
	Stop() error
}

// Config carries everything the dashboard displays. All data arrives as
// plain values or closures so the package stays free of torrent internals.
type Config struct {
	// Out is the terminal writer, normally os.Stderr (stdout carries the
	// stream URL and must stay clean).
	Out io.Writer

	FileName string
	FileSize int64
	// FileProgress returns completed bytes of the streamed file.
	FileProgress func() int64
	// Snapshot returns the current transfer snapshot.
	Snapshot func() engine.Snapshot

	Trackers []string

	// Backend is the active playback backend: "chromecast", "vlc" or
	// "http".
	Backend string
	// Device describes the cast target ("Name (Model)"), empty otherwise.
	Device string
	// Controls is non-nil only while casting.
	Controls CastController
}

// UI runs the Bubble Tea dashboard. It implements the cli session UI seam.
type UI struct {
	cfg     Config
	program *tea.Program

	mu      sync.Mutex
	started bool
	pending []tea.Msg
}

// New builds the dashboard UI.
func New(cfg Config) *UI {
	return &UI{cfg: cfg}
}

// Run blocks until the user quits or ctx is canceled.
func (u *UI) Run(ctx context.Context) error {
	program := tea.NewProgram(
		newModel(u.cfg),
		tea.WithOutput(u.cfg.Out),
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	)

	u.mu.Lock()
	u.program = program
	u.started = true
	pending := u.pending
	u.pending = nil
	u.mu.Unlock()

	go func() {
		for _, msg := range pending {
			program.Send(msg)
		}
	}()

	_, err := program.Run()
	if err != nil && (errors.Is(err, tea.ErrProgramKilled) || errors.Is(ctx.Err(), context.Canceled)) {
		return nil
	}

	return err
}

// send delivers msg to the program, buffering it if Run has not started.
func (u *UI) send(msg tea.Msg) {
	u.mu.Lock()

	if !u.started {
		u.pending = append(u.pending, msg)
		u.mu.Unlock()

		return
	}

	program := u.program
	u.mu.Unlock()

	program.Send(msg)
}

// Logf adds a timestamped line to the log pane.
func (u *UI) Logf(format string, args ...any) {
	u.send(logMsg{at: time.Now(), line: fmt.Sprintf(format, args...)})
}

// CastStatus updates the playback pane.
func (u *UI) CastStatus(status cast.MediaStatus) {
	u.send(castMsg(status))
}
