package web

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

const restoreArchiveFormField = "archive"

func (server *Server) restoreControl(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method != http.MethodPost {
		return false
	}
	switch request.URL.Path {
	case "/api/v1/maintenance/restore/upload":
		server.restoreUpload(writer, request)
	case "/api/v1/maintenance/restore/prepare":
		server.restorePrepare(writer, request)
	case "/api/v1/maintenance/restore/commit":
		server.restoreCommit(writer, request)
	default:
		return false
	}
	return true
}

func (server *Server) restoreUpload(writer http.ResponseWriter, request *http.Request) {
	if !server.restoreMutation(writer, request, false) {
		return
	}
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || strings.TrimSpace(parameters["boundary"]) == "" {
		server.invalidMaintenanceInput(writer)
		return
	}
	if request.ContentLength > maxRestoreUploadBytes {
		server.apiError(writer, http.StatusRequestEntityTooLarge, "invalid_argument", "restore upload is too large")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRestoreUploadBytes)
	reader, err := request.MultipartReader()
	if err != nil {
		server.invalidMaintenanceInput(writer)
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != restoreArchiveFormField || part.FileName() == "" {
		server.invalidMaintenanceInput(writer)
		return
	}
	defer part.Close()
	receipt, err := server.restore.Stage(request.Context(), part, multipartDeclaredSize(part.Header.Get("Content-Length")))
	if err != nil {
		server.restoreError(writer, err)
		return
	}
	next, nextErr := reader.NextPart()
	if nextErr != io.EOF || next != nil {
		if next != nil {
			_ = next.Close()
		}
		_ = server.restoreDiscard(receipt.Handle)
		server.invalidMaintenanceInput(writer)
		return
	}
	writeAPI(writer, http.StatusCreated, receipt)
}

func (server *Server) restorePrepare(writer http.ResponseWriter, request *http.Request) {
	if !server.restoreMutation(writer, request, true) {
		return
	}
	var input application.RestorePrepareRequest
	if err := decodeControl(request, &input); err != nil {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.restore.Prepare(request.Context(), input)
	if err != nil {
		server.restoreError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) restoreCommit(writer http.ResponseWriter, request *http.Request) {
	if !server.restoreMutation(writer, request, true) {
		return
	}
	var input application.RestoreCommitRequest
	if err := decodeControl(request, &input); err != nil {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.restore.Commit(request.Context(), input)
	if err != nil {
		server.restoreError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
	server.closeAfterRestore()
}

func (server *Server) restoreMutation(writer http.ResponseWriter, request *http.Request, jsonBody bool) bool {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return false
	}
	if _, ok := server.authorizeMutation(request); !ok {
		server.apiError(writer, http.StatusForbidden, "forbidden", "workspace mutation authorization is required")
		return false
	}
	if jsonBody && !server.validMutationShape(request) {
		server.invalidMaintenanceInput(writer)
		return false
	}
	if server.restore == nil {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace restore capability is not available")
		return false
	}
	return true
}

func (server *Server) restoreDiscard(handle application.UploadHandle) error {
	if server.restore == nil {
		return nil
	}
	// A malformed multipart body must not retain a partially accepted archive.
	return server.restore.Discard(context.Background(), handle)
}

func (server *Server) restoreError(writer http.ResponseWriter, err error) {
	if errors.Is(err, application.ErrUnavailable) {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace restore capability is not available")
		return
	}
	if errors.Is(err, application.ErrRestoreConfirmationRequired) {
		server.apiError(writer, http.StatusBadRequest, "confirmation_required", "restore confirmation is invalid or expired")
		return
	}
	server.apiError(writer, http.StatusBadRequest, "invalid_argument", "restore request is invalid")
}

func multipartDeclaredSize(value string) int64 {
	if value == "" {
		return -1
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size < 0 {
		return -1
	}
	return size
}
