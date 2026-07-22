package cast

import (
	"github.com/vishen/go-chromecast/application"
)

// newRealApp builds a real go-chromecast session.
func newRealApp() App {
	return application.NewApplication()
}
