package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestReadAPIProvidesVersionedBoundedWorkspaceData(t *testing.T) {
	app := &apiApplication{
		runtime:  domain.RuntimeStatus{Version: "fixture", Profile: "fixture-profile", Paths: domain.RuntimePaths{Data: "/private/profile"}, Storage: domain.StorageStatus{Articles: 2}},
		session:  wechat.Session{State: wechat.SessionAuthenticated, AccountID: "account-1", AccountName: "Fixture"},
		accounts: domain.Page[domain.Account]{Items: []domain.Account{{ID: "account-1", Name: "Fixture"}}, Total: 1},
		articles: domain.Page[domain.Article]{Items: []domain.Article{{ID: "article-1", AccountID: "account-1", Title: "Fixture", CanonicalURL: "https://example.test/article"}}, Total: 1},
		albums:   domain.Page[domain.Album]{Items: []domain.Album{{ID: "album-1", Name: "Album"}}, Total: 1},
		jobs:     domain.Page[domain.Job]{Items: []domain.Job{{ID: "11111111-1111-1111-1111-111111111111", Kind: "sync", State: domain.JobRunning}}, Total: 1},
		saved:    []domain.SavedArticleQuery{{Name: "recent"}},
		job:      domain.Job{ID: "11111111-1111-1111-1111-111111111111", Kind: "sync", State: domain.JobRunning},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	for _, target := range []string{
		"/api/v1/runtime", "/api/v1/session", "/api/v1/accounts?keyword=fixture&limit=100", "/api/v1/articles?accountId=account-1&deleted=false&messageType=1&messageType=2&sort=published:desc",
		"/api/v1/albums?accountId=account-1&keyword=album&offset=0&limit=20", "/api/v1/saved-queries?limit=100", "/api/v1/jobs?state=running", "/api/v1/jobs/11111111-1111-1111-1111-111111111111", "/api/v1/storage", "/api/v1/events/snapshot", "/api/v1/snapshot",
	} {
		response := get(t, client, base+target)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		var envelope apiEnvelope
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			response.Body.Close()
			t.Fatalf("decode %s: %v", target, err)
		}
		response.Body.Close()
		if envelope.APIVersion != apiVersion || envelope.Data == nil {
			t.Fatalf("GET %s envelope = %#v", target, envelope)
		}
	}
	if app.accountQuery.Limit != 100 || app.articleQuery.Limit != application.WorkspaceDefaultPageLimit || app.articleQuery.Deleted == nil || *app.articleQuery.Deleted || len(app.articleQuery.MessageTypes) != 2 || len(app.articleQuery.Sorts) != 1 || app.albumQuery != (domain.AlbumQuery{AccountID: "account-1", Keyword: "album", Limit: 20}) {
		t.Fatalf("queries not parsed/bounded: account=%#v article=%#v album=%#v", app.accountQuery, app.articleQuery, app.albumQuery)
	}
	response := get(t, client, base+"/api/v1/articles")
	body := readResponse(t, response)
	if !strings.Contains(body, `"accountName":"Fixture"`) || strings.Contains(body, `"accountId":"account-1"`) {
		t.Fatalf("article list did not project readable account name: %s", body)
	}
	response = get(t, client, base+"/api/v1/runtime")
	body = readResponse(t, response)
	if strings.Contains(body, "/private/profile") {
		t.Fatalf("runtime response leaked absolute path: %s", body)
	}
}

