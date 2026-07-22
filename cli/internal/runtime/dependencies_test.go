package runtimeenv

import (
	"io"
	"io/fs"
	"testing"
)

type fixedFilesystem struct{ calls int }

func (filesystem *fixedFilesystem) Open(string) (fs.File, error) {
	filesystem.calls++
	return nil, fs.ErrNotExist
}
func (filesystem *fixedFilesystem) Create(string) (io.WriteCloser, error) {
	filesystem.calls++
	return nil, fs.ErrPermission
}
func (filesystem *fixedFilesystem) MkdirAll(string, fs.FileMode) error {
	filesystem.calls++
	return nil
}
func (filesystem *fixedFilesystem) Rename(string, string) error { filesystem.calls++; return nil }
func (filesystem *fixedFilesystem) Remove(string) error         { filesystem.calls++; return nil }
func (filesystem *fixedFilesystem) Stat(string) (fs.FileInfo, error) {
	filesystem.calls++
	return nil, fs.ErrNotExist
}
func (filesystem *fixedFilesystem) Chmod(string, fs.FileMode) error { filesystem.calls++; return nil }

func TestNormalizePreservesInjectedFilesystem(t *testing.T) {
	filesystem := &fixedFilesystem{}
	dependencies := Normalize(Dependencies{FS: filesystem})
	if dependencies.FS != filesystem {
		t.Fatalf("filesystem dependency = %T, want injected instance", dependencies.FS)
	}
}

func TestNormalizeUsesOSFilesystemByDefault(t *testing.T) {
	if _, ok := Normalize(Dependencies{}).FS.(OSFilesystem); !ok {
		t.Fatalf("default filesystem = %T", Normalize(Dependencies{}).FS)
	}
}
