package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

// JobControls is the optional application-level capability for durable job
// state changes. It deliberately contains no database or profile runtime
// handles, so local presentation adapters cannot bypass Application.
type JobControls interface {
	Pause(context.Context, domain.JobID) (domain.Job, error)
	Resume(context.Context, domain.JobID) (domain.Job, error)
	Retry(context.Context, domain.JobID) (domain.Job, error)
	RetryExport(context.Context, domain.JobID) (domain.Job, error)
}

// WorkspaceLoginFlow is safe for the authenticated, no-store local browser
// response. The QR payload is transient scan data, not a stored credential;
// raw gateway structs and cookies never cross this boundary.
type WorkspaceLoginFlow struct {
	SessionID string    `json:"sessionId"`
	QRCode    string    `json:"qrCode,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

func (workspace *Workspace) LoginBegin(ctx context.Context, clientSessionID string) (WorkspaceLoginFlow, error) {
	flow, err := workspace.application.BeginLogin(ctx, strings.TrimSpace(clientSessionID))
	if err != nil {
		return WorkspaceLoginFlow{}, workspaceError(err)
	}
	result := WorkspaceLoginFlow{SessionID: flow.SessionID, ExpiresAt: flow.ExpiresAt}
	if len(flow.QRBytes) > 0 {
		result.QRCode = base64.StdEncoding.EncodeToString(flow.QRBytes)
	}
	return result, nil
}

func (workspace *Workspace) SearchAccounts(ctx context.Context, input WorkspaceAccountQuery) (WorkspacePage[domain.Account], error) {
	page, err := input.Page.normalize()
	if err != nil {
		return WorkspacePage[domain.Account]{}, err
	}
	result, err := workspace.application.SearchAccounts(ctx, domain.AccountQuery{Keyword: strings.TrimSpace(input.Keyword), Offset: page.Offset, Limit: page.Limit})
	return workspacePage(result), workspaceError(err)
}

func (workspace *Workspace) SaveAccount(ctx context.Context, account domain.Account) (domain.Account, error) {
	if strings.TrimSpace(account.FakeID) == "" || strings.TrimSpace(account.Name) == "" {
		return domain.Account{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "account fakeid and name are required"}
	}
	account.ID = ""
	account.FakeID, account.Name = strings.TrimSpace(account.FakeID), strings.TrimSpace(account.Name)
	return workspaceAccountResult(workspace.application.SaveAccount(ctx, account))
}

func (workspace *Workspace) UpdateAccount(ctx context.Context, id domain.AccountID, account domain.Account) (domain.Account, error) {
	if strings.TrimSpace(string(id)) == "" || strings.TrimSpace(account.Name) == "" {
		return domain.Account{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "account identifier and name are required"}
	}
	account.ID, account.Name = id, strings.TrimSpace(account.Name)
	return workspaceAccountResult(workspace.application.UpdateAccount(ctx, account))
}

func workspaceAccountResult(account domain.Account, err error) (domain.Account, error) {
	return account, workspaceError(err)
}

func (workspace *Workspace) DeleteAccounts(ctx context.Context, ids []domain.AccountID, confirmation string) (domain.AccountDeleteReport, error) {
	if len(ids) == 0 {
		return domain.AccountDeleteReport{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "at least one account identifier is required"}
	}
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = string(id)
		if strings.TrimSpace(values[index]) == "" {
			return domain.AccountDeleteReport{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "account identifier is required"}
		}
	}
	required := "delete-accounts:" + strings.Join(values, ",")
	if confirmation != required {
		return domain.AccountDeleteReport{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "account deletion confirmation is invalid"}
	}
	report, err := workspace.application.DeleteAccounts(ctx, ids)
	return report, workspaceError(err)
}

func (workspace *Workspace) PauseJob(ctx context.Context, id domain.JobID, confirmation string) (domain.Job, error) {
	if err := workspaceJobConfirmation(id, confirmation, "pause-job:"); err != nil {
		return domain.Job{}, err
	}
	controls, ok := workspace.application.(interface {
		PauseJob(context.Context, domain.JobID) (domain.Job, error)
	})
	if !ok {
		return domain.Job{}, workspaceError(fmt.Errorf("pause job: %w", ErrUnavailable))
	}
	job, err := controls.PauseJob(ctx, id)
	return job, workspaceError(err)
}

func (workspace *Workspace) ResumeJob(ctx context.Context, id domain.JobID) (domain.Job, error) {
	if strings.TrimSpace(string(id)) == "" {
		return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "job identifier is required"}
	}
	controls, ok := workspace.application.(interface {
		ResumeJob(context.Context, domain.JobID) (domain.Job, error)
	})
	if !ok {
		return domain.Job{}, workspaceError(fmt.Errorf("resume job: %w", ErrUnavailable))
	}
	job, err := controls.ResumeJob(ctx, id)
	return job, workspaceError(err)
}

func (workspace *Workspace) RetryJob(ctx context.Context, id domain.JobID, confirmation string) (domain.Job, error) {
	if err := workspaceJobConfirmation(id, confirmation, "retry-job:"); err != nil {
		return domain.Job{}, err
	}
	controls, ok := workspace.application.(interface {
		RetryJob(context.Context, domain.JobID) (domain.Job, error)
	})
	if !ok {
		return domain.Job{}, workspaceError(fmt.Errorf("retry job: %w", ErrUnavailable))
	}
	job, err := controls.RetryJob(ctx, id)
	return job, workspaceError(err)
}

func (workspace *Workspace) CancelJobConfirmed(ctx context.Context, id domain.JobID, confirmation string) (domain.Job, error) {
	if err := workspaceJobConfirmation(id, confirmation, "cancel-job:"); err != nil {
		return domain.Job{}, err
	}
	return workspace.CancelJob(ctx, id)
}

func workspaceJobConfirmation(id domain.JobID, confirmation, prefix string) error {
	if strings.TrimSpace(string(id)) == "" {
		return &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "job identifier is required"}
	}
	if confirmation != prefix+string(id) {
		return &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "job operation confirmation is invalid"}
	}
	return nil
}

func (service *Service) PauseJob(ctx context.Context, id domain.JobID) (domain.Job, error) {
	controls, ok := service.jobs.(JobControls)
	if !ok {
		return domain.Job{}, fmt.Errorf("pause job: %w", ErrUnavailable)
	}
	return controls.Pause(ctx, id)
}

func (service *Service) ResumeJob(ctx context.Context, id domain.JobID) (domain.Job, error) {
	return service.restartJob(ctx, id, false)
}

func (service *Service) RetryJob(ctx context.Context, id domain.JobID) (domain.Job, error) {
	return service.restartJob(ctx, id, true)
}

func (service *Service) restartJob(ctx context.Context, id domain.JobID, retry bool) (domain.Job, error) {
	controls, ok := service.jobs.(JobControls)
	if !ok || service.starter == nil {
		return domain.Job{}, fmt.Errorf("restart job: %w", ErrUnavailable)
	}
	var (
		job domain.Job
		err error
	)
	if retry {
		current, getErr := service.jobs.Get(ctx, id)
		if getErr != nil {
			return domain.Job{}, getErr
		}
		if current.Kind == "export" {
			job, err = controls.RetryExport(ctx, id)
		} else {
			job, err = controls.Retry(ctx, id)
		}
	} else {
		job, err = controls.Resume(ctx, id)
	}
	if err != nil {
		return domain.Job{}, err
	}
	if err := service.starter.Start(ctx, job); err != nil {
		_, _ = controls.Pause(context.Background(), job.ID)
		return domain.Job{}, errors.Join(fmt.Errorf("restart %s worker", job.Kind), err)
	}
	return job, nil
}
