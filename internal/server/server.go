// Package server serves a single video source over HTTP with Range support.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Source is a streamable file. It is implemented by engine.File.
type Source interface {
	// Name returns the file name (used for the URL, title and content type).
	Name() string
	// Size returns the total file size in bytes.
	Size() int64
	// NewReader returns a fresh reader over the file. Each HTTP request
	// must use its own reader. Reads abort when ctx is canceled.
	NewReader(ctx context.Context) io.ReadSeekCloser
}

// contentTypes maps video extensions Go's mime table gets wrong or misses.
var contentTypes = map[string]string{
	".avi":  "video/x-msvideo",
	".m2ts": "video/mp2t",
	".mkv":  "video/x-matroska",
	".mov":  "video/quicktime",
	".mp4":  "video/mp4",
	".ts":   "video/mp2t",
	".webm": "video/webm",
}

// Server streams a Source over HTTP.
type Server struct {
	src      Source
	listener net.Listener
	httpSrv  *http.Server
}

// New creates a Server for src.
func New(src Source) *Server {
	return &Server{src: src}
}

// Start listens on addr ("" means 127.0.0.1 on a random free port) and
// returns the full stream URL. When advertiseHost is non-empty the URL uses
// it instead of the bind address (needed when binding 0.0.0.0 for devices
// that fetch the stream over the LAN, like a Chromecast). It does not serve
// yet; call Serve.
func (s *Server) Start(addr, advertiseHost string) (string, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("server: listen on %s: %w", addr, err)
	}

	name := filepath.Base(s.src.Name())
	streamPath := "/stream/" + url.PathEscape(name)

	mux := http.NewServeMux()
	mux.HandleFunc("/stream/", s.handleStream)

	s.listener = listener
	s.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	host := listener.Addr().String()

	if advertiseHost != "" {
		_, port, err := net.SplitHostPort(host)
		if err != nil {
			return "", fmt.Errorf("server: split listen address %s: %w", host, err)
		}

		host = net.JoinHostPort(advertiseHost, port)
	}

	streamURL := url.URL{
		Scheme: "http",
		Host:   host,
		Path:   streamPath,
	}

	return streamURL.String(), nil
}

// Serve accepts connections until Close is called.
func (s *Server) Serve() error {
	err := s.httpSrv.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

// Close stops the server immediately. http.Server.Shutdown is deliberately
// avoided: an active stream never finishes, so Shutdown would hang forever.
func (s *Server) Close() error {
	if s.httpSrv == nil {
		return nil
	}

	return s.httpSrv.Close()
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	name := path.Base(s.src.Name())

	reader := s.src.NewReader(r.Context())
	defer reader.Close() //nolint:errcheck // best-effort close on a stream reader

	w.Header().Set("Content-Type", ContentType(name))

	http.ServeContent(w, r, name, time.Time{}, reader)
}

// ContentType resolves the Content-Type for a file name, preferring the
// explicit video map over Go's mime table (which lacks .mkv, for example).
func ContentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))

	if ct, ok := contentTypes[ext]; ok {
		return ct
	}

	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}

	return "application/octet-stream"
}
