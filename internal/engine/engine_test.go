package engine

import "testing"

func TestIsMagnet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want bool
	}{
		{"magnet:?xt=urn:btih:abc", true},
		{"magnet:", true},
		{"movie.torrent", false},
		{"/path/to/movie.torrent", false},
		{"http://example.com/movie.torrent", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsMagnet(tt.src); got != tt.want {
			t.Errorf("IsMagnet(%q) = %v, want %v", tt.src, got, tt.want)
		}
	}
}

func TestNewRequiresDataDir(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{}); err == nil {
		t.Fatal("New with empty DataDir: expected error, got nil")
	}
}
