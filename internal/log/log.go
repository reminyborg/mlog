// Package log owns the on-disk mlog markdown file and exposes typed
// operations (list, create, complete, delete, search, ...) over it.
// Every mutating call reads the whole file, edits the slice of lines,
// then writes atomically via a tempfile + rename.
package log

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

type Store struct {
	Path string
	now  func() time.Time
}

func New(path string) *Store {
	return &Store{Path: path, now: time.Now}
}

// SetClock overrides the clock used to compute today's date. Intended for tests.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// TodayKey returns the YYYY-MM-DD key for today in the configured clock.
func (s *Store) TodayKey() string { return s.now().Format("2006-01-02") }

type Task struct {
	Line        string `json:"line"`
	LineIndex   int    `json:"lineIndex"`
	Section     string `json:"section"`
	Project     string `json:"project,omitempty"`
	Description string `json:"description"`
}

type SearchResult struct {
	Section   string `json:"section"`
	LineIndex int    `json:"lineIndex"`
	Line      string `json:"line"`
}

// AmbiguousMatchError is returned by Complete/Uncomplete/Delete when more
// than one task matches the substring. Callers can disambiguate with the
// ByLine variants using a Candidates' LineIndex.
type AmbiguousMatchError struct {
	Match      string
	Candidates []Task
}

func (e *AmbiguousMatchError) Error() string {
	return fmt.Sprintf("%d tasks match %q", len(e.Candidates), e.Match)
}

// ValidationIssue describes a single structural problem found in the log file.
type ValidationIssue struct {
	Line    int // 1-based line number
	Message string
}

// ValidationError is returned by Validate when one or more issues are detected.
type ValidationError struct {
	Issues []ValidationIssue
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d issue(s) in log file:", len(e.Issues))
	for _, iss := range e.Issues {
		fmt.Fprintf(&b, "\n  line %d: %s", iss.Line, iss.Message)
	}
	return b.String()
}

// ---- File I/O --------------------------------------------------------------

func (s *Store) read() ([]string, error) {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(b), "\n"), nil
}

