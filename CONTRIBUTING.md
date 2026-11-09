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
`nix develop` gives a shell with both, from [`flake.nix`](./flake.nix);
[`.tool-versions`](./.tool-versions) pins Go for [asdf](https://asdf-vm.com) instead.

## Building and testing

```shell
go build -o git-work .   # the binary, into ./git-work
make install             # stamped with the version, into $GOPATH/bin
make build/debug         # a debugger-friendly build (no optimisation, no inlining)
make test                # the Go test suite
```

To test the Jira sync against a real Jira, see [`doc/jira-sandbox.md`](./doc/jira-sandbox.md).

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

## Versions

A version is a `v*` tag on the commit that sets [`VERSION`](./VERSION).
A Nix build has no `.git` to read tags from,
so the flake stamps the binary from that file, with the commit:
`v0.1.0+664ae8d`.
`make` stamps it with `git describe` instead.
To release, set `VERSION` in a commit on `trunk`, tag that commit `v<VERSION>`,
and push the tag;
CI fails a tag whose name differs from `VERSION`.
There are no release artifacts: devenv and Nix build from the tag.

## The flake

[`flake.nix`](./flake.nix) is how the team installs git-work
([`INSTALLATION.md`](./INSTALLATION.md)).
Its `vendorHash` is a hash of the Go dependencies,
and goes stale whenever `go.sum` changes;
CI runs `nix build` and fails when it has.
To update it, set it to `pkgs.lib.fakeHash`, run `nix build`,
and copy the hash it reports.
