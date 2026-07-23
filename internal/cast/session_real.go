package cast

import (
	"time"

	"github.com/vishen/go-chromecast/application"
)

// realApp adapts go-chromecast's application to the App interface.
type realApp struct {
	*application.Application
}

// MediaStatus maps the current receiver media state to a MediaStatus.
func (r realApp) MediaStatus() (MediaStatus, bool) {
	_, media, _ := r.Status()
	if media == nil {
		return MediaStatus{}, false
	}

	return MediaStatus{
		State:    media.PlayerState,
		Reason:   media.IdleReason,
		Position: time.Duration(media.CurrentTime) * time.Second,
		Duration: time.Duration(media.Media.Duration) * time.Second,
	}, true
}

// newRealApp builds a real go-chromecast session.
func newRealApp() App {
	return realApp{application.NewApplication()}
}
