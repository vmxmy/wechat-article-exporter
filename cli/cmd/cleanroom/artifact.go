package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	maximumArchiveMemberBytes = 256 << 20
	maximumArchiveTotalBytes  = 512 << 20
	maximumArchiveMembers     = 128
	maximumArchiveInputBytes  = 512 << 20
	maximumSBOMInputBytes     = 32 << 20
	maximumChecksumInputBytes = 8 << 20
)

type candidateArtifact struct {
	BinaryPath             string
	BuildInfoPath          string
	BinaryMember           string
	BuildInfoMember        string
	BinarySHA256           string
	BuildInfoSHA256        string
	ArchiveSHA256          string
	ChecksumManifestSHA256 string
	SBOMSHA256             string
	GOOS                   string
	GOARCH                 string
	CGOEnabled             string
	Module                 string
	Commit                 string
}

func inspectCandidateArtifact(options runOptions, destination string) (candidateArtifact, error) {
	info, err := os.Lstat(options.Archive)
	if err != nil || !info.Mode().IsRegular() {
		return candidateArtifact{}, errors.New("release archive is not a regular file")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return candidateArtifact{}, err
	}
	staging := filepath.Join(destination, ".inputs")
	if err := os.Mkdir(staging, 0o700); err != nil {
		return candidateArtifact{}, err
	}
	archiveSuffix := releaseArchiveSuffix(options.Archive)
	if archiveSuffix == "" {
		return candidateArtifact{}, errors.New("release archive must be .tar.gz or .zip")
	}
	stagedArchive, err := stageRegularFile(options.Archive, staging, "candidate-archive"+archiveSuffix, maximumArchiveInputBytes)
	if err != nil {
		return candidateArtifact{}, err
	}
	stagedSBOM, err := stageRegularFile(options.SBOM, staging, "candidate-sbom", maximumSBOMInputBytes)
	if err != nil {
		return candidateArtifact{}, err
	}
	stagedChecksums, err := stageRegularFile(options.ChecksumManifest, staging, "checksums.txt", maximumChecksumInputBytes)
	if err != nil {
		return candidateArtifact{}, err
	}
	archiveName := filepath.Base(options.Archive)
	sbomName := filepath.Base(options.SBOM)
	if archiveName == sbomName {
		return candidateArtifact{}, errors.New("release archive and SBOM must have distinct asset names")
	}
	sbomDigest, err := sha256File(stagedSBOM)
	if err != nil {
		return candidateArtifact{}, err
	}
	archiveDigest, err := sha256File(stagedArchive)
	if err != nil {
		return candidateArtifact{}, err
	}
	if err := verifyChecksumManifest(stagedChecksums, map[string]string{
		archiveName: archiveDigest,
		sbomName:    sbomDigest,
	}); err != nil {
		return candidateArtifact{}, err
	}
	members, err := extractReleaseArchive(stagedArchive, destination)
	if err != nil {
		return candidateArtifact{}, err
	}
	archiveBase := filepath.Base(options.Archive)
	rootName := archiveBase[:len(archiveBase)-len(archiveSuffix)]
	binaryName := "wechat-article"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryMember := filepath.ToSlash(filepath.Join(rootName, binaryName))
	buildInfoMember := filepath.ToSlash(filepath.Join(rootName, "build-info.txt"))
	for _, required := range []string{binaryMember, buildInfoMember, filepath.ToSlash(filepath.Join(rootName, "README.md")), filepath.ToSlash(filepath.Join(rootName, "LICENSE"))} {
		if _, ok := members[required]; !ok {
			return candidateArtifact{}, fmt.Errorf("archive lacks required member %q", required)
		}
	}
	binaryPath := members[binaryMember]
	buildInfoPath := members[buildInfoMember]
	binaryDigest, err := sha256File(binaryPath)
	if err != nil {
		return candidateArtifact{}, err
	}
	buildInfoDigest, err := sha256File(buildInfoPath)
	if err != nil {
		return candidateArtifact{}, err
	}
	metadata, err := parseBuildInfo(buildInfoPath)
	if err != nil {
		return candidateArtifact{}, err
	}
	expectedTarget := "_" + runtime.GOOS + "_" + runtime.GOARCH
	if !strings.HasSuffix(rootName, expectedTarget) {
		return candidateArtifact{}, fmt.Errorf("archive root %q does not match native target %s/%s", rootName, runtime.GOOS, runtime.GOARCH)
	}
	if metadata.GOOS != runtime.GOOS || metadata.GOARCH != runtime.GOARCH || metadata.CGOEnabled != "0" {
		return candidateArtifact{}, fmt.Errorf("build metadata target=%s/%s cgo=%s, want native %s/%s cgo=0",
			metadata.GOOS, metadata.GOARCH, metadata.CGOEnabled, runtime.GOOS, runtime.GOARCH)
	}
	if metadata.Module != "github.com/wechat-article/wechat-article-exporter/cli" {
		return candidateArtifact{}, fmt.Errorf("unexpected module %q", metadata.Module)
	}
	if metadata.Commit != "" && metadata.Commit != options.Commit {
		return candidateArtifact{}, fmt.Errorf("build commit %q does not match release commit %q", metadata.Commit, options.Commit)
	}
	if options.Binary != "" {
		suppliedDigest, digestErr := sha256File(options.Binary)
		if digestErr != nil || suppliedDigest != binaryDigest {
			return candidateArtifact{}, errors.New("supplied --binary is not the binary extracted from the archive")
		}
	}
	if options.BuildInfo != "" {
		suppliedDigest, digestErr := sha256File(options.BuildInfo)
		if digestErr != nil || suppliedDigest != buildInfoDigest {
			return candidateArtifact{}, errors.New("supplied --build-info is not the archive build-info.txt")
		}
	}
	if err := verifyArtifactSBOM(stagedSBOM, options.Version, runtime.GOOS, runtime.GOARCH); err != nil {
		return candidateArtifact{}, err
	}
	checksumDigest, err := sha256File(stagedChecksums)
	if err != nil {
		return candidateArtifact{}, err
	}
	return candidateArtifact{BinaryPath: binaryPath, BuildInfoPath: buildInfoPath, BinaryMember: binaryMember,
		BuildInfoMember: buildInfoMember, BinarySHA256: binaryDigest, BuildInfoSHA256: buildInfoDigest,
		ArchiveSHA256: archiveDigest, ChecksumManifestSHA256: checksumDigest, SBOMSHA256: sbomDigest,
		GOOS: metadata.GOOS, GOARCH: metadata.GOARCH, CGOEnabled: metadata.CGOEnabled, Module: metadata.Module, Commit: metadata.Commit}, nil
}

