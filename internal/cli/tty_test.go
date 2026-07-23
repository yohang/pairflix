package cli

import (
	"bytes"
	"os"
	"testing"
)

func TestUseTUI(t *testing.T) {
	t.Parallel()

	// Pipes have fds but are not terminals; buffers have no fd at all.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	t.Cleanup(func() {
		r.Close() //nolint:errcheck // test cleanup
		w.Close() //nolint:errcheck // test cleanup
	})

	var buf bytes.Buffer

	tests := []struct {
		name   string
		noTUI  bool
		stderr any
		stdin  any
		want   bool
	}{
		{"flag disables", true, w, r, false},
		{"pipe stderr", false, w, r, false},
		{"buffer stderr", false, &buf, r, false},
		{"buffer stdin", false, &buf, &buf, false},
	}

	for _, tt := range tests {
		if got := useTUI(tt.noTUI, tt.stderr, tt.stdin); got != tt.want {
			t.Errorf("%s: useTUI = %v, want %v", tt.name, got, tt.want)
		}
	}
}
