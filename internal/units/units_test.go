package units

import (
	"testing"
	"time"
)

func TestHumanBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.0 KiB"},
		{5 << 20, "5.0 MiB"},
		{4 << 30, "4.0 GiB"},
		{1536, "1.5 KiB"},
	}

	for _, tt := range tests {
		if got := HumanBytes(tt.n); got != tt.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestHumanRate(t *testing.T) {
	t.Parallel()

	if got := HumanRate(3.5 * 1024 * 1024); got != "3.5 MiB/s" {
		t.Errorf("HumanRate = %q, want 3.5 MiB/s", got)
	}
}

func TestPlayTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "00:00"},
		{83 * time.Second, "01:23"},
		{59*time.Minute + 59*time.Second, "59:59"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1:02:03"},
	}

	for _, tt := range tests {
		if got := PlayTime(tt.d); got != tt.want {
			t.Errorf("PlayTime(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
