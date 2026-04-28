package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	mlog "github.com/reminyborg/mlog/internal/log"
)

type promptKind int

const (
	promptNone promptKind = iota
	promptCreate
)

type model struct {
	store *mlog.Store

	tasks []mlog.Task

	cursor int

	// prompt state
	prompt      promptKind
	input       textinput.Model
	createToday bool

	status string // last action result
	errMsg string
}

func Run(store *mlog.Store) error {
	m, err := initialModel(store)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func initialModel(store *mlog.Store) (model, error) {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Width = 60

	m := model{
		store: store,
		input: ti,
	}
	if err := m.refresh(); err != nil {
		return m, err
	}
	return m, nil
}

func (m *model) refresh() error {
	tasks, err := m.store.ListIncomplete()
	if err != nil {
		return err
	}
	m.tasks = tasks
	if m.cursor >= len(tasks) {
		m.cursor = len(tasks) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return nil
}

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.prompt != promptNone {
			return m.updatePrompt(msg)
		}
		return m.updateNav(msg)
	}
	return m, nil
}

func (m model) updateNav(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	m.errMsg = ""

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "r":
		if err := m.refresh(); err != nil {
			m.errMsg = err.Error()
		}
		return m, nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.tasks)-1 {
			m.cursor++
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = len(m.tasks) - 1
	case "enter", " ":
		if len(m.tasks) == 0 {
			return m, nil
		}
		task := m.tasks[m.cursor]
		line, err := m.store.CompleteTask(task.Line)
		if err != nil {
			m.errMsg = err.Error()
		} else {
			m.status = "completed: " + line
		}
		if err := m.refresh(); err != nil {
			m.errMsg = err.Error()
		}
		return m, nil
	case "d":
		if len(m.tasks) == 0 {
			return m, nil
		}
		task := m.tasks[m.cursor]
		line, err := m.store.DeleteByLine(task.LineIndex)
		if err != nil {
			m.errMsg = err.Error()
		} else {
			m.status = "deleted: " + line
		}
		if err := m.refresh(); err != nil {
			m.errMsg = err.Error()
		}
		return m, nil
	case "n":
		m.prompt = promptCreate
		m.createToday = false
		m.input.Reset()
		m.input.Placeholder = "description (prefix with [proj] to tag a project)"
		m.input.Focus()
		return m, textinput.Blink
	case "N":
		m.prompt = promptCreate
		m.createToday = true
		m.input.Reset()
		m.input.Placeholder = "description for TODAY (prefix with [proj] to tag)"
		m.input.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.prompt = promptNone
		m.input.Blur()
		return m, nil
	case tea.KeyEnter:
		return m.submitPrompt()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// parseProjectPrefix extracts "[proj] rest" into ("proj", "rest"); returns ("", full) if absent.
func parseProjectPrefix(s string) (string, string) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") {
		return "", s
	}
	end := strings.Index(s, "]")
	if end == -1 {
		return "", s
	}
	proj := s[1:end]
	rest := strings.TrimSpace(s[end+1:])
	return proj, rest
}

func (m model) submitPrompt() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())

	switch m.prompt {
	case promptCreate:
		if text == "" {
			m.errMsg = "description required"
			m.prompt = promptNone
			m.input.Blur()
			return m, nil
		}
		proj, desc := parseProjectPrefix(text)
		if err := m.store.CreateTask(proj, desc, m.createToday); err != nil {
			m.errMsg = err.Error()
		} else {
			where := "Todo"
			if m.createToday {
				where = "Today"
			}
			m.status = fmt.Sprintf("created in %s: %s", where, desc)
		}
		m.prompt = promptNone
		m.input.Blur()
		if err := m.refresh(); err != nil {
			m.errMsg = err.Error()
		}
		return m, nil
	}

	m.prompt = promptNone
	m.input.Blur()
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(" mlog "))
	b.WriteString("  ")
	b.WriteString(mutedStyle.Render(m.store.Path))
	b.WriteString("\n\n")

	b.WriteString(m.renderTasks())

	if m.prompt != promptNone {
		b.WriteString("\n")
		label := ""
		if m.prompt == promptCreate {
			if m.createToday {
				label = "new (today): "
			} else {
				label = "new (todo): "
			}
		}
		b.WriteString(inputPromptStyle.Render(label))
		b.WriteString(m.input.View())
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("enter to submit • esc to cancel"))
	} else {
		b.WriteString("\n")
		b.WriteString(m.renderHelp())
	}

	if m.status != "" {
		b.WriteString("\n")
		b.WriteString(statusStyle.Render("✓ " + m.status))
	}
	if m.errMsg != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("✗ " + m.errMsg))
	}

	return b.String()
}

func (m model) renderTasks() string {
	if len(m.tasks) == 0 {
		return mutedStyle.Render("  (no incomplete tasks — press n to create one)")
	}
	var b strings.Builder
	currentSection := ""
	for i, t := range m.tasks {
		if t.Section != currentSection {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(sectionStyle.Render("  " + t.Section))
			b.WriteString("\n")
			currentSection = t.Section
		}
		cursor := "  "
		if i == m.cursor {
			cursor = "› "
		}
		line := renderTaskLine(t)
		if i == m.cursor {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		}
		b.WriteString("  " + cursor + line + "\n")
	}
	return b.String()
}

func renderTaskLine(t mlog.Task) string {
	if t.Project != "" {
		return projectStyle.Render("["+t.Project+"]") + " " + t.Description
	}
	return t.Description
}

func (m model) renderHelp() string {
	keys := "↑/↓ move  •  enter complete  •  d delete  •  n new  •  N new (today)  •  r refresh  •  q quit"
	return helpStyle.Render(keys)
}
