package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

const (
	candidateStdoutLimit = 16 << 20
	candidateStderrLimit = 2 << 20
)

type candidateCommandResult struct {
	Envelope commandEnvelope
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type candidateRunner struct {
	binary string
	env    []string
}

func (runner candidateRunner) command(arguments ...string) *exec.Cmd {
	command := exec.Command(runner.binary, arguments...)
	command.Env = candidateEnvironment(runner.env)
	return command
}

func (runner candidateRunner) runJSON(ctx context.Context, arguments ...string) (candidateCommandResult, error) {
	command := runner.command(arguments...)
	stdout := newBoundedBuffer(candidateStdoutLimit)
	stderr := newBoundedBuffer(candidateStderrLimit)
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := runCandidateProcess(ctx, command)
	exitCode := candidateExitCode(runErr)
	result := candidateCommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode}
	var outputErr error
	if stdout.Overflowed() {
		outputErr = fmt.Errorf("candidate stdout exceeded %d bytes", candidateStdoutLimit)
	}
	if stderr.Overflowed() {
		outputErr = errors.Join(outputErr, fmt.Errorf("candidate stderr exceeded %d bytes", candidateStderrLimit))
	}
	decodeErr := decodeSingleCommandEnvelope(result.Stdout, &result.Envelope)
	if runErr != nil {
		combined := errors.Join(
			fmt.Errorf("candidate %q exited %d: %w", strings.Join(arguments, " "), exitCode, runErr),
			outputErr,
			wrapDecodeError(arguments, decodeErr),
		)
		if contextErr := ctx.Err(); contextErr != nil {
			return result, &candidateContextError{contextErr: contextErr, waitErr: combined}
		}
		return result, combined
	}
	if outputErr != nil || decodeErr != nil {
		return result, errors.Join(outputErr, wrapDecodeError(arguments, decodeErr))
	}
	if !result.Envelope.Success {
		return result, fmt.Errorf("candidate %q returned success=false", strings.Join(arguments, " "))
	}
	return result, nil
}

func (runner candidateRunner) runStdio(ctx context.Context, input string, arguments ...string) ([]byte, []byte, int, error) {
	command := runner.command(arguments...)
	command.Stdin = strings.NewReader(input)
	stdout := newBoundedBuffer(candidateStdoutLimit)
	stderr := newBoundedBuffer(candidateStderrLimit)
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := runCandidateProcess(ctx, command)
	if stdout.Overflowed() {
		runErr = errors.Join(runErr, fmt.Errorf("candidate stdout exceeded %d bytes", candidateStdoutLimit))
	}
	if stderr.Overflowed() {
		runErr = errors.Join(runErr, fmt.Errorf("candidate stderr exceeded %d bytes", candidateStderrLimit))
	}
	return stdout.Bytes(), stderr.Bytes(), candidateExitCode(runErr), runErr
}

func candidateExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

func decodeSingleCommandEnvelope(body []byte, target *commandEnvelope) error {
	if len(body) > candidateStdoutLimit {
		return fmt.Errorf("stdout exceeds %d bytes", candidateStdoutLimit)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("stdout contains multiple JSON values")
		}
		return err
	}
	return nil
}

func wrapDecodeError(arguments []string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("decode candidate %q JSON: %w", strings.Join(arguments, " "), err)
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func newBoundedBuffer(limit int) boundedBuffer { return boundedBuffer{limit: limit} }

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = buffer.buffer.Write(value)
	}
	if original > remaining {
		buffer.overflow = true
	}
	return original, nil
}

func (buffer *boundedBuffer) Bytes() []byte { return append([]byte(nil), buffer.buffer.Bytes()...) }
func (buffer *boundedBuffer) Overflowed() bool {
	return buffer.overflow
}

func decodeEnvelopeData[T any](envelope commandEnvelope) (T, error) {
	var value T
	if len(envelope.Data) == 0 {
		return value, errors.New("command envelope has no data")
	}
	if err := json.Unmarshal(envelope.Data, &value); err != nil {
		return value, err
	}
	return value, nil
}