func (s *Store) write(lines []string) error {
	dir := filepath.Dir(s.Path)
	tmp, err := os.CreateTemp(dir, ".mlog-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(strings.Join(lines, "\n")); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, s.Path)
}

// ---- Regexes ---------------------------------------------------------------

var (
	reHeading      = regexp.MustCompile(`^#{1,3} `)
	reH1           = regexp.MustCompile(`^# `)
	reH2           = regexp.MustCompile(`^## `)
	reH3           = regexp.MustCompile(`^### `)
	reDateH1       = regexp.MustCompile(`^# \d{4}-\d{2}-\d{2}\s*$`)
	reTodoBacklog  = regexp.MustCompile(`^# (Todo|Backlog)\s*$`)
	reOpenBox      = regexp.MustCompile(`^- \[ \]`)
	reCompletedBox = regexp.MustCompile(`^- \[[xX]\]`)
	reAnyTaskBox   = regexp.MustCompile(`^- \[[ xX]\]`)
	reTaskParts    = regexp.MustCompile(`^- \[[ xX]\] (?:\[([^\]]+)\])?\s*(.*)`)
	reProjectRef   = regexp.MustCompile(`^\[([^\]]+)\]:\s*(.*?)\s*$`)
)

// sectionName extracts "Todo" from "# Todo", "2026-04-15" from "# 2026-04-15", etc.
func sectionName(line string) string {
	return strings.TrimSpace(reHeading.ReplaceAllString(line, ""))
}

// ---- Slice / line helpers --------------------------------------------------

// splice removes deleteCount items starting at start and inserts items in
// their place, returning the new slice. Behavior matches JS Array.splice.
func splice[T any](s []T, start, deleteCount int, items ...T) []T {
	out := make([]T, 0, len(s)-deleteCount+len(items))
	out = append(out, s[:start]...)
	out = append(out, items...)
	out = append(out, s[start+deleteCount:]...)
	return out
}

// findHeader returns the line index of an exact header (e.g. "# 2026-04-15"),
// matching after trimming whitespace. -1 if absent.
func findHeader(lines []string, header string) int {
	return slices.IndexFunc(lines, func(l string) bool { return strings.TrimSpace(l) == header })
}

// nextHeading returns the first index strictly after start matching pred,
// or len(lines) when none. Used to find the boundary of a section.
func nextHeading(lines []string, start int, pred func(string) bool) int {
	for i := start + 1; i < len(lines); i++ {
		if pred(lines[i]) {
			return i
		}
	}
	return len(lines)
}

// insertLineWithSpacing inserts item at idx, keeping a trailing blank when
// the next line is a heading so we don't glue content to the next section.
func insertLineWithSpacing(lines []string, idx int, item string) []string {
	if idx < len(lines) && reHeading.MatchString(lines[idx]) {
		return splice(lines, idx, 0, item, "")
	}
	return splice(lines, idx, 0, item)
}

// newDateHeaderInsertPos returns the line index where a brand-new date H1
// should be inserted. Anchors to the section after the last date-based H1
// so the new entry lands chronologically with its siblings, regardless of
// where # Todo / # Backlog live in the file. Falls back to the first
// # Todo / # Backlog (then end-of-file) when no date H1 exists yet.
func newDateHeaderInsertPos(lines []string) int {
	last := -1
	for i, l := range lines {
		if reDateH1.MatchString(l) {
			last = i
		}
	}
	if last != -1 {
		// Only an H1 ends the last day entry; ## / ### inside it are note prose,
		// so a new date header goes after them, not into the middle of them.
		return nextHeading(lines, last, reH1.MatchString)
	}
	if i := slices.IndexFunc(lines, reTodoBacklog.MatchString); i != -1 {
		return i
	}
	return len(lines)
}

// ensureTodayHeader returns lines (possibly with today's H1 inserted) and
// the index of the today header.
func (s *Store) ensureTodayHeader(lines []string) ([]string, int) {
	return s.ensureDateHeader(lines, s.TodayKey())
}

// ensureDateHeader returns lines (possibly with the given date's H1 inserted)
// and the index of the date header. date must be in YYYY-MM-DD format.
func (s *Store) ensureDateHeader(lines []string, date string) ([]string, int) {
	header := "# " + date
	if idx := findHeader(lines, header); idx != -1 {
		return lines, idx
	}
	at := newDateHeaderInsertPos(lines)
	return splice(lines, at, 0, "", header, ""), at + 1
}

// ---- Validation ------------------------------------------------------------

// Validate reads the log file and checks structural invariants:
//   - at most one # Todo header
//   - at most one # Backlog header
//   - no duplicate # YYYY-MM-DD headers
//   - every heading has a blank line before it (except the very first line)
//
// Returns *ValidationError listing all problems, or nil when the file is valid.
func (s *Store) Validate() error {
	lines, err := s.read()
	if err != nil {
		return err
	}
	return validateLines(lines)
}

func validateLines(lines []string) error {
	var issues []ValidationIssue
	todoCount := 0
	backlogCount := 0
	datesSeen := map[string]int{} // date → 1-based line number of first occurrence

	for i, line := range lines {
		lineNum := i + 1

		switch {
		case reTodoBacklog.MatchString(line):
			switch sectionName(line) {
			case "Todo":
				todoCount++
				if todoCount > 1 {
					issues = append(issues, ValidationIssue{Line: lineNum, Message: "duplicate # Todo header"})
				}
			case "Backlog":
				backlogCount++
				if backlogCount > 1 {
					issues = append(issues, ValidationIssue{Line: lineNum, Message: "duplicate # Backlog header"})
				}
			}
		case reDateH1.MatchString(line):
			date := sectionName(line)
			if prev, ok := datesSeen[date]; ok {
				issues = append(issues, ValidationIssue{
					Line:    lineNum,
					Message: fmt.Sprintf("duplicate date header %q (first seen at line %d)", date, prev),
				})
			} else {
				datesSeen[date] = lineNum
			}
		}

		// Every heading must have a blank line before it, except the very first line.
		if i > 0 && reHeading.MatchString(line) && strings.TrimSpace(lines[i-1]) != "" {
			issues = append(issues, ValidationIssue{
				Line:    lineNum,
				Message: fmt.Sprintf("heading %q has no blank line before it", strings.TrimSpace(line)),
			})
		}
	}

	if len(issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: issues}
}

// ---- Listing / search ------------------------------------------------------

func (s *Store) ListIncomplete() ([]Task, error) {
	lines, err := s.read()
	if err != nil {
		return nil, err
	}
	var tasks []Task
	section := "top"
	for i, line := range lines {
		if reHeading.MatchString(line) {
			section = sectionName(line)
			continue
		}
		if !reOpenBox.MatchString(line) {
			continue
		}
		m := reTaskParts.FindStringSubmatch(line)
		tasks = append(tasks, Task{
			Line:        strings.TrimSpace(line),
			LineIndex:   i,
			Section:     section,
			Project:     m[1],
			Description: m[2],
		})
	}
	return tasks, nil
}

func (s *Store) Search(query string) ([]SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	lines, err := s.read()
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(query)
	var results []SearchResult
	section := "top"
	for i, line := range lines {
		if reHeading.MatchString(line) {
			section = sectionName(line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), lower) {
			results = append(results, SearchResult{
				Section:   section,
				LineIndex: i,
				Line:      strings.TrimSpace(line),
			})
		}
	}
	return results, nil
}

// findTaskMatches returns task lines that match matchText (case-insensitive
// substring) and have a checkbox of the requested kind.
func findTaskMatches(lines []string, matchText string, box *regexp.Regexp) []Task {
	lower := strings.ToLower(matchText)
	var matches []Task
	section := "top"
	for i, line := range lines {
		if reHeading.MatchString(line) {
			section = sectionName(line)
			continue
		}
		if !box.MatchString(line) || !strings.Contains(strings.ToLower(line), lower) {
			continue
		}
		m := reTaskParts.FindStringSubmatch(line)
		matches = append(matches, Task{
			Line:        strings.TrimSpace(line),
			LineIndex:   i,
			Section:     section,
			Project:     m[1],
			Description: m[2],
		})
	}
	return matches
}

// ---- Mutation dispatch -----------------------------------------------------

// mutateFn applies an in-memory edit at idx and writes the file. The
// returned string is what gets reported back to the user.
type mutateFn func(s *Store, lines []string, idx int) (string, error)

// mutateByMatch reads the file, finds tasks of `kind` matching matchText,
// and dispatches to fn for the unique match. Returns AmbiguousMatchError
// when more than one task matches.
func (s *Store) mutateByMatch(matchText, kind string, box *regexp.Regexp, fn mutateFn) (string, error) {
	lines, err := s.read()
	if err != nil {
		return "", err
	}
	matches := findTaskMatches(lines, matchText, box)
	switch {
	case len(matches) == 0:
		return "", fmt.Errorf("no %s task matching %q found", kind, matchText)
	case len(matches) > 1:
		return "", &AmbiguousMatchError{Match: matchText, Candidates: matches}
	}
	return fn(s, lines, matches[0].LineIndex)
}

// mutateByLine reads the file, validates that lineIndex is in range and
// the line matches `want`, then dispatches to fn.
func (s *Store) mutateByLine(lineIndex int, kind string, want *regexp.Regexp, fn mutateFn) (string, error) {
	lines, err := s.read()
	if err != nil {
		return "", err
	}
	if lineIndex < 0 || lineIndex >= len(lines) {
		return "", fmt.Errorf("line %d is out of range (file has %d lines)", lineIndex, len(lines))
	}
	if !want.MatchString(lines[lineIndex]) {
		return "", fmt.Errorf("line %d is not %s: %q", lineIndex, kind, strings.TrimSpace(lines[lineIndex]))
	}
	return fn(s, lines, lineIndex)
}

// ---- Create ----------------------------------------------------------------

// ParseDate resolves a human-readable date string to a YYYY-MM-DD key using
// the store's clock. Accepts "today", "tomorrow", "yesterday", or YYYY-MM-DD.
func (s *Store) ParseDate(input string) (string, error) {
	now := s.now()
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "today":
		return now.Format("2006-01-02"), nil
	case "tomorrow":
		return now.AddDate(0, 0, 1).Format("2006-01-02"), nil
	case "yesterday":
		return now.AddDate(0, 0, -1).Format("2006-01-02"), nil
	default:
		if _, err := time.Parse("2006-01-02", input); err != nil {
			return "", fmt.Errorf("invalid date %q: expected YYYY-MM-DD, 'today', 'tomorrow', or 'yesterday'", input)
		}
		return input, nil
	}
}

