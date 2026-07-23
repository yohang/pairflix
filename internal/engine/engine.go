// Package engine wraps the anacrolix/torrent client for streaming use.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
)

// Config holds the engine configuration.
type Config struct {
	// DataDir is the directory where torrent data is written. Required.
	DataDir string
	// NoUpload disables uploading chunks to peers.
	NoUpload bool
	// Readahead is the number of bytes to prioritize ahead of the current
	// stream position.
	Readahead int64
}

// FileInfo describes a file inside a torrent.
type FileInfo struct {
	Index  int
	Path   string
	Length int64
}

// Stats is a snapshot of download progress.
type Stats struct {
	BytesCompleted int64
	Length         int64
	Peers          int
}

// Engine drives a torrent client for a single torrent.
type Engine struct {
	client    *torrent.Client
	t         *torrent.Torrent
	readahead int64
}

// New creates an Engine backed by an anacrolix/torrent client.
func New(cfg Config) (*Engine, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("engine: DataDir is required")
	}

	ccfg := torrent.NewDefaultClientConfig()
	ccfg.DataDir = cfg.DataDir
	ccfg.NoUpload = cfg.NoUpload
	ccfg.Logger = discardLogger()
	// Random BitTorrent listen port: avoids clashes between concurrent
	// pairflix instances (and parallel tests).
	ccfg.ListenPort = 0

	client, err := torrent.NewClient(ccfg)
	if err != nil {
		return nil, fmt.Errorf("engine: create client: %w", err)
	}

	return &Engine{client: client, readahead: cfg.Readahead}, nil
}

// discardLogger returns an anacrolix logger that drops everything, keeping
// the terminal free for our own output.
func discardLogger() log.Logger {
	l := log.NewLogger()
	l.Handlers = []log.Handler{log.DiscardHandler}

	return l
}

// IsMagnet reports whether src looks like a magnet link.
func IsMagnet(src string) bool {
	return strings.HasPrefix(src, "magnet:")
}

// Open adds a torrent from a magnet link or a .torrent file path and waits
// for its metadata. It returns early if ctx is canceled.
func (e *Engine) Open(ctx context.Context, src string) error {
	var (
		t   *torrent.Torrent
		err error
	)

	if IsMagnet(src) {
		t, err = e.client.AddMagnet(src)
	} else {
		t, err = e.client.AddTorrentFromFile(src)
	}

	if err != nil {
		return fmt.Errorf("engine: add torrent: %w", err)
	}

	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return fmt.Errorf("engine: waiting for metadata: %w", ctx.Err())
	}

	e.t = t

	return nil
}

// Name returns the torrent display name.
func (e *Engine) Name() string {
	return e.t.Name()
}

// Files returns all files in the torrent.
func (e *Engine) Files() []FileInfo {
	files := e.t.Files()
	infos := make([]FileInfo, len(files))

	for i, f := range files {
		infos[i] = FileInfo{Index: i, Path: f.DisplayPath(), Length: f.Length()}
	}

	return infos
}

// VideoFiles returns the files that look like streamable video.
func (e *Engine) VideoFiles() []FileInfo {
	var videos []FileInfo

	for _, fi := range e.Files() {
		if IsVideo(fi.Path) {
			videos = append(videos, fi)
		}
	}

	return videos
}

// Select marks the file at index for download and disables all others.
func (e *Engine) Select(index int) (*File, error) {
	files := e.t.Files()
	if index < 0 || index >= len(files) {
		return nil, fmt.Errorf("engine: file index %d out of range [0-%d]", index, len(files)-1)
	}

	for i, f := range files {
		if i == index {
			f.SetPriority(torrent.PiecePriorityNormal)
		} else {
			f.SetPriority(torrent.PiecePriorityNone)
		}
	}

	return &File{tf: files[index], readahead: e.readahead}, nil
}

// Stats returns a snapshot of download progress for the whole torrent.
func (e *Engine) Stats() Stats {
	ts := e.t.Stats()

	return Stats{
		BytesCompleted: e.t.BytesCompleted(),
		Length:         e.t.Length(),
		Peers:          ts.ActivePeers,
	}
}

// Close shuts the client down. Downloaded data is kept on disk.
func (e *Engine) Close() error {
	errs := e.client.Close()

	return errors.Join(errs...)
}

// File is a selected torrent file ready to be streamed.
type File struct {
	tf        *torrent.File
	readahead int64
}

// Name returns the file path inside the torrent.
func (f *File) Name() string {
	return f.tf.DisplayPath()
}

// Size returns the file length in bytes.
func (f *File) Size() int64 {
	return f.tf.Length()
}

// BytesCompleted returns how many bytes of the file are downloaded.
func (f *File) BytesCompleted() int64 {
	return f.tf.BytesCompleted()
}

// NewReader returns a reader over the file that prioritizes pieces around
// the read position. Reads abort when ctx is canceled.
func (f *File) NewReader(ctx context.Context) io.ReadSeekCloser {
	r := f.tf.NewReader()
	r.SetResponsive()

	if f.readahead > 0 {
		r.SetReadahead(f.readahead)
	}

	return &ctxReader{r: r, ctx: ctx}
}

// ctxReader binds a torrent.Reader to a context so blocking reads are
// canceled when the context is done (e.g. HTTP client disconnect).
type ctxReader struct {
	r   torrent.Reader
	ctx context.Context
}

func (c *ctxReader) Read(p []byte) (int, error) {
	return c.r.ReadContext(c.ctx, p)
}

// Seek implements io.Seeker by delegating to the torrent reader.
func (c *ctxReader) Seek(offset int64, whence int) (int64, error) {
	return c.r.Seek(offset, whence)
}

// Close implements io.Closer by delegating to the torrent reader.
func (c *ctxReader) Close() error {
	return c.r.Close()
}
