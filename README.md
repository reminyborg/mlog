# mlog

A small CLI for editing a single personal markdown log file — an *mlog* — that mixes daily task tracking with free-form notes.

The format is plain markdown: dated `# YYYY-MM-DD` sections, `# Todo` and `# Backlog` lists, and `- [ ] [project] description` task lines.

## The mlog format

An *mlog* is a single file that mixes daily entries, task tracking, and free-form notes:

```markdown
[myproject]: https://github.com/user/myproject

# 2025-05-15

Shipped the auth refactor. Surprisingly smooth.

- [x] [myproject] Refactor authentication
- [ ] [myproject] Write migration guide

# Todo

- [ ] [myproject] Add rate limiting
- [ ] [myproject] Update docs

# Backlog

- [myproject] Explore edge caching
```

Key conventions:

- **Project references** (`[name]: url`) declared once at the top.
- **`# YYYY-MM-DD` sections** in chronological order — oldest first, newest just above `# Todo`.
- **`# Todo` / `# Backlog`** sit once at the very bottom, for work not tied to a specific day.
- **Tasks** use `- [ ]` / `- [x]`; completing one moves the line under today's date entry.

See [`mlog-format.md`](mlog-format.md) for the full specification, spacing rules, and philosophy.

## Install

### Download a release

Grab the binary for your platform from the [Releases page](https://github.com/reminyborg/mlog/releases) and put it on your `$PATH`. For example, on macOS/Linux:

```sh
# pick the right asset for your OS/arch
curl -fsSL https://github.com/reminyborg/mlog/releases/latest/download/mlog_<version>_<os>_<arch>.tar.gz \
  | tar -xz -C /tmp \
  && install /tmp/mlog ~/.local/bin/mlog
```

### With `go install`

Requires Go 1.25+.

```sh
go install github.com/reminyborg/mlog@latest
```

### Build from source

```sh
go build -o ~/.local/bin/mlog .
```

Or with [mise](https://mise.jdx.dev/):

```sh
mise run install
```

### Install the agent skill

The repo ships a `SKILL.md` describing the CLI to coding agents. It's embedded in the binary so you can install it without a separate download:

```sh
mlog skill install                  # write SKILL.md into every detected agent skill dir
mlog skill install --target claude  # only the named agent
mlog skill install --dir <path>     # write SKILL.md straight into an arbitrary directory
mlog skill install --dry-run        # print what would be written, change nothing
mlog skill print                    # dump SKILL.md to stdout (pipe wherever)
```

Auto-detection currently knows about Claude Code (`~/.claude/skills/mlog/SKILL.md`). For any other agent, use `mlog skill print > <path>` or `mlog skill install --dir <path>`.

## Usage

By default `mlog` reads and writes `~/log/log.md`. Override the path with the `--log <path>` flag or the `MLOG_FILE` environment variable — both accept `~/`-prefixed paths. Run `mlog path` to print the resolved path that is currently in use.

Running `mlog` with no arguments runs `list`. It's safe to call from scripts.

### Subcommands

| Command | Purpose |
| --- | --- |
| `list` | List incomplete tasks, grouped by section. |
| `create [-p project] [-t] <description>` | Create a task in `# Todo` (or today's entry with `-t`). |
| `complete <substring>` | Mark a task done and move it under today's date. |
| `schedule <substring>` | Move an incomplete task to today's entry. |
| `uncomplete <substring>` | Flip a `- [x]` back to `- [ ]` in place. |
| `delete <substring>` | Remove a task line (open or completed). |
| `today` | Print today's entry. |
| `show <YYYY-MM-DD>` | Print a specific date's entry. |
| `search <query>` | Case-insensitive substring search across the log. |
| `note [text]` | Append a free-form note to today's entry. With no text, composes it in `$VISUAL` / `$EDITOR`. |
| `sync [-p] [-P] [-m msg]` | Pull, commit, and push the log file via git. |
| `path` | Print the resolved path to the log file (respects `MLOG_FILE` and `--log`). |
| `edit [-t] [-d date]` | Open the log file in `$VISUAL` / `$EDITOR`; with `-t`/`-d`, only that day's entry. |
| `skill install` / `skill print` | Install the embedded `SKILL.md` into agent skill dirs, or print it to stdout. |

`complete`, `schedule`, `uncomplete`, and `delete` exit non-zero with `--line N` hints when a substring matches more than one task. Pass `-` (or pipe stdin) to `create` and `note` for multi-line input.

### Writing in your editor

Running `mlog note` with no text on a terminal opens a scratch markdown buffer in your editor, like `git commit` with no `-m`; saving an empty buffer aborts the note. Because the text never passes through the shell this way, backticks, `$`, and `[brackets]` are written to the log verbatim — no quoting needed.

`mlog edit -t` opens **today's whole entry** — heading, tasks, and every note under it — so a day you've already written can be revised, reworded, or reorganized as many times as you like. `mlog edit -d 2026-07-04` (or `-d yesterday`, `-d tomorrow`) does the same for any other day, creating the entry if it doesn't exist yet.

The edit is checked before it's saved: the entry has to keep its date heading and can't introduce a second `#` heading (that would start a new section), and the whole file must still pass `validate`. If a rewrite is rejected, nothing is written and your buffer is left on disk at the path printed in the error, so no work is lost.

`sync` defaults to pull-then-push. Use `-p`/`--pull-only` to only pull, `-P`/`--push-only` to only commit and push, and `-m`/`--message` to override the commit message.

Read commands (`list`, `search`, `today`, `show`) accept `--json` for stable, scriptable output with `lineIndex` and `section` fields.

## Development

```sh
mise run test    # go test ./...
mise run build   # go build -o mlog .
mise run run     # go run main.go
```

Source layout:

- `main.go` — Kong CLI wiring.
- `internal/log` — file parsing and all mutating operations.

## License

[MIT](LICENSE)
