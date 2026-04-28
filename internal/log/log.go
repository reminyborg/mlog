package log

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// SetNow overrides the clock used to compute today's date. Intended for tests.
func SetNow(s *Store, now func() time.Time) {
	s.now = now
}

type Task struct {
	Line        string `json:"line"`
	LineIndex   int    `json:"lineIndex"`
	Section     string `json:"section"`
	Project     string `json:"project,omitempty"`
	Description string `json:"description"`
}

func (s *Store) todayKey() string {
	return s.now().Format("2006-01-02")
}

// TodayKey returns the YYYY-MM-DD key for today in the configured clock.
func (s *Store) TodayKey() string {
	return s.todayKey()
}

// Entry returns the body of the H1 section for the given date, a found flag,
// and any read error. The body excludes the date heading itself.
func (s *Store) Entry(date string) (string, bool, error) {
	return s.entryFor(date)
}

func (s *Store) read() ([]string, error) {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(b), "\n"), nil
}

func (s *Store) write(lines []string) error {
	content := strings.Join(lines, "\n")
	dir := filepath.Dir(s.Path)
	tmp, err := os.CreateTemp(dir, ".mlog-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
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

var (
	reHeading      = regexp.MustCompile(`^#+\s+`)
	reAnyHash      = regexp.MustCompile(`^#`)
	reH1           = regexp.MustCompile(`^# `)
	reH2           = regexp.MustCompile(`^## `)
	reH3           = regexp.MustCompile(`^### `)
	reTodoBacklg   = regexp.MustCompile(`^## (Todo|Backlog)`)
	reTodoHdr      = regexp.MustCompile(`^## Todo`)
	reIncomplete   = regexp.MustCompile(`^- \[ \] (?:\[([^\]]+)\])?\s*(.*)`)
	reOpenBox      = regexp.MustCompile(`^- \[ \]`)
	reCompletedBox = regexp.MustCompile(`^- \[[xX]\]`)
	reAnyTaskBox   = regexp.MustCompile(`^- \[[ xX]\]`)
	reAnyTaskParts = regexp.MustCompile(`^- \[[ xX]\] (?:\[([^\]]+)\])?\s*(.*)`)
	reProjectTag   = regexp.MustCompile(`^- \[.\] \[([^\]]+)\]`)
)

func (s *Store) ListIncomplete() ([]Task, error) {
	lines, err := s.read()
	if err != nil {
		return nil, err
	}
	var tasks []Task
	currentSection := "top"
	for i, line := range lines {
		if reH1.MatchString(line) || reH2.MatchString(line) || reH3.MatchString(line) {
			currentSection = strings.TrimSpace(reHeading.ReplaceAllString(line, ""))
		}
		m := reIncomplete.FindStringSubmatch(line)
		if m != nil {
			tasks = append(tasks, Task{
				Line:        strings.TrimSpace(line),
				LineIndex:   i,
				Section:     currentSection,
				Project:     m[1],
				Description: m[2],
			})
		}
	}
	return tasks, nil
}

// splice mimics JS Array.prototype.splice: removes deleteCount items at start,
// inserts items in their place, returns the new slice.
func splice[T any](s []T, start, deleteCount int, items ...T) []T {
	if start < 0 {
		start = 0
	}
	if start > len(s) {
		start = len(s)
	}
	end := start + deleteCount
	if end > len(s) {
		end = len(s)
	}
	out := make([]T, 0, len(s)-(end-start)+len(items))
	out = append(out, s[:start]...)
	out = append(out, items...)
	out = append(out, s[end:]...)
	return out
}

func findIndex[T any](s []T, pred func(T, int) bool) int {
	for i, v := range s {
		if pred(v, i) {
			return i
		}
	}
	return -1
}

func (s *Store) CreateTask(project, description string, forToday bool) error {
	lines, err := s.read()
	if err != nil {
		return err
	}

	var taskLine string
	if project != "" {
		taskLine = fmt.Sprintf("- [ ] [%s] %s", project, description)
	} else {
		taskLine = fmt.Sprintf("- [ ] %s", description)
	}

	if forToday {
		today := s.todayKey()
		header := "# " + today
		headerIdx := findIndex(lines, func(l string, _ int) bool { return strings.TrimSpace(l) == header })

		if headerIdx == -1 {
			todoIdx := findIndex(lines, func(l string, _ int) bool { return reTodoBacklg.MatchString(l) })
			insertAt := todoIdx
			if insertAt == -1 {
				insertAt = len(lines)
			}
			lines = splice(lines, insertAt, 0, "", header, "", taskLine, "")
		} else {
			insertAt := headerIdx + 1
			for insertAt < len(lines) && !reH1.MatchString(lines[insertAt]) && !reH2.MatchString(lines[insertAt]) {
				insertAt++
			}
			needsTrailing := insertAt < len(lines) && reAnyHash.MatchString(lines[insertAt])
			if needsTrailing {
				lines = splice(lines, insertAt, 0, taskLine, "")
			} else {
				lines = splice(lines, insertAt, 0, taskLine)
			}
		}
	} else {
		todoIdx := findIndex(lines, func(l string, _ int) bool { return reTodoHdr.MatchString(l) })

		if todoIdx == -1 {
			lines = append(lines, "", "## Todo", "", taskLine, "")
		} else {
			todoEndIdx := findIndex(lines, func(l string, i int) bool { return i > todoIdx && reH2.MatchString(l) })
			todoEnd := todoEndIdx
			if todoEnd == -1 {
				todoEnd = len(lines)
			}

			insertAt := -1

			if project != "" {
				lowerProject := strings.ToLower(project)
				for i := todoIdx + 1; i < todoEnd; i++ {
					if reH3.MatchString(lines[i]) && strings.Contains(strings.ToLower(lines[i]), lowerProject) {
						subEnd := i + 1
						for subEnd < todoEnd && !reAnyHash.MatchString(lines[subEnd]) {
							subEnd++
						}
						for subEnd > i+1 && strings.TrimSpace(lines[subEnd-1]) == "" {
							subEnd--
						}
						insertAt = subEnd
						break
					}
				}
			}

			if insertAt == -1 {
				directEnd := findIndex(lines, func(l string, i int) bool { return i > todoIdx && reH3.MatchString(l) })
				if directEnd == -1 || directEnd >= todoEnd {
					directEnd = todoEnd
				}
				for directEnd > todoIdx+1 && strings.TrimSpace(lines[directEnd-1]) == "" {
					directEnd--
				}
				insertAt = directEnd
			}

			needsTrailing := insertAt < len(lines) && reAnyHash.MatchString(lines[insertAt])
			if needsTrailing {
				lines = splice(lines, insertAt, 0, taskLine, "")
			} else {
				lines = splice(lines, insertAt, 0, taskLine)
			}
		}
	}

	return s.write(lines)
}

// AmbiguousMatchError is returned by CompleteTask when more than one incomplete
// task matches the substring. The caller can disambiguate using the Candidates'
// LineIndex with CompleteTaskByLine.
type AmbiguousMatchError struct {
	Match      string
	Candidates []Task
}

func (e *AmbiguousMatchError) Error() string {
	return fmt.Sprintf("%d incomplete tasks match %q", len(e.Candidates), e.Match)
}

func findTaskMatches(lines []string, matchText string, box *regexp.Regexp) []Task {
	lowerMatch := strings.ToLower(matchText)
	var matches []Task
	currentSection := "top"
	for i, line := range lines {
		if reH1.MatchString(line) || reH2.MatchString(line) || reH3.MatchString(line) {
			currentSection = strings.TrimSpace(reHeading.ReplaceAllString(line, ""))
			continue
		}
		if !box.MatchString(line) || !strings.Contains(strings.ToLower(line), lowerMatch) {
			continue
		}
		project, desc := "", strings.TrimSpace(line)
		if m := reAnyTaskParts.FindStringSubmatch(line); m != nil {
			project = m[1]
			desc = m[2]
		}
		matches = append(matches, Task{
			Line:        strings.TrimSpace(line),
			LineIndex:   i,
			Section:     currentSection,
			Project:     project,
			Description: desc,
		})
	}
	return matches
}

func (s *Store) CompleteTask(matchText string) (string, error) {
	lines, err := s.read()
	if err != nil {
		return "", err
	}
	matches := findTaskMatches(lines, matchText, reOpenBox)
	if len(matches) == 0 {
		return "", fmt.Errorf("no incomplete task matching %q found", matchText)
	}
	if len(matches) > 1 {
		return "", &AmbiguousMatchError{Match: matchText, Candidates: matches}
	}
	return s.completeAtLine(lines, matches[0].LineIndex)
}

// CompleteTaskByLine completes the incomplete task at the exact line index
// (matching the LineIndex returned by ListIncomplete). The line index can go
// stale across writes, so callers should fetch a fresh listing first.
func (s *Store) CompleteTaskByLine(lineIndex int) (string, error) {
	lines, err := s.read()
	if err != nil {
		return "", err
	}
	if lineIndex < 0 || lineIndex >= len(lines) {
		return "", fmt.Errorf("line %d is out of range (file has %d lines)", lineIndex, len(lines))
	}
	if !reOpenBox.MatchString(lines[lineIndex]) {
		return "", fmt.Errorf("line %d is not an incomplete task: %q", lineIndex, strings.TrimSpace(lines[lineIndex]))
	}
	return s.completeAtLine(lines, lineIndex)
}

func (s *Store) completeAtLine(lines []string, idx int) (string, error) {
	completedLine := strings.Replace(lines[idx], "- [ ]", "- [x]", 1)

	lines = splice(lines, idx, 1)
	if idx > 0 && idx < len(lines) && strings.TrimSpace(lines[idx-1]) == "" && strings.TrimSpace(lines[idx]) == "" {
		lines = splice(lines, idx, 1)
	}

	today := s.todayKey()
	header := "# " + today
	headerIdx := findIndex(lines, func(l string, _ int) bool { return strings.TrimSpace(l) == header })
	if headerIdx == -1 {
		todoIdx := findIndex(lines, func(l string, _ int) bool { return reTodoBacklg.MatchString(l) })
		insertAt := todoIdx
		if insertAt == -1 {
			insertAt = len(lines)
		}
		lines = splice(lines, insertAt, 0, "", header, "")
		headerIdx = insertAt + 1
	}

	endIdx := findIndex(lines, func(l string, i int) bool { return i > headerIdx && reAnyHash.MatchString(l) })
	if endIdx == -1 {
		endIdx = len(lines)
	}

	var project string
	if m := reProjectTag.FindStringSubmatch(completedLine); m != nil {
		project = m[1]
	}

	insertIdx := -1
	if project != "" {
		projectPattern := regexp.MustCompile(`(?i)^- \[.\] \[` + regexp.QuoteMeta(project) + `\]`)
		for i := endIdx - 1; i > headerIdx; i-- {
			if projectPattern.MatchString(lines[i]) {
				insertIdx = i + 1
				break
			}
		}
	}
	if insertIdx == -1 {
		insertIdx = endIdx
	}

	needsTrailing := insertIdx < len(lines) && reAnyHash.MatchString(lines[insertIdx])
	if needsTrailing {
		lines = splice(lines, insertIdx, 0, completedLine, "")
	} else {
		lines = splice(lines, insertIdx, 0, completedLine)
	}

	if err := s.write(lines); err != nil {
		return "", err
	}
	return completedLine, nil
}

func (s *Store) entryFor(date string) (string, bool, error) {
	lines, err := s.read()
	if err != nil {
		return "", false, err
	}
	header := "# " + date
	start := findIndex(lines, func(l string, _ int) bool { return strings.TrimSpace(l) == header })
	if start == -1 {
		return "", false, nil
	}
	end := findIndex(lines, func(l string, i int) bool { return i > start && reAnyHash.MatchString(l) })
	var slice []string
	if end == -1 {
		slice = lines[start:]
	} else {
		slice = lines[start:end]
	}
	return strings.TrimSpace(strings.Join(slice, "\n")), true, nil
}

func (s *Store) GetToday() (string, error) {
	today := s.todayKey()
	entry, ok, err := s.entryFor(today)
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
	entry, ok, err := s.entryFor(date)
	if err != nil {
		return "", err
	}
	if !ok {
		return "No entry for " + date + ".", nil
	}
	return entry, nil
}

type SearchResult struct {
	Section   string `json:"section"`
	LineIndex int    `json:"lineIndex"`
	Line      string `json:"line"`
}

func (s *Store) Search(query string) ([]SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	lines, err := s.read()
	if err != nil {
		return nil, err
	}
	lowerQuery := strings.ToLower(query)
	var results []SearchResult
	currentSection := "top"
	for i, line := range lines {
		if reH1.MatchString(line) || reH2.MatchString(line) || reH3.MatchString(line) {
			currentSection = strings.TrimSpace(reHeading.ReplaceAllString(line, ""))
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), lowerQuery) {
			results = append(results, SearchResult{
				Section:   currentSection,
				LineIndex: i,
				Line:      strings.TrimSpace(line),
			})
		}
	}
	return results, nil
}

// Uncomplete flips a unique completed task (`- [x]`) back to `- [ ]` in place.
// Disambiguates with AmbiguousMatchError just like CompleteTask. The line is
// not moved — it stays in the section it was completed under.
func (s *Store) Uncomplete(matchText string) (string, error) {
	lines, err := s.read()
	if err != nil {
		return "", err
	}
	matches := findTaskMatches(lines, matchText, reCompletedBox)
	if len(matches) == 0 {
		return "", fmt.Errorf("no completed task matching %q found", matchText)
	}
	if len(matches) > 1 {
		return "", &AmbiguousMatchError{Match: matchText, Candidates: matches}
	}
	return s.uncompleteAtLine(lines, matches[0].LineIndex)
}

func (s *Store) UncompleteByLine(lineIndex int) (string, error) {
	lines, err := s.read()
	if err != nil {
		return "", err
	}
	if lineIndex < 0 || lineIndex >= len(lines) {
		return "", fmt.Errorf("line %d is out of range (file has %d lines)", lineIndex, len(lines))
	}
	if !reCompletedBox.MatchString(lines[lineIndex]) {
		return "", fmt.Errorf("line %d is not a completed task: %q", lineIndex, strings.TrimSpace(lines[lineIndex]))
	}
	return s.uncompleteAtLine(lines, lineIndex)
}

func (s *Store) uncompleteAtLine(lines []string, idx int) (string, error) {
	lines[idx] = reCompletedBox.ReplaceAllString(lines[idx], "- [ ]")
	if err := s.write(lines); err != nil {
		return "", err
	}
	return strings.TrimSpace(lines[idx]), nil
}

// Delete removes a unique task line (open or completed) from the file.
// Disambiguates with AmbiguousMatchError. After removal, two consecutive
// blank lines are collapsed so the file's spacing convention is preserved.
func (s *Store) Delete(matchText string) (string, error) {
	lines, err := s.read()
	if err != nil {
		return "", err
	}
	matches := findTaskMatches(lines, matchText, reAnyTaskBox)
	if len(matches) == 0 {
		return "", fmt.Errorf("no task matching %q found", matchText)
	}
	if len(matches) > 1 {
		return "", &AmbiguousMatchError{Match: matchText, Candidates: matches}
	}
	return s.deleteAtLine(lines, matches[0].LineIndex)
}

func (s *Store) DeleteByLine(lineIndex int) (string, error) {
	lines, err := s.read()
	if err != nil {
		return "", err
	}
	if lineIndex < 0 || lineIndex >= len(lines) {
		return "", fmt.Errorf("line %d is out of range (file has %d lines)", lineIndex, len(lines))
	}
	if !reAnyTaskBox.MatchString(lines[lineIndex]) {
		return "", fmt.Errorf("line %d is not a task: %q", lineIndex, strings.TrimSpace(lines[lineIndex]))
	}
	return s.deleteAtLine(lines, lineIndex)
}

func (s *Store) deleteAtLine(lines []string, idx int) (string, error) {
	deleted := strings.TrimSpace(lines[idx])
	lines = splice(lines, idx, 1)
	if idx > 0 && idx < len(lines) && strings.TrimSpace(lines[idx-1]) == "" && strings.TrimSpace(lines[idx]) == "" {
		lines = splice(lines, idx, 1)
	}
	if err := s.write(lines); err != nil {
		return "", err
	}
	return deleted, nil
}

func (s *Store) AppendToToday(text string) error {
	lines, err := s.read()
	if err != nil {
		return err
	}
	today := s.todayKey()
	header := "# " + today
	headerIdx := findIndex(lines, func(l string, _ int) bool { return strings.TrimSpace(l) == header })

	if headerIdx == -1 {
		todoIdx := findIndex(lines, func(l string, _ int) bool { return reTodoBacklg.MatchString(l) })
		insertAt := todoIdx
		if insertAt == -1 {
			insertAt = len(lines)
		}
		lines = splice(lines, insertAt, 0, "", header, "", text, "")
	} else {
		endIdx := findIndex(lines, func(l string, i int) bool { return i > headerIdx && reAnyHash.MatchString(l) })
		if endIdx == -1 {
			endIdx = len(lines)
		}
		needsLeading := endIdx > 0 && strings.TrimSpace(lines[endIdx-1]) != ""
		needsTrailing := endIdx < len(lines) && reAnyHash.MatchString(lines[endIdx])
		var toInsert []string
		if needsLeading {
			toInsert = append(toInsert, "")
		}
		toInsert = append(toInsert, text)
		if needsTrailing {
			toInsert = append(toInsert, "")
		}
		lines = splice(lines, endIdx, 0, toInsert...)
	}
	return s.write(lines)
}
