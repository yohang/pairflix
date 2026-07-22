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
	var opts options

	cmd := &cobra.Command{
		Use:   "pairflix <torrent-file|magnet-link>",
		Short: "Stream torrent and magnet video files to your favorite player",
		Long: `pairflix streams video from torrent files and magnet links while
downloading, and plays it in VLC, any HTTP video player, or a Chromecast.

The stream URL is printed on stdout; open it in any player, or pass --vlc
to launch VLC directly. Downloaded data is kept on disk.`,
		Example: `  pairflix movie.torrent
  pairflix "magnet:?xt=urn:btih:..." --vlc
  pairflix movie.torrent --listen 0.0.0.0:8888`,
		Args:          cobra.ExactArgs(1),
		Version:       fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, args[0], opts)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.vlc, "vlc", false, "launch VLC on the stream and exit when it closes")
	flags.StringVar(&opts.listen, "listen", "", "HTTP listen address (default: 127.0.0.1 on a random port)")
	flags.StringVar(&opts.path, "path", "", "download directory (default: a new temp dir, kept on exit)")
	flags.BoolVar(&opts.noUpload, "no-upload", false, "do not upload to peers")
	flags.Int64Var(&opts.readahead, "readahead", 16, "stream readahead in MiB")

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
