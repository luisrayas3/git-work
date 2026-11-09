# Installation

`git-work` is a single Go binary.
It needs `git` on your `PATH` at run time:
config reads and remote transport go through it.

git dispatches `git work <command>` to any `git-work` on the `PATH`,
so both spellings work.

## With devenv

The repository is a Nix flake,
so a [devenv](https://devenv.sh) project takes it as an input.
Nix builds it from source with its own Go;
nothing needs Go installed.

```yaml
# devenv.yaml
inputs:
  git-work:
    url: github:chef-robotics/git-work
```

```nix
# devenv.nix
{ pkgs, inputs, ... }:
{
  packages = [ inputs.git-work.packages.${pkgs.stdenv.system}.default ];
}
```

`devenv.lock` pins the commit, so everyone on the project runs the same one;
`devenv update` moves it to the latest `trunk`.
To follow releases instead, name a tag: `github:chef-robotics/git-work?ref=v0.1.0`.
The first shell builds it, in about a minute;
after that it comes from the Nix store.
The package carries the man pages and the bash, zsh and fish completions.

## With Nix alone

```sh
nix profile install github:chef-robotics/git-work   # onto your PATH
nix run github:chef-robotics/git-work -- version     # or once, without installing
```

## From source, with Go

You need Go at the version `go.mod` names.

```sh
git clone https://github.com/chef-robotics/git-work
cd git-work
go build -o git-work .
```

Put the binary on your `PATH`, for instance with a symlink that follows rebuilds:

```sh
ln -s "$PWD/git-work" ~/.local/bin/git-work
```

`make install` instead builds with the version stamped in
and installs into `$(go env GOPATH)/bin`.

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

The version is the release in `VERSION` with the commit it was built from,
such as `v0.1.0+664ae8d`.