func TestSessionAccountSwitchingAPIUsesSafeWorkspaceDTOs(t *testing.T) {
	app := &apiApplication{
		switchable: []wechat.SwitchableAccount{{ID: "account-1", Name: "Fixture", AvatarURL: "https://sensitive.example/avatar", Alias: "fixture"}},
		switched:   wechat.Session{State: wechat.SessionAuthenticated, AccountID: "account-1", AccountName: "Fixture", Token: "session-secret"},
	}
	server, client := startAPIApplicationServer(t, app)
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")

	response := get(t, client, base+"/api/v1/session/accounts")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	base = authorizeAPI(t, client, server.URL())
	response = get(t, client, base+"/api/v1/session/accounts")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body := readResponse(t, response)
	for _, forbidden := range []string{"session-secret", "token", "resource", "digest", "path", "sensitive.example"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("switchable account response leaked %q: %s", forbidden, body)
		}
	}
	var accounts application.WorkspaceSwitchableAccounts
	if err := json.Unmarshal([]byte(body), &accounts); err != nil || !accounts.Available || len(accounts.Accounts) != 1 || accounts.Accounts[0].ID != "account-1" {
		t.Fatalf("accounts=%#v err=%v", accounts, err)
	}

	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	request := requestWith(t, http.MethodPost, base+"/api/v1/session/accounts/account-1/switch", strings.NewReader(`{}`), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("switch status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body = readResponse(t, response)
	if strings.Contains(body, "session-secret") || app.switchedAccountID != "account-1" {
		t.Fatalf("switch response=%s account=%q", body, app.switchedAccountID)
	}

	for _, request := range []*http.Request{
		requestWith(t, http.MethodGet, base+"/api/v1/session/accounts/account-1/switch", nil, nil),
		requestWith(t, http.MethodPost, base+"/api/v1/session/accounts/%2e%2e%2Fsecret/switch", strings.NewReader(`{}`), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf}),
		requestWith(t, http.MethodPost, base+"/api/v1/session/accounts/account-1/switch", strings.NewReader(`{}`), map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json", "X-CSRF-Token": csrf}),
	} {
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusBadRequest && response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusMethodNotAllowed && response.StatusCode != http.StatusNotFound {
			t.Fatalf("request %s %s status=%d body=%s", request.Method, request.URL, response.StatusCode, readResponse(t, response))
		}
		response.Body.Close()
	}

	app.switchableErr = wechat.ErrAccountSwitching
	response = get(t, client, base+"/api/v1/session/accounts")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unavailable GET status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	if err := json.NewDecoder(response.Body).Decode(&struct {
		Data application.WorkspaceSwitchableAccounts `json:"data"`
	}{}); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestSnapshotPollingRevisionAdvancesOnlyForObservedStateChanges(t *testing.T) {
	// The HTTP cookie jar evaluates Expires against the real clock. Keep the
	// controlled clock in the future so bootstrap session cookies remain valid
	// regardless of when this deterministic snapshot test is run.
	now := time.Now().UTC().Truncate(time.Second)
	app := &apiApplication{
		runtime: domain.RuntimeStatus{Profile: "fixture-profile", Storage: domain.StorageStatus{Articles: 1}},
		session: wechat.Session{State: wechat.SessionMissing},
		jobs:    domain.Page[domain.Job]{Items: []domain.Job{{ID: "11111111-1111-1111-1111-111111111111", State: domain.JobRunning}}, Total: 1},
	}
	server, err := New(Options{Application: app, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background()) }()
	t.Cleanup(func() {
		_ = server.Close()
		if err := <-done; err != nil {
			t.Errorf("server stopped with error: %v", err)
		}
	})
	client := newTestClient(t)
	base := authorizeAPI(t, client, server.URL())

	readSnapshot := func() workspaceSnapshot {
		t.Helper()
		response := get(t, client, base+"/api/v1/events/snapshot")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("snapshot status=%d body=%s", response.StatusCode, readResponse(t, response))
		}
		var envelope apiEnvelope
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		data, err := json.Marshal(envelope.Data)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot workspaceSnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}

	first := readSnapshot()
	now = now.Add(time.Second)
	second := readSnapshot()
	if first.Revision != 1 || second.Revision != first.Revision || !first.ObservedAt.Equal(now.Add(-time.Second)) || !second.ObservedAt.Equal(now) {
		t.Fatalf("unchanged snapshots = %#v, %#v", first, second)
	}
	app.jobs = domain.Page[domain.Job]{Items: []domain.Job{{ID: "11111111-1111-1111-1111-111111111111", State: domain.JobCompleted}}, Total: 1}
	now = now.Add(time.Second)
	third := readSnapshot()
	if third.Revision != 2 || !third.ObservedAt.Equal(now) || len(third.Jobs.Items) != 1 || third.Jobs.Items[0].State != domain.JobCompleted {
		t.Fatalf("changed snapshot = %#v", third)
	}
	encoded, err := json.Marshal(third)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/private", "Paths"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestJobDetailAPIUsesSafeBoundedWorkspaceDTO(t *testing.T) {
	const jobID = "11111111-1111-1111-1111-111111111111"
	app := &apiApplication{job: domain.Job{ID: jobID, Kind: "download", State: domain.JobRunning}, jobDetail: application.WorkspaceJobDetail{
		Job: application.WorkspaceJob{ID: jobID, Kind: "download", State: domain.JobRunning, PermittedActions: []application.WorkspaceJobAction{application.WorkspaceJobActionPause, application.WorkspaceJobActionCancel}}, Items: []application.WorkspaceJobItemDetail{{ID: "item-1", State: domain.JobRunning, AttemptCount: 1, ErrorClass: "network"}},
		ItemsTotal: 1, Logs: []application.WorkspaceJobLogDetail{{ID: 1, ItemID: "item-1", Level: "info", Message: "sanitized local progress"}}, Lease: application.WorkspaceJobLeaseDetail{Active: true},
	}}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/jobs/"+jobID+"/detail")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body := readResponse(t, response)
	for _, forbidden := range []string{"checkpoint", "leaseOwner", "fields", "/private", "secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("detail API leaked %q: %s", forbidden, body)
		}
	}
	var detail application.WorkspaceJobDetail
	if err := json.Unmarshal([]byte(body), &detail); err != nil || detail.Job.ID != jobID || len(detail.Job.PermittedActions) != 2 || len(detail.Items) != 1 || len(detail.Logs) != 1 || !detail.Lease.Active {
		t.Fatalf("detail DTO=%#v err=%v", detail, err)
	}

	response = get(t, client, base+"/api/v1/jobs/"+jobID+"/detail?limit=1")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("detail query status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
}

func TestArticleResourcesAPIProvidesOnlySafeCompletenessDTO(t *testing.T) {
	app := &apiApplication{resourceAvailability: library.ArticleResourceAvailability{Total: 2, Available: 1}}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/articles/article-1/resources")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resources status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body := readResponse(t, response)
	for _, forbidden := range []string{"resourceId", "sourceUrl", "originalUrl", "objectDigest", "digest", "mediaType", "https://"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("resources API leaked %q: %s", forbidden, body)
		}
	}
	var value application.WorkspaceArticleResources
	if err := json.Unmarshal([]byte(body), &value); err != nil || value != (application.WorkspaceArticleResources{ArticleID: "article-1", Total: 2, Available: 1, Missing: 1}) {
		t.Fatalf("resources DTO=%#v err=%v", value, err)
	}
	if app.resourceArticleID != "article-1" {
		t.Fatalf("resource lookup article ID=%q", app.resourceArticleID)
	}

	response = get(t, client, base+"/api/v1/articles/article-1/resources?unexpected=1")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("resources query status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
}

func TestArticleDetailAPIProvidesBoundedSafeMetricsAndResourceDetails(t *testing.T) {
	capturedAt := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	app := &apiApplication{
		metrics:         library.ArticleMetrics{ReadCount: 12, OldLikeCount: 3, LikeCount: 4, ShareCount: 5, CommentCount: 6, CapturedAt: capturedAt},
		resourceDetails: domain.Page[library.ArticleResourceDetail]{Items: []library.ArticleResourceDetail{{Role: "image", Ordinal: 0, Available: true}, {Role: "audio", Ordinal: 1}}, Total: 3},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/articles/article-1/detail?offset=1&limit=2")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body := readResponse(t, response)
	for _, forbidden := range []string{"resourceId", "sourceUrl", "originalUrl", "objectDigest", "digest", "mediaType", "credential", "https://", "/private"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("detail API leaked %q: %s", forbidden, body)
		}
	}
	var value application.WorkspaceArticleDetail
	if err := json.Unmarshal([]byte(body), &value); err != nil || value.ArticleID != "article-1" || !value.Metrics.Available || value.Metrics.ReadCount != 12 || !value.Metrics.CapturedAt.Equal(capturedAt) || value.Resources.Total != 3 || value.Resources.Offset != 1 || value.Resources.Limit != 2 || len(value.Resources.Items) != 2 || value.Resources.Items[0] != (application.WorkspaceArticleResourceDetail{Role: "image", Ordinal: 0, Available: true}) {
		t.Fatalf("detail DTO=%#v err=%v", value, err)
	}
	if app.detailArticleID != "article-1" || app.detailOffset != 1 || app.detailLimit != 2 {
		t.Fatalf("detail lookup = article=%q offset=%d limit=%d", app.detailArticleID, app.detailOffset, app.detailLimit)
	}

	response = get(t, client, base+"/api/v1/articles/article-1/detail?unexpected=1")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("detail query status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
}

func TestArticleCommentsAPIUsesBoundedSafeLocalProjections(t *testing.T) {
	createdAt := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	articleID := "article:efc3a405910aa8d4dd98bb2e095017b6"
	app := &apiApplication{
		comments: domain.Page[library.CommentRecord]{Items: []library.CommentRecord{{
			ID: "database-comment-id", UpstreamID: "comment-1", AuthorName: "Reader", Content: "Stored comment", LikeCount: 3, CreatedAt: createdAt,
			RawObjectDigest: "secret-digest", FetchedAt: createdAt, ReplyTotal: 2,
		}}, Total: 3},
		replies: domain.Page[library.ReplyRecord]{Items: []library.ReplyRecord{{
			ID: "database-reply-id", UpstreamID: "reply-1", AuthorName: "Author", Content: "Stored reply", LikeCount: 2, CreatedAt: createdAt,
			RawObjectDigest: "secret-reply-digest", FetchedAt: createdAt,
		}}, Total: 2},
		pendingReplyThreads: []library.ReplyThread{{ContentID: "comment-1", LastError: "upstream token=secret", MaxReplyID: 42}},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/articles/"+articleID+"/comments?offset=1&limit=2")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("comments status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body := readResponse(t, response)
	for _, forbidden := range []string{"database-comment-id", "secret-digest", "fetchedAt", "maxReply", "token", "lastError", "buffer", "path", "url"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("comments response leaked %q: %s", forbidden, body)
		}
	}
	var comments application.WorkspaceArticleComments
	if err := json.Unmarshal([]byte(body), &comments); err != nil || comments.ArticleID != domain.ArticleID(articleID) || comments.Comments.Total != 3 || comments.Comments.Offset != 1 || comments.Comments.Limit != 2 || comments.PendingReplies != 1 || len(comments.Comments.Items) != 1 || comments.Comments.Items[0].ID != "comment-1" || comments.Comments.Items[0].ReplyStatus != "pending" {
		t.Fatalf("comments=%#v err=%v", comments, err)
	}
	if app.commentsArticleID != domain.ArticleID(articleID) || app.commentsOffset != 1 || app.commentsLimit != 2 {
		t.Fatalf("comments lookup = article=%q offset=%d limit=%d", app.commentsArticleID, app.commentsOffset, app.commentsLimit)
	}

	response = get(t, client, base+"/api/v1/articles/"+articleID+"/comments/comment-1/replies?page=2&page_size=1")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("replies status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body = readResponse(t, response)
	for _, forbidden := range []string{"database-reply-id", "secret-reply-digest", "fetchedAt", "token", "path", "url"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("replies response leaked %q: %s", forbidden, body)
		}
	}
	var replies application.WorkspacePage[application.WorkspaceArticleReply]
	if err := json.Unmarshal([]byte(body), &replies); err != nil || replies.Total != 2 || replies.Offset != 1 || replies.Limit != 1 || len(replies.Items) != 1 || replies.Items[0].ID != "reply-1" {
		t.Fatalf("replies=%#v err=%v", replies, err)
	}
	if app.repliesArticleID != domain.ArticleID(articleID) || app.repliesCommentID != "comment-1" || app.repliesOffset != 1 || app.repliesLimit != 1 {
		t.Fatalf("replies lookup = article=%q comment=%q offset=%d limit=%d", app.repliesArticleID, app.repliesCommentID, app.repliesOffset, app.repliesLimit)
	}

	for _, target := range []string{
		"/api/v1/articles/article%20one/comments", "/api/v1/articles/article:xyz/comments", "/api/v1/articles/" + articleID + "/comments?limit=101", "/api/v1/articles/" + articleID + "/comments?wat=1",
		"/api/v1/articles/" + articleID + "/comments/comment%20one/replies", "/api/v1/articles/" + articleID + "/comments/comment-1/replies?offset=0&page=1",
	} {
		response := get(t, client, base+target)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		assertAPIError(t, response, "invalid_argument")
	}
	response = get(t, client, base+"/api/v1/articles/article:efc3a405910aa8d4dd98bb2e095017b6%2F..%2Fsecret/comments")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("encoded slash path status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "not_found")
}

func TestReadAPIRejectsUnauthorizedUnsupportedAndUnboundedQueries(t *testing.T) {
	server, client := startAPIApplicationServer(t, &apiApplication{})
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")
	if response := get(t, client, base+"/api/v1/accounts"); response.StatusCode != http.StatusUnauthorized {
		response.Body.Close()
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
	authorize(t, client, server.URL())

	for _, target := range []string{
		"/api/v1/articles?limit=101", "/api/v1/articles?wat=1", "/api/v1/articles?deleted=maybe", "/api/v1/articles?state=one&state=two", "/api/v1/articles?sort=published:asc&direction=desc", "/api/v1/articles?readMin=9&readMax=1", "/api/v1/articles?publishedFrom=bad", "/api/v1/articles?sort=unsafe:asc", "/api/v1/jobs?state=wat", "/api/v1/saved-queries?offset=-1", "/api/v1/accounts?sort=name",
	} {
		response := get(t, client, base+target)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		assertAPIError(t, response, "invalid_argument")
	}
	request := requestWith(t, http.MethodPost, base+"/api/v1/accounts", nil, nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("method status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "method_not_allowed")

	response = get(t, client, base+"/api/v1/jobs/not-a-uuid")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid job path status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
}

func TestAlbumsAPIRejectsInvalidQueriesBeforeWorkspaceCalls(t *testing.T) {
	app := &apiApplication{}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	for _, test := range []struct {
		name   string
		target string
	}{
		{name: "unsupported key", target: "/api/v1/albums?wat=1"},
		{name: "repeated account ID", target: "/api/v1/albums?accountId=account-1&accountId=account-2"},
		{name: "limit above maximum", target: "/api/v1/albums?limit=101"},
		{name: "page below minimum", target: "/api/v1/albums?page=0"},
		{name: "offset and page mixed", target: "/api/v1/albums?offset=0&page=1"},
		{name: "page size below minimum", target: "/api/v1/albums?page_size=0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := get(t, client, base+test.target)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("GET %s status=%d body=%s", test.target, response.StatusCode, readResponse(t, response))
			}
			assertAPIError(t, response, "invalid_argument")
			if app.albumQueryCalls != 0 {
				t.Fatalf("GET %s called QueryAlbums %d times", test.target, app.albumQueryCalls)
			}
		})
	}
}

func TestReadAPIErrorModelDoesNotLeakApplicationFailures(t *testing.T) {
	server, client := startAPIApplicationServer(t, &apiApplication{accountsErr: errors.New("sqlite at /private/token=secret")})
	base := authorizeAPI(t, client, server.URL())
	response := get(t, client, base+"/api/v1/accounts")
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.StatusCode)
	}
	body := readResponse(t, response)
	if strings.Contains(body, "/private") || strings.Contains(body, "secret") {
		t.Fatalf("unsafe error leaked: %s", body)
	}
	var envelope apiErrorEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil || envelope.APIVersion != apiVersion || envelope.Error.Code != "internal" || envelope.Error.Message != "workspace operation failed" {
		t.Fatalf("error envelope = %#v err=%v", envelope, err)
	}
}

func TestControlAPIUsesWorkspaceFacadeWithExactConfirmations(t *testing.T) {
	const jobID = "11111111-1111-1111-1111-111111111111"
	app := &apiApplication{
		loginFlow: applicationLoginFlow(),
		poll:      wechat.PollResult{State: wechat.QRScanned, AccountCount: 1},
		completed: wechat.Session{State: wechat.SessionAuthenticated, AccountID: "account-1", AccountName: "Fixture"},
		saved:     []domain.SavedArticleQuery{},
		job:       domain.Job{ID: jobID, Kind: "article_download", State: domain.JobQueued},
		account:   domain.Account{ID: "account-1", FakeID: "fixture", Name: "Fixture"},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	mutate := func(path string, body string) *http.Response {
		request := requestWith(t, http.MethodPost, base+path, strings.NewReader(body), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	for _, input := range []struct {
		path, body string
		status     int
		job        bool
	}{
		{"/api/v1/login/begin", `{"sessionId":"browser-session"}`, http.StatusOK, false},
		{"/api/v1/login/poll", `{}`, http.StatusOK, false},
		{"/api/v1/login/complete", `{}`, http.StatusOK, false},
		{"/api/v1/accounts/account-1/sync", `{"incremental":true,"pageSize":20}`, http.StatusAccepted, true},
		{"/api/v1/ingest/url", `{"url":"https://mp.weixin.qq.com/s/fixture"}`, http.StatusAccepted, true},
		{"/api/v1/articles/download", `{"articleIds":["article-1"]}`, http.StatusAccepted, true},
		{"/api/v1/articles/metadata", `{"articleIds":["article-1"]}`, http.StatusAccepted, true},
		{"/api/v1/articles/comments", `{"articleIds":["article-1"]}`, http.StatusAccepted, true},
		{"/api/v1/articles/resources", `{"articleIds":["article-1"],"force":true}`, http.StatusAccepted, true},
		{"/api/v1/albums/album-1/traverse", `{"accountId":"account-1","order":"reverse","download":true}`, http.StatusAccepted, true},
		{"/api/v1/jobs/" + jobID + "/pause", `{"confirm":"pause-job:` + jobID + `"}`, http.StatusOK, true},
		{"/api/v1/jobs/" + jobID + "/resume", `{}`, http.StatusOK, true},
		{"/api/v1/jobs/" + jobID + "/retry", `{"confirm":"retry-job:` + jobID + `"}`, http.StatusOK, true},
		{"/api/v1/jobs/" + jobID + "/cancel", `{"confirm":"cancel-job:` + jobID + `"}`, http.StatusOK, true},
	} {
		response := mutate(input.path, input.body)
		if response.StatusCode != input.status {
			t.Fatalf("POST %s status=%d body=%s", input.path, response.StatusCode, readResponse(t, response))
		}
		if input.job {
			assertStableJobResponse(t, response, input.path, jobID)
			continue
		}
		response.Body.Close()
	}
	if app.loginSessionID != "browser-session" || app.syncRequest.AccountID != "account-1" || app.albumRequest.AccountID != "account-1" || app.albumRequest.AlbumID != "album-1" || app.albumRequest.Order != wechat.AlbumReverse || !app.albumBatch || len(app.downloadRequests) != 5 {
		t.Fatalf("control inputs were not routed through application: login=%q sync=%#v album=%#v batch=%t downloads=%#v", app.loginSessionID, app.syncRequest, app.albumRequest, app.albumBatch, app.downloadRequests)
	}
	if !app.syncRequest.Incremental || app.syncRequest.PageSize != 20 {
		t.Fatalf("incremental account sync request = %#v", app.syncRequest)
	}
	response := mutate("/api/v1/accounts/account-1/sync", `{"incremental":false}`)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("full account sync status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertStableJobResponse(t, response, "/api/v1/accounts/account-1/sync", jobID)
	if app.syncRequest.Incremental {
		t.Fatalf("full account sync request = %#v", app.syncRequest)
	}
	if app.downloadRequests[0].URLs[0] != "https://mp.weixin.qq.com/s/fixture" || app.downloadRequests[1].Kind != "article" || app.downloadRequests[2].Kind != "metadata" || app.downloadRequests[3].Kind != "comments" || app.downloadRequests[4].Kind != "resources" || !app.downloadRequests[4].Force {
		t.Fatalf("download jobs = %#v", app.downloadRequests)
	}
	response = mutate("/api/v1/jobs/"+jobID+"/cancel", `{}`)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing confirmation status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
	response = mutate("/api/v1/session/logout", `{}`)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	if !app.loggedOut {
		t.Fatal("application logout was not invoked")
	}
}

func TestAPIAlbumBatchTraversalQueuesOneBoundedDurableOperation(t *testing.T) {
	app := &apiApplication{job: domain.Job{ID: "11111111-1111-1111-1111-111111111111", Kind: "album_sync", State: domain.JobQueued}}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	request := requestWith(t, http.MethodPost, base+"/api/v1/albums/traverse", strings.NewReader(`{"albumIds":["album-2","album-1"],"order":"reverse","download":true}`), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertStableJobResponse(t, response, "/api/v1/albums/traverse", string(app.job.ID))
	if !reflect.DeepEqual(app.multiAlbumRequest, []domain.AlbumID{"album-2", "album-1"}) || !app.albumBatch {
		t.Fatalf("multi album request=%#v download=%t", app.multiAlbumRequest, app.albumBatch)
	}
	for _, body := range []string{
		`{"albumIds":[]}`, `{"albumIds":["album-1","album-1"]}`, `{"albumIds":["album-1"],"accountId":"account-1"}`,
	} {
		request = requestWith(t, http.MethodPost, base+"/api/v1/albums/traverse", strings.NewReader(body), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
		response, err = client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, response.StatusCode, readResponse(t, response))
		}
		response.Body.Close()
	}
}

func assertStableJobResponse(t *testing.T, response *http.Response, path, wantID string) {
	t.Helper()
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if envelope.Data.ID != wantID {
		t.Fatalf("POST %s persistent job response ID=%q, want %q", path, envelope.Data.ID, wantID)
	}
}

func TestAccountCRUDAndSearchUseAuthenticatedWorkspaceFacade(t *testing.T) {
	app := &apiApplication{
		account:        domain.Account{ID: "account-1", FakeID: "fixture", Name: "Fixture"},
		searchAccounts: domain.Page[domain.Account]{Items: []domain.Account{{ID: "account-1", FakeID: "fixture", Name: "Fixture"}}, Total: 1},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	mutate := func(method, path, body string) *http.Response {
		request := requestWith(t, method, base+path, strings.NewReader(body), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	response := get(t, client, base+"/api/v1/accounts/search?search=%20fixture%20&page=2&page_size=25")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("account search status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	if app.searchQuery.Keyword != "fixture" || app.searchQuery.Offset != 25 || app.searchQuery.Limit != 25 {
		t.Fatalf("account search query=%#v", app.searchQuery)
	}

	response = mutate(http.MethodPost, "/api/v1/accounts", `{"id":"ignored","fakeid":" fixture ","name":" Fixture "}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("account create status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	if app.savedAccount.ID != "" || app.savedAccount.FakeID != "fixture" || app.savedAccount.Name != "Fixture" {
		t.Fatalf("created account=%#v", app.savedAccount)
	}

	response = mutate(http.MethodPatch, "/api/v1/accounts/account-1", `{"id":"wrong","fakeid":"ignored","name":" Updated "}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("account update status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	if app.updatedAccount.ID != "account-1" || app.updatedAccount.Name != "Updated" {
		t.Fatalf("updated account=%#v", app.updatedAccount)
	}

	response = mutate(http.MethodDelete, "/api/v1/accounts", `{"ids":["account-1","account-2"],"confirm":"delete-accounts:account-1,account-2"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("account delete status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	if got := app.deletedAccounts; len(got) != 2 || got[0] != "account-1" || got[1] != "account-2" {
		t.Fatalf("deleted accounts=%#v", got)
	}
}

func TestAccountResolveRoutesUseAuthenticatedWorkspaceFacade(t *testing.T) {
	const articleURL = "https://mp.weixin.qq.com/s/article-fixture"

	t.Run("resolve returns the full account when discovery is authenticated", func(t *testing.T) {
		app := &apiApplication{
			resolvedAccount: domain.Account{ID: "resolved-1", FakeID: "fixture-fakeid", Name: "Fixture Account", Alias: "fixture"},
		}
		server, client := startAPIApplicationServer(t, app)
		base := authorizeAPI(t, client, server.URL())

		response := get(t, client, base+"/api/v1/accounts/resolve?url="+url.QueryEscape(articleURL))
		if response.StatusCode != http.StatusOK {
			t.Fatalf("resolve status=%d body=%s", response.StatusCode, readResponse(t, response))
		}
		if app.resolvedArticleURL != articleURL {
			t.Fatalf("resolve url=%q want %q", app.resolvedArticleURL, articleURL)
		}
		var envelope struct {
			APIVersion string         `json:"apiVersion"`
			Data       domain.Account `json:"data"`
			FakeID     string         `json:"fakeid"`
			Name       string         `json:"name"`
		}
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		if envelope.FakeID != "fixture-fakeid" || envelope.Name != "Fixture Account" {
			t.Fatalf("resolve envelope=%#v", envelope)
		}
	})

	t.Run("resolve maps an expired session to authentication_required", func(t *testing.T) {
		app := &apiApplication{
			resolveAccountErr: wechat.ErrDiscoveryAuthentication,
		}
		server, client := startAPIApplicationServer(t, app)
		base := authorizeAPI(t, client, server.URL())

		response := get(t, client, base+"/api/v1/accounts/resolve?url="+url.QueryEscape(articleURL))
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("resolve status=%d want 401", response.StatusCode)
		}
		assertAPIError(t, response, string(application.WorkspaceErrorAuthentication))
	})

	t.Run("resolve maps an unavailable discovery capability to unavailable", func(t *testing.T) {
		app := &apiApplication{
			resolveAccountErr: application.ErrUnavailable,
		}
		server, client := startAPIApplicationServer(t, app)
		base := authorizeAPI(t, client, server.URL())

		response := get(t, client, base+"/api/v1/accounts/resolve?url="+url.QueryEscape(articleURL))
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("resolve status=%d want 503", response.StatusCode)
		}
		assertAPIError(t, response, string(application.WorkspaceErrorUnavailable))
	})

	t.Run("resolve requires a url query parameter", func(t *testing.T) {
		app := &apiApplication{}
		server, client := startAPIApplicationServer(t, app)
		base := authorizeAPI(t, client, server.URL())

		response := get(t, client, base+"/api/v1/accounts/resolve")
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("resolve status=%d want 400", response.StatusCode)
		}
		response.Body.Close()
		if app.resolvedArticleURL != "" {
			t.Fatalf("resolve called application without url=%q", app.resolvedArticleURL)
		}
	})

	t.Run("resolve-name returns the public account name without a session", func(t *testing.T) {
		app := &apiApplication{resolvedName: "Fixture Account"}
		server, client := startAPIApplicationServer(t, app)
		base := authorizeAPI(t, client, server.URL())

		response := get(t, client, base+"/api/v1/accounts/resolve-name?url="+url.QueryEscape(articleURL))
		if response.StatusCode != http.StatusOK {
			t.Fatalf("resolve-name status=%d body=%s", response.StatusCode, readResponse(t, response))
		}
		if app.resolvedNameURL != articleURL {
			t.Fatalf("resolve-name url=%q want %q", app.resolvedNameURL, articleURL)
		}
		var envelope struct {
			APIVersion string `json:"apiVersion"`
			Data       struct {
				Name string `json:"name"`
			} `json:"data"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		if envelope.Name != "Fixture Account" {
			t.Fatalf("resolve-name envelope=%#v", envelope)
		}
	})

	t.Run("resolve-name requires a url query parameter", func(t *testing.T) {
		app := &apiApplication{}
		server, client := startAPIApplicationServer(t, app)
		base := authorizeAPI(t, client, server.URL())

		response := get(t, client, base+"/api/v1/accounts/resolve-name")
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("resolve-name status=%d want 400", response.StatusCode)
		}
		response.Body.Close()
	})
}

func TestSelectorAndReadableProjectionAPIContracts(t *testing.T) {
	app := &apiApplication{
		accounts: domain.Page[domain.Account]{Items: []domain.Account{
			{ID: "account-1", Name: "Readable", Alias: "alias", FakeID: "private-fakeid", Description: "private-description", AvatarURL: "https://private.example/avatar"},
			{ID: "account-2", FakeID: "private-fakeid-2"},
		}, Total: 102},
		articles: domain.Page[domain.Article]{Items: []domain.Article{
			{ID: "article-1", AccountID: "account-1", Title: "Known owner"},
			{ID: "article-2", AccountID: "account-missing", Title: "Unknown owner"},
		}, Total: 2},
		albums: domain.Page[domain.Album]{Items: []domain.Album{
			{ID: "album-1", AccountID: "account-1", Name: "Album", UpstreamID: "private-upstream", Description: "private-description", ArticleCount: 3},
			{ID: "album-2", AccountID: "account-missing"},
		}, Total: 2},
		jobs: domain.Page[domain.Job]{Items: []domain.Job{{ID: "11111111-1111-1111-1111-111111111111", Kind: "article_download"}}, Total: 1},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/selectors/accounts?search=read&page=2&page_size=25")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("account selector status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	accountBody := readResponse(t, response)
	if app.accountQuery != (domain.AccountQuery{Keyword: "read", Offset: 25, Limit: 25}) {
		t.Fatalf("account selector query=%#v", app.accountQuery)
	}
	for _, required := range []string{`"id":"account-1"`, `"displayName":"Readable"`, `"displayNameAvailable":true`, `"id":"account-2"`, `"displayNameAvailable":false`, `"pageSize":25`} {
		if !strings.Contains(accountBody, required) {
			t.Fatalf("account selector missing %q: %s", required, accountBody)
		}
	}
	for _, forbidden := range []string{"private-fakeid", "fakeid", "private-description", "avatarUrl", "private.example", "syncCursor"} {
		if strings.Contains(accountBody, forbidden) {
			t.Fatalf("account selector leaked %q: %s", forbidden, accountBody)
		}
	}

	response = get(t, client, base+"/api/v1/selectors/albums?search=album&limit=20")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("album selector status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	albumBody := readResponse(t, response)
	for _, required := range []string{`"id":"album-1"`, `"accountId":"account-1"`, `"displayName":"Album"`, `"accountName":"Readable"`, `"accountNameAvailable":true`, `"id":"album-2"`, `"displayNameAvailable":false`, `"accountNameAvailable":false`} {
		if !strings.Contains(albumBody, required) {
			t.Fatalf("album selector missing %q: %s", required, albumBody)
		}
	}
	for _, forbidden := range []string{"private-upstream", "upstreamId", "private-description", "description", "articleCount"} {
		if strings.Contains(albumBody, forbidden) {
			t.Fatalf("album selector leaked %q: %s", forbidden, albumBody)
		}
	}

	response = get(t, client, base+"/api/v1/articles?limit=20")
	articleBody := readResponse(t, response)
	for _, required := range []string{`"accountName":"Readable"`, `"accountNameAvailable":true`, `"accountNameAvailable":false`} {
		if !strings.Contains(articleBody, required) {
			t.Fatalf("article projection missing %q: %s", required, articleBody)
		}
	}
	if strings.Contains(articleBody, `"accountId"`) {
		t.Fatalf("article list exposed account IDs: %s", articleBody)
	}

	response = get(t, client, base+"/api/v1/albums?limit=20")
	legacyAlbumBody := readResponse(t, response)
	for _, required := range []string{`"id":"album-1"`, `"upstreamId":"private-upstream"`, `"accountName":"Readable"`, `"accountNameAvailable":true`} {
		if !strings.Contains(legacyAlbumBody, required) {
			t.Fatalf("additive album response missing %q: %s", required, legacyAlbumBody)
		}
	}

	response = get(t, client, base+"/api/v1/jobs?limit=20")
	jobBody := readResponse(t, response)
	if !strings.Contains(jobBody, `"kind":"article_download"`) || !strings.Contains(jobBody, `"label":"Article Download"`) {
		t.Fatalf("job projection lost compatibility or label: %s", jobBody)
	}

	for _, target := range []string{
		"/api/v1/selectors/accounts?limit=101",
		"/api/v1/selectors/albums?page=1&page_size=101",
	} {
		response = get(t, client, base+target)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		assertAPIError(t, response, "invalid_argument")
	}
}

func TestArticleSelectorAPIForwardsBoundedQueryAndExposesOnlyHumanReadableOptions(t *testing.T) {
	app := &apiApplication{
		accounts: domain.Page[domain.Account]{Items: []domain.Account{
			{ID: "account-1", Name: "Readable account"},
		}},
		articles: domain.Page[domain.Article]{Items: []domain.Article{
			{
				ID: "article-action-1", AccountID: "account-1", Title: "Known owner", Author: "private-author",
				Digest: "private-body", CanonicalURL: "https://private.example/article", CoverURL: "https://private.example/cover",
				Aid: "private-aid", AppMsgID: 42, ItemIndex: 3, MessageType: 1, HasContent: true,
				ReadCount: 100, OldLikeCount: 20, ShareCount: 10, LikeCount: 5, CommentCount: 2,
			},
			{ID: "article-action-2", AccountID: "account-missing", Title: "Unknown owner"},
		}, Total: 17},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/selectors/articles?accountId=account-1&albumId=album-1&search=fixture&author=writer&state=published&publishedFrom=2026-07-01T08%3A00%3A00Z&publishedTo=2026-07-02T08%3A00%3A00Z&hasContent=true&messageType=1&messageType=2&readMin=10&readMax=20&sort=published%3Adesc&page=3&page_size=10")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("article selector status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body := readResponse(t, response)
	var selectorResponse struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &selectorResponse); err != nil {
		t.Fatal(err)
	}
	if len(selectorResponse.Data) != 2 || !reflect.DeepEqual(selectorResponse.Data[0], map[string]any{
		"id":                   "article-action-1",
		"title":                "Known owner",
		"accountName":          "Readable account",
		"accountNameAvailable": true,
	}) {
		t.Fatalf("article selector projection = %#v", selectorResponse.Data)
	}
	if _, ok := selectorResponse.Data[1]["accountName"]; ok || selectorResponse.Data[1]["accountNameAvailable"] != false {
		t.Fatalf("article selector fallback = %#v", selectorResponse.Data[1])
	}
	if app.articleQuery.AccountID != "account-1" || app.articleQuery.AlbumID != "album-1" || app.articleQuery.Keyword != "fixture" || app.articleQuery.Author != "writer" || app.articleQuery.State != "published" ||
		app.articleQuery.Offset != 20 || app.articleQuery.Limit != 10 || !app.articleQuery.PublishedFrom.Equal(time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)) || !app.articleQuery.PublishedTo.Equal(time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)) ||
		app.articleQuery.HasContent == nil || !*app.articleQuery.HasContent || !reflect.DeepEqual(app.articleQuery.MessageTypes, []int{1, 2}) || app.articleQuery.ReadMin == nil || *app.articleQuery.ReadMin != 10 || app.articleQuery.ReadMax == nil || *app.articleQuery.ReadMax != 20 ||
		!reflect.DeepEqual(app.articleQuery.Sorts, []domain.ArticleSort{{Field: "published", Direction: domain.SortDescending}}) {
		t.Fatalf("article selector query = %#v", app.articleQuery)
	}
	for _, required := range []string{
		`"id":"article-action-1"`, `"title":"Known owner"`, `"accountName":"Readable account"`, `"accountNameAvailable":true`,
		`"id":"article-action-2"`, `"title":"Unknown owner"`, `"accountNameAvailable":false`, `"page":3`, `"pageSize":10`, `"total":17`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("article selector missing %q: %s", required, body)
		}
	}
	for _, forbidden := range []string{
		`"accountId"`, "private-body", "private-author", "private-aid", "canonicalUrl", "coverUrl", "digest", "appmsgId",
		"messageType", "readCount", "oldLikeCount", "shareCount", "likeCount", "commentCount", "hasContent", "albums",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("article selector leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"id":"article-action-1"`) {
		t.Fatalf("article selector changed stable action ID: %s", body)
	}

	for _, target := range []string{
		"/api/v1/selectors/articles?limit=101",
		"/api/v1/selectors/articles?offset=-1&limit=10",
		"/api/v1/selectors/articles?offset=100001&limit=10",
		"/api/v1/selectors/articles?page=10002&page_size=10",
		"/api/v1/selectors/articles?page=0&page_size=10",
		"/api/v1/selectors/articles?deleted=maybe",
		"/api/v1/selectors/articles?readMin=20&readMax=10",
		"/api/v1/selectors/articles?unknown=1",
	} {
		response = get(t, client, base+target)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		assertAPIError(t, response, "invalid_argument")
	}
}

func TestWorkspaceListQueriesRejectOverlongTextBeforeReachingApplication(t *testing.T) {
	app := &apiApplication{}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())
	longSearch := strings.Repeat("x", maximumWorkspaceQueryTextLength+1)

	for _, target := range []string{
		"/api/v1/accounts?search=" + longSearch,
		"/api/v1/albums?keyword=" + longSearch,
		"/api/v1/selectors/articles?search=" + longSearch,
	} {
		response := get(t, client, base+target)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		assertAPIError(t, response, "invalid_argument")
	}
	if app.accountQueryCalls != 0 || app.albumQueryCalls != 0 || app.articleQueryCalls != 0 {
		t.Fatalf("overlong query reached application: account calls=%d album calls=%d article calls=%d", app.accountQueryCalls, app.albumQueryCalls, app.articleQueryCalls)
	}
}

func TestArticleSelectorRequiresSessionAndOnlyPermitsGET(t *testing.T) {
	app := &apiApplication{}
	server, client := startAPIApplicationServer(t, app)
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")

	response := get(t, client, base+"/api/v1/selectors/articles")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated selector status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "authentication_required")
	if !reflect.DeepEqual(app.articleQuery, domain.ArticleQuery{}) {
		t.Fatalf("unauthenticated selector reached application: %#v", app.articleQuery)
	}

	base = authorizeAPI(t, client, server.URL())
	request := requestWith(t, http.MethodPost, base+"/api/v1/selectors/articles", strings.NewReader(`{}`), map[string]string{"Content-Type": "application/json"})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("POST selector status=%d allow=%q body=%s", response.StatusCode, response.Header.Get("Allow"), readResponse(t, response))
	}
	assertAPIError(t, response, "method_not_allowed")
	if !reflect.DeepEqual(app.articleQuery, domain.ArticleQuery{}) {
		t.Fatalf("POST selector reached application: %#v", app.articleQuery)
	}
}

func TestAccountAPIRejectsInvalidMutationInputsBeforeWorkspaceCalls(t *testing.T) {
	app := &apiApplication{}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	for _, request := range []*http.Request{
		requestWith(t, http.MethodPost, base+"/api/v1/accounts", strings.NewReader(`{"fakeid":"fixture"}`), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf}),
		requestWith(t, http.MethodDelete, base+"/api/v1/accounts", strings.NewReader(`{"ids":["account-1"],"confirm":"wrong"}`), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf}),
		requestWith(t, http.MethodPost, base+"/api/v1/accounts", strings.NewReader(`{"fakeid":"fixture","name":"Fixture"}`), map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json", "X-CSRF-Token": csrf}),
	} {
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusBadRequest && response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("invalid account mutation status=%d body=%s", response.StatusCode, readResponse(t, response))
		}
		assertAPIError(t, response, map[int]string{http.StatusBadRequest: "invalid_argument", http.StatusForbidden: "forbidden", http.StatusMethodNotAllowed: "method_not_allowed"}[response.StatusCode])
	}
	if app.savedAccount != (domain.Account{}) || app.updatedAccount != (domain.Account{}) || len(app.deletedAccounts) != 0 {
		t.Fatalf("invalid account mutation reached application: %#v", app)
	}
}

func TestArticlePreviewUsesSafeWorkspaceHandoff(t *testing.T) {
	app := &apiApplication{article: domain.Article{ID: "article-1", Title: "Fixture", HasContent: true}}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())
	response := get(t, client, base+"/api/v1/articles/preview?articleId=article-1")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	var preview application.WorkspaceArticlePreview
	if err := json.NewDecoder(response.Body).Decode(&preview); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if preview.ArticleID != "article-1" || preview.Title != "Fixture" || !preview.Available || preview.DocumentURL != "/api/v1/articles/preview/document?articleId=article-1" {
		t.Fatalf("preview = %#v", preview)
	}
	response = get(t, client, base+"/api/v1/articles/preview")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing article ID status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
}

func TestSavedQueryCRUDUsesWorkspaceFacadeAndExactDeleteConfirmation(t *testing.T) {
	app := &apiApplication{}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	mutate := func(method, path, body string) *http.Response {
		request := requestWith(t, method, base+path, strings.NewReader(body), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	response := mutate(http.MethodPost, "/api/v1/saved-queries", `{"name":" recent ","query":{"keyword":" fixture ","offset":25,"limit":10}}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	if app.savedName != "recent" || app.savedQuery.Keyword != "fixture" || app.savedQuery.Offset != 0 || app.savedQuery.Limit != 0 {
		t.Fatalf("saved query was not normalized: name=%q query=%#v", app.savedName, app.savedQuery)
	}

	response = mutate(http.MethodDelete, "/api/v1/saved-queries", `{"name":"recent","confirm":"wrong"}`)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong confirmation status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
	response = mutate(http.MethodDelete, "/api/v1/saved-queries", `{"name":"recent","confirm":"delete-saved-query:recent"}`)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	if app.deletedSavedName != "recent" {
		t.Fatalf("deleted saved query = %q", app.deletedSavedName)
	}
}

func TestArticlePreviewDocumentRequiresRendererAndSetsRestrictiveHeaders(t *testing.T) {
	app := &apiApplication{article: domain.Article{ID: "article-1", Title: "Fixture", HasContent: true}}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())
	response := get(t, client, base+"/api/v1/articles/preview/document?articleId=article-1")
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("missing renderer status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "unavailable")
}

func TestArticlePreviewDocumentUsesStrictContentSecurityPolicy(t *testing.T) {
	app := &apiApplication{article: domain.Article{ID: "article-1", Title: "Fixture", HasContent: true}}
	server, client := startAPIApplicationServerWithPreview(t, app, previewRenderer{articleID: "article-1", html: "<!doctype html><p>fixture</p>"})
	base := authorizeAPI(t, client, server.URL())
	response := get(t, client, base+"/api/v1/articles/preview/document?articleId=article-1")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	policy := response.Header.Get("Content-Security-Policy")
	response.Body.Close()
	if strings.Contains(policy, "unsafe-inline") || !strings.Contains(policy, "style-src 'none'") || !strings.Contains(policy, "default-src 'none'") {
		t.Fatalf("unsafe preview policy = %q", policy)
	}
}

func applicationLoginFlow() wechat.LoginFlow {
	return wechat.LoginFlow{SessionID: "login-flow", QRBytes: []byte("sanitized-fixture")}
}

func TestReadAPIAdaptsExistingBrowserClientDTO(t *testing.T) {
	app := &apiApplication{
		runtime:  domain.RuntimeStatus{Version: "fixture", Profile: "fixture-profile", Storage: domain.StorageStatus{Articles: 2}},
		articles: domain.Page[domain.Article]{Items: []domain.Article{{ID: "article-1", Title: "Fixture"}}, Total: 1},
		jobs:     domain.Page[domain.Job]{Items: []domain.Job{{ID: "job-1", Kind: "sync"}}, Total: 1},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/articles?page=2&page_size=25&search=fixture&sort=publishedAt&direction=desc")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("article status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	var page struct {
		APIVersion string           `json:"apiVersion"`
		Data       []domain.Article `json:"data"`
		Pagination struct {
			Page     int `json:"page"`
			PageSize int `json:"pageSize"`
			Total    int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if page.APIVersion != apiVersion || len(page.Data) != 1 || page.Pagination.Page != 2 || page.Pagination.PageSize != 25 || page.Pagination.Total != 1 {
		t.Fatalf("article DTO = %#v", page)
	}
	if app.articleQuery.Keyword != "fixture" || app.articleQuery.Offset != 25 || app.articleQuery.Limit != 25 || len(app.articleQuery.Sorts) != 1 || app.articleQuery.Sorts[0] != (domain.ArticleSort{Field: "published", Direction: domain.SortDescending}) {
		t.Fatalf("browser article query = %#v", app.articleQuery)
	}

	response = get(t, client, base+"/api/v1/snapshot")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("snapshot status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	var snapshot struct {
		APIVersion string `json:"apiVersion"`
		Runtime    struct {
			Profile string `json:"profile"`
		} `json:"runtime"`
		Storage domain.StorageStatus `json:"storage"`
		Jobs    struct {
			Items []domain.Job `json:"items"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if snapshot.APIVersion != apiVersion || snapshot.Runtime.Profile != "fixture-profile" || snapshot.Storage.Articles != 2 || len(snapshot.Jobs.Items) != 1 {
		t.Fatalf("snapshot DTO = %#v", snapshot)
	}
}

func startAPIApplicationServer(t *testing.T, app application.Application) (*Server, *http.Client) {
	return startAPIApplicationServerWithPreview(t, app, nil)
}

func startAPIApplicationServerWithPreview(t *testing.T, app application.Application, preview application.WorkspaceArticlePreviewRenderer) (*Server, *http.Client) {
	t.Helper()
	server, err := New(Options{Application: app, Preview: preview, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background()) }()
	t.Cleanup(func() {
		_ = server.Close()
		if err := <-done; err != nil {
			t.Errorf("server stopped with error: %v", err)
		}
	})
	return server, newTestClient(t)
}

type previewRenderer struct {
	articleID domain.ArticleID
	html      string
}

func (renderer previewRenderer) RenderArticlePreview(context.Context, domain.ArticleID) (application.WorkspaceRenderedArticlePreview, error) {
	return application.WorkspaceRenderedArticlePreview{ArticleID: renderer.articleID, HTML: []byte(renderer.html)}, nil
}

func newTestClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return nil }}
}

func authorizeAPI(t *testing.T, client *http.Client, bootstrapURL string) string {
	t.Helper()
	authorize(t, client, bootstrapURL)
	return strings.TrimSuffix(strings.Split(bootstrapURL, "?")[0], "/")
}

func assertAPIError(t *testing.T, response *http.Response, code string) {
	t.Helper()
	var envelope apiErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if envelope.APIVersion != apiVersion || envelope.Error.Code != code || envelope.Error.Message == "" {
		t.Fatalf("error envelope = %#v", envelope)
	}
}

type apiApplication struct {
	testApplication
	runtime              domain.RuntimeStatus
	session              wechat.Session
	accounts             domain.Page[domain.Account]
	articles             domain.Page[domain.Article]
	albums               domain.Page[domain.Album]
	jobs                 domain.Page[domain.Job]
	saved                []domain.SavedArticleQuery
	job                  domain.Job
	jobDetail            application.WorkspaceJobDetail
	article              domain.Article
	resourceAvailability library.ArticleResourceAvailability
	resourceArticleID    domain.ArticleID
	metrics              library.ArticleMetrics
	resourceDetails      domain.Page[library.ArticleResourceDetail]
	detailArticleID      domain.ArticleID
	detailOffset         int
	detailLimit          int
	comments             domain.Page[library.CommentRecord]
	replies              domain.Page[library.ReplyRecord]
	pendingReplyThreads  []library.ReplyThread
	commentsArticleID    domain.ArticleID
	commentsOffset       int
	commentsLimit        int
	repliesArticleID     domain.ArticleID
	repliesCommentID     string
	repliesOffset        int
	repliesLimit         int
	accountsErr          error
	accountQueryCalls    int
	articleQueryCalls    int
	accountQuery         domain.AccountQuery
	articleQuery         domain.ArticleQuery
	albumQuery           domain.AlbumQuery
	albumQueryCalls      int
	jobQuery             domain.JobQuery
	loginFlow            wechat.LoginFlow
	poll                 wechat.PollResult
	completed            wechat.Session
	loginSessionID       string
	account              domain.Account
	searchAccounts       domain.Page[domain.Account]
	searchQuery          domain.AccountQuery
	resolvedArticleURL   string
	resolvedAccount      domain.Account
	resolveAccountErr    error
	resolvedNameURL      string
	resolvedName         string
	resolveNameErr       error
	savedAccount         domain.Account
	updatedAccount       domain.Account
	deletedAccounts      []domain.AccountID
	syncRequest          domain.SynchronizeAccountRequest
	downloadRequests     []domain.DownloadRequest
	albumRequest         application.WorkspaceAlbumTraversalRequest
	multiAlbumRequest    []domain.AlbumID
	albumBatch           bool
	loggedOut            bool
	savedName            string
	savedQuery           domain.ArticleQuery
	deletedSavedName     string
	switchable           []wechat.SwitchableAccount
	switchableErr        error
	switched             wechat.Session
	switchedAccountID    string
}

func (app *apiApplication) BeginLogin(_ context.Context, id string) (wechat.LoginFlow, error) {
	app.loginSessionID = id
	return app.loginFlow, nil
}
func (app *apiApplication) PollLogin(context.Context) (wechat.PollResult, error) {
	return app.poll, nil
}
func (app *apiApplication) CompleteLogin(context.Context) (wechat.Session, error) {
	return app.completed, nil
}
func (app *apiApplication) Logout(context.Context) error { app.loggedOut = true; return nil }
func (app *apiApplication) SaveAccount(_ context.Context, account domain.Account) (domain.Account, error) {
	app.savedAccount = account
	return app.account, nil
}
func (app *apiApplication) UpdateAccount(_ context.Context, account domain.Account) (domain.Account, error) {
	app.updatedAccount = account
	return app.account, nil
}
func (app *apiApplication) DeleteAccounts(_ context.Context, ids []domain.AccountID) (domain.AccountDeleteReport, error) {
	app.deletedAccounts = append([]domain.AccountID(nil), ids...)
	return domain.AccountDeleteReport{AccountsDeleted: 1}, nil
}
func (app *apiApplication) SynchronizeAccount(_ context.Context, request domain.SynchronizeAccountRequest) (domain.Job, error) {
	app.syncRequest = request
	return app.job, nil
}
func (app *apiApplication) StartDownload(_ context.Context, request domain.DownloadRequest) (domain.Job, error) {
	app.downloadRequests = append(app.downloadRequests, request)
	return app.job, nil
}
func (app *apiApplication) SaveArticleQuery(_ context.Context, name string, query domain.ArticleQuery) (domain.SavedArticleQuery, error) {
	app.savedName, app.savedQuery = name, query
	return domain.SavedArticleQuery{Name: name, Query: query}, nil
}
func (app *apiApplication) DeleteSavedArticleQuery(_ context.Context, name string) (bool, error) {
	app.deletedSavedName = name
	return true, nil
}
func (app *apiApplication) SynchronizeAlbum(_ context.Context, accountID domain.AccountID, albumID domain.AlbumID) (domain.Job, error) {
	app.albumRequest = application.WorkspaceAlbumTraversalRequest{AccountID: accountID, AlbumID: albumID}
	return app.job, nil
}
func (app *apiApplication) SynchronizeAlbumAndDownload(_ context.Context, accountID domain.AccountID, albumID domain.AlbumID) (domain.Job, error) {
	app.albumRequest = application.WorkspaceAlbumTraversalRequest{AccountID: accountID, AlbumID: albumID, Download: true}
	app.albumBatch = true
	return app.job, nil
}
func (app *apiApplication) SynchronizeAlbumWithOrder(_ context.Context, accountID domain.AccountID, albumID domain.AlbumID, order wechat.AlbumOrder) (domain.Job, error) {
	app.albumRequest = application.WorkspaceAlbumTraversalRequest{AccountID: accountID, AlbumID: albumID, Order: order}
	return app.job, nil
}
func (app *apiApplication) SynchronizeAlbumWithOrderAndDownload(_ context.Context, accountID domain.AccountID, albumID domain.AlbumID, order wechat.AlbumOrder) (domain.Job, error) {
	app.albumRequest = application.WorkspaceAlbumTraversalRequest{AccountID: accountID, AlbumID: albumID, Order: order, Download: true}
	app.albumBatch = true
	return app.job, nil
}
func (app *apiApplication) SynchronizeAlbumsWithOrder(_ context.Context, albumIDs []domain.AlbumID, _ wechat.AlbumOrder, download bool) (domain.Job, error) {
	app.multiAlbumRequest = append([]domain.AlbumID(nil), albumIDs...)
	app.albumBatch = download
	return app.job, nil
}
func (app *apiApplication) PauseJob(context.Context, domain.JobID) (domain.Job, error) {
	return app.job, nil
}
func (app *apiApplication) ResumeJob(context.Context, domain.JobID) (domain.Job, error) {
	return app.job, nil
}
func (app *apiApplication) RetryJob(context.Context, domain.JobID) (domain.Job, error) {
	return app.job, nil
}
func (app *apiApplication) CancelJob(context.Context, domain.JobID) (domain.Job, error) {
	return app.job, nil
}

func (app *apiApplication) RuntimeStatus(context.Context) (domain.RuntimeStatus, error) {
	return app.runtime, nil
}
func (app *apiApplication) SessionStatus(context.Context) (wechat.Session, error) {
	return app.session, nil
}
func (app *apiApplication) ListSwitchableAccounts(context.Context) ([]wechat.SwitchableAccount, error) {
	return append([]wechat.SwitchableAccount(nil), app.switchable...), app.switchableErr
}
func (app *apiApplication) SwitchAccount(_ context.Context, accountID string) (wechat.Session, error) {
	app.switchedAccountID = accountID
	return app.switched, nil
}
func (app *apiApplication) QueryAccounts(_ context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	app.accountQueryCalls++
	app.accountQuery = query
	page := app.accounts
	page.Offset, page.Limit = query.Offset, query.Limit
	return page, app.accountsErr
}
func (app *apiApplication) AccountNames(_ context.Context, ids []domain.AccountID) (map[domain.AccountID]string, error) {
	names := make(map[domain.AccountID]string)
	for _, id := range ids {
		for _, account := range app.accounts.Items {
			if account.ID == id {
				names[id] = account.Name
			}
		}
	}
	return names, nil
}
func (app *apiApplication) SearchAccounts(_ context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	app.searchQuery = query
	page := app.searchAccounts
	page.Offset, page.Limit = query.Offset, query.Limit
	return page, nil
}
func (app *apiApplication) ResolveAccountName(_ context.Context, articleURL string) (string, error) {
	app.resolvedNameURL = articleURL
	return app.resolvedName, app.resolveNameErr
}
func (app *apiApplication) ResolveAccountFromArticle(_ context.Context, articleURL string) (domain.Account, error) {
	app.resolvedArticleURL = articleURL
	return app.resolvedAccount, app.resolveAccountErr
}
func (app *apiApplication) QueryArticles(_ context.Context, query domain.ArticleQuery) (domain.Page[domain.Article], error) {
	app.articleQueryCalls++
	app.articleQuery = query
	page := app.articles
	page.Offset, page.Limit = query.Offset, query.Limit
	return page, nil
}
func (app *apiApplication) GetArticle(context.Context, domain.ArticleID) (domain.Article, error) {
	return app.article, nil
}
func (app *apiApplication) ArticleResourceAvailability(_ context.Context, id domain.ArticleID) (library.ArticleResourceAvailability, error) {
	app.resourceArticleID = id
	availability := app.resourceAvailability
	availability.ArticleID = id
	return availability, nil
}
func (app *apiApplication) LatestArticleMetrics(_ context.Context, id domain.ArticleID) (library.ArticleMetrics, error) {
	app.detailArticleID = id
	return app.metrics, nil
}
func (app *apiApplication) ListArticleResourceDetails(_ context.Context, id domain.ArticleID, offset, limit int) (domain.Page[library.ArticleResourceDetail], error) {
	app.detailArticleID, app.detailOffset, app.detailLimit = id, offset, limit
	page := app.resourceDetails
	page.Offset, page.Limit = offset, limit
	return page, nil
}
func (app *apiApplication) ListArticleComments(_ context.Context, id domain.ArticleID, offset, limit int) (domain.Page[library.CommentRecord], error) {
	app.commentsArticleID, app.commentsOffset, app.commentsLimit = id, offset, limit
	page := app.comments
	page.Offset, page.Limit = offset, limit
	return page, nil
}
func (app *apiApplication) PendingArticleReplyThreads(context.Context, domain.ArticleID) ([]library.ReplyThread, error) {
	return append([]library.ReplyThread(nil), app.pendingReplyThreads...), nil
}
func (app *apiApplication) ListArticleCommentReplies(_ context.Context, id domain.ArticleID, commentID string, offset, limit int) (domain.Page[library.ReplyRecord], error) {
	app.repliesArticleID, app.repliesCommentID, app.repliesOffset, app.repliesLimit = id, commentID, offset, limit
	page := app.replies
	page.Offset, page.Limit = offset, limit
	return page, nil
}
func (app *apiApplication) QueryAlbums(_ context.Context, query domain.AlbumQuery) (domain.Page[domain.Album], error) {
	app.albumQueryCalls++
	app.albumQuery = query
	page := app.albums
	page.Offset, page.Limit = query.Offset, query.Limit
	return page, nil
}
func (app *apiApplication) ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error) {
	return app.saved, nil
}
func (app *apiApplication) QueryJobs(_ context.Context, query domain.JobQuery) (domain.Page[domain.Job], error) {
	app.jobQuery = query
	return app.jobs, nil
}
func (app *apiApplication) GetJob(context.Context, domain.JobID) (domain.Job, error) {
	return app.job, nil
}
func (app *apiApplication) JobDetails(context.Context, domain.JobID) (application.WorkspaceJobDetail, error) {
	return app.jobDetail, nil
}

var _ application.Application = (*apiApplication)(nil)
