# gh-kit

A GitHub CLI extension for everyday workflow utilities. Built around frequently used tasks, with commands kept reusable across repositories.

## Local installation

Install dependencies with [Homebrew](https://brew.sh/):

```sh
brew bundle
```

Authenticate with [GitHub CLI](https://cli.github.com/) using `gh auth login`. From a checkout directory named `gh-kit`, run:

```sh
just install
```

## Usage

From a GitHub repository checkout, request cancellation of active workflow runs for a pull request's source branch, including earlier commits:

```sh
gh kit workflows cancel-all --pr 123
```

Completed runs are skipped. Runs without a PR association, such as push-triggered runs on the same source branch, are included.

## Development

Use the [justfile](justfile) for development commands. Run `just --list` to see available recipes.