// CreateTask adds a new task. date is a YYYY-MM-DD string selecting which
// dated section to use; an empty date adds to # Todo instead.
func (s *Store) CreateTask(project, description, date string) error {
	lines, err := s.read()
	if err != nil {
		return err
	}
	taskLine := formatTask(project, description)

	if date != "" {
		var headerIdx int
		lines, headerIdx = s.ensureDateHeader(lines, date)
		end := nextHeading(lines, headerIdx, func(l string) bool {
			return reH1.MatchString(l) || reH2.MatchString(l)
		})
		lines = insertLineWithSpacing(lines, end, taskLine)
		return s.write(lines)
	}

	todoIdx := slices.IndexFunc(lines, func(l string) bool {
		return reTodoBacklog.MatchString(l) && sectionName(l) == "Todo"
	})
	if todoIdx == -1 {
		lines = append(lines, "", "# Todo", "", taskLine, "")
		return s.write(lines)
	}

	insertAt := insertPosInTodo(lines, todoIdx, project)
	lines = insertLineWithSpacing(lines, insertAt, taskLine)
	return s.write(lines)
}

func formatTask(project, description string) string {
	if project == "" {
		return "- [ ] " + description
	}
	return "- [ ] [" + project + "] " + description
}

// insertPosInTodo finds where to put a new task inside the # Todo section.
// If `project` matches an `### <project>` H3 within Todo, append to that H3;
// otherwise, append above the first H3 (or at section end if there are none),
// trimming trailing blank lines so the new task sits flush.
func insertPosInTodo(lines []string, todoIdx int, project string) int {
	todoEnd := nextHeading(lines, todoIdx, reH1.MatchString)

	if project != "" {
		needle := strings.ToLower(project)
		for i := todoIdx + 1; i < todoEnd; i++ {
			if !reH3.MatchString(lines[i]) || !strings.Contains(strings.ToLower(lines[i]), needle) {
				continue
			}
			subEnd := nextHeading(lines, i, reHeading.MatchString)
			if subEnd > todoEnd {
				subEnd = todoEnd
			}
			return trimTrailingBlanks(lines, i+1, subEnd)
		}
	}

	directEnd := nextHeading(lines, todoIdx, reH3.MatchString)
	if directEnd > todoEnd {
		directEnd = todoEnd
	}
	return trimTrailingBlanks(lines, todoIdx+1, directEnd)
}

