package application

import (
	"context"
	"strings"
	"testing"
)

func TestCredentialUploadImportsParsedJSON(t *testing.T) {
	maintenance := &credentialUploadMaintenance{metadata: CredentialMetadata{ID: "credential-1"}}
	service := NewCredentialUpload(NewMaintenance(MaintenanceOptions{Credentials: maintenance}))
	metadata, err := service.ImportJSON(context.Background(), strings.NewReader(`{"biz":"biz-secret","uin":"uin-secret","key":"key-secret","pass_ticket":"ticket-secret","wap_sid2":"sid-secret","appmsg_token":"token-secret","cookie":"cookie-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ID != "credential-1" || maintenance.request.Cookie != "cookie-secret" {
		t.Fatalf("metadata=%#v request=%#v", metadata, maintenance.request)
	}
}

func TestCredentialUploadRejectsInvalidJSONBeforeImport(t *testing.T) {
	maintenance := &credentialUploadMaintenance{}
	service := NewCredentialUpload(NewMaintenance(MaintenanceOptions{Credentials: maintenance}))
	if _, err := service.ImportJSON(context.Background(), strings.NewReader(`{"biz":"fixture"}`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if maintenance.calls != 0 {
		t.Fatalf("imports=%d, want 0", maintenance.calls)
	}
}

type credentialUploadMaintenance struct {
	metadata CredentialMetadata
	request  CredentialImportRequest
	calls    int
}

func (*credentialUploadMaintenance) ListCredentialMetadata(context.Context) ([]CredentialMetadata, error) {
	return nil, nil
}
func (maintenance *credentialUploadMaintenance) ImportCredential(_ context.Context, request CredentialImportRequest) (CredentialMetadata, error) {
	maintenance.calls++
	maintenance.request = request
	return maintenance.metadata, nil
}
func (*credentialUploadMaintenance) RemoveCredential(context.Context, string) error { return nil }

var _ CredentialMaintenance = (*credentialUploadMaintenance)(nil)
