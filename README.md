# pairflix

[![CI](https://github.com/yohang/pairflix/actions/workflows/ci.yml/badge.svg)](https://github.com/yohang/pairflix/actions/workflows/ci.yml)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

A [peerflix](https://github.com/mafintosh/peerflix) equivalent written in Go:
stream video from torrents while they download.

## Features

- Stream video from `.torrent` files and magnet links while downloading
- Serve an HTTP video stream (Range/seek support) for any player
- Play directly in VLC (`--vlc`)
- Cast to Google Chromecast and Chromecast with Google TV (`--cast`)

Built on [anacrolix/torrent](https://github.com/anacrolix/torrent) (BitTorrent
engine), [spf13/cobra](https://github.com/spf13/cobra) (CLI) and
[vishen/go-chromecast](https://github.com/vishen/go-chromecast) (casting).

## Install

```sh
go install github.com/yohang/pairflix/cmd/pairflix@latest
```

Or grab a binary from the [releases page](https://github.com/yohang/pairflix/releases).

## Usage

```sh
# Stream and print the URL (open it in any player)
pairflix movie.torrent
pairflix "magnet:?xt=urn:btih:..."

# Launch VLC directly; pairflix exits when VLC closes
pairflix movie.torrent --vlc

# Cast to a Chromecast; pairflix exits when playback stops
pairflix movie.torrent --cast                     # discover, pick if several
pairflix movie.torrent --cast="Living Room TV"    # target by name (the "=" is required!)
pairflix movie.torrent --cast=192.168.1.21        # direct IP, no discovery

# Options
pairflix movie.torrent --listen 0.0.0.0:8888      # fixed listen address
pairflix movie.torrent --path ~/Downloads/movie   # keep data somewhere specific
pairflix movie.torrent --no-upload                # don't upload to peers
pairflix movie.torrent --no-tui                   # plain line output
```

## Dashboard

In a terminal, pairflix shows a full-screen dashboard: transfer rates,
peer table, piece map, tracker list, playback state and an event log.
Keys: `q` quit, `space` pause/resume cast, `s` stop cast. It falls back to
plain line output with `--no-tui` or automatically when output is piped —
the stream URL is always written raw to stdout either way.

The stream URL is the only stdout output, so it can be piped. Downloaded
data is always kept on disk (the directory is printed on exit).

### Casting notes

- The Chromecast fetches the stream itself, so pairflix binds `0.0.0.0` on a
  random port while casting and hands the device your LAN IP. Your firewall
  must allow inbound connections to that port (macOS will prompt).
- `--cast NAME` without `=` does not work — pflag optional-value flags
  require `--cast="NAME"`.
- Some devices (notably Chromecast with Google TV) answer only a fraction of
  mDNS queries: a name search retries for up to 30s, and `--cast=IP[:port]`
  skips discovery entirely.
- The default Chromecast receiver plays mp4/webm reliably; mkv/avi may fail
  to load (pairflix warns but casts anyway; Google TV devices are more
  capable).

## Development

Requirements: Go ≥ 1.26, `make`.

```sh
make tools   # install golangci-lint, govulncheck, lefthook, goreleaser
make hooks   # install git hooks (lint on commit, tests on push)

make lint    # golangci-lint
make test    # go test -race + coverage
make build   # build bin/pairflix with version info
make vuln    # govulncheck
make help    # list all targets
```

CI runs lint, tests (Linux/macOS/Windows), govulncheck, CodeQL and a GoReleaser
config check on every push and pull request. Tags matching `v*` publish
cross-platform release binaries automatically.

## License

[GPL-3.0](LICENSE)
