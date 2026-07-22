package input

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const maxInputBytes = 16 << 20

type Options struct {
	Inline    string
	File      string
	Stdin     bool
	InlineSet bool
	FileSet   bool
}

func Load(options Options, stdin io.Reader) (map[string]any, error) {
	sourceCount := 0
	if options.InlineSet || options.Inline != "" {
		sourceCount++
	}
	if options.FileSet || options.File != "" {
		sourceCount++
	}
	if options.Stdin {
		sourceCount++
	}
	if sourceCount > 1 {
		return nil, fmt.Errorf("use exactly one JSON input source: --input, --file, or --stdin")
	}
	if options.InlineSet || options.Inline != "" {
		return ParseObject([]byte(options.Inline), "--input")
	}
	if options.FileSet || options.File != "" {
		file, err := os.Open(options.File)
		if err != nil {
			return nil, fmt.Errorf("read input file: %w", err)
		}
		defer file.Close()
		data, err := readLimited(file)
		if err != nil {
			return nil, fmt.Errorf("read input file: %w", err)
		}
		return ParseObject(data, "file "+options.File)
	}
	if options.Stdin {
		data, err := readLimited(stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return ParseObject(data, "stdin")
	}
	return map[string]any{}, nil
}

func ParseObject(data []byte, source string) (map[string]any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid JSON from %s", source)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("invalid JSON from %s: trailing data", source)
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf("JSON from %s must be an object", source)
	}
	return object, nil
}

func readLimited(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxInputBytes {
		return nil, fmt.Errorf("JSON input exceeds %d bytes", maxInputBytes)
	}
	return data, nil
}
