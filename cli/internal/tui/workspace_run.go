package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

var ErrNonInteractive = errors.New("interactive workspace requires TTY stdin and stdout; use a command or explicitly force TUI mode")

func ShouldStartWorkspace(input, output interface{}, force bool) bool {
	if force {
		return true
	}
	inputReader, inputOK := input.(interface{ Read([]byte) (int, error) })
	outputWriter, outputOK := output.(interface{ Write([]byte) (int, error) })
	return inputOK && outputOK && IsInteractive(inputReader, outputWriter)
}

func RunWorkspace(ctx context.Context, options WorkspaceOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	options.Context = ctx
	if !ShouldStartWorkspace(options.Input, options.Output, options.Force) {
		return ErrNonInteractive
	}
	programOptions := []tea.ProgramOption{tea.WithContext(ctx)}
	if options.Input != nil {
		programOptions = append(programOptions, tea.WithInput(options.Input))
	}
	if options.Output != nil {
		programOptions = append(programOptions, tea.WithOutput(options.Output))
	}
	_, err := tea.NewProgram(NewWorkspace(options), programOptions...).Run()
	return err
}
