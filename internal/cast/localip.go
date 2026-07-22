package cast

import (
	"fmt"
	"net"
)

// LocalIPFor returns the local source IP the OS routes toward addr
// ("host:port"). The UDP dial performs no network I/O.
func LocalIPFor(addr string) (string, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return "", fmt.Errorf("cast: resolve local IP for %s: %w", addr, err)
	}

	defer conn.Close() //nolint:errcheck // no I/O happened on this socket

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", fmt.Errorf("cast: unexpected local address type %T", conn.LocalAddr())
	}

	return localAddr.IP.String(), nil
}