// trimTrailingBlanks returns the largest j ≤ end with j > minStart and
// lines[j-1] not blank — i.e., the index where appended content should land
// to sit flush against the last non-blank line in [minStart, end).
func trimTrailingBlanks(lines []string, minStart, end int) int {
	for end > minStart && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return end
}

// ---- Complete --------------------------------------------------------------

func (s *Store) CompleteTask(matchText string) (string, error) {
	return s.mutateByMatch(matchText, "incomplete", reOpenBox, (*Store).completeAtLine)
}

func (s *Store) CompleteTaskByLine(lineIndex int) (string, error) {
	return s.mutateByLine(lineIndex, "an incomplete task", reOpenBox, (*Store).completeAtLine)
}

func (s *Store) completeAtLine(lines []string, idx int) (string, error) {
	completed := strings.Replace(lines[idx], "- [ ]", "- [x]", 1)

	lines = splice(lines, idx, 1)
	lines = collapseDoubleBlank(lines, idx)

	var headerIdx int
	lines, headerIdx = s.ensureTodayHeader(lines)
	endIdx := nextHeading(lines, headerIdx, reHeading.MatchString)

	insertIdx := lastProjectLineIn(lines, headerIdx, endIdx, projectOf(completed))
	if insertIdx == -1 {
		insertIdx = endIdx
	}

	lines = insertLineWithSpacing(lines, insertIdx, completed)
	if err := s.write(lines); err != nil {
		return "", err
	}
	return completed, nil
}

