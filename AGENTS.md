# AGENTS.md

`gh-kit` is a Go-based GitHub CLI extension for reusable workflow utilities. See [README.md](README.md) for the public overview.

## Ground rules

- Prefer existing `justfile` recipes for development tasks. Run tools directly when no suitable recipe exists.
- Run one shell command per invocation. Do not chain commands or wrap sequences in `bash -c`. A pipeline is allowed for a single data flow.
- Run `git` from the project working directory. Use `git -C` only when targeting another repository.
- Draft commit messages and PR descriptions in Markdown files under `tmp/`, then pass them with `git commit -F` or `gh pr create --body-file`.
- Use Conventional Commits. When creating a branch, use a type prefix such as `feature/`, `fix/`, `refactor/`, `docs/`, `chore/`, or `test/`.
- Never add AI or co-author attribution to commits, PRs, code, or comments.
- Use `jq` for shell-based JSON processing and `yq` for YAML. Use native Go parsing in application code.
- Write prose without em dashes or semicolons. Use full sentences.
- Keep documentation concise. Link to configuration and code rather than duplicating details that can drift.
- Wrap Go errors with context using `fmt.Errorf("context: %w", err)` and lowercase messages. Log an error or return it, never both.
- Add a focused failing test before implementing new command behavior or fixing bugs. Test GitHub API behavior with the real REST client and a controlled HTTP transport, not live requests.
- Verify changes with the relevant justfile recipes before claiming completion or committing. For Go changes, run `just build test vet`. Run `just format` for formatting.
- Keep durable project instructions in `AGENTS.md`, not a separate memory system.

## Bundled Agent Skills

- Keep bundled skill content embedded in the binary through `cmd/agent/bundled.go`. Do not fetch skills at runtime or depend on files beside the executable.
- When changing public commands, update the relevant bundled skill under `cmd/agent/skills/` so its examples and guidance remain accurate.
- Preserve the installer's ownership and recovery guarantees. Preflight the full selection before mutations, never overwrite unmanaged files or follow installation symlinks, and keep dry runs write-free. Retain regression tests for conflicts and interrupted operations.
- Test skill installation and removal only in temporary directories. Isolate the home directory and working directory when testing default and project scope. Never use real user or project skill directories for smoke tests.