func stageRegularFile(source, destination, name string, limit int64) (target string, resultErr error) {
	input, info, err := openRegularFileNoFollow(source)
	if err != nil {
		return "", fmt.Errorf("open release input %q: %w", filepath.Base(source), err)
	}
	if info.Size() < 0 || info.Size() > limit {
		_ = input.Close()
		return "", fmt.Errorf("release input %q is not a bounded regular file", filepath.Base(source))
	}
	defer input.Close()
	target = filepath.Join(destination, name)
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer func() {
		if resultErr != nil {
			_ = os.Remove(target)
		}
	}()
	written, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if written > limit {
		copyErr = errors.Join(copyErr, errors.New("release input exceeded size limit"))
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		return "", err
	}
	return target, nil
}

func releaseArchiveSuffix(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(lower, ".zip"):
		return ".zip"
	default:
		return ""
	}
}

func extractReleaseArchive(path, destination string) (map[string]string, error) {
	if strings.HasSuffix(path, ".zip") || archiveLooksLikeZIP(path) {
		return extractZIPArchive(path, destination)
	}
	if strings.HasSuffix(path, ".tar.gz") || archiveLooksLikeGzip(path) {
		return extractTarGzipArchive(path, destination)
	}
	return nil, errors.New("release archive must be .tar.gz or .zip")
}