func projectOf(line string) string {
	if m := reTaskParts.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// lastProjectLineIn finds the last `- [.] [project] ...` line in (headerIdx, end)
// and returns the index immediately after it, so a new task for the same
// project lands grouped. -1 if no project given or no match.
func lastProjectLineIn(lines []string, headerIdx, end int, project string) int {
	if project == "" {
		return -1
	}
	pat := regexp.MustCompile(`(?i)^- \[.\] \[` + regexp.QuoteMeta(project) + `\]`)
	for i := end - 1; i > headerIdx; i-- {
		if pat.MatchString(lines[i]) {
			return i + 1
		}
	}
	return -1
}

// collapseDoubleBlank removes a redundant blank line at idx if both idx-1
// and idx are blank. Mirrors completeAtLine/deleteAtLine spacing cleanup.
func collapseDoubleBlank(lines []string, idx int) []string {
	if idx > 0 && idx < len(lines) && strings.TrimSpace(lines[idx-1]) == "" && strings.TrimSpace(lines[idx]) == "" {
		return splice(lines, idx, 1)
	}
	return lines
}

// ---- Schedule -------------------------------------------------------------

// ScheduleTask moves a unique incomplete task to today's entry (keeping it open).
func (s *Store) ScheduleTask(matchText string) (string, error) {
	return s.mutateByMatch(matchText, "incomplete", reOpenBox, (*Store).scheduleAtLine)
}

func (s *Store) ScheduleTaskByLine(lineIndex int) (string, error) {
	return s.mutateByLine(lineIndex, "an incomplete task", reOpenBox, (*Store).scheduleAtLine)
}

func (s *Store) scheduleAtLine(lines []string, idx int) (string, error) {
	task := strings.TrimSpace(lines[idx])

	lines = splice(lines, idx, 1)
	lines = collapseDoubleBlank(lines, idx)

	var headerIdx int
	lines, headerIdx = s.ensureTodayHeader(lines)
	endIdx := nextHeading(lines, headerIdx, reHeading.MatchString)

	insertIdx := lastProjectLineIn(lines, headerIdx, endIdx, projectOf(task))
	if insertIdx == -1 {
		insertIdx = endIdx
	}

	lines = insertLineWithSpacing(lines, insertIdx, task)
	if err := s.write(lines); err != nil {
		return "", err
	}
	return task, nil
}

// ---- Uncomplete ------------------------------------------------------------

// Uncomplete flips a unique completed task (`- [x]`) back to `- [ ]` in place.
// The line is not moved — it stays in the section it was completed under.
func (s *Store) Uncomplete(matchText string) (string, error) {
	return s.mutateByMatch(matchText, "completed", reCompletedBox, (*Store).uncompleteAtLine)
}

func (s *Store) UncompleteByLine(lineIndex int) (string, error) {
	return s.mutateByLine(lineIndex, "a completed task", reCompletedBox, (*Store).uncompleteAtLine)
}

func (s *Store) uncompleteAtLine(lines []string, idx int) (string, error) {
	lines[idx] = reCompletedBox.ReplaceAllString(lines[idx], "- [ ]")
	if err := s.write(lines); err != nil {
		return "", err
	}
	return strings.TrimSpace(lines[idx]), nil
}

// ---- Delete ----------------------------------------------------------------

// Delete removes a unique task line (open or completed) from the file.
func (s *Store) Delete(matchText string) (string, error) {
	return s.mutateByMatch(matchText, "matching", reAnyTaskBox, (*Store).deleteAtLine)
}

func (s *Store) DeleteByLine(lineIndex int) (string, error) {
	return s.mutateByLine(lineIndex, "a task", reAnyTaskBox, (*Store).deleteAtLine)
}

func (s *Store) deleteAtLine(lines []string, idx int) (string, error) {
	deleted := strings.TrimSpace(lines[idx])
	lines = splice(lines, idx, 1)
	lines = collapseDoubleBlank(lines, idx)
	if err := s.write(lines); err != nil {
		return "", err
	}
	return deleted, nil
}

// ---- Projects -------------------------------------------------------------

// Project represents a project reference link definition at the top of the file.
type Project struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	LineIndex int    `json:"lineIndex"`
}

