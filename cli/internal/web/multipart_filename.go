package web

import (
	"mime"
	"mime/multipart"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maximumMultipartFilenameBytes = 255

// validMultipartFilename reports whether a browser-supplied filename is one
// portable filename. multipart.Part.FileName intentionally applies
// filepath.Base, so validate the raw Content-Disposition parameter as well to
// prevent normalized traversal or host-path forms from being accepted.
func validMultipartFilename(part *multipart.Part) bool {
	if part == nil {
		return false
	}
	_, parameters, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil {
		return false
	}
	raw, ok := parameters["filename"]
	if !ok || raw != part.FileName() {
		return false
	}
	return validMultipartFilenameValue(raw)
}

func validMultipartFilenameValue(name string) bool {
	if name == "" || len(name) > maximumMultipartFilenameBytes || !utf8.ValidString(name) {
		return false
	}
	if strings.ContainsAny(name, `/\\`) || strings.HasPrefix(name, `\\`) || windowsAbsolutePath(name) {
		return false
	}
	for _, value := range name {
		if unicode.IsControl(value) {
			return false
		}
	}
	return true
}

func windowsAbsolutePath(name string) bool {
	return len(name) >= 3 && isASCIILetter(name[0]) && name[1] == ':' && (name[2] == '/' || name[2] == '\\')
}

func isASCIILetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
