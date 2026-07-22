package cast

import "testing"

func TestIsVideoDevice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ca    string
		model string
		want  bool
	}{
		{"5", "Chromecast", true},           // VIDEO_OUT|AUDIO_OUT
		{"4101", "Chromecast Ultra", true},  // video bit set + extras
		{"199691", "Google TV", true},       // video bit set
		{"4", "Chromecast Audio", false},    // audio only
		{"2052", "Chromecast Audio", false}, // audio only, extras
		{"32", "Google Cast Group", false},  // multizone group
		{"", "Chromecast Ultra", true},      // no ca → model fallback
		{"", "Chromecast Audio", false},
		{"", "Google Home Mini", false},
		{"", "Google Nest Hub", false},
		{"", "Google Cast Group", false},
		{"garbage", "SHIELD Android TV", true},
	}

	for _, tt := range tests {
		if got := isVideoDevice(tt.ca, tt.model); got != tt.want {
			t.Errorf("isVideoDevice(%q, %q) = %v, want %v", tt.ca, tt.model, got, tt.want)
		}
	}
}

func TestProbablySupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{"movie.mp4", true},
		{"movie.MP4", true},
		{"movie.webm", true},
		{"movie.m4v", true},
		{"movie.mkv", false},
		{"movie.MKV", false},
		{"movie.avi", false},
		{"movie.ts", false},
		{"noext", false},
	}

	for _, tt := range tests {
		if got := ProbablySupported(tt.name); got != tt.want {
			t.Errorf("ProbablySupported(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
