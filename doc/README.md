# Documentation

## Using git-work

- [`README.md`](../README.md): what git-work is and a first session.
- [The command reference](./md/git-work.md), generated from the binary
  (or `man git-work`, from [`man`](./man)).
- `git work quickstart`: the model, every command, and this repository's
  types and flows on one page, written for an AI agent and complete for anyone.
- [`AGENTS.md`](../AGENTS.md): the architecture and conventions, for working on git-work itself.
- [The command line](./design/cli.md): the shape every command follows.

## Design

The design is in [`design`](./design/README.md),
one document per aspect of the system,
each saying what holds now and why.

## Reference

- [The Jira Cloud REST API](./reference/jira-api.md), as far as the Jira sync relies on it,
  each fact confirmed or unconfirmed with its source.
- [The Jira sandbox](./jira-sandbox.md): how to set up a live Jira site to test the sync against.

## The engine inherited from git-bug

git-work is a hard fork of [git-bug](https://github.com/git-bug/git-bug)
and keeps its distributed entity engine.

- [The store](./design/store.md): how an entity is stored in git and merged.
- [`entity/dag/example_test.go`](../entity/dag/example_test.go):
  how to build a distributed entity of your own.
