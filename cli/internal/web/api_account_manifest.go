package web

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

const accountManifestFormField = "manifest"

func (server *Server) accountManifestRead(writer http.ResponseWriter, request *http.Request) bool {
	if request.URL.Path != "/api/v1/accounts/manifest" {
		return false
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return true
	}
	service := server.accountManifestService(writer)
	if service == nil {
		return true
	}
	manifest, err := service.Export(request.Context())
	if err != nil {
		server.accountManifestError(writer, err)
		return true
	}
	defer manifest.Close()
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Content-Disposition", "attachment; filename=wechat-article-accounts-manifest.json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_, _ = io.Copy(writer, manifest)
	return true
}

func (server *Server) accountManifestControl(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method != http.MethodPost {
		return false
	}
	switch request.URL.Path {
	case "/api/v1/accounts/manifest/upload":
		server.accountManifestUpload(writer, request)
	case "/api/v1/accounts/manifest/import":
		server.accountManifestImport(writer, request)
	default:
		return false
	}
	return true
}

func (server *Server) accountManifestUpload(writer http.ResponseWriter, request *http.Request) {
	if !server.accountManifestMutation(writer, request, false) {
		return
	}
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || strings.TrimSpace(parameters["boundary"]) == "" {
		server.invalidAccountManifestInput(writer)
		return
	}
	maximum := application.AccountManifestMaximumBytes + 64<<10
	if request.ContentLength > maximum {
		server.apiError(writer, http.StatusRequestEntityTooLarge, "invalid_argument", "account manifest upload is too large")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maximum)
	reader, err := request.MultipartReader()
	if err != nil {
		server.invalidAccountManifestInput(writer)
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != accountManifestFormField || part.FileName() == "" {
		server.invalidAccountManifestInput(writer)
		return
	}
	defer part.Close()
	service := server.accountManifestService(writer)
	if service == nil {
		return
	}
	receipt, err := service.Stage(request.Context(), part, multipartDeclaredSize(part.Header.Get("Content-Length")))
	if err != nil {
		server.accountManifestError(writer, err)
		return
	}
	next, nextErr := reader.NextPart()
	if nextErr != io.EOF || next != nil {
		if next != nil {
			_ = next.Close()
		}
		_ = service.Discard(request.Context(), receipt.Handle)
		server.invalidAccountManifestInput(writer)
		return
	}
	writeAPI(writer, http.StatusCreated, receipt)
}

func (server *Server) accountManifestImport(writer http.ResponseWriter, request *http.Request) {
	if !server.accountManifestMutation(writer, request, true) {
		return
	}
	var input struct {
		UploadHandle application.UploadHandle `json:"uploadHandle"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.invalidAccountManifestInput(writer)
		return
	}
	result, err := server.accountManifests.Import(request.Context(), input.UploadHandle)
	if err != nil {
		server.accountManifestError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, result)
}

func (server *Server) accountManifestMutation(writer http.ResponseWriter, request *http.Request, jsonBody bool) bool {
	if _, ok := server.authorizeMutation(request); !ok {
		server.apiError(writer, http.StatusForbidden, "forbidden", "workspace mutation authorization is required")
		return false
	}
	if jsonBody && !server.validMutationShape(request) {
		server.invalidAccountManifestInput(writer)
		return false
	}
	return server.accountManifestService(writer) != nil
}

func (server *Server) accountManifestService(writer http.ResponseWriter) *application.AccountManifestService {
	if server.accountManifests == nil {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace account manifest capability is not available")
		return nil
	}
	return server.accountManifests
}

func (server *Server) invalidAccountManifestInput(writer http.ResponseWriter) {
	server.apiError(writer, http.StatusBadRequest, "invalid_argument", "account manifest request is invalid")
}

func (server *Server) accountManifestError(writer http.ResponseWriter, err error) {
	if errors.Is(err, application.ErrUnavailable) {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace account manifest capability is not available")
		return
	}
	server.apiError(writer, http.StatusBadRequest, "invalid_argument", "account manifest request is invalid")
}
