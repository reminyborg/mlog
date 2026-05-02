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

// SetNow is the package-level form of SetClock for callers that only have a *Store.
// Kept as a thin shim because external test packages (e.g. internal/tui) use it.
func SetNow(s *Store, now func() time.Time) { s.SetClock(now) }

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
	reTodoBacklog  = regexp.MustCompile(`^## (Todo|Backlog)`)
	reOpenBox      = regexp.MustCompile(`^- \[ \]`)
	reCompletedBox = regexp.MustCompile(`^- \[[xX]\]`)
	reAnyTaskBox   = regexp.MustCompile(`^- \[[ xX]\]`)
	reTaskParts    = regexp.MustCompile(`^- \[[ xX]\] (?:\[([^\]]+)\])?\s*(.*)`)
	reProjectTag   = regexp.MustCompile(`^- \[.\] \[([^\]]+)\]`)
)

// sectionName extracts "Todo" from "## Todo", "2026-04-15" from "# 2026-04-15", etc.
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
// where ## Todo / ## Backlog live in the file. Falls back to the first
// ## Todo / ## Backlog (then end-of-file) when no date H1 exists yet.
func newDateHeaderInsertPos(lines []string) int {
	last := -1
	for i, l := range lines {
		if reDateH1.MatchString(l) {
			last = i
		}
	}
	if last != -1 {
		return nextHeading(lines, last, func(l string) bool {
			return reH1.MatchString(l) || reH2.MatchString(l)
		})
	}
	if i := slices.IndexFunc(lines, reTodoBacklog.MatchString); i != -1 {
		return i
	}
	return len(lines)
}

// ensureTodayHeader returns lines (possibly with today's H1 inserted) and
// the index of the today header.
func (s *Store) ensureTodayHeader(lines []string) ([]string, int) {
	header := "# " + s.TodayKey()
	if idx := findHeader(lines, header); idx != -1 {
		return lines, idx
	}
	at := newDateHeaderInsertPos(lines)
	return splice(lines, at, 0, "", header, ""), at + 1
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

func (s *Store) CreateTask(project, description string, forToday bool) error {
	lines, err := s.read()
	if err != nil {
		return err
	}
	taskLine := formatTask(project, description)

	if forToday {
		var headerIdx int
		lines, headerIdx = s.ensureTodayHeader(lines)
		end := nextHeading(lines, headerIdx, func(l string) bool {
			return reH1.MatchString(l) || reH2.MatchString(l)
		})
		lines = insertLineWithSpacing(lines, end, taskLine)
		return s.write(lines)
	}

	todoIdx := slices.IndexFunc(lines, func(l string) bool { return reH2.MatchString(l) && strings.HasPrefix(l, "## Todo") })
	if todoIdx == -1 {
		lines = append(lines, "", "## Todo", "", taskLine, "")
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

// insertPosInTodo finds where to put a new task inside the ## Todo section.
// If `project` matches an `### <project>` H3 within Todo, append to that H3;
// otherwise, append above the first H3 (or at section end if there are none),
// trimming trailing blank lines so the new task sits flush.
func insertPosInTodo(lines []string, todoIdx int, project string) int {
	todoEnd := nextHeading(lines, todoIdx, reH2.MatchString)

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
	if m := reProjectTag.FindStringSubmatch(line); m != nil {
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

	endIdx := nextHeading(lines, headerIdx, reHeading.MatchString)
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

// Entry returns the body of the H1 section for the given date, a found flag,
// and any read error. The body excludes the date heading itself, with
// surrounding blank lines trimmed.
func (s *Store) Entry(date string) (string, bool, error) {
	lines, err := s.read()
	if err != nil {
		return "", false, err
	}
	start := findHeader(lines, "# "+date)
	if start == -1 {
		return "", false, nil
	}
	end := nextHeading(lines, start, reHeading.MatchString)
	return strings.TrimSpace(strings.Join(lines[start:end], "\n")), true, nil
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
