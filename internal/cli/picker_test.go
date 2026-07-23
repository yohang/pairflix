package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/yohang/pairflix/internal/cast"
	"github.com/yohang/pairflix/internal/engine"
)

func candidates() []engine.FileInfo {
	return []engine.FileInfo{
		{Index: 0, Path: "movie.mkv", Length: 4 << 30},
		{Index: 2, Path: "extras/bonus.mp4", Length: 700 << 20},
		{Index: 5, Path: "sample.avi", Length: 20 << 20},
	}
}

func TestPickFileSingleCandidate(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	single := []engine.FileInfo{{Index: 3, Path: "movie.mkv", Length: 1 << 30}}

	got, err := pickFile(strings.NewReader(""), &out, single)
	if err != nil {
		t.Fatalf("pickFile: %v", err)
	}

	if got != 3 {
		t.Errorf("index = %d, want 3", got)
	}

	if !strings.Contains(out.String(), "movie.mkv") {
		t.Errorf("output should announce the file, got %q", out.String())
	}
}

func TestPickFileChoice(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	got, err := pickFile(strings.NewReader("2\n"), &out, candidates())
	if err != nil {
		t.Fatalf("pickFile: %v", err)
	}

	if got != 2 {
		t.Errorf("index = %d, want 2 (second candidate's torrent index)", got)
	}
}

func TestPickFileInvalidThenValid(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	got, err := pickFile(strings.NewReader("x\n99\n1\n"), &out, candidates())
	if err != nil {
		t.Fatalf("pickFile: %v", err)
	}

	if got != 0 {
		t.Errorf("index = %d, want 0", got)
	}

	if !strings.Contains(out.String(), "Invalid choice") {
		t.Errorf("output should mention invalid choices, got %q", out.String())
	}
}

func TestPickFileEOF(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	if _, err := pickFile(strings.NewReader(""), &out, candidates()); !errors.Is(err, errNoSelection) {
		t.Errorf("error = %v, want errNoSelection", err)
	}
}

func TestPickFileNoCandidates(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	if _, err := pickFile(strings.NewReader(""), &out, nil); err == nil {
		t.Error("expected error for empty candidates")
	}
}

func TestPickDevice(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	devices := []cast.Device{
		{Name: "Bedroom TV", Model: "Chromecast", Addr: "192.168.1.11", Port: 8009},
		{Name: "Living Room TV", Model: "Google TV", Addr: "192.168.1.12", Port: 8009},
	}

	dev, err := pickDevice(strings.NewReader("2\n"), &out, devices)
	if err != nil {
		t.Fatalf("pickDevice: %v", err)
	}

	if dev.Name != "Living Room TV" {
		t.Errorf("device = %q, want Living Room TV", dev.Name)
	}

	if !strings.Contains(out.String(), "Casting to: Living Room TV") {
		t.Errorf("output should announce the device, got %q", out.String())
	}
}
