# Documentation

## Using git-work

- [`README.md`](../README.md): what git-work is and a first session.
- [`AGENTS.md`](../AGENTS.md): the complete command reference and conventions.
- [The command reference](./md/git-work.md), generated from the binary
  (or `man git-work`, from [`man`](./man)).
- `git work quickstart`: the model and this repository's types on one page,
  for an AI agent.
- [The command-line map](./design/cli-convention.md): the shape every command follows.

## Design

Every feature has a design document in [`design`](./design),
recording what was decided and why.
A few to start with:

- [The terminal renderer](./design/terminal-renderer.md): every view kind and its keys.
- [Config entities](./design/config-entity.md): schema and flows stored as entities.
- [The configurable schema](./design/configurable-schema.md).
- [Jira sync](./design/jira-sync.md).
- [The store migration](./design/store-migration.md) from git-bug's format.

## The engine inherited from git-bug

git-work is a hard fork of [git-bug](https://github.com/git-bug/git-bug)
and keeps its distributed entity engine unmodified.

- [The data model](./design/data-model.md): how an entity is stored in git and merged.
- [`entity/dag/example_test.go`](../entity/dag/example_test.go):
  how to build a distributed entity of your own.
- [The DAG read order fix](./design/dag-read-order.md), the one bug fix made to it.
- [The architecture](./design/architecture.md), upstream's, which predates the fork.
