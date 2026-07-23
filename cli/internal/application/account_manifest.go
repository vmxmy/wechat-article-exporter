package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

const (
	// AccountManifestMaximumBytes bounds browser-originated JSON before it is
	// decoded. The upload staging limit remains the outer streaming bound.
	AccountManifestMaximumBytes int64 = 8 << 20
	accountManifestMaximumItems       = 100_000
)

// AccountManifestService is the browser-safe account import/export facade.
// It deliberately depends only on Application and opaque upload staging; the
// browser never receives a staging location or a host filesystem path.
type AccountManifestService struct {
	application Application
	uploads     *UploadStagingService
}

type AccountManifestImportResult struct {
	Report AccountManifestImportReport `json:"report"`
}

// AccountManifestImportReport keeps the presentation DTO independent from
// persistence details while preserving the shared import outcome.
type AccountManifestImportReport struct {
	Added     int `json:"added"`
	Merged    int `json:"merged"`
	Unchanged int `json:"unchanged"`
}

func NewAccountManifestService(application Application, uploads *UploadStagingService) (*AccountManifestService, error) {
	if application == nil {
		return nil, errors.New("account manifest application is required")
	}
	if uploads == nil {
		return nil, errors.New("account manifest upload staging is required")
	}
	return &AccountManifestService{application: application, uploads: uploads}, nil
}

func (service *AccountManifestService) Export(ctx context.Context) (io.ReadCloser, error) {
	if service == nil || service.application == nil {
		return nil, ErrUnavailable
	}
	manifest, err := service.application.ExportAccounts(ctx, domain.AccountQuery{})
	if err != nil {
		return nil, fmt.Errorf("export account manifest: %w", err)
	}
	reader, writer := io.Pipe()
	go func() {
		err := json.NewEncoder(writer).Encode(manifest)
		if err != nil {
			err = errors.New("encode account manifest failed")
		}
		_ = writer.CloseWithError(err)
	}()
	return reader, nil
}

// Stage streams the browser multipart part into private staging and returns a
// capability that expires and is removed on server shutdown.
func (service *AccountManifestService) Stage(ctx context.Context, source io.Reader, declaredSize int64) (UploadReceipt, error) {
	if service == nil || service.uploads == nil {
		return UploadReceipt{}, ErrUnavailable
	}
	return service.uploads.Stage(ctx, source, declaredSize)
}

// Import consumes the opaque upload exactly once. JSON decoding is bounded,
// accepts one value only, and delegates all merge and persistence semantics to
// Application.ImportAccounts.
func (service *AccountManifestService) Import(ctx context.Context, handle UploadHandle) (AccountManifestImportResult, error) {
	if service == nil || service.application == nil || service.uploads == nil {
		return AccountManifestImportResult{}, ErrUnavailable
	}
	if !validUploadHandle(handle) {
		return AccountManifestImportResult{}, errors.New("account manifest upload is unavailable")
	}
	reader, _, err := service.uploads.Consume(ctx, handle)
	if err != nil {
		return AccountManifestImportResult{}, errors.New("account manifest upload is unavailable")
	}
	defer reader.Close()

	limited := &accountManifestLimitReader{Reader: reader, maximum: AccountManifestMaximumBytes}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var manifest domain.AccountManifest
	if err := decoder.Decode(&manifest); err != nil {
		return AccountManifestImportResult{}, errors.New("account manifest is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return AccountManifestImportResult{}, errors.New("account manifest must contain one JSON value")
	}
	if limited.exceeded || len(manifest.Accounts) > accountManifestMaximumItems {
		return AccountManifestImportResult{}, errors.New("account manifest is invalid")
	}
	report, err := service.application.ImportAccounts(ctx, manifest)
	if err != nil {
		return AccountManifestImportResult{}, errors.New("account manifest could not be imported")
	}
	return AccountManifestImportResult{Report: AccountManifestImportReport{Added: report.Added, Merged: report.Merged, Unchanged: report.Unchanged}}, nil
}

func (service *AccountManifestService) Discard(ctx context.Context, handle UploadHandle) error {
	if service == nil || service.uploads == nil {
		return ErrUnavailable
	}
	return service.uploads.Discard(ctx, handle)
}

type accountManifestLimitReader struct {
	io.Reader
	maximum  int64
	read     int64
	exceeded bool
}

func (reader *accountManifestLimitReader) Read(buffer []byte) (int, error) {
	if reader.read >= reader.maximum {
		var probe [1]byte
		count, err := reader.Reader.Read(probe[:])
		if count > 0 {
			reader.exceeded = true
			return 0, errors.New("account manifest exceeds size limit")
		}
		return 0, err
	}
	remaining := reader.maximum - reader.read
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	count, err := reader.Reader.Read(buffer)
	reader.read += int64(count)
	return count, err
}

func (service *AccountManifestService) Close(ctx context.Context) error {
	if service == nil || service.uploads == nil {
		return ErrUnavailable
	}
	return service.uploads.Close(ctx)
}
