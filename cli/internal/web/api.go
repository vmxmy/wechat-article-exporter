package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

const apiVersion = "v1"

type apiEnvelope struct {
	APIVersion string `json:"apiVersion"`
	Data       any    `json:"data"`
}

type apiErrorEnvelope struct {
	APIVersion string   `json:"apiVersion"`
	Error      apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// api is the authenticated P0 read surface. Every route is GET-only and
// delegates to the shared WorkspaceReader facade, preserving the same bounded
// query behavior used by other presentation adapters.
func (server *Server) api(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return
	}
	if _, ok := server.authorize(request); !ok {
		server.apiError(writer, http.StatusUnauthorized, "authentication_required", "workspace session is required")
		return
	}

	switch request.URL.Path {
	case "/api/v1/runtime":
		server.runtime(writer, request)
	case "/api/v1/session":
		server.session(writer, request)
	case "/api/v1/accounts":
		server.accounts(writer, request)
	case "/api/v1/accounts/search":
		server.accountSearch(writer, request)
	case "/api/v1/articles":
		server.articles(writer, request)
	case "/api/v1/albums":
		server.albums(writer, request)
	case "/api/v1/saved-queries":
		server.savedQueries(writer, request)
	case "/api/v1/jobs":
		server.jobs(writer, request)
	case "/api/v1/storage":
		server.storage(writer, request)
	case "/api/v1/events/snapshot", "/api/v1/snapshot":
		server.snapshot(writer, request)
	case "/api/v1/exports":
		server.exportsList(writer, request)
	default:
		if server.exportRead(writer, request) {
			return
		}
		if id, ok := strings.CutPrefix(request.URL.Path, "/api/v1/jobs/"); ok {
			if !validJobID(id) {
				server.apiError(writer, http.StatusBadRequest, "invalid_argument", "job identifier is invalid")
				return
			}
			server.job(writer, request, domain.JobID(id))
			return
		}
		server.apiError(writer, http.StatusNotFound, "not_found", "workspace resource was not found")
	}
}

func (server *Server) accountSearch(writer http.ResponseWriter, request *http.Request) {
	query, err := parseAccountQuery(request)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	value, err := server.workspace.SearchAccounts(request.Context(), query)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writePage(writer, http.StatusOK, value)
}

func (server *Server) runtime(writer http.ResponseWriter, request *http.Request) {
	value, err := server.workspace.Runtime(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) session(writer http.ResponseWriter, request *http.Request) {
	value, err := server.workspace.Session(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) accounts(writer http.ResponseWriter, request *http.Request) {
	query, err := parseAccountQuery(request)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	value, err := server.workspace.Accounts(request.Context(), query)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writePage(writer, http.StatusOK, value)
}

func (server *Server) articles(writer http.ResponseWriter, request *http.Request) {
	query, err := parseArticleQuery(request)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	value, err := server.workspace.Articles(request.Context(), query)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writePage(writer, http.StatusOK, value)
}

func (server *Server) albums(writer http.ResponseWriter, request *http.Request) {
	query, err := parseAlbumQuery(request)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	value, err := server.workspace.Albums(request.Context(), query)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writePage(writer, http.StatusOK, value)
}

func (server *Server) savedQueries(writer http.ResponseWriter, request *http.Request) {
	page, err := parsePage(request)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	value, err := server.workspace.SavedArticleQueries(request.Context(), page)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writePage(writer, http.StatusOK, value)
}

func (server *Server) jobs(writer http.ResponseWriter, request *http.Request) {
	query, err := parseJobQuery(request)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	value, err := server.workspace.Jobs(request.Context(), query)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writePage(writer, http.StatusOK, value)
}

func (server *Server) job(writer http.ResponseWriter, request *http.Request, id domain.JobID) {
	value, err := server.application.GetJob(request.Context(), id)
	if err != nil {
		server.applicationError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) storage(writer http.ResponseWriter, request *http.Request) {
	runtime, err := server.workspace.Runtime(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, runtime.Storage)
}

// snapshot intentionally supports polling only. It aggregates already-bounded
// read models; it does not create a streaming connection or start background
// work.
func (server *Server) snapshot(writer http.ResponseWriter, request *http.Request) {
	runtime, err := server.workspace.Runtime(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	session, err := server.workspace.Session(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	jobs, err := server.workspace.Jobs(request.Context(), application.WorkspaceJobQuery{Page: application.WorkspacePageRequest{Limit: 100}})
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, map[string]any{"runtime": runtime, "session": session, "storage": runtime.Storage, "jobs": jobs})
}

func (server *Server) workspaceError(writer http.ResponseWriter, err error) {
	var workspaceErr *application.WorkspaceError
	if errors.As(err, &workspaceErr) {
		status := http.StatusInternalServerError
		switch workspaceErr.Code {
		case application.WorkspaceErrorInvalidArgument:
			status = http.StatusBadRequest
		case application.WorkspaceErrorNotFound:
			status = http.StatusNotFound
		case application.WorkspaceErrorUnavailable:
			status = http.StatusServiceUnavailable
		case application.WorkspaceErrorAuthentication:
			status = http.StatusUnauthorized
		case application.WorkspaceErrorCancelled:
			status = http.StatusRequestTimeout
		}
		server.apiError(writer, status, string(workspaceErr.Code), workspaceErr.Message)
		return
	}
	server.apiError(writer, http.StatusInternalServerError, "internal", "workspace operation failed")
}

func (server *Server) applicationError(writer http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		server.apiError(writer, http.StatusNotFound, "not_found", "workspace item was not found")
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		server.apiError(writer, http.StatusRequestTimeout, "cancelled", "workspace operation was cancelled")
		return
	}
	if errors.Is(err, application.ErrUnavailable) {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace capability is not available")
		return
	}
	server.apiError(writer, http.StatusInternalServerError, "internal", "workspace operation failed")
}

func (server *Server) apiError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, apiErrorEnvelope{APIVersion: apiVersion, Error: apiError{Code: code, Message: message}})
}

// writeAPI retains the shared envelope's data field while projecting its safe
// DTO fields at the top level. The projection keeps the existing browser
// client compatible without making it infer a second response shape.
func writeAPI(writer http.ResponseWriter, status int, data any) {
	payload := map[string]any{"apiVersion": apiVersion, "data": data}
	encoded, err := json.Marshal(data)
	if err == nil {
		var fields map[string]any
		if json.Unmarshal(encoded, &fields) == nil {
			for key, value := range fields {
				payload[key] = value
			}
		}
	}
	writeJSON(writer, status, payload)
}

func writePage[T any](writer http.ResponseWriter, status int, page application.WorkspacePage[T]) {
	limit := page.Limit
	if limit <= 0 {
		limit = application.WorkspaceDefaultPageLimit
	}
	writeJSON(writer, status, map[string]any{
		"apiVersion": apiVersion,
		"data":       page.Items,
		"pagination": map[string]int{"page": pageNumber(page.Offset, limit), "pageSize": limit, "total": page.Total},
		"items":      page.Items,
		"total":      page.Total,
		"offset":     page.Offset,
		"limit":      limit,
	})
}

func pageNumber(offset, limit int) int {
	if limit <= 0 {
		return 1
	}
	return offset/limit + 1
}

func validJobID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		switch {
		case index == 8 || index == 13 || index == 18 || index == 23:
			if character != '-' {
				return false
			}
		case character >= '0' && character <= '9', character >= 'a' && character <= 'f', character >= 'A' && character <= 'F':
		default:
			return false
		}
	}
	return true
}
