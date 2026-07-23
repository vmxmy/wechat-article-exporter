package app

import "github.com/wechat-article/wechat-article-exporter/cli/internal/application"

func newWebCredentialUpload(maintenance *application.MaintenanceService) *application.CredentialUploadService {
	return application.NewCredentialUpload(maintenance)
}
