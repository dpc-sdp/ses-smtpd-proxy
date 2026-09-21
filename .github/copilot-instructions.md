# Copilot Instructions

## General best practices
- Keep changes focused and minimal; avoid unrelated refactors.
- Prefer small, readable functions and clear naming.
- Preserve existing behavior unless the task explicitly requires a change.
- Add or update tests when logic changes.
- Follow existing project patterns and keep documentation in sync with code changes.

## Pull request requirements
- Use **Release Please-compatible** pull request titles.
- Titles must follow Conventional Commits format: `<type>: <short description>`.
- Allowed types in this repository include: `feat`, `fix`, `chore`, `docs`, `refactor`, `perf`, `test`, `build`, `ci`, `revert`, `style`.
- Example titles:
  - `fix: handle SMTP timeout when SES endpoint is slow`
  - `docs: clarify environment variable defaults`
