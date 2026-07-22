package engine

import "testing"

func TestIsVideo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{"movie.mkv", true},
		{"movie.mp4", true},
		{"dir/movie.avi", true},
		{"MOVIE.MKV", true},
		{"movie.WebM", true},
		{"clip.m2ts", true},
		{"sample.ogv", true},
		{"subs.srt", false},
		{"cover.jpg", false},
		{"movie.mkv.nfo", false},
		{"noext", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsVideo(tt.path); got != tt.want {
			t.Errorf("IsVideo(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
