package objects

import (
	"context"
	"io"
)

type Object struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType,omitempty"`
}

type Store interface {
	Put(context.Context, io.Reader, string) (Object, error)
	Open(context.Context, string) (io.ReadCloser, Object, error)
	Validate(context.Context, string) error
}
