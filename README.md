# pairflix

[![CI](https://github.com/yohang/pairflix/actions/workflows/ci.yml/badge.svg)](https://github.com/yohang/pairflix/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/yohang/pairflix)](https://goreportcard.com/report/github.com/yohang/pairflix)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

A [peerflix](https://github.com/mafintosh/peerflix) equivalent written in Go:
stream video from torrents while they download.

> **Status: early development.** Project scaffold only — streaming features are
> not implemented yet.

## Planned features

- Stream video from `.torrent` files
- Stream video from magnet links
- Play directly in VLC
- Serve an HTTP video stream for any player
- Cast to Chromecast

Built on [anacrolix/torrent](https://github.com/anacrolix/torrent) (BitTorrent
engine, added when streaming lands) and [spf13/cobra](https://github.com/spf13/cobra) (CLI).

## Install

```sh
go install github.com/yohang/pairflix/cmd/pairflix@latest
```

Or grab a binary from the [releases page](https://github.com/yohang/pairflix/releases).

## Usage

```sh
pairflix --help
pairflix --version
```

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
