package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeCredentialMaintenance struct {
	items      []CredentialMetadata
	validation CredentialValidation
	imported   CredentialImportRequest
	removedID  string
	importedTo CredentialMetadata
}

func (fake *fakeCredentialMaintenance) ValidateCredential(_ context.Context, request CredentialImportRequest) (CredentialValidation, error) {
	fake.imported = request
	return fake.validation, nil
}

func (fake *fakeCredentialMaintenance) ListCredentialMetadata(context.Context) ([]CredentialMetadata, error) {
	return fake.items, nil
}

func (fake *fakeCredentialMaintenance) ImportCredential(_ context.Context, request CredentialImportRequest) (CredentialMetadata, error) {
	fake.imported = request
	return fake.importedTo, nil
}

func (fake *fakeCredentialMaintenance) RemoveCredential(_ context.Context, id string) error {
	fake.removedID = id
	return nil
}

type fakeProxyMaintenance struct {
	routes []ProxyRoute
	added  ProxyAddRequest
}

func (fake *fakeProxyMaintenance) ListProxies(context.Context) ([]ProxyRoute, error) {
	return fake.routes, nil
}
func (fake *fakeProxyMaintenance) AddProxy(_ context.Context, request ProxyAddRequest) (ProxyRoute, error) {
	fake.added = request
	return ProxyRoute{Name: request.Name, Endpoint: request.Endpoint, Trust: request.Trust, Classes: request.Classes, Priority: request.Priority}, nil
}
func (fake *fakeProxyMaintenance) RemoveProxy(_ context.Context, id string) (ProxyRoute, error) {
	return ProxyRoute{ID: id, Endpoint: "https://proxy.test/?token=not-for-output"}, nil
}
func (fake *fakeProxyMaintenance) SetProxyEnabled(_ context.Context, id string, enabled bool) (ProxyRoute, error) {
	return ProxyRoute{ID: id, Enabled: enabled}, nil
}
func (fake *fakeProxyMaintenance) TestProxy(_ context.Context, id string) (ProxyProbeResult, error) {
	return ProxyProbeResult{Route: ProxyRoute{ID: id, Endpoint: "https://proxy.test/?token=not-for-output"}}, nil
}

type fakePreferencesMaintenance struct {
	preferences Preferences
	patch       PreferencesPatch
}

func (fake *fakePreferencesMaintenance) Preferences(context.Context) (Preferences, error) {
	return fake.preferences, nil
}
func (fake *fakePreferencesMaintenance) PatchPreferences(_ context.Context, patch PreferencesPatch) (Preferences, error) {
	fake.patch = patch
	return fake.preferences, nil
}

func TestMaintenanceCredentialsAreMetadataOnlyAndWriteOnly(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	credentials := &fakeCredentialMaintenance{items: []CredentialMetadata{{ID: "credential-1", AccountID: "account-1", Kind: "wechat-article", Status: "valid", CreatedAt: now}}, importedTo: CredentialMetadata{ID: "credential-2"}}
	service := NewMaintenance(MaintenanceOptions{Credentials: credentials})

	metadata, err := service.Credentials(context.Background())
	if err != nil || !reflect.DeepEqual(metadata, credentials.items) {
		t.Fatalf("Credentials() = %#v, %v", metadata, err)
	}
	request := CredentialImportRequest{Biz: "fixture-biz", UIN: "uin-secret", Key: "key-secret", PassTicket: "ticket-secret", WapSID2: "sid-secret", AppMsgToken: "token-secret", Cookie: "cookie-secret"}
	imported, err := service.ImportCredential(context.Background(), request)
	if err != nil || imported.ID != "credential-2" || !reflect.DeepEqual(credentials.imported, request) {
		t.Fatalf("ImportCredential() = %#v, %v; request = %#v", imported, err, credentials.imported)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture-biz", "uin-secret", "key-secret", "ticket-secret", "sid-secret", "token-secret", "cookie-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("credential import JSON echoed %q: %s", secret, encoded)
		}
	}
	if err := service.RemoveCredential(context.Background(), " credential-2 "); err != nil || credentials.removedID != "credential-2" {
		t.Fatalf("RemoveCredential() error = %v, id = %q", err, credentials.removedID)
	}
}

