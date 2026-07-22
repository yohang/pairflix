package vlc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

var errNotInPath = errors.New("not in PATH")

func testLauncher(goos string) *Launcher {
	return &Launcher{
		LookPath: func(string) (string, error) { return "", errNotInPath },
		Stat:     func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist },
		Getenv:   func(string) string { return "" },
		GOOS:     goos,
	}
}

func TestFindInPath(t *testing.T) {
	t.Parallel()

	l := testLauncher("linux")
	l.LookPath = func(file string) (string, error) {
		if file != "vlc" {
			t.Errorf("LookPath(%q), want vlc", file)
		}

		return "/usr/bin/vlc", nil
	}

	got, err := l.Find()
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if got != "/usr/bin/vlc" {
		t.Errorf("Find = %q, want /usr/bin/vlc", got)
	}
}

func TestFindDarwinFallback(t *testing.T) {
	t.Parallel()

	const appPath = "/Applications/VLC.app/Contents/MacOS/VLC"

	l := testLauncher("darwin")
	l.Stat = func(name string) (os.FileInfo, error) {
		if name == appPath {
			return nil, nil //nolint:nilnil // fake Stat success, FileInfo unused
		}

		return nil, fs.ErrNotExist
	}

	got, err := l.Find()
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if got != appPath {
		t.Errorf("Find = %q, want %q", got, appPath)
	}
}

func TestFindWindowsFallback(t *testing.T) {
	t.Parallel()

	want := filepath.Join(`C:\Program Files`, "VideoLAN", "VLC", "vlc.exe")

	l := testLauncher("windows")
	l.Getenv = func(key string) string {
		if key == "ProgramFiles" {
			return `C:\Program Files`
		}

		return ""
	}
	l.Stat = func(name string) (os.FileInfo, error) {
		if name == want {
			return nil, nil //nolint:nilnil // fake Stat success, FileInfo unused
		}

		return nil, fs.ErrNotExist
	}

	got, err := l.Find()
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if got != want {
		t.Errorf("Find = %q, want %q", got, want)
	}
}

func TestFindNotFound(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"linux", "darwin", "windows"} {
		l := testLauncher(goos)

		if _, err := l.Find(); !errors.Is(err, ErrNotFound) {
			t.Errorf("GOOS %s: Find error = %v, want ErrNotFound", goos, err)
		}
	}
}