// findProjectLine returns the line index of the [name]: ... reference, or -1.
func findProjectLine(lines []string, name string) int {
	for i, line := range lines {
		if m := reProjectRef.FindStringSubmatch(line); m != nil {
			if m[1] == name {
				return i
			}
		}
	}
	return -1
}

// projectRefBlockEnd returns the index just after the last project-reference
// line at the top of the file (skipping blank lines between refs). If there
// are no refs yet, it returns 0.
func projectRefBlockEnd(lines []string) int {
	last := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if reProjectRef.MatchString(line) {
			last = i
		} else {
			break
		}
	}
	if last == -1 {
		return 0
	}
	return last + 1
}

func (s *Store) ListProjects() ([]Project, error) {
	lines, err := s.read()
	if err != nil {
		return nil, err
	}
	var projects []Project
	for i, line := range lines {
		if m := reProjectRef.FindStringSubmatch(line); m != nil {
			projects = append(projects, Project{Name: m[1], URL: m[2], LineIndex: i})
		}
	}
	return projects, nil
}

func (s *Store) AddProject(name, url string) error {
	lines, err := s.read()
	if err != nil {
		return err
	}
	if findProjectLine(lines, name) != -1 {
		return fmt.Errorf("project %q already exists", name)
	}
	newLine := "[" + name + "]: " + url
	if url == "" {
		newLine = "[" + name + "]:"
	}
	insertAt := projectRefBlockEnd(lines)
	lines = splice(lines, insertAt, 0, newLine)
	// Ensure a blank line separates the refs block from the rest of the file.
	afterInsert := insertAt + 1
	if afterInsert < len(lines) && strings.TrimSpace(lines[afterInsert]) != "" && !reProjectRef.MatchString(lines[afterInsert]) {
		lines = splice(lines, afterInsert, 0, "")
	}
	return s.write(lines)
}

func (s *Store) DeleteProject(name string) error {
	lines, err := s.read()
	if err != nil {
		return err
	}
	idx := findProjectLine(lines, name)
	if idx == -1 {
		return fmt.Errorf("project %q not found", name)
	}
	lines = splice(lines, idx, 1)
	lines = collapseDoubleBlank(lines, idx)
	return s.write(lines)
}

// EditProject updates a project's name and/or URL. If newName is non-empty and
// differs from name, all task references ([name]) and ### name H3 headings are
// also rewritten. Pass empty string to leave name or URL unchanged.
func (s *Store) EditProject(name, newName, newURL string) error {
	lines, err := s.read()
	if err != nil {
		return err
	}
	idx := findProjectLine(lines, name)
	if idx == -1 {
		return fmt.Errorf("project %q not found", name)
	}
	m := reProjectRef.FindStringSubmatch(lines[idx])
	if m == nil {
		return fmt.Errorf("unexpected: line %d is not a project ref", idx)
	}
	if newName == "" {
		newName = m[1]
	}
	if newURL == "" {
		newURL = m[2]
	}
	if newName != name {
		if findProjectLine(lines, newName) != -1 {
			return fmt.Errorf("project %q already exists", newName)
		}
		// Rewrite all task references and H3 headings.
		oldTag := "[" + name + "]"
		newTag := "[" + newName + "]"
		for i, line := range lines {
			if i == idx {
				continue
			}
			if strings.Contains(line, oldTag) {
				lines[i] = strings.ReplaceAll(line, oldTag, newTag)
			}
			if reH3.MatchString(line) && strings.TrimSpace(line) == "### "+name {
				lines[i] = "### " + newName
			}
		}
	}
	if newURL != "" {
		lines[idx] = "[" + newName + "]: " + newURL
	} else {
		lines[idx] = "[" + newName + "]:"
	}
	return s.write(lines)
}

