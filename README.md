# mlog

A small CLI for editing a single personal markdown log file — an *mlog* — that mixes daily task tracking with free-form notes.

The format is plain markdown: dated `# YYYY-MM-DD` sections, `## Todo` and `## Backlog` lists, and `- [ ] [project] description` task lines.

## The mlog format

An *mlog* is a single file that mixes daily entries, task tracking, and free-form notes:

```markdown
[myproject]: https://github.com/user/myproject

# 2025-05-15

Shipped the auth refactor. Surprisingly smooth.

- [x] [myproject] Refactor authentication
- [ ] [myproject] Write migration guide

## Todo

- [ ] [myproject] Add rate limiting
- [ ] [myproject] Update docs

## Backlog

- [myproject] Explore edge caching
```

Key conventions:

- **Project references** (`[name]: url`) declared once at the top.
- **`# YYYY-MM-DD` sections** in chronological order — oldest first, newest just above `## Todo`.
- **`## Todo` / `## Backlog`** sit once at the very bottom, for work not tied to a specific day.
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

By default `mlog` reads and writes `~/log/log.md`. Override with `--log <path>` or `MLOG_FILE`.

Running `mlog` with no arguments runs `list`. It's safe to call from scripts.

### Subcommands

| Command | Purpose |
| --- | --- |
| `list` | List incomplete tasks, grouped by section. |
| `create [-p project] [-t] <description>` | Create a task in `## Todo` (or today's entry with `-t`). |
| `complete <substring>` | Mark a task done and move it under today's date. |
| `schedule <substring>` | Move an incomplete task to today's entry. |
| `uncomplete <substring>` | Flip a `- [x]` back to `- [ ]` in place. |
| `delete <substring>` | Remove a task line (open or completed). |
| `today` | Print today's entry. |
| `show <YYYY-MM-DD>` | Print a specific date's entry. |
| `search <query>` | Case-insensitive substring search across the log. |
| `note <text>` | Append a free-form note to today's entry. |
| `sync [-p] [-P] [-m msg]` | Pull, commit, and push the log file via git. |
| `edit` | Open the log file in `$VISUAL` / `$EDITOR`. |
| `skill install` / `skill print` | Install the embedded `SKILL.md` into agent skill dirs, or print it to stdout. |

`complete`, `schedule`, `uncomplete`, and `delete` exit non-zero with `--line N` hints when a substring matches more than one task. Pass `-` (or pipe stdin) to `create` and `note` for multi-line input.

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
