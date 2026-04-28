package log

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T, content string) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "log.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	// Pin "today" to a deterministic date for tests that don't need real today.
	s.now = func() time.Time { return time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC) }
	return s
}

func (s *Store) setTodayKey(key string) {
	t, _ := time.Parse("2006-01-02", key)
	s.now = func() time.Time { return t }
}

func (s *Store) readAll(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestListIncomplete_ReturnsOnlyIncomplete(t *testing.T) {
	s := newTestStore(t, `# 2026-01-01

- [ ] [pedal] Fix the bug
- [x] [pedal] Done task
- [ ] Plain task

## Backlog

- [ ] [intwine] Backlog item
`)
	tasks, err := s.ListIncomplete()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("want 3 tasks, got %d", len(tasks))
	}
	for _, tk := range tasks {
		if strings.Contains(tk.Line, "[x]") {
			t.Fatalf("incomplete list contained completed line: %q", tk.Line)
		}
	}
}

func TestListIncomplete_ExtractsProjectAndDescription(t *testing.T) {
	s := newTestStore(t, `# 2026-01-01

- [ ] [pedal] Fix the bug
- [ ] Plain task

## Backlog

- [ ] [intwine] Backlog item
`)
	tasks, err := s.ListIncomplete()
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Project != "pedal" || tasks[0].Description != "Fix the bug" {
		t.Errorf("task[0] = %+v", tasks[0])
	}
	if tasks[1].Project != "" || tasks[1].Description != "Plain task" {
		t.Errorf("task[1] = %+v", tasks[1])
	}
	if tasks[2].Project != "intwine" {
		t.Errorf("task[2] = %+v", tasks[2])
	}
}

func TestListIncomplete_TracksSection(t *testing.T) {
	s := newTestStore(t, `# 2026-01-01

- [ ] [pedal] Fix the bug

## Backlog

- [ ] [intwine] Backlog item
`)
	tasks, _ := s.ListIncomplete()
	if tasks[0].Section != "2026-01-01" {
		t.Errorf("want section 2026-01-01, got %q", tasks[0].Section)
	}
	if tasks[1].Section != "Backlog" {
		t.Errorf("want section Backlog, got %q", tasks[1].Section)
	}
}

func TestListIncomplete_EmptyWhenAllComplete(t *testing.T) {
	s := newTestStore(t, "# 2026-01-01\n\n- [x] Done\n")
	tasks, _ := s.ListIncomplete()
	if len(tasks) != 0 {
		t.Errorf("expected empty, got %+v", tasks)
	}
}

func TestCreateTask_WithoutForToday_AddsToTodoWithProject(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] existing\n")
	if err := s.CreateTask("myproject", "New task", false); err != nil {
		t.Fatal(err)
	}
	content := s.readAll(t)
	if !strings.Contains(content, "- [ ] [myproject] New task") {
		t.Errorf("content missing new task:\n%s", content)
	}
}

func TestCreateTask_WithoutProject(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] existing\n")
	if err := s.CreateTask("", "No project task", false); err != nil {
		t.Fatal(err)
	}
	content := s.readAll(t)
	if !strings.Contains(content, "- [ ] No project task") {
		t.Errorf("missing plain task:\n%s", content)
	}
	if strings.Contains(content, "- [ ] [] No project task") {
		t.Errorf("empty project brackets present:\n%s", content)
	}
}

