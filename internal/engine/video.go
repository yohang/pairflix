package engine

import (
	"path/filepath"
	"strings"
)

// videoExtensions lists file extensions considered streamable video.
var videoExtensions = map[string]struct{}{
	".3gp":  {},
	".avi":  {},
	".flv":  {},
	".m2ts": {},
	".m4v":  {},
	".mkv":  {},
	".mov":  {},
	".mp4":  {},
	".mpeg": {},
	".mpg":  {},
	".ogv":  {},
	".ts":   {},
	".webm": {},
	".wmv":  {},
}

// IsVideo reports whether path has a known video file extension.
func IsVideo(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))

	_, ok := videoExtensions[ext]

	return ok
}
