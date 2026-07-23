package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yohang/pairflix/internal/cast"
	"github.com/yohang/pairflix/internal/engine"
	"github.com/yohang/pairflix/internal/units"
)

// sessionUI displays a running streaming session. Implementations: plainUI
// (line-based, non-TTY safe) and the full-screen TUI.
type sessionUI interface {
	// Run blocks until ctx is done or the user quits the UI.
	Run(ctx context.Context) error
	// Logf reports a session event.
	Logf(format string, args ...any)
	// CastStatus reports a playback status snapshot.
	CastStatus(status cast.MediaStatus)
}

// plainUI is the historical line-based output: a self-overwriting progress
// line on a 2s ticker plus one line per cast state transition.
type plainUI struct {
	out  io.Writer
	eng  *engine.Engine
	file *engine.File

	status    atomic.Pointer[cast.MediaStatus]
	lastState atomic.Pointer[string]
}

func newPlainUI(out io.Writer, eng *engine.Engine, file *engine.File) *plainUI {
	return &plainUI{out: out, eng: eng, file: file}
}

// Run prints the progress line every 2s until ctx is done.
func (p *plainUI) Run(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			stats := p.eng.Stats()
			done := p.file.BytesCompleted()
			total := p.file.Size()

			var pct float64
			if total > 0 {
				pct = float64(done) / float64(total) * 100
			}

			fmt.Fprintf(p.out, "\r⇣ %s / %s (%.1f%%)  peers: %d%s   ",
				units.HumanBytes(done), units.HumanBytes(total), pct, stats.Peers,
				castSuffix(p.status.Load()))
		}
	}
}

// Logf prints an event on its own line.
func (p *plainUI) Logf(format string, args ...any) {
	fmt.Fprintf(p.out, "\n"+format+"\n", args...)
}

// CastStatus stores the latest status for the progress line and prints
// state transitions.
func (p *plainUI) CastStatus(status cast.MediaStatus) {
	p.status.Store(&status)

	if status.State == "" {
		return
	}

	if last := p.lastState.Load(); last != nil && *last == status.State {
		return
	}

	p.lastState.Store(&status.State)

	line := strings.ToLower(status.State)
	if status.Reason != "" {
		line += " (" + strings.ToLower(status.Reason) + ")"
	}

	fmt.Fprintf(p.out, "\nChromecast: %s\n", line)
}

// castSuffix renders the playback part of the progress line, e.g.
// " | ▶ 01:23/14:48". Empty when no playback status is known.
func castSuffix(status *cast.MediaStatus) string {
	if status == nil || status.State == "" ||
		status.State == cast.StateConnected || status.State == cast.StateDisconnected {
		return ""
	}

	icons := map[string]string{
		"PLAYING":   "▶",
		"PAUSED":    "⏸",
		"BUFFERING": "◌",
	}

	icon, ok := icons[status.State]
	if !ok {
		icon = strings.ToLower(status.State)
	}

	if status.Duration <= 0 {
		return " | " + icon
	}

	return fmt.Sprintf(" | %s %s/%s", icon,
		units.PlayTime(status.Position), units.PlayTime(status.Duration))
}