func TestCreateTask_ForToday_AppendsToExisting(t *testing.T) {
	s := newTestStore(t, "")
	s.setTodayKey("2026-04-15")
	if err := os.WriteFile(s.Path, []byte("# 2026-04-15\n\n- [ ] existing task\n\n## Backlog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask("", "Another task", true); err != nil {
		t.Fatal(err)
	}
	entry, _ := s.GetToday()
	if !strings.Contains(entry, "existing task") || !strings.Contains(entry, "Another task") {
		t.Errorf("today entry missing tasks:\n%s", entry)
	}
}

func TestCompleteTask_MarksMatchingComplete(t *testing.T) {
	s := newTestStore(t, "# 2026-01-01\n\n- [ ] [pedal] Fix bug\n")
	if _, err := s.CompleteTask("Fix bug"); err != nil {
		t.Fatal(err)
	}
	tasks, _ := s.ListIncomplete()
	for _, tk := range tasks {
		if strings.Contains(tk.Description, "Fix bug") {
			t.Errorf("still incomplete: %+v", tk)
		}
	}
}

func TestCompleteTask_MovesToTodayAndRemovesOriginal(t *testing.T) {
	today := "2026-04-15"
	s := newTestStore(t, "")
	s.setTodayKey(today)
	content := "# 2026-01-01\n\n- [ ] Old task\n\n# " + today + "\n\n- [ ] Other task\n"
	os.WriteFile(s.Path, []byte(content), 0o644)

	if _, err := s.CompleteTask("Old task"); err != nil {
		t.Fatal(err)
	}
	entry, _ := s.GetToday()
	if !strings.Contains(entry, "- [x] Old task") {
		t.Errorf("completed task not in today:\n%s", entry)
	}
	full := s.readAll(t)
	oldSection := strings.Split(full, "# "+today)[0]
	if strings.Contains(oldSection, "Old task") {
		t.Errorf("task still present in old section:\n%s", oldSection)
	}
	if strings.Count(full, "Old task") != 1 {
		t.Errorf("want 1 occurrence, got %d:\n%s", strings.Count(full, "Old task"), full)
	}
}

func TestCompleteTask_ErrorsWhenNoMatch(t *testing.T) {
	s := newTestStore(t, "# 2026-01-01\n\n- [ ] Real task\n")
	if _, err := s.CompleteTask("nonexistent"); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestCompleteTask_AmbiguousMatchReturnsCandidates(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] alpha one\n- [ ] alpha two\n- [ ] beta\n")
	_, err := s.CompleteTask("alpha")
	var amb *AmbiguousMatchError
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.As(err, &amb) {
		t.Fatalf("expected *AmbiguousMatchError, got %T: %v", err, err)
	}
	if len(amb.Candidates) != 2 {
		t.Errorf("want 2 candidates, got %d: %+v", len(amb.Candidates), amb.Candidates)
	}
	if amb.Candidates[0].LineIndex == amb.Candidates[1].LineIndex {
		t.Errorf("candidates should have distinct line indexes: %+v", amb.Candidates)
	}
}

func TestCompleteTaskByLine_CompletesExactLine(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] alpha one\n- [ ] alpha two\n")
	s.setTodayKey("2026-04-15")
	tasks, _ := s.ListIncomplete()
	var target Task
	for _, tk := range tasks {
		if strings.Contains(tk.Description, "alpha two") {
			target = tk
			break
		}
	}
	if _, err := s.CompleteTaskByLine(target.LineIndex); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if !strings.Contains(full, "- [x] alpha two") {
		t.Errorf("alpha two not completed:\n%s", full)
	}
	if !strings.Contains(full, "- [ ] alpha one") {
		t.Errorf("alpha one should still be incomplete:\n%s", full)
	}
}

func TestCompleteTaskByLine_RejectsNonTaskLine(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] real task\n")
	if _, err := s.CompleteTaskByLine(0); err == nil {
		t.Error("expected error for non-task line, got nil")
	}
}

func TestCompleteTaskByLine_RejectsCompletedLine(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [x] already done\n")
	if _, err := s.CompleteTaskByLine(2); err == nil {
		t.Error("expected error for completed line, got nil")
	}
}


func TestCompleteTask_CaseInsensitiveMatch(t *testing.T) {
	s := newTestStore(t, "# 2026-01-01\n\n- [ ] Fix Bug\n")
	if _, err := s.CompleteTask("fix bug"); err != nil {
		t.Fatal(err)
	}
	entry, _ := s.GetToday()
	if !strings.Contains(entry, "- [x]") {
		t.Errorf("task not marked complete:\n%s", entry)
	}
}

func TestGetToday_ReturnsTodaySection(t *testing.T) {
	today := "2026-04-15"
	s := newTestStore(t, "")
	s.setTodayKey(today)
	os.WriteFile(s.Path, []byte("# "+today+"\n\n- [ ] task\n\n# 2020-01-01\n\n- [ ] old\n"), 0o644)
	entry, _ := s.GetToday()
	if !strings.Contains(entry, "# "+today) || strings.Contains(entry, "2020-01-01") {
		t.Errorf("entry wrong:\n%s", entry)
	}
}

