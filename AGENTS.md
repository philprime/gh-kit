# AGENTS.md

`gh-kit` is a Go-based GitHub CLI extension for reusable workflow utilities. See [README.md](README.md) for the public overview.

## Ground rules

- Use the `justfile` as the entry point for development tasks. Do not run Go tools, formatters, or analyzers directly. If a recipe is missing, but will be used many times, adapt an existing one or add one before using it.
- Run one shell command per invocation. Do not chain commands or wrap sequences in `bash -c`. A pipeline is allowed for a single data flow.
- Run `git` from the project working directory. Use `git -C` only when targeting another repository.
- Draft commit messages and PR descriptions in Markdown files under `tmp/`, then pass them with `git commit -F` or `gh pr create --body-file`.
- Use Conventional Commits. When creating a branch, use a type prefix such as `feature/`, `fix/`, `refactor/`, `docs/`, `chore/`, or `test/`.
- Never add AI or co-author attribution to commits, PRs, code, or comments.
- Use `jq` for shell-based JSON processing and `yq` for YAML. Use native Go parsing in application code.
- Write prose without em dashes or semicolons. Use full sentences.
- Keep documentation concise. Link to configuration and code rather than duplicating details that can drift.
- Wrap Go errors with context using `fmt.Errorf("context: %w", err)` and lowercase messages. Log an error or return it, never both.
- Verify changes with the relevant justfile recipes before claiming completion or committing. For Go changes, run `just build test vet`. Run `just format` for formatting.
- Keep durable project instructions in `AGENTS.md`, not a separate memory system.
