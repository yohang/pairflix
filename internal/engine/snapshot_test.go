package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// writeTestTorrent builds a minimal single-file .torrent on disk and
// returns its path. No swarm, no data — metadata only.
func writeTestTorrent(t *testing.T) string {
	t.Helper()

	const (
		pieceLength = 16384
		fileLength  = 4*pieceLength + 100 // 5 pieces
		numPieces   = 5
	)

	info := metainfo.Info{
		Name:        "sample.mp4",
		PieceLength: pieceLength,
		Length:      fileLength,
		Pieces:      make([]byte, numPieces*20),
	}

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}

	mi := metainfo.MetaInfo{
		InfoBytes: infoBytes,
		AnnounceList: [][]string{
			{"udp://tracker.example.org:1337/announce"},
			{"http://backup.example.org/announce"},
		},
	}

	path := filepath.Join(t.TempDir(), "sample.torrent")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	defer f.Close() //nolint:errcheck // test file

	if err := mi.Write(f); err != nil {
		t.Fatalf("write metainfo: %v", err)
	}

	return path
}

func openTestEngine(t *testing.T) *Engine {
	t.Helper()

	eng, err := New(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { eng.Close() }) //nolint:errcheck // test cleanup

	if err := eng.Open(context.Background(), writeTestTorrent(t)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	return eng
}

func TestSnapshotShape(t *testing.T) {
	t.Parallel()

	eng := openTestEngine(t)

	snap := eng.Snapshot()

	if snap.Length != 4*16384+100 {
		t.Errorf("Length = %d, want file length", snap.Length)
	}

	if snap.BytesCompleted != 0 || snap.BytesReadData != 0 {
		t.Errorf("fresh torrent should have zero completed/read, got %d/%d",
			snap.BytesCompleted, snap.BytesReadData)
	}

	if len(snap.Peers) != 0 {
		t.Errorf("no swarm: peers = %d, want 0", len(snap.Peers))
	}

	total := 0
	for _, run := range snap.Pieces {
		total += run.Length
	}

	if total != 5 {
		t.Errorf("piece runs sum = %d, want 5 pieces", total)
	}

	if snap.Taken.IsZero() {
		t.Error("Taken should be set")
	}
}

func TestTrackers(t *testing.T) {
	t.Parallel()

	eng := openTestEngine(t)

	trackers := eng.Trackers()
	if len(trackers) != 2 {
		t.Fatalf("trackers = %v, want 2 URLs", trackers)
	}
}
