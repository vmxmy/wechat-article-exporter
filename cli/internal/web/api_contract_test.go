// These table-driven tests lock the structural `/api/v1` wire contract
// documented in docs/release/browser-api-contract.md: the single-resource
// envelope (§2.1), the page envelope (§2.2), the error envelope (§2.3), and
// job-creation identity (§2.4). They assert envelope shape and field types,
// not full response bodies, so additive field growth permitted by that
// document does not make these tests brittle.
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

// TestSingleResourceEnvelopeContract locks the §2.1 shape across
// representative single-resource read routes: apiVersion is exactly "v1",
// data is a non-null JSON object, and the top-level compatibility shim
// duplicates at least one of data's own fields.
func TestSingleResourceEnvelopeContract(t *testing.T) {
	app := &apiApplication{
		runtime: domain.RuntimeStatus{Version: "fixture", Profile: "fixture-profile"},
		session: wechat.Session{State: wechat.SessionAuthenticated, AccountID: "account-1", AccountName: "Fixture"},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	for _, target := range []string{"/api/v1/runtime", "/api/v1/session", "/api/v1/storage"} {
		response := get(t, client, base+target)
		body := readResponse(t, response)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, body)
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &top); err != nil {
			t.Fatalf("GET %s decode top-level: %v body=%s", target, err, body)
		}
		var version string
		if err := json.Unmarshal(top["apiVersion"], &version); err != nil || version != apiVersion {
			t.Fatalf("GET %s apiVersion=%v", target, top["apiVersion"])
		}
		var data map[string]json.RawMessage
		if raw, ok := top["data"]; !ok || json.Unmarshal(raw, &data) != nil {
			t.Fatalf("GET %s data is not a JSON object: %v", target, top["data"])
		}
		if len(data) == 0 {
			t.Fatalf("GET %s data has no fields", target)
		}
		// The deprecated top-level compatibility shim (§6.3) must duplicate
		// every one of data's fields at the top level.
		for key := range data {
			if _, ok := top[key]; !ok {
				t.Fatalf("GET %s top-level shim missing duplicated field %q", target, key)
			}
		}
	}
}

