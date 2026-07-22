// Package cast discovers Chromecast video devices and plays streams on them.
package cast

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Device is a discovered Chromecast video device.
type Device struct {
	Name  string // friendly name (mDNS TXT fn=)
	Model string // model (mDNS TXT md=)
	Addr  string // IPv4 address
	Port  int
}

// Entry is a raw mDNS answer, decoupled from the go-chromecast types so
// filtering logic and tests stay dependency-free.
type Entry struct {
	UUID  string
	Name  string
	Model string
	CA    string // capability bitmask (mDNS TXT ca=)
	Addr  string
	Port  int
}

// videoOutCapability is bit 0 of the ca= capability bitmask.
const videoOutCapability = 1

// audioOnlyModels are model prefixes for devices without video output,
// used when the ca= field is missing or unparsable.
var audioOnlyModels = []string{
	"Chromecast Audio",
	"Google Cast Group",
	"Google Home",
	"Google Nest",
}

// isVideoDevice reports whether a device can render video: the VIDEO_OUT
// capability bit when available, a model denylist otherwise.
func isVideoDevice(ca, model string) bool {
	if bits, err := strconv.Atoi(ca); err == nil {
		return bits&videoOutCapability != 0
	}

	for _, prefix := range audioOnlyModels {
		if strings.HasPrefix(model, prefix) {
			return false
		}
	}

	return true
}

// ProbablySupported reports whether the default Chromecast media receiver
// is likely to play the container of name (mp4 and webm are safe bets).
func ProbablySupported(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".webm", ".m4v":
		return true
	default:
		return false
	}
}
