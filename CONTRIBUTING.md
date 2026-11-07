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

CI builds, vets and tests on Linux and macOS, with the Go version `AGENTS.md`
requires and the next one. It needs no secrets: the tests that need a Jira
token skip without one, so `go test ./...` passes on a fresh clone too.
It also checks that the Go code is formatted (`gofmt -l`), that the generated
files below are up to date, and that no dependency has a known vulnerability
reachable from our code. That last one is `make secure` locally.

The command reference in `doc/md`, the man pages in `doc/man` and the shell
completions in `misc/completion` are generated from the command tree and
committed. If you add or change a command, run `go generate` and commit the
result. If it changes how git-work is used, update `host/quickstart.md`, the
static half of `git work quickstart`, in the same change; a test checks that
every command it names exists.

## Design first

Every story's design is written on the story issue,
and approved there before its code is written:
the decisions and why, not a plan of steps.
The change that builds it updates the design documents in
[`doc/design`](./doc/design/README.md),
which are organized by aspect
and say what holds now and why.

## Releases

Releases are cut by [GoReleaser](https://goreleaser.com) when a `v*` tag is
pushed; see [`.goreleaser.yaml`](./.goreleaser.yaml) and
[`.github/workflows/release.yml`](./.github/workflows/release.yml).
`goreleaser release --snapshot --clean` builds one locally into `dist/`
without publishing anything.
