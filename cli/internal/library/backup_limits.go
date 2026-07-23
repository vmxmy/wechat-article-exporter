package library

import (
	"archive/zip"
	"fmt"
	"math"
	"path"
	"strings"
	"unicode/utf8"
)

// BackupArchiveLimits bounds work performed while inspecting or restoring a
// backup archive. Defaults are intentionally generous enough for existing CLI
// backups while preventing a malformed archive from consuming unbounded disk
// space or CPU.
type BackupArchiveLimits struct {
	MaximumEntries          int
	MaximumEntryBytes       int64
	MaximumTotalBytes       int64
	MaximumCompressionRatio int64
}

var DefaultBackupArchiveLimits = BackupArchiveLimits{
	MaximumEntries:          10_000,
	MaximumEntryBytes:       512 << 20,
	MaximumTotalBytes:       2 << 30,
	MaximumCompressionRatio: 100,
}

func (limits BackupArchiveLimits) normalized() BackupArchiveLimits {
	defaults := DefaultBackupArchiveLimits
	if limits.MaximumEntries <= 0 {
		limits.MaximumEntries = defaults.MaximumEntries
	}
	if limits.MaximumEntryBytes <= 0 {
		limits.MaximumEntryBytes = defaults.MaximumEntryBytes
	}
	if limits.MaximumTotalBytes <= 0 {
		limits.MaximumTotalBytes = defaults.MaximumTotalBytes
	}
	if limits.MaximumCompressionRatio <= 0 {
		limits.MaximumCompressionRatio = defaults.MaximumCompressionRatio
	}
	return limits
}

func validateBackupArchive(files []*zip.File, limits BackupArchiveLimits) []string {
	limits = limits.normalized()
	failures := make([]string, 0)
	if len(files) > limits.MaximumEntries {
		failures = append(failures, fmt.Sprintf("archive has %d entries; limit is %d", len(files), limits.MaximumEntries))
	}
	var total uint64
	for _, file := range files {
		if !validArchivePath(file.Name) {
			failures = append(failures, "unsafe archive path "+file.Name)
		}
		if file.UncompressedSize64 > uint64(limits.MaximumEntryBytes) {
			failures = append(failures, fmt.Sprintf("archive entry %s exceeds %d bytes", file.Name, limits.MaximumEntryBytes))
		}
		if math.MaxUint64-total < file.UncompressedSize64 {
			failures = append(failures, "archive total size overflows limit")
		} else {
			total += file.UncompressedSize64
		}
		if file.UncompressedSize64 > 0 {
			if file.CompressedSize64 == 0 || file.UncompressedSize64/file.CompressedSize64 > uint64(limits.MaximumCompressionRatio) ||
				(file.UncompressedSize64/file.CompressedSize64 == uint64(limits.MaximumCompressionRatio) && file.UncompressedSize64%file.CompressedSize64 != 0) {
				failures = append(failures, fmt.Sprintf("archive entry %s exceeds compression ratio limit", file.Name))
			}
		}
	}
	if total > uint64(limits.MaximumTotalBytes) {
		failures = append(failures, fmt.Sprintf("archive total uncompressed size exceeds %d bytes", limits.MaximumTotalBytes))
	}
	return failures
}

func validArchivePath(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, character := range part {
			if character < 0x20 || character == 0x7f {
				return false
			}
		}
	}
	return path.Clean(name) == name
}
