package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	mlog "github.com/reminyborg/mlog/internal/log"
	"github.com/reminyborg/mlog/internal/tui"
)

type Context struct {
	Store *mlog.Store
	JSON  bool
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type ListCmd struct{}

func (c *ListCmd) Run(ctx *Context) error {
	tasks, err := ctx.Store.ListIncomplete()
	if err != nil {
		return err
	}
	if ctx.JSON {
		if tasks == nil {
			tasks = []mlog.Task{}
		}
		return emitJSON(tasks)
	}
	if len(tasks) == 0 {
		fmt.Println("No incomplete tasks.")
		return nil
	}
	currentSection := ""
	for i, t := range tasks {
		if t.Section != currentSection {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("# %s\n", t.Section)
			currentSection = t.Section
		}
		fmt.Printf("  %2d. %s\n", i+1, t.Line)
	}
	return nil
}

type CreateCmd struct {
	Project     string   `help:"Project tag" short:"p"`
	Today       bool     `help:"Add to today's entry instead of ## Todo" short:"t"`
	Description []string `arg:"" help:"Task description" optional:""`
}

func (c *CreateCmd) Run(ctx *Context) error {
	desc, err := resolveBody(c.Description)
	if err != nil {
		return err
	}
	if desc == "" {
		return fmt.Errorf("description is required (pass as args, '-' to read stdin, or pipe stdin)")
	}
	if strings.Contains(desc, "\n") {
		return fmt.Errorf("task description cannot contain newlines; use 'note' for multi-line entries")
	}
	if err := ctx.Store.CreateTask(c.Project, desc, c.Today); err != nil {
		return err
	}
	prefix := ""
	if c.Project != "" {
		prefix = "[" + c.Project + "] "
	}
	fmt.Printf("Created: - [ ] %s%s\n", prefix, desc)
	return nil
}

type CompleteCmd struct {
	Line  int      `help:"Complete the task at exact lineIndex (from 'list --json')." default:"-1"`
	Match []string `arg:"" help:"Substring matching the task" optional:""`
}

func (c *CompleteCmd) Run(ctx *Context) error {
	return runTaskAction(taskAction{
		verb:        "Completed",
		line:        c.Line,
		match:       c.Match,
		byLine:      ctx.Store.CompleteTaskByLine,
		bySubstring: ctx.Store.CompleteTask,
	})
}

type UncompleteCmd struct {
	Line  int      `help:"Uncomplete the task at exact lineIndex (from 'search --json')." default:"-1"`
	Match []string `arg:"" help:"Substring matching the completed task" optional:""`
}

func (c *UncompleteCmd) Run(ctx *Context) error {
	return runTaskAction(taskAction{
		verb:        "Uncompleted",
		line:        c.Line,
		match:       c.Match,
		byLine:      ctx.Store.UncompleteByLine,
		bySubstring: ctx.Store.Uncomplete,
	})
}

type DeleteCmd struct {
	Line  int      `help:"Delete the task at exact lineIndex (from 'list --json' or 'search --json')." default:"-1"`
	Match []string `arg:"" help:"Substring matching the task (open or completed)" optional:""`
}

func (c *DeleteCmd) Run(ctx *Context) error {
	return runTaskAction(taskAction{
		verb:        "Deleted",
		line:        c.Line,
		match:       c.Match,
		byLine:      ctx.Store.DeleteByLine,
		bySubstring: ctx.Store.Delete,
	})
}

type taskAction struct {
	verb        string
	line        int
	match       []string
	byLine      func(int) (string, error)
	bySubstring func(string) (string, error)
}

func runTaskAction(a taskAction) error {
	if a.line >= 0 {
		line, err := a.byLine(a.line)
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", a.verb, line)
		return nil
	}
	match := strings.Join(a.match, " ")
	if match == "" {
		return fmt.Errorf("provide a match substring or --line N")
	}
	line, err := a.bySubstring(match)
	if err != nil {
		var amb *mlog.AmbiguousMatchError
		if errors.As(err, &amb) {
			fmt.Fprintf(os.Stderr, "%d tasks match %q. Re-run with --line N:\n", len(amb.Candidates), amb.Match)
			for _, t := range amb.Candidates {
				fmt.Fprintf(os.Stderr, "  --line %d  %s  (%s)\n", t.LineIndex, t.Line, t.Section)
			}
			os.Exit(1)
		}
		return err
	}
	fmt.Printf("%s: %s\n", a.verb, line)
	return nil
}

type entryJSON struct {
	Date    string `json:"date"`
	Found   bool   `json:"found"`
	Content string `json:"content"`
}

type TodayCmd struct{}

func (c *TodayCmd) Run(ctx *Context) error {
	if ctx.JSON {
		date := ctx.Store.TodayKey()
		content, found, err := ctx.Store.Entry(date)
		if err != nil {
			return err
		}
		return emitJSON(entryJSON{Date: date, Found: found, Content: content})
	}
	entry, err := ctx.Store.GetToday()
	if err != nil {
		return err
	}
	fmt.Println(entry)
	return nil
}

type NoteCmd struct {
	Text []string `arg:"" help:"Note text to append to today's entry. Pass '-' or pipe stdin for multi-line." optional:""`
}

func (c *NoteCmd) Run(ctx *Context) error {
	text, err := resolveBody(c.Text)
	if err != nil {
		return err
	}
	if text == "" {
		return fmt.Errorf("note text is required (pass as args, '-' to read stdin, or pipe stdin)")
	}
	return ctx.Store.AppendToToday(text)
}

type TuiCmd struct{}

func (c *TuiCmd) Run(ctx *Context) error {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return (&ListCmd{}).Run(ctx)
	}
	return tui.Run(ctx.Store)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// resolveBody returns the user-supplied body text. If args is empty and stdin
// is piped, or if args is exactly ["-"], it reads from stdin. Otherwise it
// joins the args with spaces. Trailing newlines from stdin are stripped.
func resolveBody(args []string) (string, error) {
	if len(args) == 1 && args[0] == "-" {
		return readStdin()
	}
	if len(args) == 0 {
		if !isTerminal(os.Stdin) {
			return readStdin()
		}
		return "", nil
	}
	return strings.Join(args, " "), nil
}

func readStdin() (string, error) {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\n"), nil
}

type SearchCmd struct {
	Query []string `arg:"" help:"Search query (case-insensitive substring)"`
}

func (c *SearchCmd) Run(ctx *Context) error {
	query := strings.Join(c.Query, " ")
	if query == "" {
		return fmt.Errorf("query is required")
	}
	results, err := ctx.Store.Search(query)
	if err != nil {
		return err
	}
	if ctx.JSON {
		if results == nil {
			results = []mlog.SearchResult{}
		}
		return emitJSON(results)
	}
	if len(results) == 0 {
		fmt.Println("No matches.")
		return nil
	}
	currentSection := ""
	for i, r := range results {
		if r.Section != currentSection {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("# %s\n", r.Section)
			currentSection = r.Section
		}
		fmt.Printf("  %s\n", r.Line)
	}
	return nil
}

type ShowCmd struct {
	Date string `arg:"" help:"Date in YYYY-MM-DD format"`
}

func (c *ShowCmd) Run(ctx *Context) error {
	if ctx.JSON {
		if _, err := time.Parse("2006-01-02", c.Date); err != nil {
			return fmt.Errorf("invalid date %q: expected YYYY-MM-DD", c.Date)
		}
		content, found, err := ctx.Store.Entry(c.Date)
		if err != nil {
			return err
		}
		return emitJSON(entryJSON{Date: c.Date, Found: found, Content: content})
	}
	entry, err := ctx.Store.GetEntry(c.Date)
	if err != nil {
		return err
	}
	fmt.Println(entry)
	return nil
}

type EditCmd struct{}

func (c *EditCmd) Run(ctx *Context) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command("sh", "-c", editor+` "$@"`, "sh", ctx.Store.Path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var CLI struct {
	Log     string           `help:"Path to log.md" env:"MLOG_FILE" default:"~/log/log.md"`
	JSON    bool             `help:"Emit machine-readable JSON for read commands (list, search, today, show)"`
	Version kong.VersionFlag `help:"Show version and exit"`

	List       ListCmd       `cmd:"" help:"List incomplete tasks"`
	Create     CreateCmd     `cmd:"" help:"Create a new task"`
	Complete   CompleteCmd   `cmd:"" help:"Complete a task by matching substring"`
	Uncomplete UncompleteCmd `cmd:"" help:"Flip a completed task back to incomplete (in place)"`
	Delete     DeleteCmd     `cmd:"" help:"Delete a task line (open or completed)"`
	Today      TodayCmd      `cmd:"" help:"Print today's entry"`
	Show       ShowCmd       `cmd:"" help:"Print a specific date's entry"`
	Search     SearchCmd     `cmd:"" help:"Search the log for matching lines"`
	Note       NoteCmd       `cmd:"" help:"Append a free-form note to today's entry"`
	Edit       EditCmd       `cmd:"" help:"Open the log file in $EDITOR"`
	Tui        TuiCmd        `cmd:"" help:"Launch interactive TUI" default:"withargs"`
}

func main() {
	ctx := kong.Parse(&CLI,
		kong.Name("mlog"),
		kong.Description("Edit your mlog markdown task file from the terminal."),
		kong.UsageOnError(),
		kong.Vars{"version": fmt.Sprintf("mlog %s (commit %s, built %s)", version, commit, date)},
	)
	if strings.HasPrefix(CLI.Log, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		CLI.Log = filepath.Join(home, CLI.Log[2:])
	}
	store := mlog.New(CLI.Log)
	if err := ctx.Run(&Context{Store: store, JSON: CLI.JSON}); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
