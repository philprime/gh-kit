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

From a GitHub repository checkout, request cancellation of active workflow runs for a pull request's source branch, including earlier commits:

```sh
gh kit workflows cancel-all --pr 123
```

Completed runs are skipped. Runs without a PR association, such as push-triggered runs on the same source branch, are included.

## Development

Use the [justfile](justfile) for development commands. Run `just --list` to see available recipes.
