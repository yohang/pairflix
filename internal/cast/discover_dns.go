package cast

import (
	"context"

	"github.com/vishen/go-chromecast/dns"
)

// discoverDNS adapts go-chromecast's mDNS discovery to the Entry stream.
// All interfaces are used (nil interface = library default).
func discoverDNS(ctx context.Context) (<-chan Entry, error) {
	castEntries, err := dns.DiscoverCastDNSEntries(ctx, nil)
	if err != nil {
		return nil, err
	}

	entries := make(chan Entry)

	go func() {
		defer close(entries)

		for ce := range castEntries {
			entries <- Entry{
				UUID:  ce.UUID,
				Name:  ce.DeviceName,
				Model: ce.Device,
				CA:    ce.InfoFields["ca"],
				Addr:  ce.AddrV4.String(),
				Port:  ce.Port,
			}
		}
	}()

	return entries, nil
}
