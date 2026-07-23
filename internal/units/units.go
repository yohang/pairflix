// Package units formats byte sizes, rates and playback times for display.
package units

import (
	"fmt"
	"time"
)

// HumanBytes formats n as a human-readable size in binary units.
func HumanBytes(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// HumanRate formats a bytes-per-second rate.
func HumanRate(bytesPerSec float64) string {
	return HumanBytes(int64(bytesPerSec)) + "/s"
}

// PlayTime formats a playback position as mm:ss, or h:mm:ss past an hour.
func PlayTime(d time.Duration) string {
	d = d.Round(time.Second)

	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}

	return fmt.Sprintf("%02d:%02d", m, s)
}
