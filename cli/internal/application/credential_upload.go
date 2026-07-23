package application

import (
	"context"
	"io"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
)

// CredentialUploadService immediately parses and imports one bounded JSON
// stream. It deliberately has no path, file name, upload handle, or retained
// bytes, so credentials never enter a browser-visible staging lifecycle.
type CredentialUploadService struct{ maintenance *MaintenanceService }

func NewCredentialUpload(maintenance *MaintenanceService) *CredentialUploadService {
	return &CredentialUploadService{maintenance: maintenance}
}

func (service *CredentialUploadService) ImportJSON(ctx context.Context, source io.Reader) (CredentialMetadata, error) {
	if service == nil || service.maintenance == nil {
		return CredentialMetadata{}, ErrUnavailable
	}
	record, err := credentials.ParseJSON(source)
	if err != nil {
		return CredentialMetadata{}, err
	}
	return service.maintenance.ImportCredential(ctx, CredentialImportRequest{
		Nickname: record.Nickname, Biz: record.Biz, UIN: record.UIN, Key: record.Key,
		PassTicket: record.PassTicket, WapSID2: record.WapSID2, AppMsgToken: record.AppMsgToken,
		Cookie: record.Cookie, ExpiresAt: record.ExpiresAt,
	})
}