func TestGetToday_NoEntryMessage(t *testing.T) {
	s := newTestStore(t, "# 2020-01-01\n\n- [ ] old\n")
	s.setTodayKey("2026-04-15")
	entry, _ := s.GetToday()
	if !strings.Contains(entry, "No entry for") {
		t.Errorf("want 'No entry for', got:\n%s", entry)
	}
}

func TestAppendToToday_AppendsToExisting(t *testing.T) {
	today := "2026-04-15"
	s := newTestStore(t, "")
	s.setTodayKey(today)
	os.WriteFile(s.Path, []byte("# "+today+"\n\n- [ ] task\n\n## Backlog\n"), 0o644)
	if err := s.AppendToToday("A note here"); err != nil {
		t.Fatal(err)
	}
	entry, _ := s.GetToday()
	if !strings.Contains(entry, "A note here") {
		t.Errorf("note missing:\n%s", entry)
	}
}

func TestAppendToToday_CreatesSectionIfMissing(t *testing.T) {
	s := newTestStore(t, "## Backlog\n\n- [ ] something\n")
	s.setTodayKey("2026-04-15")
	if err := s.AppendToToday("Fresh note"); err != nil {
		t.Fatal(err)
	}
	entry, _ := s.GetToday()
	if !strings.Contains(entry, "Fresh note") {
		t.Errorf("note missing:\n%s", entry)
	}
}

func TestGetEntry_ReturnsNamedDate(t *testing.T) {
	s := newTestStore(t, "# 2026-04-10\n\n- [x] earlier task\n\n# 2026-04-15\n\n- [ ] later task\n")
	entry, err := s.GetEntry("2026-04-10")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry, "earlier task") || strings.Contains(entry, "later task") {
		t.Errorf("got wrong entry:\n%s", entry)
	}
}

func TestGetEntry_NotFoundMessage(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [ ] task\n")
	entry, err := s.GetEntry("1999-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry, "No entry for 1999-01-01") {
		t.Errorf("want 'No entry for', got:\n%s", entry)
	}
}

func TestSearch_FindsMatchingLines(t *testing.T) {
	s := newTestStore(t, `# 2026-04-15

- [ ] [pedal] Fix the bug
- [x] [pedal] Write the docs

## Backlog

- [ ] [intwine] migrate database
`)
	results, err := s.Search("docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d: %+v", len(results), results)
	}
	if !strings.Contains(results[0].Line, "Write the docs") {
		t.Errorf("unexpected match: %q", results[0].Line)
	}
	if results[0].Section != "2026-04-15" {
		t.Errorf("want section 2026-04-15, got %q", results[0].Section)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [ ] Fix the BUG\n")
	results, err := s.Search("bug")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1, got %d", len(results))
	}
}

func TestSearch_TracksSection(t *testing.T) {
	s := newTestStore(t, `# 2026-04-15

- [ ] foo

## Backlog

- [ ] foo bar
`)
	results, _ := s.Search("foo")
	if len(results) != 2 {
		t.Fatalf("want 2, got %d", len(results))
	}
	if results[0].Section != "2026-04-15" {
		t.Errorf("results[0].Section = %q", results[0].Section)
	}
	if results[1].Section != "Backlog" {
		t.Errorf("results[1].Section = %q", results[1].Section)
	}
}

func TestSearch_SkipsHeadingsAndBlankLines(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [ ] real match\n")
	results, _ := s.Search("2026")
	if len(results) != 0 {
		t.Errorf("want 0 (heading shouldn't match), got %+v", results)
	}
}

func TestSearch_EmptyQueryErrors(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n")
	if _, err := s.Search(""); err == nil {
		t.Error("expected error for empty query")
	}
}

func TestUncomplete_FlipsBackInPlace(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [x] [pedal] Fix bug\n- [ ] Other\n")
	line, err := s.Uncomplete("Fix bug")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "- [ ]") {
		t.Errorf("returned line should be incomplete, got %q", line)
	}
	full := s.readAll(t)
	if !strings.Contains(full, "- [ ] [pedal] Fix bug") {
		t.Errorf("task not flipped:\n%s", full)
	}
	if strings.Contains(full, "- [x] [pedal] Fix bug") {
		t.Errorf("completed marker still present:\n%s", full)
	}
	if strings.Count(full, "Fix bug") != 1 {
		t.Errorf("task duplicated; want 1, got %d:\n%s", strings.Count(full, "Fix bug"), full)
	}
}

