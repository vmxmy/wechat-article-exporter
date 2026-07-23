package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

// apiControl is the authenticated mutation surface. It only calls the
// application-owned Workspace facade; handlers never receive profile runtime,
// filesystem, database, cookie, or secret-store capabilities.
func (server *Server) apiControl(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method == http.MethodGet {
		return false
	}
	if server.maintenanceControl(writer, request) {
		return true
	}
	if server.diagnosticBundleControl(writer, request) {
		return true
	}
	if server.restoreControl(writer, request) {
		return true
	}
	switch request.URL.Path {
	case "/api/v1/saved-queries":
		switch request.Method {
		case http.MethodPost:
			server.savedQuerySave(writer, request)
		case http.MethodDelete:
			server.savedQueryDelete(writer, request)
		default:
			return false
		}
	case "/api/v1/export-directories/authorize":
		server.exportDirectoryAuthorize(writer, request)
	case "/api/v1/export-directories":
		server.exportDirectoryCreate(writer, request)
	case "/api/v1/exports/start":
		server.exportStart(writer, request)
	case "/api/v1/login/begin":
		server.loginBegin(writer, request)
	case "/api/v1/login/poll":
		server.loginPoll(writer, request)
	case "/api/v1/login/complete":
		server.loginComplete(writer, request)
	case "/api/v1/session/logout":
		server.logout(writer, request)
	case "/api/v1/accounts":
		switch request.Method {
		case http.MethodPost:
			// Preserve the read endpoint's historical method contract when a
			// browser has not supplied mutation credentials.
			if _, ok := server.authorizeMutation(request); !ok {
				return false
			}
			server.accountSave(writer, request)
		case http.MethodDelete:
			if _, ok := server.authorizeMutation(request); !ok {
				return false
			}
			server.accountDelete(writer, request)
		default:
			return false
		}
	case "/api/v1/accounts/search":
		return false
	case "/api/v1/ingest/url":
		server.ingestURL(writer, request)
	case "/api/v1/articles/download":
		server.articleDownload(writer, request)
	case "/api/v1/articles/metadata":
		server.articleDownloadKind(writer, request, "metadata")
	case "/api/v1/articles/comments":
		server.articleDownloadKind(writer, request, "comments")
	case "/api/v1/articles/resources":
		server.articleDownloadKind(writer, request, "resources")
	default:
		if server.exportControl(writer, request) {
			return true
		}
		if id, ok := strings.CutPrefix(request.URL.Path, "/api/v1/accounts/"); ok {
			if accountID, suffix, found := strings.Cut(id, "/"); found && suffix == "sync" {
				server.accountSync(writer, request, domain.AccountID(accountID))
			} else if !found {
				server.accountUpdate(writer, request, domain.AccountID(id))
			} else {
				return false
			}
		} else if id, ok := strings.CutPrefix(request.URL.Path, "/api/v1/jobs/"); ok {
			jobID, action, found := strings.Cut(id, "/")
			if !found {
				return false
			}
			if !validJobID(jobID) {
				server.apiError(writer, http.StatusBadRequest, "invalid_argument", "job identifier is invalid")
				return true
			}
			switch action {
			case "cancel", "pause", "resume", "retry":
				server.jobControl(writer, request, domain.JobID(jobID), action)
			default:
				return false
			}
		} else if id, ok := strings.CutPrefix(request.URL.Path, "/api/v1/albums/"); ok {
			albumID, action, found := strings.Cut(id, "/")
			if !found || action != "traverse" {
				return false
			}
			server.albumTraverse(writer, request, domain.AlbumID(albumID))
		} else {
			return false
		}
	}
	return true
}

