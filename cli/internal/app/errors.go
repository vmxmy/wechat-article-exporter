package app

import (
	"errors"
	"strconv"
	"strings"
)

type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

func usage(message string) error { return &UsageError{Message: message} }

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var usageError *UsageError
	if errors.As(err, &usageError) {
		return 2
	}
	return 1
}

func JSONRequested(args []string) bool {
	requested := false
	for _, argument := range args {
		if argument == "--" {
			break
		}
		if argument == "--json" {
			requested = true
			continue
		}
		if value, found := strings.CutPrefix(argument, "--json="); found {
			if parsed, err := strconv.ParseBool(value); err == nil {
				requested = parsed
			}
		}
	}
	return requested
}
