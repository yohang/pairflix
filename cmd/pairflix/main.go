// Command pairflix streams torrent video files to local players and devices.
package main

import (
	"os"

	"github.com/yohang/pairflix/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