func (server *Server) savedQuerySave(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		Name  string              `json:"name"`
		Query domain.ArticleQuery `json:"query"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	item, err := server.workspace.SaveArticleQuery(request.Context(), input.Name, input.Query)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, item)
}

func (server *Server) savedQueryDelete(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodDelete) {
		return
	}
	var input struct {
		Name         string `json:"name"`
		Confirmation string `json:"confirm"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	if err := server.workspace.DeleteSavedArticleQuery(request.Context(), input.Name, input.Confirmation); err != nil {
		server.workspaceError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) apiMutation(writer http.ResponseWriter, request *http.Request, method string) bool {
	if request.Method != method {
		writer.Header().Set("Allow", method)
		server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return false
	}
	if _, ok := server.authorizeMutation(request); !ok {
		server.apiError(writer, http.StatusForbidden, "forbidden", "workspace mutation authorization is required")
		return false
	}
	return true
}

func decodeControl(request *http.Request, destination any) error {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return invalidArgument("request body is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return invalidArgument("request body must contain one JSON value")
	}
	return nil
}

func (server *Server) loginBegin(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		SessionID string `json:"sessionId"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	flow, err := server.workspace.LoginBegin(request.Context(), input.SessionID)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, flow)
}

func (server *Server) loginPoll(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct{}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	result, err := server.workspace.PollLogin(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, result)
}

func (server *Server) loginComplete(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct{}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	result, err := server.workspace.CompleteLogin(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, result)
}

func (server *Server) logout(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	if err := server.workspace.Logout(request.Context()); err != nil {
		server.workspaceError(writer, err)
		return
	}
	server.deleteSession(request)
	server.clearSessionCookies(writer)
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) accountSave(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input domain.Account
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	account, err := server.workspace.SaveAccount(request.Context(), input)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusCreated, account)
}

func (server *Server) accountUpdate(writer http.ResponseWriter, request *http.Request, id domain.AccountID) {
	if !server.apiMutation(writer, request, http.MethodPatch) {
		return
	}
	if strings.TrimSpace(string(id)) == "" {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "account identifier is invalid")
		return
	}
	var input domain.Account
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	account, err := server.workspace.UpdateAccount(request.Context(), id, input)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, account)
}

func (server *Server) accountDelete(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodDelete) {
		return
	}
	var input struct {
		IDs          []domain.AccountID `json:"ids"`
		Confirmation string             `json:"confirm"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	report, err := server.workspace.DeleteAccounts(request.Context(), input.IDs, input.Confirmation)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, report)
}

func (server *Server) accountSync(writer http.ResponseWriter, request *http.Request, id domain.AccountID) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	if strings.TrimSpace(string(id)) == "" {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "account identifier is invalid")
		return
	}
	var input domain.SynchronizeAccountRequest
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	input.AccountID, input.AccountIDs = id, nil
	job, err := server.workspace.SynchronizeAccount(request.Context(), input)
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusAccepted, job)
}

func (server *Server) ingestURL(writer http.ResponseWriter, request *http.Request) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		URL   string `json:"url"`
		Force bool   `json:"force"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	if strings.TrimSpace(input.URL) == "" {
		server.apiError(writer, http.StatusBadRequest, "invalid_argument", "article URL is required")
		return
	}
	job, err := server.workspace.StartDownload(request.Context(), domain.DownloadRequest{Kind: "article", URLs: []string{strings.TrimSpace(input.URL)}, Force: input.Force})
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusAccepted, job)
}

func (server *Server) articleDownload(writer http.ResponseWriter, request *http.Request) {
	server.articleDownloadKind(writer, request, "article")
}

func (server *Server) articleDownloadKind(writer http.ResponseWriter, request *http.Request, kind string) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		ArticleIDs []domain.ArticleID `json:"articleIds"`
		Force      bool               `json:"force"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	job, err := server.workspace.StartDownload(request.Context(), domain.DownloadRequest{Kind: kind, ArticleIDs: input.ArticleIDs, Force: input.Force})
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusAccepted, job)
}

func (server *Server) albumTraverse(writer http.ResponseWriter, request *http.Request, albumID domain.AlbumID) {
	if !server.apiMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		AccountID domain.AccountID `json:"accountId"`
		Download  bool             `json:"download"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.workspaceError(writer, err)
		return
	}
	job, err := server.workspace.SynchronizeAlbum(request.Context(), application.WorkspaceAlbumTraversalRequest{
		AccountID: input.AccountID, AlbumID: albumID, Download: input.Download,
	})
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusAccepted, job)
}

func (server *Server) jobControl(writer http.ResponseWriter, request *http.Request, id domain.JobID, action string) {
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
	var (
		job domain.Job
		err error
	)
	switch action {
	case "cancel":
		job, err = server.workspace.CancelJobConfirmed(request.Context(), id, input.Confirmation)
	case "pause":
		job, err = server.workspace.PauseJob(request.Context(), id, input.Confirmation)
	case "resume":
		job, err = server.workspace.ResumeJob(request.Context(), id)
	case "retry":
		job, err = server.workspace.RetryJob(request.Context(), id, input.Confirmation)
	}
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, job)
}
