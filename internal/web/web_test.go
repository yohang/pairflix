package web

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yohang/pairflix/internal/stream"
)

// stubController records calls and returns scripted results.
type stubController struct {
	mu        sync.Mutex
	status    stream.Status
	startErr  error
	selectErr error
	started   []string
	opts      []stream.StartOptions
	selected  []int
	stopped   int
}

func (s *stubController) Status() stream.Status { return s.status }

func (s *stubController) Start(src string, opts stream.StartOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.started = append(s.started, src)
	s.opts = append(s.opts, opts)

	return s.startErr
}

func (s *stubController) Select(index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.selected = append(s.selected, index)

	return s.selectErr
}

func (s *stubController) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stopped++

	return nil
}

func serve(t *testing.T, c Controller) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(NewHandler(c, Info{Backend: "chromecast", Device: "TV (Google TV)"}, nil))
	t.Cleanup(srv.Close)

	return srv
}

func TestIndexServed(t *testing.T) {
	t.Parallel()

	srv := serve(t, &stubController{})

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}

	defer resp.Body.Close() //nolint:errcheck // test response

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)

	if !strings.Contains(buf.String(), "pairflix") || buf.Len() < 500 {
		t.Errorf("index page suspicious (%d bytes)", buf.Len())
	}
}

func TestStatusShape(t *testing.T) {
	t.Parallel()

	c := &stubController{status: stream.Status{
		State:     stream.StateStreaming,
		Torrent:   "Sintel",
		FileName:  "Sintel.mp4",
		StreamURL: "http://192.168.1.114:1234/stream/Sintel.mp4",
	}}
	srv := serve(t, c)

	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}

	defer resp.Body.Close() //nolint:errcheck // test response

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for key, want := range map[string]string{
		"state": "streaming", "torrent": "Sintel", "file": "Sintel.mp4",
		"backend": "chromecast", "device": "TV (Google TV)",
	} {
		if body[key] != want {
			t.Errorf("%s = %v, want %s", key, body[key], want)
		}
	}
}

func postJSON(t *testing.T, url string, body string) *http.Response {
	t.Helper()

	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}

	t.Cleanup(func() { resp.Body.Close() }) //nolint:errcheck // test response

	return resp
}

func TestStreamMagnet(t *testing.T) {
	t.Parallel()

	c := &stubController{}
	srv := serve(t, c)

	resp := postJSON(t, srv.URL+"/api/stream", `{"magnet":"magnet:?xt=urn:btih:abc"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	if len(c.started) != 1 || c.started[0] != "magnet:?xt=urn:btih:abc" {
		t.Errorf("started = %v", c.started)
	}

	if c.opts[0].RemoveSource {
		t.Error("magnet start should not remove source")
	}

	if c.opts[0].AdvertiseHost == "" {
		t.Error("AdvertiseHost should carry the request host")
	}
}

func TestStreamBadMagnet(t *testing.T) {
	t.Parallel()

	srv := serve(t, &stubController{})

	if resp := postJSON(t, srv.URL+"/api/stream", `{"magnet":"http://nope"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestStreamBusy(t *testing.T) {
	t.Parallel()

	srv := serve(t, &stubController{startErr: stream.ErrBusy})

	if resp := postJSON(t, srv.URL+"/api/stream", `{"magnet":"magnet:?x"}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestStreamUpload(t *testing.T) {
	t.Parallel()

	c := &stubController{}
	srv := serve(t, c)

	var buf bytes.Buffer

	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("torrent", "movie.torrent")
	_, _ = fw.Write([]byte("d8:announce0:e"))
	_ = mw.Close()

	resp, err := http.Post(srv.URL+"/api/stream", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("POST upload: %v", err)
	}

	defer resp.Body.Close() //nolint:errcheck // test response

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	if len(c.started) != 1 || !c.opts[0].RemoveSource {
		t.Errorf("upload should start with RemoveSource, got %v %v", c.started, c.opts)
	}
}

func TestStreamWrongContentType(t *testing.T) {
	t.Parallel()

	srv := serve(t, &stubController{})

	resp, err := http.Post(srv.URL+"/api/stream", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}

	defer resp.Body.Close() //nolint:errcheck // test response

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", resp.StatusCode)
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()

	c := &stubController{}
	srv := serve(t, c)

	if resp := postJSON(t, srv.URL+"/api/select", `{"index":2}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if len(c.selected) != 1 || c.selected[0] != 2 {
		t.Errorf("selected = %v", c.selected)
	}
}

func TestSelectNotSelecting(t *testing.T) {
	t.Parallel()

	srv := serve(t, &stubController{selectErr: stream.ErrNotSelecting})

	if resp := postJSON(t, srv.URL+"/api/select", `{"index":0}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestStop(t *testing.T) {
	t.Parallel()

	c := &stubController{}
	srv := serve(t, c)

	resp, err := http.Post(srv.URL+"/api/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("POST stop: %v", err)
	}

	defer resp.Body.Close() //nolint:errcheck // test response

	if resp.StatusCode != http.StatusOK || c.stopped != 1 {
		t.Errorf("status=%d stopped=%d", resp.StatusCode, c.stopped)
	}
}
