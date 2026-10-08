# Contributing to git-work

Start with [`AGENTS.md`](./AGENTS.md).
It is written for coding agents but holds for everyone:
the architecture, the conventions, and the one rule that matters most —
the seven packages git-work inherits from git-bug unmodified
(`entity/dag`, `entity`, `repository`, `entities/identity`,
`util/lamport`, `util/text`, `util/timestamp`) are never edited
without an explicit decision.

## Development environment

git-work is Go only; there is no JavaScript toolchain.
You need `git` and the Go version `go.mod` names.
[`.tool-versions`](./.tool-versions) pins what development uses,
for [asdf](https://asdf-vm.com) (`asdf install` in the repository).
`goreleaser` there is only for building release artifacts locally.

## Building and testing

```shell
go build -o git-work .   # the binary, into ./git-work
make install             # stamped with the version, into $GOPATH/bin
make build/debug         # a debugger-friendly build (no optimisation, no inlining)
make test                # the Go test suite
```

## Checks

CI checks that the Go code is formatted (`gofmt -l`), that the generated files
below are up to date, and that no dependency has a known vulnerability
reachable from our code. That last one is `make secure` locally.

The command reference in `doc/md`, the man pages in `doc/man` and the shell
completions in `misc/completion` are generated from the command tree and
committed. If you add or change a command, run `go generate` and commit the
result. If it changes a command an agent uses, update `AGENTS.md` and
`host/quickstart.md` in the same change; a test checks that every command the
quickstart names exists.

## Design first

Every story gets a design document in [`doc/design`](./doc/design),
approved before its code is written.
It records the decisions and why, not a plan of steps.

## Releases

Releases are cut by [GoReleaser](https://goreleaser.com) when a `v*` tag is
pushed; see [`.goreleaser.yaml`](./.goreleaser.yaml) and
[`.github/workflows/release.yml`](./.github/workflows/release.yml).
`goreleaser release --snapshot --clean` builds one locally into `dist/`
without publishing anything.
