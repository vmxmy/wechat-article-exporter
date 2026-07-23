package web

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

const (
	defaultExportDirectoryConfirmation = "authorize-default-export-directory"
	maximumExportVerifications         = 4
)

// exportRead owns the authenticated, read-only export routes. It deliberately
// receives only WorkspaceExportService DTOs, never an export root or a file
// descriptor. Artifact bytes and desktop opening are therefore unavailable
// until the application facade grows a separately-authorized streaming seam.
func (server *Server) exportRead(writer http.ResponseWriter, request *http.Request) bool {
	if request.URL.Path == "/api/v1/export-directories" {
		server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return true
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v1/exports/")
	if path == request.URL.Path {
		return false
	}
	if id, suffix, found := strings.Cut(path, "/"); found {
		switch suffix {
		case "manifest":
			server.exportManifest(writer, request, id)
		case "verify":
			server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		case "artifact", "open":
			server.exportArtifactUnavailable(writer, request)
		default:
			server.apiError(writer, http.StatusNotFound, "not_found", "workspace resource was not found")
		}
		return true
	}
	if strings.TrimSpace(path) == "" {
		return false
	}
	server.exportDetail(writer, request, path)
	return true
}

func (server *Server) exportControl(writer http.ResponseWriter, request *http.Request) bool {
	path := strings.TrimPrefix(request.URL.Path, "/api/v1/exports/")
	if path == request.URL.Path {
		return false
	}
	id, action, found := strings.Cut(path, "/")
	if !found || strings.TrimSpace(id) == "" {
		return false
	}
	switch action {
	case "verify":
		server.exportVerify(writer, request, id)
	case "artifact", "open":
		server.exportArtifactUnavailable(writer, request)
	default:
		return false
	}
	return true
}

func (server *Server) exportService(writer http.ResponseWriter) application.WorkspaceExportService {
	if server.exports == nil {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace export capability is not available")
		return nil
	}
	return server.exports
}

func (server *Server) exportDirectoryAuthorize(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		Confirmation string `json:"confirm"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	if input.Confirmation != defaultExportDirectoryConfirmation {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "export directory authorization confirmation is invalid")
		return
	}
	service := server.exportService(writer)
	if service == nil {
		return
	}
	directory, err := service.DefaultExportDirectory(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusCreated, directory)
}

func (server *Server) exportDirectoryCreate(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		ParentToken  application.WorkspaceDirectoryHandle `json:"parentToken"`
		Name         string                               `json:"name"`
		Confirmation string                               `json:"confirm"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	if input.Confirmation != exportDirectoryCreateConfirmation(input.ParentToken, input.Name) {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "export directory creation confirmation is invalid")
		return
	}
	if !validExportDirectoryName(input.Name) {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "export directory name must be a single path component")
		return
	}
	service := server.exportService(writer)
	if service == nil {
		return
	}
	directory, err := service.CreateExportDirectory(request.Context(), application.WorkspaceCreateExportDirectoryRequest{ParentToken: input.ParentToken, Name: input.Name})
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusCreated, directory)
}

func (server *Server) exportStart(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		application.WorkspaceStartExportRequest
		Confirmation string `json:"confirm"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	if input.Confirmation != exportStartConfirmation(input.DirectoryToken) {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "export start confirmation is invalid")
		return
	}
	if !validDirectoryToken(input.DirectoryToken) || !validExportSubdirectory(input.Subdirectory) {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "export directory capability or subdirectory is invalid")
		return
	}
	service := server.exportService(writer)
	if service == nil {
		return
	}
	job, err := service.StartExport(request.Context(), input.WorkspaceStartExportRequest)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	// The facade queues persistent work. Never wait for execution in a browser
	// handler, and keep the response to the durable job identifier only.
	writeAPI(writer, http.StatusAccepted, map[string]string{"jobId": string(job.ID)})
}

func (server *Server) exportsList(writer http.ResponseWriter, request *http.Request) {
	service := server.exportService(writer)
	if service == nil {
		return
	}
	page, err := parsePage(request)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	value, err := service.ExportRecords(request.Context(), page)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writePage(writer, http.StatusOK, value)
}

func (server *Server) exportDetail(writer http.ResponseWriter, request *http.Request, id string) {
	service := server.exportService(writer)
	if service == nil {
		return
	}
	value, err := service.ExportManifest(request.Context(), id)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) exportManifest(writer http.ResponseWriter, request *http.Request, id string) {
	server.exportDetail(writer, request, id)
}

func (server *Server) exportVerify(writer http.ResponseWriter, request *http.Request, id string) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		Confirmation string `json:"confirm"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	if input.Confirmation != exportVerifyConfirmation(id) {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "export verification confirmation is invalid")
		return
	}
	if !server.allowExportVerification() {
		writer.Header().Set("Retry-After", "60")
		server.apiError(writer, http.StatusTooManyRequests, "rate_limited", "export verification rate limit exceeded")
		return
	}
	service := server.exportService(writer)
	if service == nil {
		return
	}
	value, err := service.VerifyExport(request.Context(), id)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) exportArtifactUnavailable(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return
	}
	server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "safe artifact streaming and desktop opening are not available")
}

func exportDirectoryCreateConfirmation(parent application.WorkspaceDirectoryHandle, name string) string {
	return "create-export-directory:" + string(parent) + ":" + strings.TrimSpace(name)
}

func exportStartConfirmation(token application.WorkspaceDirectoryHandle) string {
	return "start-export:" + string(token)
}

func exportVerifyConfirmation(id string) string {
	return "verify-export:" + strings.TrimSpace(id)
}

func validDirectoryToken(token application.WorkspaceDirectoryHandle) bool {
	value := string(token)
	return value != "" && !filepath.IsAbs(value) && filepath.VolumeName(value) == "" && !strings.ContainsAny(value, `/\\`)
}

func validExportDirectoryName(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`)
}

func validExportSubdirectory(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." {
		return true
	}
	if strings.Contains(value, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	return !filepath.IsAbs(value) && filepath.VolumeName(value) == "" && clean == filepath.FromSlash(value) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func (server *Server) allowExportVerification() bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	now := server.now()
	if now.Sub(server.exportVerificationWindow) >= time.Minute {
		server.exportVerificationWindow, server.exportVerifications = now, 0
	}
	if server.exportVerifications >= maximumExportVerifications {
		return false
	}
	server.exportVerifications++
	return true
}