func archiveLooksLikeZIP(path string) bool  { return fileHasPrefix(path, []byte{'P', 'K', 3, 4}) }
func archiveLooksLikeGzip(path string) bool { return fileHasPrefix(path, []byte{0x1f, 0x8b}) }

func fileHasPrefix(path string, prefix []byte) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	body := make([]byte, len(prefix))
	_, err = io.ReadFull(file, body)
	return err == nil && string(body) == string(prefix)
}

func extractZIPArchive(path, destination string) (map[string]string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	members := make(map[string]string)
	var totalBytes uint64
	for _, member := range archive.File {
		if member.FileInfo().IsDir() {
			continue
		}
		if member.Mode()&os.ModeSymlink != 0 || !member.Mode().IsRegular() {
			return nil, fmt.Errorf("archive member %q is not a regular file", member.Name)
		}
		if member.UncompressedSize64 > maximumArchiveMemberBytes {
			return nil, fmt.Errorf("archive member %q is too large", member.Name)
		}
		if len(members) >= maximumArchiveMembers {
			return nil, fmt.Errorf("archive exceeds %d regular members", maximumArchiveMembers)
		}
		if member.UncompressedSize64 > uint64(maximumArchiveTotalBytes)-totalBytes {
			return nil, fmt.Errorf("archive exceeds %d uncompressed bytes", maximumArchiveTotalBytes)
		}
		totalBytes += member.UncompressedSize64
		name, target, err := safeArchiveTarget(destination, member.Name, members)
		if err != nil {
			return nil, err
		}
		reader, err := member.Open()
		if err != nil {
			return nil, err
		}
		if err := writeArchiveMember(target, member.Mode().Perm(), reader); err != nil {
			_ = reader.Close()
			return nil, err
		}
		if err := reader.Close(); err != nil {
			return nil, err
		}
		members[name] = target
	}
	return members, nil
}

func extractTarGzipArchive(path, destination string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	members := make(map[string]string)
	var totalBytes int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > maximumArchiveMemberBytes {
			return nil, fmt.Errorf("archive member %q is not a bounded regular file", header.Name)
		}
		if len(members) >= maximumArchiveMembers {
			return nil, fmt.Errorf("archive exceeds %d regular members", maximumArchiveMembers)
		}
		if header.Size > maximumArchiveTotalBytes-totalBytes {
			return nil, fmt.Errorf("archive exceeds %d uncompressed bytes", maximumArchiveTotalBytes)
		}
		totalBytes += header.Size
		name, target, err := safeArchiveTarget(destination, header.Name, members)
		if err != nil {
			return nil, err
		}
		if err := writeArchiveMember(target, os.FileMode(header.Mode).Perm(), io.LimitReader(reader, header.Size)); err != nil {
			return nil, err
		}
		members[name] = target
	}
	return members, nil
}

func safeArchiveTarget(destination, rawName string, members map[string]string) (string, string, error) {
	name := filepath.ToSlash(filepath.Clean(rawName))
	if rawName == "" || strings.HasPrefix(name, "/") || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, ":") {
		return "", "", fmt.Errorf("unsafe archive member %q", rawName)
	}
	if _, duplicate := members[name]; duplicate {
		return "", "", fmt.Errorf("duplicate archive member %q", name)
	}
	target := filepath.Join(destination, filepath.FromSlash(name))
	base, _ := filepath.Abs(destination)
	absolute, _ := filepath.Abs(target)
	relative, err := filepath.Rel(base, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("archive member %q escapes extraction root", rawName)
	}
	return name, absolute, nil
}

