package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var candidateHostEnvironmentAllowlist = map[string]struct{}{
	"PATH": {}, "TMPDIR": {}, "TMP": {}, "TEMP": {}, "LANG": {}, "LC_ALL": {}, "LC_CTYPE": {}, "TZ": {},
	"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {}, "SYSTEMROOT": {}, "WINDIR": {}, "COMSPEC": {}, "PATHEXT": {},
	"PROGRAMFILES": {}, "PROGRAMFILES(X86)": {}, "LOCALAPPDATA": {},
}

func candidateEnvironment(overrides []string) ([]string, error) {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		name, value, ok := splitEnvironmentEntry(entry)
		if !ok || strings.HasPrefix(strings.ToUpper(name), "WECHAT_ARTICLE_") {
			continue
		}
		if _, allowed := candidateHostEnvironmentAllowlist[strings.ToUpper(name)]; allowed {
			values[environmentKey(name)] = name + "=" + value
		}
	}
	for _, entry := range overrides {
		name, value, ok := splitEnvironmentEntry(entry)
		if !ok {
			continue
		}
		values[environmentKey(name)] = name + "=" + value
	}
	portableRoot := environmentValue(values, "WECHAT_ARTICLE_PORTABLE_ROOT")
	if portableRoot != "" {
		var err error
		portableRoot, err = normalizedPortableRootPath(portableRoot)
		if err != nil {
			return nil, fmt.Errorf("validate portable root: %w", err)
		}
		values[environmentKey("WECHAT_ARTICLE_PORTABLE_ROOT")] = "WECHAT_ARTICLE_PORTABLE_ROOT=" + portableRoot
		isolatedRoot, err := openIsolatedRoot(portableRoot)
		if err != nil {
			return nil, fmt.Errorf("open isolated portable root: %w", err)
		}
		defer func() { _ = isolatedRoot.Close() }()
		home := filepath.Join(portableRoot, "clean-room-home")
		if err := isolatedRoot.EnsureDirectory("clean-room-home"); err != nil {
			return nil, fmt.Errorf("create isolated home: %w", err)
		}
		values[environmentKey("HOME")] = "HOME=" + home
		values[environmentKey("USERPROFILE")] = "USERPROFILE=" + home
		for _, value := range []struct {
			name     string
			path     string
			relative string
		}{
			{name: "XDG_CONFIG_HOME", path: filepath.Join(home, "config"), relative: filepath.Join("clean-room-home", "config")},
			{name: "XDG_DATA_HOME", path: filepath.Join(home, "data"), relative: filepath.Join("clean-room-home", "data")},
			{name: "XDG_CACHE_HOME", path: filepath.Join(home, "cache"), relative: filepath.Join("clean-room-home", "cache")},
			{name: "XDG_STATE_HOME", path: filepath.Join(home, "state"), relative: filepath.Join("clean-room-home", "state")},
		} {
			if err := isolatedRoot.EnsureDirectory(value.relative); err != nil {
				return nil, fmt.Errorf("create isolated %s: %w", value.name, err)
			}
			values[environmentKey(value.name)] = value.name + "=" + value.path
		}
	}
	result := make([]string, 0, len(values))
	for _, entry := range values {
		result = append(result, entry)
	}
	sort.Slice(result, func(left, right int) bool {
		return strings.ToUpper(result[left]) < strings.ToUpper(result[right])
	})
	return result, nil
}

func normalizedPortableRootPath(value string) (string, error) {
	if !filepath.IsAbs(value) {
		return "", errors.New("portable root must be an absolute non-root path")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	if filepath.Dir(absolute) == absolute {
		return "", errors.New("portable root must be an absolute non-root path")
	}
	volume := filepath.VolumeName(absolute)
	rest := strings.TrimPrefix(absolute, volume)
	current := volume + string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(rest, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			root := volume + string(filepath.Separator)
			if filepath.Dir(current) != root {
				return "", fmt.Errorf("path component %q is not a real directory", current)
			}
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", resolveErr
			}
			remaining := strings.TrimPrefix(strings.TrimPrefix(absolute, current), string(filepath.Separator))
			if remaining == "" {
				absolute = resolved
			} else {
				absolute = filepath.Join(resolved, remaining)
			}
			info, err = os.Stat(resolved)
			if err != nil {
				return "", err
			}
		}
		if !info.IsDir() {
			return "", fmt.Errorf("path component %q is not a real directory", current)
		}
	}
	return filepath.Clean(absolute), nil
}

func splitEnvironmentEntry(entry string) (string, string, bool) {
	index := strings.IndexByte(entry, '=')
	if index <= 0 {
		return "", "", false
	}
	return entry[:index], entry[index+1:], true
}

func environmentKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func environmentValue(values map[string]string, name string) string {
	entry := values[environmentKey(name)]
	_, value, _ := splitEnvironmentEntry(entry)
	return value
}
