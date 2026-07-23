package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

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

// workspaceSnapshot is the bounded polling DTO for local browser state. It
// deliberately contains only existing browser-safe read models. Revision is
// local to one server process: it advances only when the observable snapshot
// changes, so reconnecting clients can distinguish a fresh observation from a
// semantic state change without treating an event stream as the source of
// truth.
type workspaceSnapshot struct {
	Runtime    application.WorkspaceRuntime                        `json:"runtime"`
	Session    application.WorkspaceSession                        `json:"session"`
	Storage    domain.StorageStatus                                `json:"storage"`
	Jobs       application.WorkspacePage[application.WorkspaceJob] `json:"jobs"`
	ObservedAt time.Time                                           `json:"observedAt"`
	Revision   uint64                                              `json:"revision"`
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
	if server.maintenanceRead(writer, request) {
		return
	}
	if server.diagnosticBundleRead(writer, request) {
		return
	}
	if server.backupArtifactRead(writer, request) {
		return
	}
	switch request.URL.Path {
	case "/api/v1/runtime":
		server.runtime(writer, request)
	case "/api/v1/session":
		server.session(writer, request)
	case "/api/v1/accounts":
		server.accounts(writer, request)
	case "/api/v1/accounts/manifest":
		server.accountManifestRead(writer, request)
	case "/api/v1/accounts/search":
		server.accountSearch(writer, request)
	case "/api/v1/articles":
		server.articles(writer, request)
	case "/api/v1/articles/preview":
		server.articlePreview(writer, request)
	case "/api/v1/articles/preview/document":
		server.articlePreviewDocument(writer, request)
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
		if suffix, ok := strings.CutPrefix(request.URL.Path, "/api/v1/articles/"); ok {
			articleID, endpoint, hasEndpoint := strings.Cut(suffix, "/")
			if !hasEndpoint || (endpoint != "resources" && endpoint != "detail") || articleID == "" || strings.Contains(articleID, "/") {
				server.apiError(writer, http.StatusNotFound, "not_found", "workspace resource was not found")
				return
			}
			if endpoint == "detail" {
				server.articleDetail(writer, request, domain.ArticleID(articleID))
				return
			}
			server.articleResources(writer, request, domain.ArticleID(articleID))
			return
		}
		if id, ok := strings.CutPrefix(request.URL.Path, "/api/v1/jobs/"); ok {
			jobID, suffix, hasSuffix := strings.Cut(id, "/")
			if hasSuffix && suffix != "detail" {
				server.apiError(writer, http.StatusNotFound, "not_found", "workspace resource was not found")
				return
			}
			if !validJobID(jobID) {
				server.apiError(writer, http.StatusBadRequest, "invalid_argument", "job identifier is invalid")
				return
			}
			if hasSuffix {
				server.jobDetails(writer, request, domain.JobID(jobID))
				return
			}
			server.job(writer, request, domain.JobID(jobID))
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

func (server *Server) articlePreview(writer http.ResponseWriter, request *http.Request) {
	articleID := domain.ArticleID(strings.TrimSpace(request.URL.Query().Get("articleId")))
	if articleID == "" {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "article identifier is required")
		return
	}
	value, err := server.workspace.ArticlePreview(request.Context(), articleID)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	if value.Available {
		value.DocumentURL = "/api/v1/articles/preview/document?articleId=" + url.QueryEscape(string(value.ArticleID))
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) articleResources(writer http.ResponseWriter, request *http.Request, articleID domain.ArticleID) {
	if len(request.URL.Query()) != 0 {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "article resources does not accept query parameters")
		return
	}
	value, err := server.workspace.ArticleResources(request.Context(), articleID)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) articleDetail(writer http.ResponseWriter, request *http.Request, articleID domain.ArticleID) {
	page, err := parsePage(request)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	value, err := server.workspace.ArticleDetail(request.Context(), articleID, page)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) articlePreviewDocument(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return
	}
	articleID := domain.ArticleID(strings.TrimSpace(request.URL.Query().Get("articleId")))
	if articleID == "" {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "article identifier is required")
		return
	}
	preview, err := server.workspace.RenderArticlePreview(request.Context(), articleID)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Content-Disposition", "inline; filename=article-preview.html")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'; img-src data:; media-src data:; style-src 'none'; font-src data:")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = writer.Write(preview.HTML)
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

func (server *Server) jobDetails(writer http.ResponseWriter, request *http.Request, id domain.JobID) {
	if len(request.URL.Query()) != 0 {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "job detail does not accept query parameters")
		return
	}
	value, err := server.workspace.JobDetails(request.Context(), id)
	if err != nil {
		server.workspaceError(writer, err)
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
	snapshot := workspaceSnapshot{Runtime: runtime, Session: session, Storage: runtime.Storage, Jobs: jobs}
	writeAPI(writer, http.StatusOK, server.observeSnapshot(snapshot))
}

func (server *Server) observeSnapshot(snapshot workspaceSnapshot) workspaceSnapshot {
	// Marshal the typed, sanitized DTO rather than any application/domain
	// object. observedAt and revision are observation metadata, not semantic
	// state, so clear them before comparing snapshots.
	semanticSnapshot := snapshot
	semanticSnapshot.ObservedAt = time.Time{}
	semanticSnapshot.Revision = 0
	semanticSnapshot.Runtime.CheckedAt = time.Time{}
	semantic, err := json.Marshal(semanticSnapshot)
	if err != nil {
		// Every field is a fixed safe DTO; retain a usable response even if a
		// future field accidentally becomes non-marshallable.
		semantic = nil
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if server.lastSnapshot.revision == 0 || server.lastSnapshot.semantic != string(semantic) {
		server.lastSnapshot.revision++
		server.lastSnapshot.semantic = string(semantic)
	}
	snapshot.Revision = server.lastSnapshot.revision
	snapshot.ObservedAt = server.now().UTC()
	return snapshot
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
