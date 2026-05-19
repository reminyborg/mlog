package main

import (
	_ "embed"
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
)

//go:embed .agents/skills/mlog/SKILL.md
var skillMarkdown string

type Context struct {
	Store *mlog.Store
	JSON  bool
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type ListCmd struct {
	Project string `help:"Filter by project tag" short:"p"`
}

func (c *ListCmd) Run(ctx *Context) error {
	tasks, err := ctx.Store.ListIncomplete()
	if err != nil {
		return err
	}
	if c.Project != "" {
		tasks = filterByProject(tasks, c.Project)
	}
	if ctx.JSON {
		if tasks == nil {
			tasks = []mlog.Task{}
		}
		return emitJSON(tasks)
	}
	if len(tasks) == 0 {
		if c.Project != "" {
			fmt.Printf("No incomplete tasks for project [%s].\n", c.Project)
		} else {
			fmt.Println("No incomplete tasks.")
		}
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

func filterByProject(tasks []mlog.Task, project string) []mlog.Task {
	var out []mlog.Task
	for _, t := range tasks {
		if strings.EqualFold(t.Project, project) {
			out = append(out, t)
		}
	}
	return out
}

type CreateCmd struct {
	Project     string   `help:"Project tag" short:"p"`
	Today       bool     `help:"Add to today's entry instead of # Todo" short:"t"`
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
	return runTaskAction("Completed", c.Line, c.Match, ctx.Store.CompleteTaskByLine, ctx.Store.CompleteTask)
}

type ScheduleCmd struct {
	Line  int      `help:"Move the task at exact lineIndex (from 'list --json')." default:"-1"`
	Match []string `arg:"" help:"Substring matching the task to move to today" optional:""`
}

func (c *ScheduleCmd) Run(ctx *Context) error {
	return runTaskAction("Scheduled", c.Line, c.Match, ctx.Store.ScheduleTaskByLine, ctx.Store.ScheduleTask)
}

type UncompleteCmd struct {
	Line  int      `help:"Uncomplete the task at exact lineIndex (from 'search --json')." default:"-1"`
	Match []string `arg:"" help:"Substring matching the completed task" optional:""`
}

func (c *UncompleteCmd) Run(ctx *Context) error {
	return runTaskAction("Uncompleted", c.Line, c.Match, ctx.Store.UncompleteByLine, ctx.Store.Uncomplete)
}

type DeleteCmd struct {
	Line  int      `help:"Delete the task at exact lineIndex (from 'list --json' or 'search --json')." default:"-1"`
	Match []string `arg:"" help:"Substring matching the task (open or completed)" optional:""`
}

func (c *DeleteCmd) Run(ctx *Context) error {
	return runTaskAction("Deleted", c.Line, c.Match, ctx.Store.DeleteByLine, ctx.Store.Delete)
}

func runTaskAction(verb string, line int, match []string, byLine func(int) (string, error), bySubstring func(string) (string, error)) error {
	if line >= 0 {
		got, err := byLine(line)
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", verb, got)
		return nil
	}
	text := strings.Join(match, " ")
	if text == "" {
		return fmt.Errorf("provide a match substring or --line N")
	}
	got, err := bySubstring(text)
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
	fmt.Printf("%s: %s\n", verb, got)
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
	Query   []string `arg:"" help:"Search query (case-insensitive substring)" optional:""`
	Project string   `help:"Restrict results to a project tag" short:"p"`
}

func (c *SearchCmd) Run(ctx *Context) error {
	query := strings.Join(c.Query, " ")
	if query == "" && c.Project == "" {
		return fmt.Errorf("provide a query and/or --project")
	}
	// When filtering by project with no query, search for the project tag itself.
	effectiveQuery := query
	if effectiveQuery == "" {
		effectiveQuery = "[" + c.Project + "]"
	}
	results, err := ctx.Store.Search(effectiveQuery)
	if err != nil {
		return err
	}
	if c.Project != "" && query != "" {
		// Further narrow: only lines that contain the project tag.
		tag := strings.ToLower("[" + c.Project + "]")
		var filtered []mlog.SearchResult
		for _, r := range results {
			if strings.Contains(strings.ToLower(r.Line), tag) {
				filtered = append(filtered, r)
			}
		}
		results = filtered
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

type SyncCmd struct {
	PullOnly bool `help:"Only pull, do not commit or push" short:"p"`
	PushOnly bool `help:"Only commit and push, do not pull" short:"P"`
	Message  string `help:"Override the commit message" short:"m"`
}

func (c *SyncCmd) Run(ctx *Context) error {
	dir := filepath.Dir(ctx.Store.Path)
	file := filepath.Base(ctx.Store.Path)

	if !c.PushOnly {
		if out, err := gitCmd(dir, "pull", "--rebase", "--autostash"); err != nil {
			return fmt.Errorf("git pull failed: %w\n%s", err, out)
		} else if out != "" {
			fmt.Print(out)
		}
	}

	if !c.PullOnly {
		if err := ctx.Store.Validate(); err != nil {
			return fmt.Errorf("refusing to sync: %w", err)
		}

		if out, err := gitCmd(dir, "add", file); err != nil {
			return fmt.Errorf("git add failed: %w\n%s", err, out)
		}
		// Only commit if there is something staged.
		_, err := gitCmd(dir, "diff", "--cached", "--quiet")
		if err != nil {
			// exit 1 means staged changes exist.
			msg := c.Message
			if msg == "" {
				msg = "mlog: sync " + time.Now().Format("2006-01-02")
			}
			if out, err := gitCmd(dir, "commit", "-m", msg); err != nil {
				return fmt.Errorf("git commit failed: %w\n%s", err, out)
			} else if out != "" {
				fmt.Print(out)
			}
			if out, err := gitCmd(dir, "push"); err != nil {
				return fmt.Errorf("git push failed: %w\n%s", err, out)
			} else if out != "" {
				fmt.Print(out)
			}
		} else {
			fmt.Println("nothing to commit")
		}
	}
	return nil
}

func gitCmd(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}

type SkillCmd struct {
	Install SkillInstallCmd `cmd:"" help:"Install SKILL.md into detected agent skill directories"`
	Print   SkillPrintCmd   `cmd:"" help:"Print the embedded SKILL.md to stdout"`
}

type SkillInstallCmd struct {
	DryRun bool `help:"Print what would be written without touching the filesystem"`
}

func (c *SkillInstallCmd) Run(ctx *Context) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dirs := []string{
		filepath.Join(home, ".agents", "skills", "mlog"),
		filepath.Join(home, ".claude", "skills", "mlog"),
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, "SKILL.md")
		if c.DryRun {
			fmt.Printf("would write %s (%d bytes)\n", path, len(skillMarkdown))
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(skillMarkdown), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
	}
	return nil
}


type SkillPrintCmd struct{}

func (c *SkillPrintCmd) Run(ctx *Context) error {
	_, err := io.WriteString(os.Stdout, skillMarkdown)
	return err
}

// ---- Project subcommands --------------------------------------------------

type ProjectCmd struct {
	List   ProjectListCmd   `cmd:"" help:"List all project definitions"`
	Add    ProjectAddCmd    `cmd:"" help:"Add a new project definition"`
	Delete ProjectDeleteCmd `cmd:"" help:"Remove a project definition"`
	Edit   ProjectEditCmd   `cmd:"" help:"Rename a project or change its URL"`
}

type ProjectListCmd struct{}

func (c *ProjectListCmd) Run(ctx *Context) error {
	projects, err := ctx.Store.ListProjects()
	if err != nil {
		return err
	}
	if ctx.JSON {
		if projects == nil {
			projects = []mlog.Project{}
		}
		return emitJSON(projects)
	}
	if len(projects) == 0 {
		fmt.Println("No projects defined.")
		return nil
	}
	for _, p := range projects {
		if p.URL != "" {
			fmt.Printf("  %-24s %s\n", p.Name, p.URL)
		} else {
			fmt.Printf("  %s\n", p.Name)
		}
	}
	return nil
}

type ProjectAddCmd struct {
	Name string `arg:"" help:"Project name"`
	URL  string `arg:"" help:"Project URL or description" optional:""`
}

func (c *ProjectAddCmd) Run(ctx *Context) error {
	if err := ctx.Store.AddProject(c.Name, c.URL); err != nil {
		return err
	}
	if c.URL != "" {
		fmt.Printf("Added: [%s]: %s\n", c.Name, c.URL)
	} else {
		fmt.Printf("Added: [%s]\n", c.Name)
	}
	return nil
}

type ProjectDeleteCmd struct {
	Name string `arg:"" help:"Project name"`
}

func (c *ProjectDeleteCmd) Run(ctx *Context) error {
	if err := ctx.Store.DeleteProject(c.Name); err != nil {
		return err
	}
	fmt.Printf("Deleted: [%s]\n", c.Name)
	return nil
}

type ProjectEditCmd struct {
	Name   string `arg:"" help:"Current project name"`
	Rename string `help:"New project name" short:"r"`
	URL    string `help:"New URL or description" short:"u"`
}

func (c *ProjectEditCmd) Run(ctx *Context) error {
	if c.Rename == "" && c.URL == "" {
		return fmt.Errorf("provide --rename <new-name> and/or --url <new-url>")
	}
	if err := ctx.Store.EditProject(c.Name, c.Rename, c.URL); err != nil {
		return err
	}
	newName := c.Rename
	if newName == "" {
		newName = c.Name
	}
	fmt.Printf("Updated: [%s]\n", newName)
	return nil
}

type PathCmd struct{}

func (c *PathCmd) Run(ctx *Context) error {
	if ctx.JSON {
		return emitJSON(map[string]string{"path": ctx.Store.Path})
	}
	fmt.Println(ctx.Store.Path)
	return nil
}

type ValidateCmd struct{}

func (c *ValidateCmd) Run(ctx *Context) error {
	if err := ctx.Store.Validate(); err != nil {
		return err
	}
	fmt.Println("ok")
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

	List       ListCmd       `cmd:"" help:"List incomplete tasks" default:"withargs"`
	Create     CreateCmd     `cmd:"" help:"Create a new task"`
	Complete   CompleteCmd   `cmd:"" help:"Complete a task by matching substring"`
	Schedule   ScheduleCmd   `cmd:"" help:"Move an incomplete task to today's entry"`
	Uncomplete UncompleteCmd `cmd:"" help:"Flip a completed task back to incomplete (in place)"`
	Delete     DeleteCmd     `cmd:"" help:"Delete a task line (open or completed)"`
	Today      TodayCmd      `cmd:"" help:"Print today's entry"`
	Show       ShowCmd       `cmd:"" help:"Print a specific date's entry"`
	Search     SearchCmd     `cmd:"" help:"Search the log for matching lines"`
	Note       NoteCmd       `cmd:"" help:"Append a free-form note to today's entry"`
	Sync       SyncCmd       `cmd:"" help:"Pull, commit, and push the log file via git"`
	Path       PathCmd       `cmd:"" help:"Print the resolved path to the log file"`
	Validate   ValidateCmd   `cmd:"" help:"Check the log file for structural issues"`
	Edit       EditCmd       `cmd:"" help:"Open the log file in $EDITOR"`
	Project    ProjectCmd    `cmd:"" help:"Manage project definitions (list, add, delete, edit)"`
	Skill      SkillCmd      `cmd:"" help:"Manage the mlog agent skill (install or print SKILL.md)"`
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
