package exporter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"golang.org/x/text/unicode/norm"
)

const (
	DefaultNamingTemplate = "${title}"
	DefaultMaximumBytes   = 180
	maximumFilenameBytes  = 255
)

var ErrInvalidFilename = errors.New("invalid export filename")

type FilenamePlatform string

const (
	PlatformPortable FilenamePlatform = "portable"
	PlatformWindows  FilenamePlatform = "windows"
	PlatformMacOS    FilenamePlatform = "macos"
	PlatformLinux    FilenamePlatform = "linux"
)

type NamingOptions struct {
	Template     string
	Extension    string
	MaximumBytes int
	Platform     FilenamePlatform
}

type NamingData struct {
	ArticleID   domain.ArticleID
	AccountID   domain.AccountID
	Account     string
	Title       string
	AID         string
	Author      string
	PublishedAt time.Time
	Index       int
}

type PlannedName struct {
	ArticleID domain.ArticleID `json:"articleId"`
	Path      string           `json:"path"`
}

func RenderFilename(options NamingOptions, data NamingData) (string, error) {
	normalized, err := normalizeNamingOptions(options)
	if err != nil {
		return "", err
	}
	values := namingValues(data)
	rendered := renderTemplate(normalized.Template, values)
	base := sanitizeFilename(rendered, normalized.Platform)
	if base == "" {
		base = stableFallbackName(data.ArticleID)
	}
	extension := sanitizeExtension(normalized.Extension)
	base = reserveExtensionBytes(base, extension, normalized.MaximumBytes)
	base = avoidReservedName(base, normalized.Platform)
	if base == "" {
		base = reserveExtensionBytes(stableFallbackName(data.ArticleID), extension, normalized.MaximumBytes)
	}
	name := base + extension
	if err := validateFilename(name, normalized.Platform, normalized.MaximumBytes); err != nil {
		return "", err
	}
	return name, nil
}

func PlanFilenames(options NamingOptions, items []NamingData) ([]PlannedName, error) {
	type candidate struct {
		index int
		id    domain.ArticleID
		name  string
		key   string
	}
	candidates := make([]candidate, len(items))
	groups := make(map[string][]int, len(items))
	for index, item := range items {
		if strings.TrimSpace(string(item.ArticleID)) == "" {
			return nil, fmt.Errorf("article %d has an empty ID: %w", index, ErrInvalidFilename)
		}
		name, err := RenderFilename(options, item)
		if err != nil {
			return nil, fmt.Errorf("article %s: %w", item.ArticleID, err)
		}
		key := collisionKey(name, options.Platform)
		candidates[index] = candidate{index: index, id: item.ArticleID, name: name, key: key}
		groups[key] = append(groups[key], index)
	}

	used := make(map[string]domain.ArticleID, len(items))
	for _, indexes := range groups {
		sort.Slice(indexes, func(i, j int) bool {
			left, right := candidates[indexes[i]], candidates[indexes[j]]
			if left.id == right.id {
				return left.index < right.index
			}
			return left.id < right.id
		})
		for ordinal, candidateIndex := range indexes {
			item := &candidates[candidateIndex]
			name := item.name
			if ordinal > 0 {
				name = withCollisionSuffix(name, item.id, options)
			}
			key := collisionKey(name, options.Platform)
			for attempt := 2; used[key] != ""; attempt++ {
				name = withCollisionSuffix(name, domain.ArticleID(string(item.id)+"-"+strconv.Itoa(attempt)), options)
				key = collisionKey(name, options.Platform)
			}
			used[key] = item.id
			item.name = name
		}
	}

	plans := make([]PlannedName, len(candidates))
	for index, item := range candidates {
		plans[index] = PlannedName{ArticleID: item.id, Path: item.name}
	}
	return plans, nil
}

func normalizeNamingOptions(options NamingOptions) (NamingOptions, error) {
	if strings.TrimSpace(options.Template) == "" {
		options.Template = DefaultNamingTemplate
	}
	if options.MaximumBytes == 0 {
		options.MaximumBytes = DefaultMaximumBytes
	}
	if options.MaximumBytes < 1 || options.MaximumBytes > maximumFilenameBytes {
		return NamingOptions{}, fmt.Errorf("maximum filename length must be between 1 and %d bytes: %w",
			maximumFilenameBytes, ErrInvalidFilename)
	}
	if options.Platform == "" {
		options.Platform = PlatformPortable
	}
	switch options.Platform {
	case PlatformPortable, PlatformWindows, PlatformMacOS, PlatformLinux:
	default:
		return NamingOptions{}, fmt.Errorf("unsupported filename platform %q: %w", options.Platform, ErrInvalidFilename)
	}
	return options, nil
}

func namingValues(data NamingData) map[string]string {
	published := data.PublishedAt.UTC()
	return map[string]string{
		"account":   data.Account,
		"accountId": string(data.AccountID),
		"title":     data.Title,
		"aid":       data.AID,
		"articleId": string(data.ArticleID),
		"author":    data.Author,
		"YYYY":      published.Format("2006"),
		"MM":        published.Format("01"),
		"DD":        published.Format("02"),
		"HH":        published.Format("15"),
		"mm":        published.Format("04"),
		"index":     strconv.Itoa(data.Index),
	}
}

