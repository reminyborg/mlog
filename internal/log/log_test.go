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

# Backlog

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

# Backlog

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

# Backlog

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
	s := newTestStore(t, "# Todo\n\n- [ ] existing\n")
	if err := s.CreateTask("myproject", "New task", ""); err != nil {
		t.Fatal(err)
	}
	content := s.readAll(t)
	if !strings.Contains(content, "- [ ] [myproject] New task") {
		t.Errorf("content missing new task:\n%s", content)
	}
}

func TestCreateTask_WithoutProject(t *testing.T) {
	s := newTestStore(t, "# Todo\n\n- [ ] existing\n")
	if err := s.CreateTask("", "No project task", ""); err != nil {
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
	if err := os.WriteFile(s.Path, []byte("# 2026-04-15\n\n- [ ] existing task\n\n# Backlog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask("", "Another task", "2026-04-15"); err != nil {
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
	s := newTestStore(t, "# Todo\n\n- [ ] alpha one\n- [ ] alpha two\n- [ ] beta\n")
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
	s := newTestStore(t, "# Todo\n\n- [ ] alpha one\n- [ ] alpha two\n")
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
	s := newTestStore(t, "# Todo\n\n- [ ] real task\n")
	if _, err := s.CompleteTaskByLine(0); err == nil {
		t.Error("expected error for non-task line, got nil")
	}
}

func TestCompleteTaskByLine_RejectsCompletedLine(t *testing.T) {
	s := newTestStore(t, "# Todo\n\n- [x] already done\n")
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

func TestAppendToToday_NewHeaderGoesAfterLastDateHeader(t *testing.T) {
	s := newTestStore(t, "# Todo\n\n- [ ] keep me\n\n# Backlog\n\n- old\n\n# 2026-04-15\n\n- [x] earlier\n")
	s.setTodayKey("2026-05-01")
	if err := s.AppendToToday("fresh"); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	headerIdx := strings.Index(full, "# 2026-05-01")
	earlierIdx := strings.Index(full, "# 2026-04-15")
	todoIdx := strings.Index(full, "# Todo")
	if headerIdx == -1 || earlierIdx == -1 || todoIdx == -1 {
		t.Fatalf("missing expected sections:\n%s", full)
	}
	if !(headerIdx > earlierIdx && headerIdx > todoIdx) {
		t.Errorf("new date header should land after last date H1 (and after Todo/Backlog when those precede it):\n%s", full)
	}
}

func TestAppendToToday_AppendsToExisting(t *testing.T) {
	today := "2026-04-15"
	s := newTestStore(t, "")
	s.setTodayKey(today)
	os.WriteFile(s.Path, []byte("# "+today+"\n\n- [ ] task\n\n# Backlog\n"), 0o644)
	if err := s.AppendToToday("A note here"); err != nil {
		t.Fatal(err)
	}
	entry, _ := s.GetToday()
	if !strings.Contains(entry, "A note here") {
		t.Errorf("note missing:\n%s", entry)
	}
}

func TestAppendToToday_CreatesSectionIfMissing(t *testing.T) {
	s := newTestStore(t, "# Backlog\n\n- [ ] something\n")
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

# Backlog

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

# Backlog

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
	s := newTestStore(t, "# Todo\n\n- [ ] open\n")
	if _, err := s.UncompleteByLine(2); err == nil {
		t.Error("expected error uncompleting an open task, got nil")
	}
}

func TestDelete_RemovesIncompleteTask(t *testing.T) {
	s := newTestStore(t, "# Todo\n\n- [ ] keep me\n- [ ] kill me\n- [ ] also keep\n")
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
	s := newTestStore(t, "# Todo\n\n- [ ] only one\n\n# Backlog\n")
	if _, err := s.Delete("only one"); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if strings.Contains(full, "\n\n\n") {
		t.Errorf("triple newline left after delete:\n%q", full)
	}
}

func TestDelete_AmbiguousReturnsCandidates(t *testing.T) {
	s := newTestStore(t, "# Todo\n\n- [ ] alpha one\n- [x] alpha two\n- [ ] beta\n")
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
	s := newTestStore(t, "# Todo\n\n- [ ] real task\n")
	if _, err := s.Delete("nonexistent"); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestDeleteByLine_RemovesExactLine(t *testing.T) {
	s := newTestStore(t, "# Todo\n\n- [ ] keep\n- [ ] zap\n")
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
	s := newTestStore(t, "# Todo\n\n- [ ] real task\n")
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

// ---- Project tests ---------------------------------------------------------

func TestListProjects_ReturnsDefined(t *testing.T) {
	s := newTestStore(t, "[myproject]: https://github.com/user/myproject\n[sideproject]: https://github.com/user/sideproject\n\n# 2026-04-15\n\n- [ ] [myproject] some task\n")
	projects, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("want 2 projects, got %d: %+v", len(projects), projects)
	}
	if projects[0].Name != "myproject" || projects[0].URL != "https://github.com/user/myproject" {
		t.Errorf("unexpected project[0]: %+v", projects[0])
	}
	if projects[1].Name != "sideproject" {
		t.Errorf("unexpected project[1]: %+v", projects[1])
	}
}

func TestListProjects_EmptyWhenNone(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [ ] plain task\n")
	projects, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Errorf("want 0 projects, got %d", len(projects))
	}
}

func TestAddProject_AppendsToRefBlock(t *testing.T) {
	s := newTestStore(t, "[alpha]: https://alpha.example\n\n# 2026-04-15\n\n- [ ] task\n")
	if err := s.AddProject("beta", "https://beta.example"); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if !strings.Contains(full, "[beta]: https://beta.example") {
		t.Errorf("new project ref not found:\n%s", full)
	}
	// Beta should come after alpha.
	alphaPos := strings.Index(full, "[alpha]:")
	betaPos := strings.Index(full, "[beta]:")
	if betaPos < alphaPos {
		t.Errorf("beta appears before alpha:\n%s", full)
	}
}

func TestAddProject_NoExistingRefs(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n\n- [ ] task\n")
	if err := s.AddProject("newproj", "https://new.example"); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if !strings.Contains(full, "[newproj]: https://new.example") {
		t.Errorf("project ref not found:\n%s", full)
	}
	// Ref block should be separated from the date heading by a blank line.
	if strings.Contains(full, "[newproj]: https://new.example\n# ") {
		t.Errorf("missing blank line between ref and heading:\n%s", full)
	}
}

func TestAddProject_ErrorsOnDuplicate(t *testing.T) {
	s := newTestStore(t, "[alpha]: https://alpha.example\n\n# 2026-04-15\n")
	if err := s.AddProject("alpha", "https://other.example"); err == nil {
		t.Error("expected error for duplicate project, got nil")
	}
}

func TestAddProject_NoURL(t *testing.T) {
	s := newTestStore(t, "# 2026-04-15\n")
	if err := s.AddProject("bare", ""); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if !strings.Contains(full, "[bare]:") {
		t.Errorf("bare project ref not found:\n%s", full)
	}
}

func TestDeleteProject_RemovesRef(t *testing.T) {
	s := newTestStore(t, "[alpha]: https://alpha.example\n[beta]: https://beta.example\n\n# 2026-04-15\n")
	if err := s.DeleteProject("alpha"); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if strings.Contains(full, "[alpha]:") {
		t.Errorf("alpha still present:\n%s", full)
	}
	if !strings.Contains(full, "[beta]:") {
		t.Errorf("beta missing after deleting alpha:\n%s", full)
	}
}

func TestDeleteProject_ErrorsWhenNotFound(t *testing.T) {
	s := newTestStore(t, "[alpha]: https://alpha.example\n\n# 2026-04-15\n")
	if err := s.DeleteProject("ghost"); err == nil {
		t.Error("expected error for missing project, got nil")
	}
}

func TestEditProject_RenameUpdatesRefAndTasks(t *testing.T) {
	content := "[oldname]: https://example.com\n\n# Todo\n\n- [ ] [oldname] do something\n\n### oldname\n\n- [ ] [oldname] subtask\n"
	s := newTestStore(t, content)
	if err := s.EditProject("oldname", "newname", ""); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if strings.Contains(full, "[oldname]") {
		t.Errorf("old name still present:\n%s", full)
	}
	if !strings.Contains(full, "[newname]: https://example.com") {
		t.Errorf("renamed ref not found:\n%s", full)
	}
	if !strings.Contains(full, "[newname] do something") {
		t.Errorf("task ref not updated:\n%s", full)
	}
	if !strings.Contains(full, "### newname") {
		t.Errorf("H3 heading not updated:\n%s", full)
	}
}

func TestEditProject_ChangeURL(t *testing.T) {
	s := newTestStore(t, "[proj]: https://old.example\n\n# 2026-04-15\n")
	if err := s.EditProject("proj", "", "https://new.example"); err != nil {
		t.Fatal(err)
	}
	full := s.readAll(t)
	if !strings.Contains(full, "[proj]: https://new.example") {
		t.Errorf("URL not updated:\n%s", full)
	}
	if strings.Contains(full, "old.example") {
		t.Errorf("old URL still present:\n%s", full)
	}
}

func TestEditProject_ErrorsWhenNotFound(t *testing.T) {
	s := newTestStore(t, "[alpha]: https://alpha.example\n\n# 2026-04-15\n")
	if err := s.EditProject("ghost", "ghost2", ""); err == nil {
		t.Error("expected error for missing project, got nil")
	}
}

func TestEditProject_ErrorsOnNameCollision(t *testing.T) {
	s := newTestStore(t, "[alpha]: https://a.example\n[beta]: https://b.example\n\n# 2026-04-15\n")
	if err := s.EditProject("alpha", "beta", ""); err == nil {
		t.Error("expected error when renaming to existing name, got nil")
	}
}

// ---- Validate tests ---------------------------------------------------------

func TestValidate_ValidFile(t *testing.T) {
	s := newTestStore(t, "# 2026-01-01\n\n- [ ] task\n\n# Todo\n\n- [ ] todo\n\n# Backlog\n\n- item\n")
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid file, got: %v", err)
	}
}

func TestValidate_EmptyFile(t *testing.T) {
	s := newTestStore(t, "")
	if err := s.Validate(); err != nil {
		t.Errorf("expected empty file to be valid, got: %v", err)
	}
}

func TestValidate_DuplicateTodo(t *testing.T) {
	s := newTestStore(t, "# Todo\n\n- [ ] a\n\n# Todo\n\n- [ ] b\n")
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate # Todo")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	found := false
	for _, iss := range ve.Issues {
		if strings.Contains(iss.Message, "duplicate # Todo") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate # Todo issue, got: %v", ve.Issues)
	}
}

func TestValidate_DuplicateBacklog(t *testing.T) {
	s := newTestStore(t, "# Backlog\n\n- a\n\n# Backlog\n\n- b\n")
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate # Backlog")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	found := false
	for _, iss := range ve.Issues {
		if strings.Contains(iss.Message, "duplicate # Backlog") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate # Backlog issue, got: %v", ve.Issues)
	}
}

func TestValidate_DuplicateDateHeader(t *testing.T) {
	s := newTestStore(t, "# 2026-01-01\n\n- [ ] first\n\n# 2026-01-01\n\n- [ ] second\n")
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate date header")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	found := false
	for _, iss := range ve.Issues {
		if strings.Contains(iss.Message, "duplicate date header") && strings.Contains(iss.Message, "2026-01-01") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate date header issue, got: %v", ve.Issues)
	}
}

func TestValidate_MissingBlankLineBeforeHeading(t *testing.T) {
	s := newTestStore(t, "# 2026-01-01\n- [ ] task\n# Todo\n")
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing blank line before heading")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	found := false
	for _, iss := range ve.Issues {
		if strings.Contains(iss.Message, "no blank line before it") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected blank-line issue, got: %v", ve.Issues)
	}
}

func TestValidate_FirstLineHeadingIsOK(t *testing.T) {
	// Heading on the very first line requires no blank line before it.
	s := newTestStore(t, "# 2026-01-01\n\n- [ ] task\n")
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid file (first-line heading), got: %v", err)
	}
}

func TestValidate_MultipleIssuesAllReported(t *testing.T) {
	// Duplicate # Todo AND missing blank before second one.
	s := newTestStore(t, "# Todo\n\n- [ ] a\n# Todo\n\n- [ ] b\n")
	err := s.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(ve.Issues) < 2 {
		t.Errorf("expected at least 2 issues (duplicate + missing blank), got %d: %v", len(ve.Issues), ve.Issues)
	}
}
