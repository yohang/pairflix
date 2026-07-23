// Package web serves the browser interface for web-triggered streaming.
package web

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/yohang/pairflix/internal/engine"
	"github.com/yohang/pairflix/internal/stream"
)

//go:embed index.html
var indexHTML []byte

// maxTorrentSize caps .torrent uploads.
const maxTorrentSize = 10 << 20

// Controller is the web layer's view of the stream manager.
type Controller interface {
	Status() stream.Status
	Start(src string, opts stream.StartOptions) error
	Select(index int) error
	Stop() error
}

// Info describes the fixed backend for display.
type Info struct {
	Backend string // "chromecast" | "vlc" | "http"
	Device  string
}

// NewHandler builds the web UI + API handler.
func NewHandler(c Controller, info Info, logf func(format string, args ...any)) http.Handler {
	if logf == nil {
		logf = func(string, ...any) {}
	}

	h := &handler{c: c, info: info, logf: logf}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.index)
	mux.HandleFunc("GET /api/status", h.status)
	mux.HandleFunc("POST /api/stream", h.stream)
	mux.HandleFunc("POST /api/select", h.selectFile)
	mux.HandleFunc("POST /api/stop", h.stop)

	return mux
}

type handler struct {
	c    Controller
	info Info
	logf func(format string, args ...any)
}

func (h *handler) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

// statusResponse is the JSON shape of GET /api/status.
type statusResponse struct {
	State     string              `json:"state"`
	Torrent   string              `json:"torrent,omitempty"`
	File      string              `json:"file,omitempty"`
	StreamURL string              `json:"streamUrl,omitempty"`
	Files     []stream.FileChoice `json:"files,omitempty"`
	Error     string              `json:"error,omitempty"`
	Backend   string              `json:"backend"`
	Device    string              `json:"device,omitempty"`
}

func (h *handler) status(w http.ResponseWriter, _ *http.Request) {
	st := h.c.Status()

	writeJSON(w, http.StatusOK, statusResponse{
		State:     string(st.State),
		Torrent:   st.Torrent,
		File:      st.FileName,
		StreamURL: st.StreamURL,
		Files:     st.Files,
		Error:     st.Err,
		Backend:   h.info.Backend,
		Device:    h.info.Device,
	})
}

func (h *handler) stream(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")

	switch {
	case strings.HasPrefix(ct, "application/json"):
		h.streamMagnet(w, r)
	case strings.HasPrefix(ct, "multipart/form-data"):
		h.streamUpload(w, r)
	default:
		writeError(w, http.StatusUnsupportedMediaType, "expected JSON magnet or multipart torrent upload")
	}
}

func (h *handler) streamMagnet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Magnet string `json:"magnet"`
	}

	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")

		return
	}

	magnet := strings.TrimSpace(body.Magnet)
	if !engine.IsMagnet(magnet) {
		writeError(w, http.StatusBadRequest, "not a magnet link")

		return
	}

	h.start(w, r, magnet, false)
}

func (h *handler) streamUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTorrentSize)

	file, header, err := r.FormFile("torrent")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "torrent file too large")

			return
		}

		writeError(w, http.StatusBadRequest, "missing torrent file field")

		return
	}

	defer file.Close() //nolint:errcheck // request-scoped reader

	tmp, err := os.CreateTemp("", "pairflix-upload-*.torrent")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot store upload")

		return
	}

	if _, err := io.Copy(tmp, file); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())

		writeError(w, http.StatusInternalServerError, "cannot store upload")

		return
	}

	// Close before handing off: Windows cannot reopen an open file.
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())

		writeError(w, http.StatusInternalServerError, "cannot store upload")

		return
	}

	h.logf("web: received torrent upload %q (%d bytes)", header.Filename, header.Size)
	h.start(w, r, tmp.Name(), true)
}

// start hands the source to the controller, mapping busy to 409.
func (h *handler) start(w http.ResponseWriter, r *http.Request, src string, removeSource bool) {
	opts := stream.StartOptions{
		AdvertiseHost: requestHost(r),
		RemoveSource:  removeSource,
	}

	if err := h.c.Start(src, opts); err != nil {
		if removeSource {
			_ = os.Remove(src)
		}

		if errors.Is(err, stream.ErrBusy) {
			writeError(w, http.StatusConflict, "a stream is already active")

			return
		}

		writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *handler) selectFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Index int `json:"index"`
	}

	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")

		return
	}

	if err := h.c.Select(body.Index); err != nil {
		if errors.Is(err, stream.ErrNotSelecting) {
			writeError(w, http.StatusConflict, "no file selection pending")

			return
		}

		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *handler) stop(w http.ResponseWriter, _ *http.Request) {
	if err := h.c.Stop(); err != nil {
		h.logf("web: stop: %v", err)
	}

	w.WriteHeader(http.StatusOK)
}

// requestHost extracts the host (no port) the client used to reach us, so
// stream URLs are reachable from the client's side of the network.
func requestHost(r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	if host == "" || host == "localhost" {
		return ""
	}

	return host
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
