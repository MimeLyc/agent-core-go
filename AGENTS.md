# Repository Guidelines

This repository is currently a minimal skeleton. Use the conventions below as you add code, tooling, and CI.

## Project Structure & Module Organization

- `src/`: production code (group by domain/module; keep public APIs small and documented).
- `tests/`: automated tests mirroring `src/` paths (e.g., `tests/auth/test_tokens.py` for `src/auth/tokens.py`).
- `scripts/`: developer/CI utilities (one task per script).
- `docs/`: design notes and runbooks (architecture, threat model, ops).
- `assets/`: static files used by docs or the app.
- `.github/`: workflows and issue/PR templates (if GitHub is used).

## Build, Test, and Development Commands

Standardize entry points so contributors don’t need to memorize tool-specific flags.

- `make setup`: install dependencies and initialize local dev environment.
- `make test`: run the full test suite locally/CI.
- `make lint`: run static analysis (linters, type checks).
- `make fmt`: apply formatters (no manual formatting).
- `make run`: run the app/CLI in development mode.
- If you don’t use `make`, provide equivalents (e.g., `npm run test`, `python -m pytest`) and document them here.

## Coding Style & Naming Conventions

- Indentation: 2 spaces for JSON/YAML/Markdown; follow language defaults elsewhere.
- Naming: `snake_case` for files and most symbols; `PascalCase` for types/classes; `SCREAMING_SNAKE_CASE` for constants.
- Tooling: prefer adding `.editorconfig` plus a formatter/linter per language, and keep CI enforcing them.

## Testing Guidelines

- Tests are required for behavior changes and bug fixes.
- Prefer small unit tests; add integration tests for security-sensitive flows.
- Keep tests deterministic (no network, no system time unless mocked).

## Commit & Pull Request Guidelines

- If no established history exists, use Conventional Commits (e.g., `feat: add policy parser`, `fix: handle empty input`).
- PRs should include: intent/summary, linked issue (if any), test plan (`make test` output or steps), and any security impact.

## Security & Configuration Tips

- Never commit secrets; use `.env`/secret managers and add generated files to `.gitignore`.
- Document required environment variables in `docs/` (with safe defaults).

## Agent Behavior

- Never ask the user questions; decide and proceed.
- If the current directory is a git repo, always check the latest commit and working tree changes to judge progress (e.g., `git log -1`, `git show -1`, `git status`, `git diff`).
- If you decide a change requires updating `README.md`, make the `README.md` updates in English.
- When you need library/API documentation, code generation patterns, or setup/configuration steps, use Context7 MCP first (do not wait for the user to explicitly ask).

# Requirement Rules

If you are asked to design new requirements, treat it as a large and complex task and follow these rules:

- The task requirements live in `docs/specs/<spec_dir>/requirements.md`; read it carefully before working.
- Break large tasks into smaller sub-tasks (and adjust the breakdown as you learn more).
- For each sub-task, write a detailed work plan in `docs/specs/<spec_dir>/plan.md`.
- If a sub-task becomes complex after deeper investigation, further break it down in the work plan.

Track development progress in `docs/specs/<spec_dir>/impl_details/task_progress.md` so you don’t lose sight of overall goals.

During codebase exploration, organize what you learn in `docs/knowledge/*.md`:

- Record facts, details, and code locations (file paths, symbols, behaviors); avoid high-level summaries.
- Before starting work, try to read from existing knowledge first.

If you complete all task items in `docs/specs/<spec_dir>/impl_details/task_progress.md`, start a new task:

- Carefully review current changes (`git diff` and the staging area), compare them with the work plan, and verify that all critical components in the design have been implemented.
- Identify engineering improvements (code quality, performance, readability, modularity, test coverage, correctness) and record them in `docs/specs/<spec_dir>/plan.md`.

Finally, never ask me any questions. You decide everything by yourself. You can always explore the code base and read from existing knowledge to resolve your doubts.
