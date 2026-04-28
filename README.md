# mlog

A small CLI and TUI for editing a single personal markdown log file — an *mlog* — that mixes daily task tracking with free-form notes.

The format is plain markdown: dated `# YYYY-MM-DD` sections, `## Todo` and `## Backlog` lists, and `- [ ] [project] description` task lines. See [`mlog-format.md`](mlog-format.md) for the full spec.

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

## Usage

By default `mlog` reads and writes `~/log/log.md`. Override with `--log <path>` or `MLOG_FILE`.

Running `mlog` with no arguments launches the Bubble Tea TUI. Piped or non-TTY invocations fall through to `list` so it's safe to call from scripts.

### Subcommands

| Command | Purpose |
| --- | --- |
| `list` | List incomplete tasks, grouped by section. |
| `create [-p project] [-t] <description>` | Create a task in `## Todo` (or today's entry with `-t`). |
| `complete <substring>` | Mark a task done and move it under today's date. |
| `uncomplete <substring>` | Flip a `- [x]` back to `- [ ]` in place. |
| `delete <substring>` | Remove a task line (open or completed). |
| `today` | Print today's entry. |
| `show <YYYY-MM-DD>` | Print a specific date's entry. |
| `search <query>` | Case-insensitive substring search across the log. |
| `note <text>` | Append a free-form note to today's entry. |
| `edit` | Open the log file in `$VISUAL` / `$EDITOR`. |
| `tui` | Launch the interactive TUI (default). |

`complete`, `uncomplete`, and `delete` exit non-zero with `--line N` hints when a substring matches more than one task. Pass `-` (or pipe stdin) to `create` and `note` for multi-line input.

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
- `internal/tui` — Bubble Tea model.

## License

[MIT](LICENSE)
