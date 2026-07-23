package main

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"
)

func isolatedRootComponents(relative string) ([]string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" || isForeignVolumePath(relative) {
		return nil, errors.New("directory path must be a relative path within the isolated root")
	}
	components := strings.FieldsFunc(relative, func(value rune) bool { return value == '/' || value == '\\' })
	if len(components) == 0 {
		return nil, errors.New("directory path must name a child of the isolated root")
	}
	if strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, `\`) {
		return nil, errors.New("directory path must be relative to the isolated root")
	}
	for _, component := range components {
		if component == "." || component == ".." || component == "" {
			return nil, errors.New("directory path must not contain traversal components")
		}
	}
	return components, nil
}

func isForeignVolumePath(path string) bool {
	return len(path) >= 2 && path[1] == ':' && unicode.IsLetter(rune(path[0]))
}
