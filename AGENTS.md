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
internal/cast         Chromecast mDNS discovery + playback session
                      (go-chromecast wrapper); only discover_dns.go and
                      session_real.go import the vishen library
internal/tui          Bubble Tea dashboard; consumes engine.Snapshot and
                      cast.MediaStatus only — never imports anacrolix
internal/units        shared byte/rate/time formatting
```

Dependency direction: `cli → engine, server, vlc, cast`. `server` knows
nothing about torrents — it consumes the `server.Source` interface (`Name`,
`Size`, `NewReader(ctx)`).

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

## Chromecast gotchas (go-chromecast v0.3.4 pinned)

- pflag `NoOptDefVal` on --cast: `--cast NAME` does NOT parse — the value
  form requires `--cast="NAME"`. Documented in help/README; keep it that way.
- The device fetches the stream over the LAN: casting binds `0.0.0.0` and
  advertises the LAN IP from `cast.LocalIPFor` (UDP dial trick, no packets).
  Localhost binds are rejected up front (`validateCastListen`).
- `application.MediaWait()` has no context — run it in a goroutine;
  `Close(true)` tears down the connection and unblocks it.
- Discovery collects the FULL 5s window (zeroconf can miss the first query)
  and dedupes by UUID; don't return on first hit — breaks multi-device pick.
- Real devices can answer as little as 2/10 queries (measured on a
  Chromecast with Google TV). Hence: enumeration runs 2 query rounds per
  window; ByName retries rounds up to NameTimeout (30s) with early exit;
  --cast accepts a literal IP[:port] to bypass discovery entirely
  (parseDeviceAddr in internal/cli/run.go).
- Audio-only filtering: TXT `ca` bitmask VIDEO_OUT bit 0; md-prefix denylist
  fallback (`internal/cast/device.go`).
- mkv/avi may LOAD_FAILED on the default receiver — warn-and-cast is the
  chosen behavior, no transcoding.
- Playback feedback: Caster.OnStatus receives a synthetic CONNECTED then
  polled MediaStatus snapshots (2s); the CLI prints state transitions and
  folds position/duration into the progress line (castSuffix in
  internal/cli/run.go).

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

## TUI notes

- The dashboard renders to stderr (`tea.WithOutput`) in alt-screen mode so
  stdout stays a bare pipeable stream URL. UI selection: `chooseUI` in
  internal/cli/run.go — TUI needs stderr AND stdin to be terminals and no
  `--no-tui`.
- The cli↔UI seam is `sessionUI` (internal/cli/ui.go); `plainUI` is the
  historical line output and must stay byte-compatible.
- The model is pure: tests drive `Update`/`View` directly, never
  `tea.Program.Run` (CI has no TTY). `tui.UI.send` buffers messages sent
  before `Run` starts.
- Cast controls run as `tea.Cmd` (network I/O must not block `Update`).
- engine.Snapshot is the only data bridge — keep anacrolix types out of
  the tui package.

## Testing rules

- No network, no real swarms — CI runs on Linux/macOS/Windows with `-race`.
- `server` is tested with httptest + a fake `Source` over `bytes.Reader`.
- `vlc.Launcher` has injectable `LookPath`/`Stat`/`Getenv`/`GOOS` so all
  platform paths are tested on every OS.
- Manual smoke test (legal content):
  `curl -LO https://webtorrent.io/torrents/sintel.torrent && ./bin/pairflix sintel.torrent`
