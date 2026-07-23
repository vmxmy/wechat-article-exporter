//go:build !unix && !windows

package main

import "errors"

type isolatedRoot struct{}

func openIsolatedRoot(string) (*isolatedRoot, error) {
	return nil, errors.New("secure isolated roots are unsupported on this platform")
}

func (root *isolatedRoot) EnsureDirectory(string) error {
	return errors.New("secure isolated roots are unsupported on this platform")
}

func (root *isolatedRoot) Close() error { return nil }
