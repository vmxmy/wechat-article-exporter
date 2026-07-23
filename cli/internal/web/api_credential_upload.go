package web

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
)

const (
	credentialUploadFormField = "credential"
	maxCredentialUploadBytes  = credentials.MaximumCredentialBytes + 64<<10
)

// credentialUploadControl streams exactly one credential JSON file directly
// into the bounded domain parser. It never accepts a host path or returns a
// filename, staging capability, or credential material.
func (server *Server) credentialUploadControl(writer http.ResponseWriter, request *http.Request) bool {
	if request.URL.Path != "/api/v1/settings/credentials/upload" {
		return false
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return true
	}
	if _, ok := server.authorizeMutation(request); !ok {
		server.apiError(writer, http.StatusForbidden, "forbidden", "workspace mutation authorization is required")
		return true
	}
	if server.credentialUploads == nil {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace credential upload capability is not available")
		return true
	}
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || strings.TrimSpace(parameters["boundary"]) == "" || request.ContentLength > maxCredentialUploadBytes {
		server.invalidMaintenanceInput(writer)
		return true
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxCredentialUploadBytes)
	reader, err := request.MultipartReader()
	if err != nil {
		server.invalidMaintenanceInput(writer)
		return true
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != credentialUploadFormField || part.FileName() == "" {
		server.invalidMaintenanceInput(writer)
		return true
	}
	defer part.Close()
	// Retain at most one credential payload in request-scoped memory before
	// importing it. This permits rejection of a second multipart part before
	// any credential mutation occurs, while avoiding a filesystem staging path
	// and keeping the sensitive bytes out of responses and persistent state.
	payload, err := io.ReadAll(io.LimitReader(part, credentials.MaximumCredentialBytes+1))
	if err != nil || int64(len(payload)) > credentials.MaximumCredentialBytes {
		server.invalidMaintenanceInput(writer)
		return true
	}
	next, nextErr := reader.NextPart()
	if nextErr != io.EOF || next != nil {
		if next != nil {
			_ = next.Close()
		}
		server.invalidMaintenanceInput(writer)
		return true
	}
	metadata, err := server.credentialUploads.ImportJSON(request.Context(), bytes.NewReader(payload))
	if err != nil {
		if errors.Is(err, application.ErrUnavailable) {
			server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace credential upload capability is not available")
		} else {
			server.invalidMaintenanceInput(writer)
		}
		return true
	}
	writeAPI(writer, http.StatusCreated, metadata)
	return true
}
