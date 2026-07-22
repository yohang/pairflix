package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/yohang/pairflix/internal/engine"
	"github.com/yohang/pairflix/internal/server"
	"github.com/yohang/pairflix/internal/vlc"
)

// options holds the root command flags.
type options struct {
	vlc       bool
	listen    string
	path      string
	noUpload  bool
	readahead int64 // MiB
}

// run streams the torrent or magnet in source until interrupted (or until
// VLC exits when --vlc is set).
func run(cmd *cobra.Command, source string, opts options) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errOut := cmd.ErrOrStderr()

	// Fail fast: locate VLC before downloading anything.
	var vlcBin string

	if opts.vlc {
		launcher := vlc.NewLauncher()

		bin, err := launcher.Find()
		if err != nil {
			return err
		}

		vlcBin = bin
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

	streamURL, err := srv.Start(opts.listen)
	if err != nil {
		return err
	}

	// The URL is the only stdout output, so it stays pipeable.
	fmt.Fprintln(cmd.OutOrStdout(), streamURL)

	return serveLoop(ctx, stop, srv, eng, file, vlcBin, streamURL, errOut)
}

// serveLoop runs the HTTP server, progress reporting and optional VLC
// session until the context is canceled.
func serveLoop(
	ctx context.Context,
	cancel context.CancelFunc,
	srv *server.Server,
	eng *engine.Engine,
	file *engine.File,
	vlcBin, streamURL string,
	errOut io.Writer,
) error {
	group, gctx := errgroup.WithContext(ctx)

	group.Go(srv.Serve)

	group.Go(func() error {
		<-gctx.Done()

		return srv.Close()
	})

	group.Go(func() error {
		reportProgress(gctx, eng, file, errOut)

		return nil
	})

	if vlcBin != "" {
		group.Go(func() error {
			launcher := vlc.NewLauncher()

			err := launcher.Run(gctx, vlcBin, streamURL)

			// VLC exiting ends the session, whatever the outcome.
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
