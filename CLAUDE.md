# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`mlog` is a Go CLI that edits a single personal markdown log file (an "mlog" — see `mlog-format.md` for the format spec). Default location is `~/log/log.md`, overridable with `--log` or `$MLOG_FILE`. The CLI exposes subcommands (`list`, `create`, `complete`, `uncomplete`, `delete`, `today`, `show`, `search`, `note`, `edit`, `tui`) and a Bubble Tea TUI (`tui` is the default subcommand when none is given).

## Commands

Defined in `mise.toml`; run with `mise run <task>` or directly with `go`:

- `mise run build` — `go build -o mlog .`
- `mise run test` — `go test ./...`
- `mise run run` — `go run main.go` (launches TUI by default)
- `mise run install` — builds to `~/.local/bin/mlog`
- Single test: `go test ./internal/log -run TestCompleteTask_MovesToTodayAndRemovesOriginal`

## Architecture

Two packages under `internal/`:

- **`internal/log`** owns all state. `Store` holds the file path and an injectable `now func() time.Time` (tests pin a fixed date via `setTodayKey`). Every mutating method (`CreateTask`, `CompleteTask`, `Uncomplete`, `Delete`, `AppendToToday`) reads the whole file into `[]string`, splices, and writes atomically (temp file + `os.Rename` in the same dir). Parsing is regex-driven (`reH1`/`reH2`/`reH3`, `reIncomplete`, `reCompletedBox`, `reAnyTaskBox`, `reProjectTag`). Generic `splice` and `findIndex` helpers mimic JS semantics. `findTaskMatches(lines, matchText, box)` is the shared lookup used by `CompleteTask`/`Uncomplete`/`Delete` — pass the relevant box regex to scope the match.
- **`internal/tui`** is a Bubble Tea model wrapping `*log.Store`. A task list view and a prompt state machine (`promptKind`) for the create flow. All persistence delegates back to the `Store` — no parsing logic lives in the TUI.

`main.go` wires Kong CLI → `Context{Store}` → subcommand `Run`.

## Non-obvious invariants

- **"Today" is local time** (`Store.todayKey` calls `time.Now()` without converting). Date headers reflect the user's wall clock.
- **`CompleteTask` moves the line** to today's H1 section, creating the header if it doesn't exist, then deletes the original line. The line ends up in exactly one place.
- **`Uncomplete` does NOT move the line** — it flips `- [x]` back to `- [ ]` in place, leaving it under whatever section it was completed under. This is asymmetric with `CompleteTask` because we don't track where the task came from.
- **`Delete` collapses one pair of adjacent blanks** after removal, mirroring `completeAtLine`'s cleanup so file spacing doesn't degrade.
- **Project-aware insertion in `## Todo`**: `CreateTask` looks for an `### <project>` H3 inside `## Todo` and appends there; otherwise inserts above the first H3 in the section (so freshly-tagged tasks don't land mid-subsection).
- **Insertion adjacent to a heading adds a trailing blank** (`needsTrailing` checks in `CreateTask`/`CompleteTask`/`AppendToToday`) to preserve the file's blank-line-before-heading convention. `AppendToToday` also adds a leading blank when the previous line is non-blank, so notes aren't glued to the prior task. When editing these functions, preserve that pattern or the file's spacing will degrade over edits.
- **Tests inject `now`** — when adding tests that depend on today's date, use `newTestStore` then `s.setTodayKey("YYYY-MM-DD")`.

## Format reference

`mlog-format.md` is the source of truth for the markdown conventions the parser expects (H1 dates, `## Todo`, `## Backlog`, `- [ ] [project] description`). When changing parser regexes or insertion rules, cross-check against that document.

## Using the CLI from Claude

When invoking `mlog` non-interactively, prefer these forms:

- **Reads:** pass `--json` to `list`, `search`, `today`, `show`. The output is stable JSON with `lineIndex`, `section`, and (where applicable) `project`/`description` fields. Without `--json` the output is human-formatted with section headers and is harder to parse reliably.
- **Completing / uncompleting / deleting tasks:** all three share the same disambiguation. If a substring matches more than one task, the command exits non-zero and prints candidates as `--line N` hints on stderr. Use `mlog list --json` (or `mlog search --json` to find completed tasks) to look up the `lineIndex` and re-run with `--line N` for an unambiguous mutation. The line index becomes stale after any mutation, so re-list before re-running. `delete` matches both open and completed task lines; `uncomplete` only matches `- [x]` lines.
- **Multi-line notes / bracketed content:** pass `-` as the arg, or omit args entirely when stdin is piped. Examples: `printf 'line1\nline2\n' | mlog note -` and `echo '[proj] new task' | mlog create -p proj -`.
- **Bare `mlog` with no TTY** falls through to `list` instead of opening the Bubble Tea TUI, so it's safe to call from scripts.
