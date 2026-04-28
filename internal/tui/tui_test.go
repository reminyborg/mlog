package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	mlog "mlog/internal/log"
)

func newTestModel(t *testing.T, content string) (model, *mlog.Store) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "log.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	store := mlog.New(path)
	mlog.SetNow(store, func() time.Time { return time.Date(2026, 4, 15, 12, 0, 0, 0, time.Local) })
	m, err := initialModel(store)
	if err != nil {
		t.Fatal(err)
	}
	return m, store
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func send(m model, msgs ...tea.Msg) model {
	var tm tea.Model = m
	for _, msg := range msgs {
		tm, _ = tm.Update(msg)
	}
	return tm.(model)
}

func read(t *testing.T, store *mlog.Store) string {
	t.Helper()
	b, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTUI_CompleteSelectedTask(t *testing.T) {
	m, store := newTestModel(t, "# 2026-01-01\n\n- [ ] [pedal] Fix bug\n- [ ] write docs\n")
	got := send(m, key("down"), key("enter"))
	if got.errMsg != "" {
		t.Fatalf("unexpected errMsg: %q", got.errMsg)
	}
	content := read(t, store)
	if !strings.Contains(content, "- [x] write docs") {
		t.Errorf("docs task not completed:\n%s", content)
	}
	if strings.Contains(content, "- [x] [pedal] Fix bug") {
		t.Errorf("wrong task completed:\n%s", content)
	}
}

func TestTUI_CreatePromptAddsTask(t *testing.T) {
	m, store := newTestModel(t, "## Todo\n\n- [ ] existing\n")
	m = send(m, key("n"))
	for _, r := range "[proj] new thing" {
		m = send(m, key(string(r)))
	}
	m = send(m, key("enter"))
	if m.errMsg != "" {
		t.Fatalf("unexpected errMsg: %q", m.errMsg)
	}
	if !strings.Contains(read(t, store), "- [ ] [proj] new thing") {
		t.Errorf("created task missing:\n%s", read(t, store))
	}
	if m.prompt != promptNone {
		t.Errorf("prompt should be reset, got %v", m.prompt)
	}
}

func TestTUI_EscCancelsPrompt(t *testing.T) {
	m, store := newTestModel(t, "## Todo\n\n- [ ] existing\n")
	before := read(t, store)
	m = send(m, key("n"), key("x"), key("esc"))
	if m.prompt != promptNone {
		t.Errorf("prompt should be reset, got %v", m.prompt)
	}
	if read(t, store) != before {
		t.Errorf("file should be unchanged after cancel")
	}
}

func TestParseProjectPrefix(t *testing.T) {
	cases := []struct{ in, proj, rest string }{
		{"[foo] bar baz", "foo", "bar baz"},
		{"no prefix here", "", "no prefix here"},
		{"[unterminated bar", "", "[unterminated bar"},
		{"  [trim] me  ", "trim", "me"},
	}
	for _, c := range cases {
		p, r := parseProjectPrefix(c.in)
		if p != c.proj || r != c.rest {
			t.Errorf("parseProjectPrefix(%q) = (%q, %q), want (%q, %q)", c.in, p, r, c.proj, c.rest)
		}
	}
}
