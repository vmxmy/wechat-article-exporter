package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
)

type WorkerLauncher interface {
	Start(context.Context, string, []string, []string) error
}

type processWorkerLauncher struct{}

func (processWorkerLauncher) Start(_ context.Context, executable string, args, environment []string) error {
	command, err := newWorkerCommand(nil, executable, args, environment)
	if err != nil {
		return err
	}
	configureDetachedProcess(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start detached job worker: %w", err)
	}
	if command.Process != nil {
		_ = command.Process.Release()
	}
	return nil
}

func newWorkerCommand(ctx context.Context, executable string, args, environment []string) (*exec.Cmd, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, errors.New("worker executable is unavailable")
	}
	var command *exec.Cmd
	if ctx == nil {
		command = exec.Command(executable, args...)
	} else {
		command = exec.CommandContext(ctx, executable, args...)
	}
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.Env = append(os.Environ(), environment...)
	return command, nil
}

// foregroundProcessWorker is enabled only by the controlled clean-room
// executable mode. It executes a worker subprocess synchronously so a command
// using --wait cannot race a detached process that is outside the harness's
// lifetime and egress observation boundary.
type foregroundProcessWorker struct{}

func (foregroundProcessWorker) Start(ctx context.Context, executable string, args, environment []string) error {
	command, err := newWorkerCommand(ctx, executable, args, environment)
	if err != nil {
		return err
	}
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("run foreground job worker: %w", ctx.Err())
		}
		return fmt.Errorf("run foreground job worker: %w", err)
	}
	return nil
}

type persistentJobStarter struct {
	executable string
	paths      profiles.Paths
	profile    domain.ProfileID
	launcher   WorkerLauncher
}

func (starter persistentJobStarter) Start(ctx context.Context, job domain.Job) error {
	if starter.launcher == nil {
		return errors.New("persistent job worker launcher is unavailable")
	}
	environment := []string{"WECHAT_ARTICLE_PROFILE=" + string(starter.profile)}
	paths := starter.paths
	if paths.Portable {
		portableRoot := strings.TrimSuffix(paths.ConfigRoot, string(os.PathSeparator)+"config")
		environment = append(environment, "WECHAT_ARTICLE_PORTABLE_ROOT="+portableRoot)
	} else {
		environment = append(environment,
			"WECHAT_ARTICLE_CONFIG_ROOT="+paths.ConfigRoot,
			"WECHAT_ARTICLE_DATA_ROOT="+paths.DataRoot,
			"WECHAT_ARTICLE_CACHE_ROOT="+paths.CacheRoot,
			"WECHAT_ARTICLE_STATE_ROOT="+paths.StateRoot,
		)
	}
	return starter.launcher.Start(ctx, starter.executable, []string{"job", "worker", string(job.ID)}, environment)
}

func (a *App) runPersistentJob(ctx context.Context, id domain.JobID) (domain.Job, error) {
	if a == nil || a.active == nil {
		return domain.Job{}, errors.New("active profile runtime is unavailable")
	}
	job, err := a.core.GetJob(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	switch job.Kind {
	case "article_download", "resource_download", "metadata_download", "comments_download", "paid_content_download":
		if a.active.Downloads == nil {
			return domain.Job{}, errors.New("download job runtime is unavailable")
		}
		return a.active.Downloads.Run(ctx, id)
	case "account_sync", "album_sync":
		if a.active.Syncs == nil {
			return domain.Job{}, errors.New("account sync job runtime is unavailable")
		}
		return a.active.Syncs.Run(ctx, id)
	case "export":
		if a.active.Exports == nil {
			return domain.Job{}, errors.New("export job runtime is unavailable")
		}
		return a.active.Exports.Run(ctx, id)
	default:
		return domain.Job{}, fmt.Errorf("job %s has unsupported worker kind %q", id, job.Kind)
	}
}
