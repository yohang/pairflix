package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/yohang/pairflix/internal/cast"
	"github.com/yohang/pairflix/internal/engine"
	"github.com/yohang/pairflix/internal/server"
	"github.com/yohang/pairflix/internal/vlc"
)

// castAuto is the sentinel --cast value meaning "discover and pick".
const castAuto = "*"

// options holds the root command flags.
type options struct {
	vlc       bool
	cast      string
	listen    string
	path      string
	noUpload  bool
	readahead int64 // MiB
}

// session groups everything serveLoop needs to run a streaming session.
type session struct {
	srv       *server.Server
	eng       *engine.Engine
	file      *engine.File
	streamURL string
	vlcBin    string
	castDev   *cast.Device
	errOut    io.Writer
}

// run streams the torrent or magnet in source until interrupted (or until
// the player session ends when --vlc or --cast is set).
func run(cmd *cobra.Command, source string, opts options) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errOut := cmd.ErrOrStderr()

	// Fail fast: locate the playback target before downloading anything.
	vlcBin, err := findVLC(opts)
	if err != nil {
		return err
	}

	castDev, err := findCastDevice(ctx, cmd, opts)
	if err != nil {
		return err
	}

	dataDir, err := resolveDataDir(opts.path)
	if err != nil {
		return err
	}

	fmt.Fprintf(errOut, "Data directory: %s\n", dataDir)

	eng, err := engine.New(engine.Config{
		DataDir:   dataDir,
		NoUpload:  opts.noUpload,
		Readahead: opts.readahead << 20,
	})
	if err != nil {
		return err
	}

	defer func() {
		_ = eng.Close()

		fmt.Fprintf(errOut, "\nData kept at %s\n", dataDir)
	}()

	fmt.Fprintln(errOut, "Fetching metadata...")

	if err := eng.Open(ctx, source); err != nil {
		return err
	}

	fmt.Fprintf(errOut, "Torrent: %s\n", eng.Name())

	file, err := selectVideoFile(cmd, eng)
	if err != nil {
		return err
	}

	srv := server.New(file)

	listenAddr, advertiseHost, err := resolveListen(opts, castDev)
	if err != nil {
		return err
	}

	streamURL, err := srv.Start(listenAddr, advertiseHost)
	if err != nil {
		return err
	}

	// The URL is the only stdout output, so it stays pipeable.
	fmt.Fprintln(cmd.OutOrStdout(), streamURL)

	if castDev != nil && !cast.ProbablySupported(file.Name()) {
		fmt.Fprintf(errOut,
			"Warning: %s may not play on the default Chromecast receiver; casting anyway.\n",
			filepath.Ext(file.Name()))
	}

	return serveLoop(ctx, stop, session{
		srv:       srv,
		eng:       eng,
		file:      file,
		streamURL: streamURL,
		vlcBin:    vlcBin,
		castDev:   castDev,
		errOut:    errOut,
	})
}

// findVLC locates the VLC binary when --vlc is set.
func findVLC(opts options) (string, error) {
	if !opts.vlc {
		return "", nil
	}

	return vlc.NewLauncher().Find()
}

// findCastDevice discovers and selects the Chromecast target when --cast is
// set: by name, automatically for a single device, interactively otherwise.
func findCastDevice(ctx context.Context, cmd *cobra.Command, opts options) (*cast.Device, error) {
	if opts.cast == "" {
		return nil, nil //nolint:nilnil // no cast requested is not an error
	}

	if err := validateCastListen(opts.listen); err != nil {
		return nil, err
	}

	errOut := cmd.ErrOrStderr()
	discoverer := cast.NewDiscoverer()

	fmt.Fprintf(errOut, "Searching for Chromecast devices (%s)...\n", discoverer.Timeout)

	if opts.cast != castAuto {
		dev, err := discoverer.ByName(ctx, opts.cast)
		if err != nil {
			return nil, err
		}

		fmt.Fprintf(errOut, "Casting to: %s (%s)\n", dev.Name, dev.Model)

		return &dev, nil
	}

	devices, err := discoverer.Devices(ctx)
	if err != nil {
		return nil, err
	}

	dev, err := pickDevice(cmd.InOrStdin(), errOut, devices)
	if err != nil {
		return nil, err
	}

	return &dev, nil
}

