package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/yohang/pairflix/internal/engine"
	"github.com/yohang/pairflix/internal/units"
)

// minWidth is the narrowest terminal the grid layout supports.
const minWidth = 40

// maxPeerRows caps the peer table length.
const maxPeerRows = 8

// View renders the dashboard.
func (m model) View() string {
	if m.width == 0 {
		return "starting..."
	}

	if m.width < minWidth {
		return m.narrowView()
	}

	width := m.width

	sections := []string{
		m.headerPane(width),
		m.middleRow(width),
		m.trackersPane(width),
		m.logPane(width),
		dimStyle.Render(" " + m.hint()),
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// narrowView is a minimal fallback for tiny terminals.
func (m model) narrowView() string {
	return fmt.Sprintf("pairflix %s\n%s / %s\n↓ %s ↑ %s\n%s\n(q quits; terminal too narrow)",
		filepath.Base(m.cfg.FileName),
		units.HumanBytes(m.progress), units.HumanBytes(m.cfg.FileSize),
		units.HumanRate(m.downRate), units.HumanRate(m.upRate),
		m.castLine())
}

// headerPane shows file info, the progress bar and the piece strip.
func (m model) headerPane(width int) string {
	name := filepath.Base(m.cfg.FileName)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")

	if ext == "" {
		ext = "?"
	}

	inner := width - 4

	var pct float64
	if m.cfg.FileSize > 0 {
		pct = float64(m.progress) / float64(m.cfg.FileSize) * 100
	}

	bar := progressBar(inner-25, pct)
	line1 := fmt.Sprintf("%s %3.0f%% %s/%s", bar, pct,
		units.HumanBytes(m.progress), units.HumanBytes(m.cfg.FileSize))

	strip := pieceStrip(m.snap.Pieces, inner)

	title := fmt.Sprintf("pairflix — %s (%s, %s)", name, ext, units.HumanBytes(m.cfg.FileSize))

	return pane(title, line1+"\n"+strip, width)
}

// middleRow joins the transfer and playback panes side by side.
func (m model) middleRow(width int) string {
	left := width / 2
	right := width - left

	transfer := pane("Transfer", m.transferContent(), left)
	playback := pane("Playback", m.playbackContent(), right)

	return lipgloss.JoinHorizontal(lipgloss.Top, transfer, playback)
}

func (m model) transferContent() string {
	s := m.snap

	lines := []string{
		fmt.Sprintf("↓ %s   ↑ %s", units.HumanRate(m.downRate), units.HumanRate(m.upRate)),
		fmt.Sprintf("read %s   sent %s",
			units.HumanBytes(s.BytesReadData), units.HumanBytes(s.BytesWrittenData)),
		fmt.Sprintf("peers %d/%d   seeders %d",
			s.ActivePeers, s.TotalPeers, s.ConnectedSeeders),
	}

	for i, p := range s.Peers {
		if i >= maxPeerRows {
			break
		}

		client := p.Client
		if client == "" {
			client = "?"
		}

		lines = append(lines, dimStyle.Render(fmt.Sprintf("%-21s %-12.12s %s",
			p.Addr, client, units.HumanRate(p.DownloadRate))))
	}

	return strings.Join(lines, "\n")
}

func (m model) playbackContent() string {
	lines := []string{"backend: " + m.cfg.Backend}

	if m.cfg.Device != "" {
		lines = append(lines, m.cfg.Device)
	}

	lines = append(lines, m.castLine())

	return strings.Join(lines, "\n")
}

// castLine renders the current playback state line.
func (m model) castLine() string {
	status := m.castStatus
	if status.State == "" {
		if m.cfg.Backend == "chromecast" {
			return dimStyle.Render("waiting for device...")
		}

		return dimStyle.Render("no playback feedback")
	}

	line := stateIcon(status.State) + " " + strings.ToLower(status.State)

	if status.Reason != "" {
		line += " (" + strings.ToLower(status.Reason) + ")"
	}

	if status.Duration > 0 {
		line += fmt.Sprintf("  %s / %s",
			units.PlayTime(status.Position), units.PlayTime(status.Duration))
	}

	if status.State == "PLAYING" {
		return okStyle.Render(line)
	}

	return line
}

func (m model) trackersPane(width int) string {
	if len(m.cfg.Trackers) == 0 {
		return pane("Trackers", dimStyle.Render("none"), width)
	}

	return pane("Trackers", strings.Join(m.cfg.Trackers, "\n"), width)
}

func (m model) logPane(width int) string {
	if len(m.logs) == 0 {
		return pane("Log", dimStyle.Render("—"), width)
	}

	return pane("Log", strings.Join(m.logs, "\n"), width)
}

// progressBar renders a fixed-width bar for pct (0-100).
func progressBar(width int, pct float64) string {
	if width < 4 {
		width = 4
	}

	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}

	if filled < 0 {
		filled = 0
	}

	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// pieceStrip renders piece completion runs scaled to width columns.
func pieceStrip(runs []engine.PieceRun, width int) string {
	total := 0
	for _, r := range runs {
		total += r.Length
	}

	if total == 0 || width < 4 {
		return dimStyle.Render(strings.Repeat("·", max(width, 1)))
	}

	chars := map[engine.PieceState]rune{
		engine.PiecePending:  '·',
		engine.PiecePartial:  '▒',
		engine.PieceChecking: '?',
		engine.PieceComplete: '█',
	}

	var out strings.Builder

	// For each display column, show the dominant state of its piece range.
	for col := range width {
		start := col * total / width
		end := (col + 1) * total / width

		if end <= start {
			end = start + 1
		}

		out.WriteRune(chars[dominantState(runs, start, end)])
	}

	return out.String()
}

// dominantState returns the most advanced state present in piece range
// [start, end).
func dominantState(runs []engine.PieceRun, start, end int) engine.PieceState {
	state := engine.PiecePending
	pos := 0

	for _, run := range runs {
		runStart := pos
		runEnd := pos + run.Length
		pos = runEnd

		if runEnd <= start {
			continue
		}

		if runStart >= end {
			break
		}

		if run.State > state {
			state = run.State
		}
	}

	return state
}
