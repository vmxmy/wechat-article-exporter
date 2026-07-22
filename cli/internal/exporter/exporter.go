package exporter

import (
	"context"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

type Result struct {
	ExportID domain.ExportID `json:"exportId"`
	Files    []string        `json:"files"`
	Warnings []string        `json:"warnings,omitempty"`
}

type Exporter interface {
	Export(context.Context, domain.ExportRequest) (Result, error)
}
