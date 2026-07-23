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
	"github.com/yohang/pairflix/internal/tui"
	"github.com/yohang/pairflix/internal/units"
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
	noTUI     bool
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
	caster    *cast.Caster
	ui        sessionUI
	errOut    io.Writer
}

// shutdownGrace is how long a graceful shutdown may take once the session
// context is canceled before the process force-exits. Guards against rare
// hangs in third-party teardown paths (cast connection, torrent client).
const shutdownGrace = 15 * time.Second

// run streams the torrent or magnet in source until interrupted (or until
// the player session ends when --vlc or --cast is set).
func run(cmd *cobra.Command, source string, opts options) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errOut := cmd.ErrOrStderr()

	go func() {
		<-ctx.Done()

		// Restore default signal behavior: a second Ctrl+C kills the
		// process immediately instead of being swallowed.
		stop()

		fmt.Fprintln(errOut, "\nShutting down (press Ctrl+C again to force)...")
		time.Sleep(shutdownGrace)
		fmt.Fprintln(errOut, "forced exit: graceful shutdown timed out")
		os.Exit(1)
	}()

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

	caster := cast.NewCaster()
	ui := chooseUI(cmd, opts, eng, file, caster, castDev, vlcBin)

	if castDev != nil && !cast.ProbablySupported(file.Name()) {
		ui.Logf("Warning: %s may not play on the default Chromecast receiver; casting anyway.",
			filepath.Ext(file.Name()))
	}

	return serveLoop(ctx, stop, session{
		srv:       srv,
		eng:       eng,
		file:      file,
		streamURL: streamURL,
		vlcBin:    vlcBin,
		castDev:   castDev,
		caster:    caster,
		ui:        ui,
		errOut:    errOut,
	})
}

// chooseUI selects the dashboard TUI when the terminal supports it (and
// --no-tui is unset), the plain line output otherwise.
func chooseUI(
	cmd *cobra.Command,
	opts options,
	eng *engine.Engine,
	file *engine.File,
	caster *cast.Caster,
	castDev *cast.Device,
	vlcBin string,
) sessionUI {
	errOut := cmd.ErrOrStderr()

	if !useTUI(opts.noTUI, errOut, cmd.InOrStdin()) {
		return newPlainUI(errOut, eng, file)
	}

	backend := "http"

	var (
		device   string
		controls tui.CastController
	)

	switch {
	case castDev != nil:
		backend = "chromecast"
		device = castDev.Name

		if castDev.Model != "" {
			device += " (" + castDev.Model + ")"
		}

		controls = caster
	case vlcBin != "":
		backend = "vlc"
	}

	return tui.New(tui.Config{
		Out:          errOut,
		FileName:     file.Name(),
		FileSize:     file.Size(),
		FileProgress: file.BytesCompleted,
		Snapshot:     eng.Snapshot,
		Trackers:     eng.Trackers(),
		Backend:      backend,
		Device:       device,
		Controls:     controls,
	})
}

// findVLC locates the VLC binary when --vlc is set.
func findVLC(opts options) (string, error) {
	if !opts.vlc {
		return "", nil
	}

	return vlc.NewLauncher().Find()
}

// findCastDevice resolves the Chromecast target when --cast is set: a
// literal IP connects directly (no discovery), a name searches until found,
// bare --cast enumerates and picks.
func findCastDevice(ctx context.Context, cmd *cobra.Command, opts options) (*cast.Device, error) {
	if opts.cast == "" {
		return nil, nil //nolint:nilnil // no cast requested is not an error
	}

	if err := validateCastListen(opts.listen); err != nil {
		return nil, err
	}

	errOut := cmd.ErrOrStderr()

	// Escape hatch for devices with broken mDNS: an IP targets directly.
	if dev, ok := parseDeviceAddr(opts.cast); ok {
		fmt.Fprintf(errOut, "Casting to: %s (direct, no discovery)\n", dev.Addr)

		return dev, nil
	}

	discoverer := cast.NewDiscoverer()

	if opts.cast != castAuto {
		fmt.Fprintf(errOut, "Searching for Chromecast %q (up to %s)...\n",
			opts.cast, discoverer.NameTimeout)

		dev, err := discoverer.ByName(ctx, opts.cast)
		if err != nil {
			return nil, err
		}

		fmt.Fprintf(errOut, "Casting to: %s (%s)\n", dev.Name, dev.Model)

		return &dev, nil
	}

	fmt.Fprintf(errOut, "Searching for Chromecast devices (%s)...\n", discoverer.Timeout)

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

// defaultCastPort is the Chromecast control port used when --cast is given
// a bare IP.
const defaultCastPort = 8009

// parseDeviceAddr interprets a --cast value as "IP" or "IP:port". Anything
// that is not a literal IP address is a device name.
func parseDeviceAddr(value string) (*cast.Device, bool) {
	if ip := net.ParseIP(value); ip != nil {
		return &cast.Device{Name: value, Addr: value, Port: defaultCastPort}, true
	}

	host, portStr, err := net.SplitHostPort(value)
	if err != nil {
		return nil, false
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, false
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, false
	}

	return &cast.Device{Name: host, Addr: host, Port: port}, true
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

// serveLoop runs the HTTP server, the session UI and the optional player
// session (VLC or Chromecast) until the context is canceled or the UI
// quits.
func serveLoop(ctx context.Context, cancel context.CancelFunc, s session) error {
	group, gctx := errgroup.WithContext(ctx)

	group.Go(s.srv.Serve)

	group.Go(func() error {
		<-gctx.Done()

		return s.srv.Close()
	})

	group.Go(func() error {
		err := s.ui.Run(gctx)

		// The UI returning (user quit) ends the whole session.
		cancel()

		return err
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
			s.caster.OnStatus = s.ui.CastStatus

			err := s.caster.Run(gctx, *s.castDev, s.streamURL, server.ContentType(s.file.Name()))

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
			fmt.Fprintf(errOut, "  %s (%s)\n", fi.Path, units.HumanBytes(fi.Length))
		}

		return nil, errors.New("no video files found in torrent")
	}

	index, err := pickFile(cmd.InOrStdin(), cmd.ErrOrStderr(), videos)
	if err != nil {
		return nil, err
	}

	return eng.Select(index)
}
