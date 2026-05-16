---
name: mlog
description: Read and mutate the personal markdown log file (~/log/log.md). Use when the user asks to log tasks, check today's tasks, complete/schedule/delete/search tasks, add notes, manage projects, or sync the log with git.
compatibility: Requires the mlog binary on $PATH. Install from https://github.com/reminyborg/mlog/releases or via go install github.com/reminyborg/mlog@latest
---

# mlog

Personal markdown log CLI. File lives at `~/log/log.md` (override with `$MLOG_FILE` or `--log <path>`).

## Reading

Always pass `--json` when scripting — output is stable and machine-readable.

```sh
mlog list --json              # incomplete tasks: [{lineIndex, section, project, description, line}]
mlog list -p myproject        # filter incomplete tasks to a single project
mlog list -p myproject --json # same, machine-readable
mlog today --json             # today's entry: {date, found, content}
mlog search "query" --json    # [{lineIndex, section, line}] — searches all lines including completed
mlog search -p myproject      # all lines tagged [myproject] (no query needed)
mlog search "query" -p myproject  # query restricted to [myproject] lines
mlog show 2026-05-14 --json   # specific date entry: {date, found, content}
```

## Mutating

```sh
mlog create -p myproject "description"   # add to ## Todo under [myproject]
mlog create -t "description"             # add to today's entry instead of ## Todo
mlog complete "substring"                # mark done, moves line to today's entry
mlog schedule "substring"                # move incomplete task to today's entry
mlog uncomplete "substring"              # flip - [x] back to - [ ] in place
mlog delete "substring"                  # remove task line (open or completed)
printf 'multi\nline\n' | mlog note -    # append free-form note to today's entry
mlog edit                                # open log in $EDITOR
```

## Disambiguation

`complete`, `schedule`, `uncomplete`, and `delete` exit non-zero when a substring matches
more than one task. Stderr prints `--line N` hints for each candidate:

```sh
mlog complete "refactor"
# error: 3 tasks match "refactor". Re-run with --line N:
#   --line 12  - [ ] [mlog] refactor parser  (## Todo)
#   --line 34  - [ ] [api] refactor auth  (## Todo)

mlog complete --line 12    # unambiguous
```

Line indexes go stale after any mutation — always re-list before re-running.

## Projects

Projects are `[name]: url` reference-link definitions at the top of the file.
They appear as `[name]` tags on task lines and optionally as `### name` H3
headings inside `## Todo`.

```sh
mlog project list --json             # [{name, url, lineIndex}]
mlog project add myproject https://github.com/user/myproject
mlog project add internal            # URL is optional
mlog project delete myproject        # removes the ref line (tasks are unchanged)
mlog project edit myproject --rename newname          # renames ref + all task tags + H3 headings
mlog project edit myproject --url https://new.example # change URL only
mlog project edit myproject --rename newname --url https://new.example
```

`edit --rename` rewrites the definition line **and** every `[oldname]` tag in
task lines **and** every `### oldname` H3 heading atomically. Errors if the
new name already exists.

## Git sync

```sh
mlog sync          # git pull --rebase --autostash, then commit + push if changed
mlog sync -p       # pull only
mlog sync -P       # commit + push only (no pull)
mlog sync -m "msg" # custom commit message (default: "mlog: sync YYYY-MM-DD")
```

## Typical agent workflow

```sh
mlog sync -p                          # pull latest before reading or mutating
mlog list --json                      # inspect open tasks
mlog create -p myproject "new task"   # mutate
mlog sync                             # commit + push when done
```