// validateCastListen rejects --listen addresses a Chromecast cannot reach.
func validateCastListen(listen string) error {
	if listen == "" {
		return nil
	}

	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("invalid --listen address %q: %w", listen, err)
	}

	if host == "localhost" {
		return errors.New("a Chromecast cannot reach a localhost-bound server; use --listen 0.0.0.0:<port> or omit --listen")
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return errors.New("a Chromecast cannot reach a loopback-bound server; use --listen 0.0.0.0:<port> or omit --listen")
	}

	return nil
}

// resolveListen returns the bind address and the host to advertise in the
// stream URL. Casting requires a LAN-reachable bind and URL.
func resolveListen(opts options, castDev *cast.Device) (string, string, error) {
	if castDev == nil {
		return opts.listen, "", nil
	}

	lanIP, err := cast.LocalIPFor(net.JoinHostPort(castDev.Addr, strconv.Itoa(castDev.Port)))
	if err != nil {
		return "", "", err
	}

	if opts.listen == "" {
		return "0.0.0.0:0", lanIP, nil
	}

	host, _, err := net.SplitHostPort(opts.listen)
	if err != nil {
		return "", "", fmt.Errorf("invalid --listen address %q: %w", opts.listen, err)
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		return opts.listen, lanIP, nil
	}

	// Explicit non-wildcard host: bind and advertise it as given.
	return opts.listen, "", nil
}

// serveLoop runs the HTTP server, progress reporting and the optional
// player session (VLC or Chromecast) until the context is canceled.
func serveLoop(ctx context.Context, cancel context.CancelFunc, s session) error {
	group, gctx := errgroup.WithContext(ctx)

	group.Go(s.srv.Serve)

	group.Go(func() error {
		<-gctx.Done()

		return s.srv.Close()
	})

	group.Go(func() error {
		reportProgress(gctx, s.eng, s.file, s.errOut)

		return nil
	})

	if s.vlcBin != "" {
		group.Go(func() error {
			launcher := vlc.NewLauncher()

			err := launcher.Run(gctx, s.vlcBin, s.streamURL)

			// VLC exiting ends the session, whatever the outcome.
			cancel()

			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}

			return nil
		})
	}

	if s.castDev != nil {
		group.Go(func() error {
			caster := cast.NewCaster()

			err := caster.Run(gctx, *s.castDev, s.streamURL, server.ContentType(s.file.Name()))

			// The cast session ending ends pairflix, like VLC.
			cancel()

			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}

			return nil
		})
	}

	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

// reportProgress writes a single self-overwriting progress line every 2s.
func reportProgress(ctx context.Context, eng *engine.Engine, file *engine.File, out io.Writer) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := eng.Stats()
			done := file.BytesCompleted()
			total := file.Size()

			var pct float64
			if total > 0 {
				pct = float64(done) / float64(total) * 100
			}

			fmt.Fprintf(out, "\r⇣ %s / %s (%.1f%%)  peers: %d   ",
				humanBytes(done), humanBytes(total), pct, stats.Peers)
		}
	}
}

// resolveDataDir returns path (created if needed) or a fresh temp dir.
func resolveDataDir(path string) (string, error) {
	if path == "" {
		dir, err := os.MkdirTemp("", "pairflix-")
		if err != nil {
			return "", fmt.Errorf("create temp dir: %w", err)
		}

		return dir, nil
	}

	if err := os.MkdirAll(path, 0o750); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}

	return path, nil
}

// selectVideoFile picks the video file to stream, interactively when the
// torrent has several candidates.
func selectVideoFile(cmd *cobra.Command, eng *engine.Engine) (*engine.File, error) {
	videos := eng.VideoFiles()
	if len(videos) == 0 {
		errOut := cmd.ErrOrStderr()

		fmt.Fprintln(errOut, "Files in torrent:")

		for _, fi := range eng.Files() {
			fmt.Fprintf(errOut, "  %s (%s)\n", fi.Path, humanBytes(fi.Length))
		}

		return nil, errors.New("no video files found in torrent")
	}

	index, err := pickFile(cmd.InOrStdin(), cmd.ErrOrStderr(), videos)
	if err != nil {
		return nil, err
	}

	return eng.Select(index)
}
