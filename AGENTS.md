# AGENTS.md

Guidance for AI coding agents working on pairflix.

## Project

pairflix is a peerflix equivalent in Go: it streams video from `.torrent`
files and magnet links over HTTP while downloading, optionally launching VLC.
Module: `github.com/yohang/pairflix`. Go ≥ 1.26.

## Commands

```sh
make build   # build bin/pairflix (version injected via ldflags)
make test    # go test -race -shuffle=on -coverprofile=coverage.out ./...
make lint    # golangci-lint run
make fmt     # golangci-lint fmt (gofumpt + goimports)
make vuln    # govulncheck ./...
make all     # lint + test + build
make tools   # install golangci-lint, govulncheck, lefthook, goreleaser
```

Always run `make lint` and `make test` before committing. Git hooks
(lefthook) run lint on commit and race tests on push.

## Architecture

```
cmd/pairflix          thin main → internal/cli.Execute()
internal/cli          cobra root command, flags, run.go orchestration,
                      interactive file picker, progress line
internal/engine       anacrolix/torrent wrapper; engine.File implements
                      server.Source
internal/server       HTTP server, /stream/{name} route, http.ServeContent
internal/vlc          VLC discovery (PATH + per-OS fallbacks) and launch
```

Dependency direction: `cli → engine, server, vlc`. `server` knows nothing
about torrents — it consumes the `server.Source` interface (`Name`, `Size`,
`NewReader(ctx)`).

The stream URL is the ONLY stdout output (pipeable); everything else goes to
stderr.

## anacrolix/torrent gotchas (pinned v1.61.0)

- Always block on `<-t.GotInfo()` (ctx-select) before `t.Files()`; magnet
  metadata can hang forever without peers.
- **One `NewReader` per HTTP request** — `http.ServeContent` seeks the
  reader; sharing one across requests races.
- Bind readers to the request context (`engine.ctxReader` uses
  `ReadContext`); otherwise disconnected clients leak goroutines blocked on
  piece downloads.
- Non-target files get `PiecePriorityNone` (see `Engine.Select`).
- `client.Close()` does NOT delete downloaded data — intentional, we keep it.
- The anacrolix client logs noisily by default; `engine.discardLogger()`
  silences it.
- `http.Server.Shutdown` would hang forever on an active infinite stream —
  `server.Server.Close()` uses `http.Server.Close()` deliberately.
- Go's mime table lacks `.mkv` → explicit Content-Type map in
  `internal/server/server.go`.

## Streaming notes

- mp4 with a trailing `moov` atom starts slowly (player must fetch the tail
  first); mkv streams best.
- VLC exiting (any exit code) is a normal end of session, not an error —
  only a failed spawn is an error (`internal/vlc/vlc.go`).

## Lint conventions (golangci-lint v2, strict)

- `wsl_v5`: blank lines around blocks, before returns, after multi-line
  statements. Run `make fmt` then fix remaining issues per package.
- `nolintlint`: nolint comments must be specific and explained:
  `//nolint:gosec // reason`. Bare `//nolint` fails.
- `fmt.Fprint*` error returns are excluded from errcheck via config; other
  ignored errors need `_ =` or a nolint with reason.
- `revive` checks doc comments on exported symbols including private-receiver
  methods.

## Testing rules

- No network, no real swarms — CI runs on Linux/macOS/Windows with `-race`.
- `server` is tested with httptest + a fake `Source` over `bytes.Reader`.
- `vlc.Launcher` has injectable `LookPath`/`Stat`/`Getenv`/`GOOS` so all
  platform paths are tested on every OS.
- Manual smoke test (legal content):
  `curl -LO https://webtorrent.io/torrents/sintel.torrent && ./bin/pairflix sintel.torrent`