// ---- Notes / entries -------------------------------------------------------

func (s *Store) AppendToToday(text string) error {
	lines, err := s.read()
	if err != nil {
		return err
	}
	today := "# " + s.TodayKey()
	headerIdx := findHeader(lines, today)
	if headerIdx == -1 {
		at := newDateHeaderInsertPos(lines)
		lines = splice(lines, at, 0, "", today, "", text, "")
		return s.write(lines)
	}

	// A day entry runs to the next H1; ## / ### subheadings inside it are part
	// of the note prose, so a new note is appended after them, not before.
	endIdx := nextHeading(lines, headerIdx, reH1.MatchString)
	endIdx = trimTrailingBlanks(lines, headerIdx+1, endIdx)
	var toInsert []string
	if endIdx > 0 && strings.TrimSpace(lines[endIdx-1]) != "" {
		toInsert = append(toInsert, "")
	}
	toInsert = append(toInsert, text)
	if endIdx < len(lines) && reHeading.MatchString(lines[endIdx]) {
		toInsert = append(toInsert, "")
	}
	lines = splice(lines, endIdx, 0, toInsert...)
	return s.write(lines)
}

// Entry returns the H1 section for the given date (heading included), a found
// flag, and any read error. The section runs to the next H1, so ## / ###
// subheadings written inside a day's notes are part of the entry.
func (s *Store) Entry(date string) (string, bool, error) {
	lines, err := s.read()
	if err != nil {
		return "", false, err
	}
	start, end, ok := s.entryBounds(lines, date)
	if !ok {
		return "", false, nil
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n")), true, nil
}

// entryBounds locates the [start, end) line range of a date's H1 section.
func (s *Store) entryBounds(lines []string, date string) (int, int, bool) {
	start := findHeader(lines, "# "+date)
	if start == -1 {
		return 0, 0, false
	}
	end := nextHeading(lines, start, reH1.MatchString)
	return start, trimTrailingBlanks(lines, start+1, end), true
}

// ReplaceEntry overwrites a date's whole H1 section with body (which must
// still start with that date's heading), creating the entry if it is absent.
// The result is validated before it is written, so a mangled edit is rejected
// rather than persisted.
func (s *Store) ReplaceEntry(date, body string) error {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return fmt.Errorf("invalid date %q: expected YYYY-MM-DD", date)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("entry is empty")
	}
	newLines := strings.Split(body, "\n")
	if strings.TrimSpace(newLines[0]) != "# "+date {
		return fmt.Errorf("the entry must still start with its %q heading", "# "+date)
	}
	if i := slices.IndexFunc(newLines[1:], reH1.MatchString); i != -1 {
		return fmt.Errorf("the entry cannot contain another H1 heading (%q); H1 starts a new section", strings.TrimSpace(newLines[1+i]))
	}

	lines, err := s.read()
	if err != nil {
		return err
	}
	start, end, ok := s.entryBounds(lines, date)
	if !ok {
		at := newDateHeaderInsertPos(lines)
		lines = splice(lines, at, 0, append(newLines, "")...)
	} else {
		lines = splice(lines, start, end-start, newLines...)
	}
	if err := validateLines(lines); err != nil {
		return err
	}
	return s.write(lines)
}

func (s *Store) GetToday() (string, error) {
	today := s.TodayKey()
	entry, ok, err := s.Entry(today)
	if err != nil {
		return "", err
	}
	if !ok {
		return "No entry for " + today + " yet.", nil
	}
	return entry, nil
}

func (s *Store) GetEntry(date string) (string, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", fmt.Errorf("invalid date %q: expected YYYY-MM-DD", date)
	}
	entry, ok, err := s.Entry(date)
	if err != nil {
		return "", err
	}
	if !ok {
		return "No entry for " + date + ".", nil
	}
	return entry, nil
}
