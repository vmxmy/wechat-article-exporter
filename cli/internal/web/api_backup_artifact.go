package web

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

const backupArtifactAttachmentName = "wechat-article-backup.zip"

// backupArtifactRead streams a local backup via a short-lived, one-shot
// opaque capability. The archive location remains inside the maintenance
// adapter; neither this handler nor its response can reveal a host path.
func (server *Server) backupArtifactRead(writer http.ResponseWriter, request *http.Request) bool {
	const prefix = "/api/v1/maintenance/backups/"
	handle, found := strings.CutPrefix(request.URL.Path, prefix)
	if !found {
		return false
	}
	if handle == "" || strings.Contains(handle, "/") || len(request.URL.Query()) != 0 || !validMaintenanceToken(handle) {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "backup artifact handle is invalid")
		return true
	}
	service := server.storageMaintenanceService(writer)
	if service == nil {
		return true
	}
	archive, err := service.OpenBackup(request.Context(), handle)
	if err != nil {
		server.backupArtifactOpenError(writer, err)
		return true
	}
	defer archive.Close()
	writer.Header().Set("Content-Type", "application/zip")
	writer.Header().Set("Content-Disposition", "attachment; filename="+mimeFilename(backupArtifactAttachmentName))
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_, _ = io.Copy(writer, archive)
	return true
}

func (server *Server) backupArtifactOpenError(writer http.ResponseWriter, err error) {
	if errors.Is(err, application.ErrUnavailable) {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace backup artifact capability is not available")
		return
	}
	server.apiError(writer, http.StatusNotFound, "not_found", "backup artifact was not found")
}
