package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeSource serves a fixed byte slice and records reader usage.
type fakeSource struct {
	name string
	data []byte

	mu      sync.Mutex
	readers int
	closed  int
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Size() int64 { return int64(len(f.data)) }

func (f *fakeSource) NewReader(_ context.Context) io.ReadSeekCloser {
	f.mu.Lock()
	f.readers++
	f.mu.Unlock()

	return &fakeReader{Reader: bytes.NewReader(f.data), src: f}
}

type fakeReader struct {
	*bytes.Reader
	src *fakeSource
}

func (r *fakeReader) Close() error {
	r.src.mu.Lock()
	r.src.closed++
	r.src.mu.Unlock()

	return nil
}

func startTestServer(t *testing.T, src Source) string {
	t.Helper()

	srv := New(src)

	streamURL, err := srv.Start("")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	go srv.Serve() //nolint:errcheck // test server, error irrelevant after Close

	t.Cleanup(func() { srv.Close() }) //nolint:errcheck // test cleanup

	return streamURL
}

func TestServeFullContent(t *testing.T) {
	t.Parallel()

	src := &fakeSource{name: "dir/Movie.mkv", data: []byte("0123456789")}
	streamURL := startTestServer(t, src)

	resp, err := http.Get(streamURL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test response

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "video/x-matroska" {
		t.Errorf("Content-Type = %q, want video/x-matroska", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "0123456789" {
		t.Errorf("body = %q, want full content", body)
	}
}

func TestServeRange(t *testing.T) {
	t.Parallel()

	src := &fakeSource{name: "movie.mp4", data: []byte("0123456789")}
	streamURL := startTestServer(t, src)

	req, _ := http.NewRequest(http.MethodGet, streamURL, nil)
	req.Header.Set("Range", "bytes=5-9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET range: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test response

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}

	if cr := resp.Header.Get("Content-Range"); cr != "bytes 5-9/10" {
		t.Errorf("Content-Range = %q, want bytes 5-9/10", cr)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "56789" {
		t.Errorf("body = %q, want 56789", body)
	}
}

func TestReaderPerRequest(t *testing.T) {
	t.Parallel()

	src := &fakeSource{name: "movie.mkv", data: []byte("abc")}
	streamURL := startTestServer(t, src)

	for range 2 {
		resp, err := http.Get(streamURL)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}

		io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining test body
		resp.Body.Close()              //nolint:errcheck // test response
	}

	src.mu.Lock()
	defer src.mu.Unlock()

	if src.readers != 2 {
		t.Errorf("readers = %d, want 2 (one per request)", src.readers)
	}

	if src.closed != 2 {
		t.Errorf("closed = %d, want 2 (each reader closed)", src.closed)
	}
}

func TestStartRandomPort(t *testing.T) {
	t.Parallel()

	src := &fakeSource{name: "movie.mkv", data: []byte("x")}
	streamURL := startTestServer(t, src)

	u, err := url.Parse(streamURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", streamURL, err)
	}

	if u.Hostname() != "127.0.0.1" {
		t.Errorf("host = %q, want 127.0.0.1", u.Hostname())
	}

	if u.Port() == "" || u.Port() == "0" {
		t.Errorf("port = %q, want a real port", u.Port())
	}

	if !strings.HasPrefix(u.Path, "/stream/") {
		t.Errorf("path = %q, want /stream/ prefix", u.Path)
	}
}

func TestContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{"movie.mkv", "video/x-matroska"},
		{"movie.MP4", "video/mp4"},
		{"movie.webm", "video/webm"},
		{"movie.unknownext", "application/octet-stream"},
	}

	for _, tt := range tests {
		if got := contentType(tt.name); got != tt.want {
			t.Errorf("contentType(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func ExampleServer() {
	src := &fakeSource{name: "movie.mkv", data: []byte("hello")}
	srv := New(src)

	streamURL, _ := srv.Start("127.0.0.1:0")
	defer srv.Close() //nolint:errcheck // example cleanup

	u, _ := url.Parse(streamURL)
	fmt.Println(u.Path)
	// Output: /stream/movie.mkv
}
