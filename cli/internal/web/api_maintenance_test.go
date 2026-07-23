package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

func TestMaintenanceAPIFailsClosedWithoutExplicitServices(t *testing.T) {
	server, client := startAPIApplicationServer(t, &apiApplication{})
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	for _, target := range []string{"/api/v1/settings/credentials", "/api/v1/settings/proxies", "/api/v1/settings/preferences", "/api/v1/maintenance/integrity", "/api/v1/maintenance/diagnostics"} {
		response := get(t, client, base+target)
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		assertAPIError(t, response, "unavailable")
	}
	request := maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/backups", `{}`, csrf)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("POST backup status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "unavailable")
}

func TestMaintenanceAPIUsesFacadesAndNeverEchoesSecretsOrPaths(t *testing.T) {
	credentials := &webCredentialMaintenance{items: []application.CredentialMetadata{{ID: "credential-1", AccountID: "account-1", Kind: "wechat", Status: "valid"}}, imported: application.CredentialMetadata{ID: "credential-2"}}
	proxies := &webProxyMaintenance{routes: []application.ProxyRoute{{ID: "proxy-1", Endpoint: "https://proxy.test/?token=not-for-output"}}}
	preferences := &webPreferencesMaintenance{preferences: application.Preferences{Download: application.DownloadPreferences{Concurrency: 4}}}
	storage := &webStorageMaintenance{
		receipt:      application.BackupReceipt{ID: "backup-1", SHA256: "digest", Omitted: []string{"Cookie: sid=backup-secret /private/archive.zip"}},
		verification: application.BackupVerification{Valid: false, Failures: []string{"/private/archive.zip appmsg_token=verify-secret"}},
		integrity:    application.IntegrityReport{Issues: []application.IntegrityIssue{{Kind: "missing", Message: "/private/data Cookie: sid=integrity-secret"}}},
		diagnostics:  application.DiagnosticsReport{Checks: []application.DiagnosticCheck{{Name: "storage", Status: "degraded", Summary: "/private/data appmsg_token=diagnostic-secret"}}},
	}
	server, client := startMaintenanceServer(t, application.NewMaintenance(application.MaintenanceOptions{Credentials: credentials, Proxies: proxies, Preferences: preferences}), application.NewMaintenanceStorage(application.MaintenanceStorageOptions{Backups: storage, Integrity: storage, Diagnostics: storage}))
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	for _, target := range []string{"/api/v1/settings/credentials", "/api/v1/settings/proxies", "/api/v1/settings/preferences", "/api/v1/maintenance/integrity", "/api/v1/maintenance/diagnostics"} {
		response := get(t, client, base+target)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		body := readResponse(t, response)
		for _, forbidden := range []string{"not-for-output", "/private/data", "integrity-secret", "diagnostic-secret"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("GET %s leaked %q: %s", target, forbidden, body)
			}
		}
	}

	response := doMaintenance(t, client, maintenanceRequest(t, http.MethodPost, base+"/api/v1/settings/credentials/import", `{"nickname":"fixture","biz":"biz-secret","uin":"uin-secret","key":"key-secret","cookie":"cookie-secret"}`, csrf))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("credential import status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	if body := readResponse(t, response); strings.Contains(body, "secret") || strings.Contains(body, "cookie") {
		t.Fatalf("credential response leaked input: %s", body)
	}
	if credentials.request.Cookie != "cookie-secret" {
		t.Fatalf("credential facade did not receive write-only input: %#v", credentials.request)
	}

	response = doMaintenance(t, client, maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/backups", `{}`, csrf))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("backup status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	if body := readResponse(t, response); strings.Contains(body, "backup-secret") || strings.Contains(body, "/private") {
		t.Fatalf("backup response leaked sensitive content: %s", body)
	}
	response = doMaintenance(t, client, maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/backups/verify", `{"backupId":"backup-1"}`, csrf))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("backup verification status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	if body := readResponse(t, response); strings.Contains(body, "verify-secret") || strings.Contains(body, "/private") {
		t.Fatalf("backup verification leaked sensitive content: %s", body)
	}
}

func TestMaintenanceAPIStrictMutationProtectionBoundedInputAndConfirmation(t *testing.T) {
	storage := &webStorageMaintenance{plan: application.GarbageCollectionPlan{ID: "gc-plan-1", Confirmation: "gc-proof-1", ExpiresAt: time.Now().Add(time.Minute)}}
	server, client := startMaintenanceServer(t, application.NewMaintenance(application.MaintenanceOptions{}), application.NewMaintenanceStorage(application.MaintenanceStorageOptions{Garbage: storage}))
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	for _, request := range []*http.Request{
		maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/gc/plan", `{}`, "wrong"),
		requestWith(t, http.MethodPost, base+"/api/v1/maintenance/gc/plan", strings.NewReader(`{}`), map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json", "X-CSRF-Token": csrf}),
	} {
		response := doMaintenance(t, client, request)
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("protected mutation status=%d body=%s", response.StatusCode, readResponse(t, response))
		}
		response.Body.Close()
	}
	response := doMaintenance(t, client, maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/gc/plan", `{}`, csrf))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GC plan status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	response = doMaintenance(t, client, maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/gc/apply", `{"planId":"gc-plan-1","confirmation":"wrong"}`, csrf))
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad GC confirmation status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "confirmation_required")
	if len(storage.applied) != 0 {
		t.Fatalf("bad confirmation reached facade: %#v", storage.applied)
	}
	response = doMaintenance(t, client, maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/gc/apply", `{"planId":"gc-plan-1","confirmation":"gc-proof-1"}`, csrf))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GC apply status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	if len(storage.applied) != 1 {
		t.Fatalf("GC apply calls=%#v", storage.applied)
	}
	response = doMaintenance(t, client, maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/backups/verify", `{"backupId":"/not-an-opaque-handle"}`, csrf))
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-opaque backup input status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")

	tooLarge := `{"cookie":"` + strings.Repeat("x", maxMutationBytes) + `"}`
	response = doMaintenance(t, client, maintenanceRequest(t, http.MethodPost, base+"/api/v1/settings/credentials/import", tooLarge, csrf))
	if response.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("oversized input status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
}

func startMaintenanceServer(t *testing.T, maintenance *application.MaintenanceService, storage *application.MaintenanceStorageService) (*Server, *http.Client) {
	t.Helper()
	server, err := New(Options{Application: &apiApplication{}, Maintenance: maintenance, StorageMaintenance: storage})
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

func maintenanceRequest(t *testing.T, method, target, body, csrf string) *http.Request {
	return requestWith(t, method, target, strings.NewReader(body), map[string]string{"Origin": strings.TrimSuffix(strings.Split(target, "/api/")[0], "/"), "Content-Type": "application/json", "X-CSRF-Token": csrf})
}
func doMaintenance(t *testing.T, client *http.Client, request *http.Request) *http.Response {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

type webCredentialMaintenance struct {
	items    []application.CredentialMetadata
	imported application.CredentialMetadata
	request  application.CredentialImportRequest
}

func (fake *webCredentialMaintenance) ListCredentialMetadata(context.Context) ([]application.CredentialMetadata, error) {
	return fake.items, nil
}
func (fake *webCredentialMaintenance) ImportCredential(_ context.Context, request application.CredentialImportRequest) (application.CredentialMetadata, error) {
	fake.request = request
	return fake.imported, nil
}
func (fake *webCredentialMaintenance) RemoveCredential(context.Context, string) error { return nil }

type webProxyMaintenance struct{ routes []application.ProxyRoute }

func (fake *webProxyMaintenance) ListProxies(context.Context) ([]application.ProxyRoute, error) {
	return fake.routes, nil
}
func (fake *webProxyMaintenance) AddProxy(context.Context, application.ProxyAddRequest) (application.ProxyRoute, error) {
	return application.ProxyRoute{}, nil
}
func (fake *webProxyMaintenance) RemoveProxy(context.Context, string) (application.ProxyRoute, error) {
	return application.ProxyRoute{}, nil
}
func (fake *webProxyMaintenance) SetProxyEnabled(context.Context, string, bool) (application.ProxyRoute, error) {
	return application.ProxyRoute{}, nil
}
func (fake *webProxyMaintenance) TestProxy(context.Context, string) (application.ProxyProbeResult, error) {
	return application.ProxyProbeResult{}, nil
}

type webPreferencesMaintenance struct{ preferences application.Preferences }

func (fake *webPreferencesMaintenance) Preferences(context.Context) (application.Preferences, error) {
	return fake.preferences, nil
}
func (fake *webPreferencesMaintenance) PatchPreferences(context.Context, application.PreferencesPatch) (application.Preferences, error) {
	return fake.preferences, nil
}

type webStorageMaintenance struct {
	receipt      application.BackupReceipt
	verification application.BackupVerification
	integrity    application.IntegrityReport
	diagnostics  application.DiagnosticsReport
	plan         application.GarbageCollectionPlan
	applied      []application.GarbageCollectionApplyRequest
}

func (fake *webStorageMaintenance) CreateBackup(context.Context) (application.BackupReceipt, error) {
	return fake.receipt, nil
}
func (fake *webStorageMaintenance) VerifyBackup(context.Context, string) (application.BackupVerification, error) {
	return fake.verification, nil
}
func (fake *webStorageMaintenance) CheckIntegrity(context.Context) (application.IntegrityReport, error) {
	return fake.integrity, nil
}
func (fake *webStorageMaintenance) CollectDiagnostics(context.Context) (application.DiagnosticsReport, error) {
	return fake.diagnostics, nil
}
func (fake *webStorageMaintenance) PlanGarbageCollection(context.Context) (application.GarbageCollectionPlan, error) {
	return fake.plan, nil
}
func (fake *webStorageMaintenance) ApplyGarbageCollection(_ context.Context, id, confirmation string) (application.GarbageCollectionResult, error) {
	if id != fake.plan.ID || confirmation != fake.plan.Confirmation {
		return application.GarbageCollectionResult{}, errors.New("unexpected plan")
	}
	fake.applied = append(fake.applied, application.GarbageCollectionApplyRequest{PlanID: id, Confirmation: confirmation})
	return application.GarbageCollectionResult{}, nil
}

var _ application.CredentialMaintenance = (*webCredentialMaintenance)(nil)
var _ application.ProxyMaintenance = (*webProxyMaintenance)(nil)
var _ application.PreferencesMaintenance = (*webPreferencesMaintenance)(nil)
var _ application.BackupMaintenance = (*webStorageMaintenance)(nil)
var _ application.IntegrityMaintenance = (*webStorageMaintenance)(nil)
var _ application.DiagnosticsMaintenance = (*webStorageMaintenance)(nil)
var _ application.GarbageCollectionMaintenance = (*webStorageMaintenance)(nil)

func TestMaintenanceAPIResponseEnvelopeIsJSON(t *testing.T) {
	// Keep a small decoding seam so API responses cannot accidentally regress to
	// an HTML error page while new maintenance endpoints are added.
	server, client := startMaintenanceServer(t, nil, nil)
	base := authorizeAPI(t, client, server.URL())
	response := get(t, client, base+"/api/v1/settings/credentials")
	defer response.Body.Close()
	var envelope apiErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "unavailable" {
		t.Fatalf("error envelope=%#v", envelope)
	}
}
