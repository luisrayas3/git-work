# Installation

`git-work` is a single Go binary.
There are no packaged releases yet, so it is built from source.

## Requirements

- `git`, on your `PATH`: config reads and remote transport go through it.
- Go, at the version `go.mod` names, to build.
  `.tool-versions` pins the versions development uses, for [asdf](https://asdf-vm.com).

## Build

```sh
git clone https://github.com/luisrayas3/git-work
cd git-work
go build -o git-work .
```

Put the binary on your `PATH`, for instance with a symlink that follows rebuilds:

```sh
ln -s "$PWD/git-work" ~/.local/bin/git-work
```

`make install` instead builds with the version stamped in
and installs into `$(go env GOPATH)/bin`.

git dispatches `git work <command>` to any `git-work` on the `PATH`,
so both spellings work.

## Completion and man pages

Completions for bash, zsh, fish and PowerShell are generated into
[`misc/completion`](misc/completion), and man pages into [`doc/man`](doc/man).
Source or install them the way your shell and system expect, for example:

```sh
# fish
ln -s "$PWD/misc/completion/fish/git-work" ~/.config/fish/completions/git-work.fish
# man
export MANPATH="$PWD/doc/man:$MANPATH"
```

## Verify

```sh
git work version
git work quickstart | head
```
