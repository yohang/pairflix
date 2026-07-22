package cast

import "testing"

func TestLocalIPForLoopback(t *testing.T) {
	t.Parallel()

	ip, err := LocalIPFor("127.0.0.1:8009")
	if err != nil {
		t.Fatalf("LocalIPFor: %v", err)
	}

	if ip != "127.0.0.1" {
		t.Errorf("ip = %q, want 127.0.0.1", ip)
	}
}

func TestLocalIPForBadAddr(t *testing.T) {
	t.Parallel()

	if _, err := LocalIPFor("not an address"); err == nil {
		t.Error("expected error for invalid address")
	}
}