func writeArchiveMember(path string, mode os.FileMode, reader io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if mode&0o111 != 0 {
		mode = 0o700
	} else {
		mode = 0o600
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(reader, maximumArchiveMemberBytes+1))
	closeErr := file.Close()
	if written > maximumArchiveMemberBytes {
		copyErr = errors.Join(copyErr, errors.New("archive member exceeded size limit"))
	}
	return errors.Join(copyErr, closeErr)
}

type artifactBuildMetadata struct {
	Module     string
	GOOS       string
	GOARCH     string
	CGOEnabled string
	Commit     string
}

func parseBuildInfo(path string) (artifactBuildMetadata, error) {
	body, err := readBoundedRegularFile(path, 4<<20)
	if err != nil {
		return artifactBuildMetadata{}, err
	}
	metadata := artifactBuildMetadata{}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && fields[0] == "mod" {
			metadata.Module = fields[1]
		}
		if len(fields) >= 2 && fields[0] == "build" {
			key, value, ok := strings.Cut(fields[1], "=")
			if !ok {
				continue
			}
			switch key {
			case "GOOS":
				metadata.GOOS = value
			case "GOARCH":
				metadata.GOARCH = value
			case "CGO_ENABLED":
				metadata.CGOEnabled = value
			case "vcs.revision":
				metadata.Commit = value
			}
		}
	}
	if metadata.Module == "" || metadata.GOOS == "" || metadata.GOARCH == "" || metadata.CGOEnabled == "" {
		return artifactBuildMetadata{}, errors.New("build-info.txt lacks module or target metadata")
	}
	return metadata, nil
}

func verifyChecksumManifest(path string, expected map[string]string) error {
	body, err := readBoundedRegularFile(path, 8<<20)
	if err != nil {
		return err
	}
	entries := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !validSHA256(fields[0]) {
			if strings.TrimSpace(line) == "" {
				continue
			}
			return errors.New("release checksum manifest contains a malformed entry")
		}
		name := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
			return errors.New("release checksum manifest contains an unsafe asset name")
		}
		if _, duplicate := entries[name]; duplicate {
			return fmt.Errorf("release checksum manifest contains duplicate asset %q", name)
		}
		entries[name] = fields[0]
	}
	for name, digest := range expected {
		if entries[name] != digest {
			return fmt.Errorf("release checksum manifest does not contain the expected digest for %q", name)
		}
	}
	return nil
}

func verifyArtifactSBOM(path, version, goos, goarch string) error {
	body, err := readBoundedRegularFile(path, 32<<20)
	if err != nil {
		return err
	}
	var sbom struct {
		Metadata struct {
			Component struct {
				Name       string `json:"name"`
				Version    string `json:"version"`
				Properties []struct {
					Name  string `json:"name"`
					Value any    `json:"value"`
				} `json:"properties"`
			} `json:"component"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &sbom); err != nil {
		return err
	}
	if sbom.Metadata.Component.Name != "github.com/wechat-article/wechat-article-exporter/cli" || sbom.Metadata.Component.Version != version {
		return errors.New("SBOM component identity does not match the candidate release")
	}
	properties := make(map[string]string)
	for _, property := range sbom.Metadata.Component.Properties {
		properties[property.Name] = fmt.Sprint(property.Value)
	}
	for name, expected := range map[string]string{
		"cdx:gomod:build:env:GOOS": goos, "cdx:gomod:build:env:GOARCH": goarch, "cdx:gomod:build:env:CGO_ENABLED": "0",
	} {
		if properties[name] != expected {
			return fmt.Errorf("SBOM property %s=%q, want %q", name, properties[name], expected)
		}
	}
	return nil
}

func exactVersionOutput(output, version string) bool {
	if output == version || output == "wechat-article version "+version || output == "wechat-article "+version {
		return true
	}
	fields := strings.Fields(output)
	return len(fields) == 3 && fields[0] == "wechat-article" && fields[1] == "version" && fields[2] == version
}

func releaseTag(version string) string {
	if strings.HasPrefix(version, "dev-") {
		return ""
	}
	return "wechat-article-v" + version
}