func TestUncomplete_ErrorsOnIncompleteMatch(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [ ] open task\n")
	if _, err := s.Uncomplete("open task"); err == nil {
		t.Error("expected error matching only incomplete tasks, got nil")
	}
}

func TestUncomplete_AmbiguousReturnsCandidates(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [x] alpha one\n- [x] alpha two\n- [x] beta\n")
	_, err := s.Uncomplete("alpha")
	var amb *AmbiguousMatchError
	if !errors.As(err, &amb) {
		t.Fatalf("expected *AmbiguousMatchError, got %T: %v", err, err)
	}
	if len(amb.Candidates) != 2 {
		t.Errorf("want 2 candidates, got %d", len(amb.Candidates))
	}
}

func TestUncompleteByLine_FlipsExactLine(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [x] alpha one\n- [x] alpha two\n")
	if _, err := s.UncompleteByLine(3); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if !strings.Contains(full, "- [ ] alpha two") {
		t.Errorf("alpha two not flipped:\n%s", full)
	}
	if !strings.Contains(full, "- [x] alpha one") {
		t.Errorf("alpha one should still be completed:\n%s", full)
	}
}

func TestUncompleteByLine_RejectsIncompleteLine(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] open\n")
	if _, err := s.UncompleteByLine(2); err == nil {
		t.Error("expected error uncompleting an open task, got nil")
	}
}

func TestDelete_RemovesIncompleteTask(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] keep me\n- [ ] kill me\n- [ ] also keep\n")
	line, err := s.Delete("kill me")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "kill me") {
		t.Errorf("returned line wrong: %q", line)
	}
	full := s.readAll(t)
	if strings.Contains(full, "kill me") {
		t.Errorf("task still present:\n%s", full)
	}
	if !strings.Contains(full, "keep me") || !strings.Contains(full, "also keep") {
		t.Errorf("siblings missing:\n%s", full)
	}
}

func TestDelete_RemovesCompletedTask(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [x] [pedal] historical mistake\n- [x] [pedal] real work\n")
	if _, err := s.Delete("historical mistake"); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if strings.Contains(full, "historical mistake") {
		t.Errorf("completed task still present:\n%s", full)
	}
	if !strings.Contains(full, "real work") {
		t.Errorf("sibling completed task missing:\n%s", full)
	}
}

func TestDelete_CollapsesAdjacentBlankLines(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] only one\n\n## Backlog\n")
	if _, err := s.Delete("only one"); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if strings.Contains(full, "\n\n\n") {
		t.Errorf("triple newline left after delete:\n%q", full)
	}
}

func TestDelete_AmbiguousReturnsCandidates(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] alpha one\n- [x] alpha two\n- [ ] beta\n")
	_, err := s.Delete("alpha")
	var amb *AmbiguousMatchError
	if !errors.As(err, &amb) {
		t.Fatalf("expected *AmbiguousMatchError, got %T: %v", err, err)
	}
	if len(amb.Candidates) != 2 {
		t.Errorf("want 2 candidates, got %d: %+v", len(amb.Candidates), amb.Candidates)
	}
}

func TestDelete_ErrorsWhenNoMatch(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] real task\n")
	if _, err := s.Delete("nonexistent"); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestDeleteByLine_RemovesExactLine(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] keep\n- [ ] zap\n")
	if _, err := s.DeleteByLine(3); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if strings.Contains(full, "zap") {
		t.Errorf("zap still present:\n%s", full)
	}
	if !strings.Contains(full, "keep") {
		t.Errorf("keep missing:\n%s", full)
	}
}

func TestDeleteByLine_RejectsNonTaskLine(t *testing.T) {
	s := newTestStore(t, "## Todo\n\n- [ ] real task\n")
	if _, err := s.DeleteByLine(0); err == nil {
		t.Error("expected error for non-task line, got nil")
	}
}

func TestSearch_NoMatchesReturnsEmpty(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [ ] task\n")
	results, err := s.Search("zzznomatch")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("want empty, got %+v", results)
	}
}
