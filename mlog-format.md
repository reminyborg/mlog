# mlog Format

A **mlog** is a personal development log format that combines technical work tracking with personal reflections and life events.

## Structure

### Date-Based Entries
Each entry uses a markdown header with the date in `YYYY-MM-DD` format:

```markdown
# 2024-04-01
```

### Project References
Projects are defined at the top of the file using markdown reference link syntax:

```markdown
[project-name]: https://github.com/user/repo
[another-project]: https://github.com/user/another-repo (Optional Description)
```

These references can then be used throughout the log by wrapping the project name in brackets.

### Task Tracking
Tasks use markdown checkbox syntax:

```markdown
- [x] Completed task
- [ ] Incomplete task
```

Tasks are typically attributed to a project:

```markdown
- [x] [project-name] Description of what was done
- [ ] [project-name] Description of what needs to be done
```

## Content Types

### Narrative Entries
Free-form text that provides context, reflections, and thoughts:

```markdown
# 2024-07-13

Worked a lot on lovefelt last night, very happy with the progress.
My work on [lovefelt] is becoming easier, unsure if this is because of all 
the work i have done for ecoonline on SwiftUI.
```

### Task Lists
Concrete items completed or planned for a specific day:

```markdown
- [x] [project] Implemented feature X
- [x] [project] Fixed bug Y
- [ ] [project] Need to refactor Z
```

### Todo Sections
Organized lists of upcoming work:

```markdown
## Todo

- [ ] [project] Feature to implement
- [ ] [project] Bug to fix
```

### Backlog Sections
Long-term or deferred items. Items can be plain bullets or use the same checkbox form as Todo:

```markdown
## Backlog

- [ ] [project] Research topic X
- [ ] [project] Investigate technology Y
- [project] Plain bullet (also allowed)
```

## Interacting with the File

### File Layout
A mlog file has three regions, top to bottom:

1. **Project references** — the `[name]: url` link definitions, declared once at the top.
2. **Date entries** — `# YYYY-MM-DD` sections in chronological order, oldest first.
3. **Global `## Todo` and `## Backlog`** — a single pair of sections at the very bottom of the file, holding work that isn't tied to a specific day. They are not per-date.

### Adding a New Day
Insert a new `# YYYY-MM-DD` header after the most recent date entry and before the global `## Todo` section. You're always appending to the end of the dated region — entries stay in chronological order and are never reordered.

### Completing a Task
A completed task is written `- [x]`. Two patterns are both valid:

- **Move it under today's date** (recommended) — relocate the line to today's `# YYYY-MM-DD` section, creating that header if it doesn't exist yet. This keeps recently finished work visible next to the day it was done.
- **Flip it in place** — change `- [ ]` to `- [x]` where it already sits. Simpler and closer to plain markdown, but the day it was finished isn't recorded.

Reopening a task is the reverse — change `- [x]` back to `- [ ]`. There's no record of where a task originally came from, so a reopened task just stays where it was checked off rather than moving back.

### Spacing
- Leave a blank line before every heading (`#`, `##`, `###`).
- Separate narrative paragraphs from task lists with a blank line.
- Keep a blank line between date entries and between the major sections so things don't run together.

## Key Characteristics

### Personal and Technical Mix
The mlog combines:
- Technical accomplishments
- Personal reflections
- Life events (family, hobbies, health)
- Learning notes

### Informal Style
- Stream-of-consciousness writing
- Acknowledgment of gaps and inconsistencies

### Non-Linear Timeline
- Gaps in dates are normal and often acknowledged
- Not meant to be a daily practice, but rather "as needed"
- Self-aware comments about consistency: *"I need to make this into a habit."*

## Example Entry

```markdown
[myproject]: https://github.com/user/myproject
[sideproject]: https://github.com/user/sideproject

# 2024-08-12

Quick start on the refactor, nothing finished yet.

# 2024-08-15

Romans 5-8 Challenges and trials creates endurance and character

I am looking into wails3 and also Taskfile. Taskfile looks really nice as a task runner.

My wife ran a half marathon today, i am very proud. And i actually enjoyed jumping into the sea.
I am very grateful for our health and the relationships we have.

- [x] [myproject] Implemented new feature
- [x] [sideproject] Fixed critical bug

## Todo

- [ ] [myproject] Refactor authentication
- [ ] [sideproject] Add tests for API endpoints

## Backlog

- [myproject] Research future improvements
```

Note how the date entries run oldest-to-newest and the `## Todo`/`## Backlog` sections sit once at the bottom, after the last day.

## Philosophy

The mlog format embraces:
- **Authenticity**: Honest about both successes and struggles
- **Holistic tracking**: Work is part of life, not separate from it
- **Flexibility**: No strict rules, adapt to your needs
- **Self-awareness**: Regular check-ins with yourself about habits and patterns
- **Context**: Future you will appreciate understanding why decisions were made
