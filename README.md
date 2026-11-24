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

## Usage

GitHub commands default to the current repository. Use `--repo` (`-R`) with `[HOST/]OWNER/REPO` or a repository URL to select another repository without a local checkout.

Use `gh kit --help` or append `--help` to any command for available options. Failed invocations also show the relevant help on standard error.

### Pull request review comments

Read all inline review comments and replies as JSON:

```sh
gh kit pr comments --pr 123
gh kit pr comments --pr 123 --repo owner/repo
```

### Cancel workflow runs

Request cancellation of active workflow runs for a pull request's source branch, including earlier commits:

```sh
gh kit workflows cancel-all --pr 123
```

Completed runs are skipped. Runs without a PR association, such as push-triggered runs on the same source branch, are included.

### Read job logs

Read raw GitHub Actions job logs, preserving ANSI escape sequences:

```sh
gh kit workflows logs --job 124568269207
```

### Rerun workflow jobs

Rerun failed or timed-out jobs for a pull request's current head commit:

```sh
gh kit workflows rerun --pr 123 --failed
gh kit --repo owner/repo workflows rerun --pr 123 --failed
```

Use `--cancelled` instead of or alongside `--failed` to include cancelled jobs. GitHub also reruns dependent jobs. Active workflow runs are skipped. The command reports progress while listing runs, inspecting jobs, and requesting reruns. Job inspection uses bounded concurrency and must finish successfully before any reruns are requested.

### Agent Skills

The CLI ships with bundled Agent Skills for coding agents. Run `gh kit agent skills --help` for details.

## Development

Use the [justfile](justfile) for development commands. Run `just --list` to see available recipes.
