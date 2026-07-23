package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var candidateHostEnvironmentAllowlist = map[string]struct{}{
	"PATH": {}, "TMPDIR": {}, "TMP": {}, "TEMP": {}, "LANG": {}, "LC_ALL": {}, "LC_CTYPE": {}, "TZ": {},
	"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {}, "DISPLAY": {}, "WAYLAND_DISPLAY": {}, "XDG_RUNTIME_DIR": {},
	"DBUS_SESSION_BUS_ADDRESS": {}, "SYSTEMROOT": {}, "WINDIR": {}, "COMSPEC": {}, "PATHEXT": {},
	"PROGRAMFILES": {}, "PROGRAMFILES(X86)": {}, "LOCALAPPDATA": {},
}

func candidateEnvironment(overrides []string) []string {
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
		home := filepath.Join(portableRoot, "clean-room-home")
		_ = os.MkdirAll(home, 0o700)
		for name, value := range map[string]string{
			"HOME": home, "USERPROFILE": home,
			"XDG_CONFIG_HOME": filepath.Join(home, "config"),
			"XDG_DATA_HOME":   filepath.Join(home, "data"),
			"XDG_CACHE_HOME":  filepath.Join(home, "cache"),
			"XDG_STATE_HOME":  filepath.Join(home, "state"),
		} {
			_ = os.MkdirAll(value, 0o700)
			values[environmentKey(name)] = name + "=" + value
		}
	}
	result := make([]string, 0, len(values))
	for _, entry := range values {
		result = append(result, entry)
	}
	sort.Slice(result, func(left, right int) bool {
		return strings.ToUpper(result[left]) < strings.ToUpper(result[right])
	})
	return result
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
