package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

const (
	// WorkspaceJobDetailMaximumItems keeps browser inspection bounded even for
	// bulk jobs. Item pagination can be added without widening this first safe
	// detail contract.
	WorkspaceJobDetailMaximumItems = 100
	workspaceJobDetailMaximumLogs  = 50
	workspaceJobDetailLogBytes     = 16 << 10
	workspaceJobDetailEntryBytes   = 2 << 10
)

// WorkspaceJobDetail is the browser-safe inspection model for one persistent
// job. It deliberately excludes opaque payloads, item keys/checkpoints,
// executor owner strings, raw failures, and log fields: each can contain a
// filesystem path or a credential-bearing upstream value.
type WorkspaceJobDetail struct {
	Job          domain.Job               `json:"job"`
	Items        []WorkspaceJobItemDetail `json:"items"`
	ItemsTotal   int                      `json:"itemsTotal"`
	ItemsLimited bool                     `json:"itemsLimited"`
	Logs         []WorkspaceJobLogDetail  `json:"logs"`
	Lease        WorkspaceJobLeaseDetail  `json:"lease"`
	RefreshedAt  time.Time                `json:"refreshedAt"`
}

type WorkspaceJobItemDetail struct {
	ID           string          `json:"id"`
	State        domain.JobState `json:"state"`
	AttemptCount int             `json:"attemptCount"`
	ErrorClass   string          `json:"errorClass,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

type WorkspaceJobLogDetail struct {
	ID        int64     `json:"id"`
	ItemID    string    `json:"itemId,omitempty"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type WorkspaceJobLeaseDetail struct {
	Active    bool      `json:"active"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// WorkspaceJobDetailProvider is an optional application capability. Keeping
// it separate from Application avoids forcing non-local adapters and narrow
// test doubles to expose storage inspection primitives.
type WorkspaceJobDetailProvider interface {
	JobDetails(context.Context, domain.JobID) (WorkspaceJobDetail, error)
}

type workspaceJobDetailStore interface {
	Get(context.Context, domain.JobID) (domain.Job, error)
	ListItems(context.Context, domain.JobID) ([]jobs.Item, error)
	ListLogsBounded(context.Context, domain.JobID, library.JobLogBudget) ([]library.JobLog, error)
	Lease(context.Context, domain.JobID) (library.JobLease, error)
}

func (workspace *Workspace) JobDetails(ctx context.Context, id domain.JobID) (WorkspaceJobDetail, error) {
	if strings.TrimSpace(string(id)) == "" {
		return WorkspaceJobDetail{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "job identifier is required"}
	}
	provider, ok := workspace.application.(WorkspaceJobDetailProvider)
	if !ok {
		return WorkspaceJobDetail{}, workspaceError(fmt.Errorf("job details: %w", ErrUnavailable))
	}
	detail, err := provider.JobDetails(ctx, id)
	return detail, workspaceError(err)
}

// JobDetails reads existing durable state only; it never starts or attaches to
// a worker. A reconnect therefore obtains a fresh SQLite-backed snapshot.
func (service *Service) JobDetails(ctx context.Context, id domain.JobID) (WorkspaceJobDetail, error) {
	if strings.TrimSpace(string(id)) == "" {
		return WorkspaceJobDetail{}, fmt.Errorf("job identifier is required")
	}
	store, ok := service.jobs.(workspaceJobDetailStore)
	if !ok {
		return WorkspaceJobDetail{}, fmt.Errorf("job details: %w", ErrUnavailable)
	}
	job, err := store.Get(ctx, id)
	if err != nil {
		return WorkspaceJobDetail{}, err
	}
	items, err := store.ListItems(ctx, id)
	if err != nil {
		return WorkspaceJobDetail{}, fmt.Errorf("list job items: %w", err)
	}
	logs, err := store.ListLogsBounded(ctx, id, library.JobLogBudget{
		MaximumRows: workspaceJobDetailMaximumLogs, MaximumRawBytes: workspaceJobDetailLogBytes, MaximumEntryBytes: workspaceJobDetailEntryBytes,
	})
	if err != nil {
		return WorkspaceJobDetail{}, fmt.Errorf("list job logs: %w", err)
	}
	lease, err := store.Lease(ctx, id)
	if err != nil {
		return WorkspaceJobDetail{}, fmt.Errorf("read job lease: %w", err)
	}

	itemsTotal := len(items)
	limited := itemsTotal > WorkspaceJobDetailMaximumItems
	if limited {
		items = items[:WorkspaceJobDetailMaximumItems]
	}
	result := WorkspaceJobDetail{Job: job, Items: make([]WorkspaceJobItemDetail, 0, len(items)), ItemsTotal: itemsTotal, ItemsLimited: limited,
		Logs: make([]WorkspaceJobLogDetail, 0, len(logs)), Lease: WorkspaceJobLeaseDetail{Active: lease.Active, ExpiresAt: lease.ExpiresAt}, RefreshedAt: service.runtime.Clock.Now()}
	for _, item := range items {
		result.Items = append(result.Items, WorkspaceJobItemDetail{ID: item.ID, State: item.State, AttemptCount: item.AttemptCount,
			ErrorClass: string(item.ErrorClass), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
	}
	for _, entry := range logs {
		result.Logs = append(result.Logs, WorkspaceJobLogDetail{ID: entry.ID, ItemID: entry.ItemID, Level: entry.Level,
			Message: workspaceSafeLogMessage(entry.Message), CreatedAt: entry.CreatedAt})
	}
	return result, nil
}

var (
	workspaceAbsolutePath = regexp.MustCompile(`(?:(?:^|\s)/[^\s]+|(?i:[A-Z]:\\[^\s]+))`)
	workspaceHomePath     = regexp.MustCompile(`(?:^|\s)~[/\\][^\s]+`)
	workspaceSecretValue  = regexp.MustCompile(`(?i)\b(?:token|secret|password|cookie|authorization|credential|key)(?:\s*[:=]\s*|\s+)[^\s,;]+`)
)

func workspaceSafeLogMessage(value string) string {
	value = safety.RedactText(value)
	value = workspaceSecretValue.ReplaceAllStringFunc(value, func(match string) string {
		key := strings.Fields(match)[0]
		if index := strings.IndexAny(key, ":="); index >= 0 {
			key = key[:index]
		}
		return key + "=[REDACTED]"
	})
	value = workspaceAbsolutePath.ReplaceAllStringFunc(value, func(match string) string {
		prefix := ""
		if strings.HasPrefix(match, " ") {
			prefix = " "
		}
		return prefix + "[PATH REDACTED]"
	})
	return workspaceHomePath.ReplaceAllString(value, " [PATH REDACTED]")
}

var _ WorkspaceJobDetailProvider = (*Service)(nil)