func TestMaintenanceTrustedProxyRequiresExactConfirmationAndDisclosesSecrets(t *testing.T) {
	proxies := &fakeProxyMaintenance{}
	service := NewMaintenance(MaintenanceOptions{Proxies: proxies})
	request := ProxyAddRequest{
		Name: " trusted ", Endpoint: "https://proxy.test/wrap?apiKey=not-for-output", Authorization: "proxy-authorization",
		Trust: ProxyTrustCredentialTrusted, Classes: []ProxyRequestClass{ProxyRequestClassArticleCredential}, Priority: 90,
	}
	disclosure, err := service.ProxyDisclosure(request)
	if err != nil {
		t.Fatal(err)
	}
	if !disclosure.Required || disclosure.Confirmation != "trust-proxy-credentials:trusted" || len(disclosure.Secrets) != 1 || !strings.Contains(disclosure.Secrets[0], "appmsg_token") {
		t.Fatalf("ProxyDisclosure() = %#v", disclosure)
	}
	if _, err := service.AddProxy(context.Background(), request); !errors.Is(err, ErrMaintenanceConfirmationRequired) || proxies.added.Name != "" {
		t.Fatalf("AddProxy() without confirmation error = %v, added = %#v", err, proxies.added)
	}
	request.Confirmation = disclosure.Confirmation + " "
	if _, err := service.AddProxy(context.Background(), request); !errors.Is(err, ErrMaintenanceConfirmationRequired) {
		t.Fatalf("AddProxy() with inexact confirmation error = %v", err)
	}
	request.Confirmation = disclosure.Confirmation
	route, err := service.AddProxy(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if proxies.added.Name != "trusted" || proxies.added.Authorization != "proxy-authorization" || proxies.added.Priority != 90 {
		t.Fatalf("proxy received %#v", proxies.added)
	}
	if route.Endpoint != "https://proxy.test/wrap?apiKey=%5BREDACTED%5D" {
		t.Fatalf("AddProxy() endpoint = %q; want normalized value-redacted output", route.Endpoint)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "proxy-authorization") || strings.Contains(string(encoded), disclosure.Confirmation) {
		t.Fatalf("proxy request JSON exposed a write-only value: %s", encoded)
	}
}

func TestMaintenanceProxyValidationAndSanitization(t *testing.T) {
	proxies := &fakeProxyMaintenance{routes: []ProxyRoute{{ID: "proxy-1", Endpoint: "https://proxy.test/?access_token=not-for-output"}}}
	service := NewMaintenance(MaintenanceOptions{Proxies: proxies})
	if _, err := service.AddProxy(context.Background(), ProxyAddRequest{Name: "public", Endpoint: "https://proxy.test", Classes: []ProxyRequestClass{ProxyRequestClassArticleCredential}}); err == nil {
		t.Fatal("AddProxy() accepted sensitive class on a public-only proxy")
	}
	routes, err := service.Proxies(context.Background())
	if err != nil || strings.Contains(routes[0].Endpoint, "not-for-output") {
		t.Fatalf("Proxies() = %#v, %v", routes, err)
	}
	probe, err := service.TestProxy(context.Background(), "proxy-1")
	if err != nil || strings.Contains(probe.Route.Endpoint, "not-for-output") {
		t.Fatalf("TestProxy() = %#v, %v", probe, err)
	}
}

func TestMaintenancePreferencesPatchPreservesFalseAndRejectsEmpty(t *testing.T) {
	preferences := &fakePreferencesMaintenance{preferences: Preferences{Download: DownloadPreferences{Concurrency: 4}, Display: DisplayPreferences{NoColor: true}}}
	service := NewMaintenance(MaintenanceOptions{Preferences: preferences})
	if _, err := service.PatchPreferences(context.Background(), PreferencesPatch{}); err == nil {
		t.Fatal("PatchPreferences() accepted an empty patch")
	}
	falseValue := false
	concurrency := 8
	patch := PreferencesPatch{Download: &DownloadPreferencesPatch{Concurrency: &concurrency}, Display: &DisplayPreferencesPatch{NoColor: &falseValue}}
	updated, err := service.PatchPreferences(context.Background(), patch)
	if err != nil || updated.Download.Concurrency != 4 || preferences.patch.Display.NoColor == nil || *preferences.patch.Display.NoColor {
		t.Fatalf("PatchPreferences() = %#v, %v; patch = %#v", updated, err, preferences.patch)
	}
	if preferences.patch.Download.Concurrency == nil || *preferences.patch.Download.Concurrency != 8 {
		t.Fatalf("PatchPreferences() lost concurrency: %#v", preferences.patch)
	}
}

func TestMaintenancePlanDTOCarriesOnlyOperationScopedProof(t *testing.T) {
	plan := MaintenancePlan{Operation: "garbage-collect", Summary: "remove unreferenced objects", Destructive: true, Confirmation: "gc:fixture"}
	confirmation := MaintenanceConfirmation{Operation: plan.Operation, Value: plan.Confirmation}
	if confirmation.Operation != "garbage-collect" || confirmation.Value != "gc:fixture" {
		t.Fatalf("maintenance proof = %#v", confirmation)
	}
}
