package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var (
	accent  = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	muted   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorUI = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
)

func IsInteractive(input io.Reader, output io.Writer) bool {
	inputFile, inputOK := input.(interface{ Fd() uintptr })
	outputFile, outputOK := output.(interface{ Fd() uintptr })
	return inputOK && outputOK && term.IsTerminal(int(inputFile.Fd())) && term.IsTerminal(int(outputFile.Fd()))
}

type Status struct {
	Label   string
	Detail  string
	Success bool
}

func RenderStatus(output io.Writer, status Status) {
	icon := errorUI.Render("●")
	if status.Success {
		icon = accent.Render("●")
	}
	fmt.Fprintf(output, "%s %s\n%s\n", icon, accent.Render(status.Label), muted.Render(status.Detail))
}

type Spinner struct {
	program *tea.Program
	done    chan struct{}
}

func StartSpinner(output io.Writer, label string) *Spinner {
	model := spinnerModel{label: label}
	program := tea.NewProgram(model, tea.WithOutput(output), tea.WithoutSignalHandler(), tea.WithoutCatchPanics(), tea.WithInput(nil))
	spinner := &Spinner{program: program, done: make(chan struct{})}
	go func() {
		_, _ = program.Run()
		close(spinner.done)
	}()
	return spinner
}

func (s *Spinner) Stop(message string, success bool) {
	if s == nil {
		return
	}
	s.program.Send(doneMsg{message: message, success: success})
	<-s.done
}

type spinnerModel struct {
	label   string
	frame   int
	message string
	done    bool
	success bool
}

type tickMsg time.Time
type doneMsg struct {
	message string
	success bool
}

func (m spinnerModel) Init() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m spinnerModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := message.(type) {
	case tickMsg:
		m.frame++
		return m, tea.Tick(90*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
	case doneMsg:
		m.done = true
		m.success = typed.success
		m.message = typed.message
		return m, tea.Quit
	}
	return m, nil
}

func (m spinnerModel) View() string {
	if m.done {
		icon := errorUI.Render("✗")
		if m.success {
			icon = accent.Render("✓")
		}
		return fmt.Sprintf("%s %s\n", icon, m.message)
	}
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return fmt.Sprintf("%s %s", accent.Render(frames[m.frame%len(frames)]), m.label)
}

type MenuItem struct {
	Title       string
	Description string
	Value       string
}

func Choose(ctx context.Context, input io.Reader, output io.Writer, title string, items []MenuItem) (string, error) {
	if len(items) == 0 {
		return "", errors.New("menu has no items")
	}
	model := menuModel{title: title, items: items}
	program := tea.NewProgram(model, tea.WithInput(input), tea.WithOutput(output), tea.WithContext(ctx))
	finalModel, err := program.Run()
	if err != nil {
		return "", err
	}
	menu := finalModel.(menuModel)
	if menu.cancelled {
		return "", context.Canceled
	}
	return menu.items[menu.cursor].Value, nil
}

type menuModel struct {
	title     string
	items     []MenuItem
	cursor    int
	cancelled bool
}

func (m menuModel) Init() tea.Cmd { return nil }

func (m menuModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m menuModel) View() string {
	var builder strings.Builder
	builder.WriteString(accent.Render(m.title))
	builder.WriteString("\n\n")
	for index, item := range m.items {
		cursor := "  "
		title := item.Title
		if index == m.cursor {
			cursor = accent.Render("› ")
			title = accent.Render(title)
		}
		builder.WriteString(cursor + title + "\n")
		if item.Description != "" {
			builder.WriteString("    " + muted.Render(item.Description) + "\n")
		}
	}
	builder.WriteString("\n" + muted.Render("↑/↓ 选择 · Enter 确认 · q 退出"))
	return builder.String()
}
