// Package vlc locates and launches the VLC media player.
package vlc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ErrNotFound is returned when no VLC installation can be located.
var ErrNotFound = errors.New("vlc: not found in PATH or default install locations")

// Launcher finds and runs VLC. The function fields exist for tests and
// default to the real implementations.
type Launcher struct {
	LookPath func(file string) (string, error)
	Stat     func(name string) (os.FileInfo, error)
	Getenv   func(key string) string
	GOOS     string
}

// NewLauncher returns a Launcher using the real OS.
func NewLauncher() *Launcher {
	return &Launcher{
		LookPath: exec.LookPath,
		Stat:     os.Stat,
		Getenv:   os.Getenv,
		GOOS:     runtime.GOOS,
	}
}

// Find returns the path to the VLC binary: PATH first, then well-known
// install locations per platform.
func (l *Launcher) Find() (string, error) {
	if path, err := l.LookPath("vlc"); err == nil {
		return path, nil
	}

	for _, candidate := range l.fallbackPaths() {
		if _, err := l.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", ErrNotFound
}

// fallbackPaths lists well-known VLC install locations for the platform.
func (l *Launcher) fallbackPaths() []string {
	switch l.GOOS {
	case "darwin":
		return []string{"/Applications/VLC.app/Contents/MacOS/VLC"}
	case "windows":
		var paths []string

		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if dir := l.Getenv(env); dir != "" {
				paths = append(paths, filepath.Join(dir, "VideoLAN", "VLC", "vlc.exe"))
			}
		}

		return paths
	default:
		return nil
	}
}

// Run starts VLC on url and blocks until it exits or ctx is canceled.
func (l *Launcher) Run(ctx context.Context, bin, url string) error {
	//nolint:gosec // bin comes from LookPath or known install locations, url from our own server
	cmd := exec.CommandContext(ctx, bin, "--play-and-exit", url)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("vlc: start %s: %w", bin, err)
	}

	// A non-zero exit (e.g. window closed) is a normal end of session,
	// not an error worth reporting.
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil
		}

		return fmt.Errorf("vlc: wait: %w", err)
	}

	return nil
}
