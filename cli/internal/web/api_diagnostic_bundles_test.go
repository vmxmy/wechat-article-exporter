package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

type webDiagnosticBundleMaintenance struct {
	artifact application.DiagnosticBundleArtifact
	contents []byte
}

func (fake *webDiagnosticBundleMaintenance) CreateDiagnosticBundle(context.Context) (application.DiagnosticBundleArtifact, error) {
	return fake.artifact, nil
}

func (fake *webDiagnosticBundleMaintenance) OpenDiagnosticBundle(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(fake.contents)), nil
}

func (*webDiagnosticBundleMaintenance) DiscardDiagnosticBundle(context.Context, string) error {
	return nil
}

func TestDiagnosticBundleAPIRequiresMutationProtectionAndStreamsOpaqueHandle(t *testing.T) {
	backend := &webDiagnosticBundleMaintenance{artifact: application.DiagnosticBundleArtifact{Reference: "/private/staging/fixture.zip", CreatedAt: time.Now(), SHA256: "digest", SizeBytes: int64(len("redacted zip"))}, contents: []byte("redacted zip")}
	bundles := application.NewDiagnosticBundleService(application.DiagnosticBundleOptions{Maintenance: backend})
	server, client := startDiagnosticBundleServer(t, bundles)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	request := maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/diagnostic-bundles", `{}`, "wrong")
	response := doMaintenance(t, client, request)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unprotected create status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()

	response = doMaintenance(t, client, maintenanceRequest(t, http.MethodPost, base+"/api/v1/maintenance/diagnostic-bundles", `{}`, csrf))
	if response.StatusCode != http.StatusCreated || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("create status=%d headers=%v body=%s", response.StatusCode, response.Header, readResponse(t, response))
	}
	var envelope apiEnvelope
	if err := decodeJSON(response.Body, &envelope); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	receipt, ok := envelope.Data.(map[string]any)
	if !ok || !strings.HasPrefix(receipt["handle"].(string), "diagnostic_") || strings.Contains(receipt["handle"].(string), "/private") {
		t.Fatalf("unsafe receipt=%#v", envelope.Data)
	}
	handle := receipt["handle"].(string)

	response = get(t, client, base+"/api/v1/maintenance/diagnostic-bundles/"+handle)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("download status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	if response.Header.Get("Content-Type") != "application/zip" || response.Header.Get("Content-Disposition") != `attachment; filename="wechat-article-diagnostics.zip"` || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Pragma") != "no-cache" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download headers=%v", response.Header)
	}
	if body := readResponse(t, response); body != "redacted zip" || strings.Contains(body, "/private") {
		t.Fatalf("download body=%q", body)
	}
	response = get(t, client, base+"/api/v1/maintenance/diagnostic-bundles/"+handle)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("replay status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "not_found")
}

func TestDiagnosticBundleAPIFailsClosedWithoutService(t *testing.T) {
	server, client := startAPIApplicationServer(t, &apiApplication{})
	base := authorizeAPI(t, client, server.URL())
	response := get(t, client, base+"/api/v1/maintenance/diagnostic-bundles/diagnostic_fixture")
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("download status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "unavailable")
}

func startDiagnosticBundleServer(t *testing.T, bundles *application.DiagnosticBundleService) (*Server, *http.Client) {
	t.Helper()
	server, err := New(Options{Application: &apiApplication{}, DiagnosticBundles: bundles})
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

func decodeJSON(reader io.Reader, destination any) error {
	return json.NewDecoder(reader).Decode(destination)
}

var _ application.DiagnosticBundleMaintenance = (*webDiagnosticBundleMaintenance)(nil)
