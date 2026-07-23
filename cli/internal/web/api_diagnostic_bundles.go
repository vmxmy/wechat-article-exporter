package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

const diagnosticBundleAttachmentName = "wechat-article-diagnostics.zip"

// diagnosticBundleRead streams a private, redacted ZIP using an opaque
// capability. The handler never sees an archive path or staging reference.
func (server *Server) diagnosticBundleRead(writer http.ResponseWriter, request *http.Request) bool {
	const prefix = "/api/v1/maintenance/diagnostic-bundles/"
	handle, found := strings.CutPrefix(request.URL.Path, prefix)
	if !found {
		return false
	}
	if handle == "" || strings.Contains(handle, "/") || len(request.URL.Query()) != 0 {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "diagnostic bundle handle is invalid")
		return true
	}
	service := server.diagnosticBundleService(writer)
	if service == nil {
		return true
	}
	archive, receipt, err := service.Open(request.Context(), handle)
	if err != nil {
		server.diagnosticBundleOpenError(writer, err)
		return true
	}
	defer archive.Close()
	writer.Header().Set("Content-Type", "application/zip")
	writer.Header().Set("Content-Disposition", "attachment; filename="+mimeFilename(diagnosticBundleAttachmentName))
	writer.Header().Set("Content-Length", fmt.Sprint(receipt.SizeBytes))
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_, _ = io.Copy(writer, archive)
	return true
}

// diagnosticBundleControl deliberately supports only creation. Restoration or
// browser uploads are not part of this capability.
func (server *Server) diagnosticBundleControl(writer http.ResponseWriter, request *http.Request) bool {
	if request.URL.Path != "/api/v1/maintenance/diagnostic-bundles" {
		return false
	}
	if !server.apiMutation(writer, request, http.MethodPost) {
		return true
	}
	var input struct{}
	if err := decodeControl(request, &input); err != nil {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "diagnostic bundle request is invalid")
		return true
	}
	service := server.diagnosticBundleService(writer)
	if service == nil {
		return true
	}
	receipt, err := service.Create(request.Context())
	if err != nil {
		server.diagnosticBundleCreateError(writer, err)
		return true
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeAPI(writer, http.StatusCreated, receipt)
	return true
}

func (server *Server) diagnosticBundleService(writer http.ResponseWriter) *application.DiagnosticBundleService {
	if server.diagnosticBundles == nil {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace diagnostic bundle capability is not available")
		return nil
	}
	return server.diagnosticBundles
}

func (server *Server) diagnosticBundleCreateError(writer http.ResponseWriter, err error) {
	if errors.Is(err, application.ErrUnavailable) {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace diagnostic bundle capability is not available")
		return
	}
	server.apiError(writer, http.StatusInternalServerError, "internal", "workspace operation failed")
}

func (server *Server) diagnosticBundleOpenError(writer http.ResponseWriter, err error) {
	if errors.Is(err, application.ErrUnavailable) {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace diagnostic bundle capability is not available")
		return
	}
	server.apiError(writer, http.StatusNotFound, "not_found", "diagnostic bundle was not found")
}
