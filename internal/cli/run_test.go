package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/yohang/pairflix/internal/cast"
)

func TestValidateCastListen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		listen  string
		wantErr bool
	}{
		{"", false},
		{":8888", false},
		{"0.0.0.0:8888", false},
		{"192.168.1.10:8888", false},
		{"localhost:8888", true},
		{"127.0.0.1:8888", true},
		{"[::1]:8888", true},
		{"no-port", true},
	}

	for _, tt := range tests {
		err := validateCastListen(tt.listen)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateCastListen(%q) error = %v, wantErr %v", tt.listen, err, tt.wantErr)
		}
	}
}

func TestResolveListenNoCast(t *testing.T) {
	t.Parallel()

	addr, advertise, err := resolveListen(options{listen: "127.0.0.1:9999"}, nil)
	if err != nil {
		t.Fatalf("resolveListen: %v", err)
	}

	if addr != "127.0.0.1:9999" || advertise != "" {
		t.Errorf("addr=%q advertise=%q, want passthrough with no advertise", addr, advertise)
	}
}

func TestResolveListenCastDefaults(t *testing.T) {
	t.Parallel()

	// Loopback device keeps LocalIPFor deterministic without a network.
	dev := &cast.Device{Name: "TV", Addr: "127.0.0.1", Port: 8009}

	addr, advertise, err := resolveListen(options{}, dev)
	if err != nil {
		t.Fatalf("resolveListen: %v", err)
	}

	if addr != "0.0.0.0:0" {
		t.Errorf("addr = %q, want 0.0.0.0:0", addr)
	}

	if advertise != "127.0.0.1" {
		t.Errorf("advertise = %q, want the route-derived local IP", advertise)
	}
}

func TestResolveListenCastExplicitHost(t *testing.T) {
	t.Parallel()

	dev := &cast.Device{Name: "TV", Addr: "127.0.0.1", Port: 8009}

	addr, advertise, err := resolveListen(options{listen: "192.168.1.10:8888"}, dev)
	if err != nil {
		t.Fatalf("resolveListen: %v", err)
	}

	if addr != "192.168.1.10:8888" || advertise != "" {
		t.Errorf("addr=%q advertise=%q, want explicit host kept and advertised as-is", addr, advertise)
	}
}

func TestResolveListenCastWildcard(t *testing.T) {
	t.Parallel()

	dev := &cast.Device{Name: "TV", Addr: "127.0.0.1", Port: 8009}

	addr, advertise, err := resolveListen(options{listen: "0.0.0.0:8888"}, dev)
	if err != nil {
		t.Fatalf("resolveListen: %v", err)
	}

	if addr != "0.0.0.0:8888" {
		t.Errorf("addr = %q, want 0.0.0.0:8888", addr)
	}

	if advertise == "" {
		t.Error("advertise should be the LAN IP for a wildcard bind")
	}
}

func TestParseDeviceAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value    string
		wantOK   bool
		wantAddr string
		wantPort int
	}{
		{"192.168.1.21", true, "192.168.1.21", 8009},
		{"192.168.1.21:9000", true, "192.168.1.21", 9000},
		{"TV", false, "", 0},
		{"Living Room TV", false, "", 0},
		{"*", false, "", 0},
		{"192.168.1.21:notaport", false, "", 0},
		{"nothost:9000", false, "", 0},
	}

	for _, tt := range tests {
		dev, ok := parseDeviceAddr(tt.value)
		if ok != tt.wantOK {
			t.Errorf("parseDeviceAddr(%q) ok = %v, want %v", tt.value, ok, tt.wantOK)

			continue
		}

		if !ok {
			continue
		}

		if dev.Addr != tt.wantAddr || dev.Port != tt.wantPort {
			t.Errorf("parseDeviceAddr(%q) = %s:%d, want %s:%d",
				tt.value, dev.Addr, dev.Port, tt.wantAddr, tt.wantPort)
		}
	}
}

func TestCastSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status *cast.MediaStatus
		want   string
	}{
		{nil, ""},
		{&cast.MediaStatus{State: cast.StateConnected}, ""},
		{&cast.MediaStatus{State: "PLAYING", Position: 83 * time.Second, Duration: 14*time.Minute + 48*time.Second}, " | ▶ 01:23/14:48"},
		{&cast.MediaStatus{State: "PAUSED", Position: time.Second, Duration: time.Minute}, " | ⏸ 00:01/01:00"},
		{&cast.MediaStatus{State: "BUFFERING"}, " | ◌"},
		{&cast.MediaStatus{State: "IDLE"}, " | idle"},
	}

	for _, tt := range tests {
		if got := castSuffix(tt.status); got != tt.want {
			t.Errorf("castSuffix(%+v) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestCastAndVLCMutuallyExclusive(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()

	var out strings.Builder

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"movie.torrent", "--vlc", "--cast"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected mutual-exclusion error for --vlc --cast")
	}

	if !strings.Contains(err.Error(), "none of the others can be") &&
		!strings.Contains(err.Error(), "were all set") {
		t.Errorf("unexpected error text: %v", err)
	}
}
