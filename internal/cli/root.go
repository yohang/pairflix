// Package cli implements the pairflix command line interface.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Build information, injected at link time via -ldflags "-X ...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// NewRootCommand builds the pairflix root command.
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pairflix",
		Short: "Stream torrent and magnet video files to your favorite player",
		Long: `pairflix streams video from torrent files and magnet links while
downloading, and plays it in VLC, any HTTP video player, or a Chromecast.`,
		Version:       fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	return cmd
}

// Execute runs the root command and returns a process exit code.
func Execute() int {
	if err := NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)

		return 1
	}

	return 0
}
