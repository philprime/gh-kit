# gh-kit

A GitHub CLI extension for everyday workflow utilities. Built around frequently used tasks, with commands kept reusable across repositories.

## Installation

Install [GitHub CLI](https://cli.github.com/) and authenticate with `gh auth login`, then install the extension:

```sh
gh extension install philprime/gh-kit
```

Update to the latest release:

```sh
gh extension upgrade philprime/gh-kit
```

## Local installation

Install development dependencies with [Homebrew](https://brew.sh/):

```sh
brew bundle
```

Authenticate with [GitHub CLI](https://cli.github.com/) using `gh auth login`. From a checkout directory named `gh-kit`, run:

```sh
just install
just install-extension
```

## Available commands

| Command                                         | Description                                                                                                                                           |
| ----------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `gh kit pr comments --pr <number>`              | Read all inline pull request review comments and replies as JSON.                                                                                     |
| `gh kit workflows cancel-all --pr <number>`     | Cancel active workflow runs for a pull request's source branch, including earlier commits.                                                            |
| `gh kit workflows logs --job <id>`              | Read raw GitHub Actions job logs, preserving ANSI escape sequences.                                                                                   |
| `gh kit workflows rerun --pr <number> --failed` | Rerun failed or timed-out jobs for a pull request's current head commit. Use `--cancelled` instead or alongside `--failed` to include cancelled jobs. |
| `gh kit agent skills list`                      | List bundled Agent Skills.                                                                                                                            |
| `gh kit agent skills get <name>`                | Print a bundled Agent Skill.                                                                                                                          |
| `gh kit agent skills install <names...>`        | Install named bundled skills, or select all with `--all`.                                                                                             |
| `gh kit agent skills uninstall <names...>`      | Remove skills managed by gh-kit, or select all with `--all --yes`.                                                                                    |

Run `gh kit --help` or append `--help` to any command for available options. Skill installation and removal default to `~/.agents/skills`. Use `--project` for project scope, `--dir` for a custom `.agents` root, or `--dry-run` to preview changes.

## Usage

From a GitHub repository checkout, request cancellation of active workflow runs for a pull request's source branch, including earlier commits:

```sh
gh kit workflows cancel-all --pr 123
```

Completed runs are skipped. Runs without a PR association, such as push-triggered runs on the same source branch, are included.

## Development

Use the [justfile](justfile) for development commands. Run `just --list` to see available recipes.
