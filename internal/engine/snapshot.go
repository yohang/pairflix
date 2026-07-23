package engine

import (
	"sort"
	"time"
)

// PeerInfo describes one connected peer for display purposes.
type PeerInfo struct {
	Addr         string
	Client       string
	DownloadRate float64 // bytes/s, as computed by the torrent client
}

// PieceState classifies a run of pieces for a piece-map display.
type PieceState int

// Piece states, ordered by "how done" they are.
const (
	PiecePending PieceState = iota
	PiecePartial
	PieceChecking
	PieceComplete
)

// PieceRun is a run of consecutive pieces sharing a state.
type PieceRun struct {
	State  PieceState
	Length int
}

// Snapshot is a point-in-time view of the torrent transfer, in plain data
// so consumers (like the TUI) never touch the torrent library.
type Snapshot struct {
	BytesCompleted int64
	Length         int64

	// Cumulative payload counters; rate = delta / time between snapshots.
	BytesReadData    int64
	BytesWrittenData int64

	TotalPeers       int
	ActivePeers      int
	PendingPeers     int
	ConnectedSeeders int

	// Peers holds connected peers sorted by download rate, fastest first.
	Peers []PeerInfo

	// Pieces summarizes piece completion as runs, in piece order.
	Pieces []PieceRun

	Taken time.Time
}

// Snapshot captures the current transfer state.
func (e *Engine) Snapshot() Snapshot {
	stats := e.t.Stats()

	snap := Snapshot{
		BytesCompleted:   e.t.BytesCompleted(),
		Length:           e.t.Length(),
		BytesReadData:    stats.BytesReadData.Int64(),
		BytesWrittenData: stats.BytesWrittenData.Int64(),
		TotalPeers:       stats.TotalPeers,
		ActivePeers:      stats.ActivePeers,
		PendingPeers:     stats.PendingPeers,
		ConnectedSeeders: stats.ConnectedSeeders,
		Taken:            time.Now(),
	}

	for _, pc := range e.t.PeerConns() {
		info := PeerInfo{Addr: pc.RemoteAddr.String()}

		if name, ok := pc.PeerClientName.Load().(string); ok {
			info.Client = name
		}

		info.DownloadRate = pc.Stats().DownloadRate

		snap.Peers = append(snap.Peers, info)
	}

	sort.Slice(snap.Peers, func(i, j int) bool {
		return snap.Peers[i].DownloadRate > snap.Peers[j].DownloadRate
	})

	for _, run := range e.t.PieceStateRuns() {
		state := PiecePending

		switch {
		case run.Complete:
			state = PieceComplete
		case run.Hashing || run.QueuedForHash:
			state = PieceChecking
		case run.Partial:
			state = PiecePartial
		}

		snap.Pieces = append(snap.Pieces, PieceRun{State: state, Length: run.Length})
	}

	return snap
}

// Trackers returns the distinct announce URLs of the torrent.
func (e *Engine) Trackers() []string {
	mi := e.t.Metainfo()

	return mi.UpvertedAnnounceList().DistinctValues()
}
