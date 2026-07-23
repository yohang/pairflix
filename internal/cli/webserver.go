package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/yohang/pairflix/internal/stream"
	"github.com/yohang/pairflix/internal/web"
)

// webserverOptions holds the webserver subcommand flags.
type webserverOptions struct {
	vlc       bool
	cast      string
	path      string
	noUpload  bool
	readahead int64 // MiB
}

// newWebserverCommand builds the `pairflix webserver` subcommand.
func newWebserverCommand() *cobra.Command {
	var opts webserverOptions

	cmd := &cobra.Command{
		Use:   "webserver <listen-addr>",
		Short: "Serve a web interface that streams submitted torrents",
		Long: `webserver starts an HTTP server with a minimal interface: paste a
magnet link or upload a .torrent, press Stream, and the media plays on
the backend chosen at launch (--cast, --vlc, or a stream URL shown in
the page). One stream at a time; Stop ends it.

The interface is unauthenticated: anyone who can reach the listen
address controls playback.`,
		Example: `  pairflix webserver 127.0.0.1:8080
  pairflix webserver 0.0.0.0:8080 --cast="TV"
  pairflix webserver 0.0.0.0:8080 --vlc`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebserver(cmd, args[0], opts)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.vlc, "vlc", false, "play every stream in a local VLC")
	flags.StringVar(&opts.cast, "cast", "",
		`cast every stream to a Chromecast; bare --cast discovers devices, --cast="Name" targets one (the "=" is required)`)
	flags.StringVar(&opts.path, "path", "", "base directory for per-stream data dirs (default: system temp)")
	flags.BoolVar(&opts.noUpload, "no-upload", false, "do not upload to peers")
	flags.Int64Var(&opts.readahead, "readahead", 16, "stream readahead in MiB")

	flags.Lookup("cast").NoOptDefVal = castAuto

	cmd.MarkFlagsMutuallyExclusive("vlc", "cast")

	return cmd
}

// runWebserver serves the web UI until interrupted.
func runWebserver(cmd *cobra.Command, addr string, opts webserverOptions) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errOut := cmd.ErrOrStderr()

	installShutdownWatchdog(ctx, stop, errOut)

	logf := func(format string, args ...any) {
		fmt.Fprintf(errOut, time.Now().Format("15:04:05 ")+format+"\n", args...)
	}

	// Resolve the backend once, before serving anything.
	rootOpts := options{vlc: opts.vlc, cast: opts.cast}

	vlcBin, err := findVLC(rootOpts)
	if err != nil {
		return err
	}

	castDev, err := findCastDevice(ctx, cmd, rootOpts)
	if err != nil {
		return err
	}

	info := web.Info{Backend: "http"}

	switch {
	case castDev != nil:
		info.Backend = "chromecast"
		info.Device = castDev.Name

		if castDev.Model != "" {
			info.Device += " (" + castDev.Model + ")"
		}
	case vlcBin != "":
		info.Backend = "vlc"
	}

	if opts.path != "" {
		if err := os.MkdirAll(opts.path, 0o750); err != nil {
			return fmt.Errorf("create base dir: %w", err)
		}
	}

	manager := stream.New(ctx, stream.Config{
		Backend:   stream.Backend{CastDev: castDev, VLCBin: vlcBin},
		BaseDir:   opts.path,
		NoUpload:  opts.noUpload,
		Readahead: opts.readahead << 20,
		Logf:      logf,
	})

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           web.NewHandler(manager, info, logf),
		ReadHeaderTimeout: 10 * time.Second,
	}

	logf("web: listening on http://%s (backend: %s)", addr, info.Backend)

	group, gctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		err := httpSrv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err
	})

	group.Go(func() error {
		<-gctx.Done()

		manager.Close()

		return httpSrv.Close()
	})

	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}