func renderTemplate(template string, values map[string]string) string {
	var builder strings.Builder
	for len(template) > 0 {
		start := strings.Index(template, "${")
		if start < 0 {
			builder.WriteString(template)
			break
		}
		builder.WriteString(template[:start])
		end := strings.IndexByte(template[start+2:], '}')
		if end < 0 {
			builder.WriteString(template[start:])
			break
		}
		end += start + 2
		key := template[start+2 : end]
		if value, exists := values[key]; exists {
			builder.WriteString(value)
		} else {
			builder.WriteString("_")
		}
		template = template[end+1:]
	}
	return builder.String()
}

func sanitizeFilename(value string, platform FilenamePlatform) string {
	value = norm.NFC.String(value)
	var builder strings.Builder
	lastReplacement := false
	for _, character := range value {
		invalid := character == 0 || unicode.IsControl(character) || character == '/' || character == '\\'
		if platform == PlatformWindows || platform == PlatformPortable {
			invalid = invalid || strings.ContainsRune(`<>:"|?*`, character)
		} else if platform == PlatformMacOS {
			invalid = invalid || character == ':'
		}
		if invalid {
			if !lastReplacement && builder.Len() > 0 {
				builder.WriteByte('_')
				lastReplacement = true
			}
			continue
		}
		builder.WriteRune(character)
		lastReplacement = false
	}
	result := strings.TrimSpace(builder.String())
	result = strings.Trim(result, ".")
	result = strings.TrimSpace(result)
	for strings.Contains(result, "..") {
		result = strings.ReplaceAll(result, "..", "_")
	}
	if platform == PlatformWindows || platform == PlatformPortable {
		result = strings.TrimRight(result, ". ")
	}
	if result == "." || result == ".." {
		return ""
	}
	return result
}

func sanitizeExtension(extension string) string {
	extension = strings.TrimSpace(norm.NFC.String(extension))
	if extension == "" {
		return ""
	}
	extension = strings.TrimLeft(extension, ".")
	var builder strings.Builder
	for _, character := range extension {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' {
			builder.WriteRune(character)
		}
	}
	if builder.Len() == 0 {
		return ""
	}
	return "." + builder.String()
}

func reserveExtensionBytes(base, extension string, maximum int) string {
	limit := maximum - len(extension)
	if limit < 1 {
		return ""
	}
	return truncateUTF8(strings.TrimRight(base, ". "), limit)
}

func truncateUTF8(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	for maximum > 0 && !utf8.RuneStart(value[maximum]) {
		maximum--
	}
	return strings.TrimRight(value[:maximum], ". ")
}

func avoidReservedName(base string, platform FilenamePlatform) string {
	if platform != PlatformWindows && platform != PlatformPortable {
		return base
	}
	device := base
	if dot := strings.IndexByte(device, '.'); dot >= 0 {
		device = device[:dot]
	}
	device = strings.ToUpper(strings.TrimRight(device, ". "))
	if isWindowsReservedDevice(device) {
		return "_" + base
	}
	return base
}

func isWindowsReservedDevice(value string) bool {
	switch value {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$":
		return true
	}
	if len(value) == 4 && (strings.HasPrefix(value, "COM") || strings.HasPrefix(value, "LPT")) {
		return value[3] >= '1' && value[3] <= '9'
	}
	return false
}

func validateFilename(name string, platform FilenamePlatform, maximum int) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("filename is empty or reserved: %w", ErrInvalidFilename)
	}
	if len(name) > maximum || len(name) > maximumFilenameBytes {
		return fmt.Errorf("filename exceeds maximum length: %w", ErrInvalidFilename)
	}
	if !utf8.ValidString(name) || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("filename contains a path component: %w", ErrInvalidFilename)
	}
	if sanitizeFilename(name, platform) == "" {
		return fmt.Errorf("filename has no valid characters: %w", ErrInvalidFilename)
	}
	return nil
}

func stableFallbackName(articleID domain.ArticleID) string {
	digest := sha256.Sum256([]byte(articleID))
	return "article-" + hex.EncodeToString(digest[:6])
}

func collisionKey(name string, platform FilenamePlatform) string {
	name = norm.NFC.String(name)
	if platform == PlatformWindows || platform == PlatformMacOS || platform == PlatformPortable {
		return strings.ToLower(name)
	}
	return name
}

func withCollisionSuffix(name string, articleID domain.ArticleID, options NamingOptions) string {
	normalized, err := normalizeNamingOptions(options)
	if err != nil {
		return name
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	digest := sha256.Sum256([]byte(articleID))
	suffix := "--" + hex.EncodeToString(digest[:5])
	base = truncateUTF8(base, normalized.MaximumBytes-len(extension)-len(suffix))
	base = strings.TrimRight(base, ". ")
	if base == "" {
		base = "article"
	}
	return base + suffix + extension
}

// AddCollisionSuffix returns a platform-safe deterministic alternative for an
// already occupied planned name while preserving its extension and byte cap.
func AddCollisionSuffix(name string, articleID domain.ArticleID, options NamingOptions) (string, error) {
	normalized, err := normalizeNamingOptions(options)
	if err != nil {
		return "", err
	}
	name = withCollisionSuffix(name, articleID, normalized)
	if err := validateFilename(name, normalized.Platform, normalized.MaximumBytes); err != nil {
		return "", err
	}
	return name, nil
}