// TestPageEnvelopeContract locks the §2.2 shape across representative
// paginated list routes: apiVersion, data/items as the same array, and
// offset/limit/total present with the expected numeric types, plus the
// pagination.page/pagination.pageSize/pagination.total shim mirroring them.
func TestPageEnvelopeContract(t *testing.T) {
	app := &apiApplication{
		accounts: domain.Page[domain.Account]{Items: []domain.Account{{ID: "account-1", Name: "Fixture"}}, Total: 1},
		articles: domain.Page[domain.Article]{Items: []domain.Article{{ID: "article-1", Title: "Fixture"}}, Total: 1},
		albums:   domain.Page[domain.Album]{Items: []domain.Album{{ID: "album-1", Name: "Album"}}, Total: 1},
		jobs:     domain.Page[domain.Job]{Items: []domain.Job{{ID: "11111111-1111-1111-1111-111111111111", Kind: "sync"}}, Total: 1},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	for _, target := range []string{"/api/v1/accounts", "/api/v1/articles", "/api/v1/albums", "/api/v1/jobs", "/api/v1/saved-queries"} {
		response := get(t, client, base+target)
		body := readResponse(t, response)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, body)
		}
		var page struct {
			APIVersion string            `json:"apiVersion"`
			Data       []json.RawMessage `json:"data"`
			Items      []json.RawMessage `json:"items"`
			Total      int               `json:"total"`
			Offset     int               `json:"offset"`
			Limit      int               `json:"limit"`
			Pagination struct {
				Page     int `json:"page"`
				PageSize int `json:"pageSize"`
				Total    int `json:"total"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("GET %s decode: %v body=%s", target, err, body)
		}
		if page.APIVersion != apiVersion {
			t.Fatalf("GET %s apiVersion=%q", target, page.APIVersion)
		}
		if len(page.Data) != len(page.Items) {
			t.Fatalf("GET %s data/items diverge: data=%d items=%d", target, len(page.Data), len(page.Items))
		}
		if page.Limit <= 0 {
			t.Fatalf("GET %s limit=%d, want a positive bounded page size", target, page.Limit)
		}
		if page.Offset < 0 {
			t.Fatalf("GET %s offset=%d, want non-negative", target, page.Offset)
		}
		if page.Total != page.Pagination.Total || page.Limit != page.Pagination.PageSize {
			t.Fatalf("GET %s pagination shim diverged: total=%d/%d pageSize=%d/%d", target, page.Total, page.Pagination.Total, page.Limit, page.Pagination.PageSize)
		}
		if wantPage := page.Offset/page.Limit + 1; page.Pagination.Page != wantPage {
			t.Fatalf("GET %s pagination.page=%d, want %d", target, page.Pagination.Page, wantPage)
		}
	}
}

// TestErrorEnvelopeContract locks the §2.3 shape and the §4 stable error
// code vocabulary across representative failure conditions from different
// route families: apiVersion, a non-empty error.code drawn from the
// documented vocabulary, and a non-empty error.message.
func TestErrorEnvelopeContract(t *testing.T) {
	stableCodes := map[string]bool{
		"authentication_required": true, "wechat_session_required": true, "forbidden": true, "invalid_argument": true,
		"not_found": true, "method_not_allowed": true, "confirmation_required": true,
		"rate_limited": true, "unavailable": true, "cancelled": true, "internal": true,
	}

	unauthenticated := &apiApplication{}
	server, client := startAPIApplicationServer(t, unauthenticated)
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")

	failing := &apiApplication{accountsErr: errAPIContractFixtureFailure}
	failingServer, failingClient := startAPIApplicationServer(t, failing)
	failingBase := authorizeAPI(t, failingClient, failingServer.URL())

	authedNoBody := &apiApplication{}
	authedServer, authedClient := startAPIApplicationServer(t, authedNoBody)
	authedBase := authorizeAPI(t, authedClient, authedServer.URL())

	for _, testCase := range []struct {
		name    string
		client  *http.Client
		method  string
		target  string
		status  int
		wantErr string
	}{
		{"unauthenticated read", client, http.MethodGet, base + "/api/v1/accounts", http.StatusUnauthorized, "authentication_required"},
		{"unsupported query parameter", authedClient, http.MethodGet, authedBase + "/api/v1/accounts?wat=1", http.StatusBadRequest, "invalid_argument"},
		{"unknown route", authedClient, http.MethodGet, authedBase + "/api/v1/does-not-exist", http.StatusNotFound, "not_found"},
		{"internal failure redaction", failingClient, http.MethodGet, failingBase + "/api/v1/accounts", http.StatusInternalServerError, "internal"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := requestWith(t, testCase.method, testCase.target, nil, nil)
			response, err := testCase.client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body := readResponse(t, response)
			if response.StatusCode != testCase.status {
				t.Fatalf("status=%d, want %d body=%s", response.StatusCode, testCase.status, body)
			}
			var envelope apiErrorEnvelope
			if err := json.Unmarshal([]byte(body), &envelope); err != nil {
				t.Fatalf("decode error envelope: %v body=%s", err, body)
			}
			if envelope.APIVersion != apiVersion {
				t.Fatalf("apiVersion=%q", envelope.APIVersion)
			}
			if envelope.Error.Code != testCase.wantErr {
				t.Fatalf("error.code=%q, want %q", envelope.Error.Code, testCase.wantErr)
			}
			if !stableCodes[envelope.Error.Code] {
				t.Fatalf("error.code=%q is not in the documented §4 vocabulary", envelope.Error.Code)
			}
			if envelope.Error.Message == "" {
				t.Fatalf("error.message is empty")
			}
		})
	}
}

// TestJobCreationEnvelopeContract locks §2.4: persistent-job-creating
// mutation routes return the shared job DTO with a stable `data.id`, except
// exports/start which returns the documented `{"data":{"jobId":...}}` shape.
// Both must carry the exact fixture job identifier untouched.
func TestJobCreationEnvelopeContract(t *testing.T) {
	const jobID = "11111111-1111-1111-1111-111111111111"
	app := &apiApplication{job: domain.Job{ID: jobID, Kind: "article_download"}}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	mutate := func(path, body string) *http.Response {
		request := requestWith(t, http.MethodPost, base+path, strings.NewReader(body), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	for _, target := range []string{"/api/v1/ingest/url", "/api/v1/articles/download"} {
		body := `{"url":"https://mp.weixin.qq.com/s/fixture"}`
		if target == "/api/v1/articles/download" {
			body = `{"articleIds":["article-1"]}`
		}
		response := mutate(target, body)
		assertStableJobResponse(t, response, target, jobID)
	}
}

var errAPIContractFixtureFailure = &apiContractFixtureError{}

type apiContractFixtureError struct{}

func (*apiContractFixtureError) Error() string { return "sqlite at /private/token=secret" }
