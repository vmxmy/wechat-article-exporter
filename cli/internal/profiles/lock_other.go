//go:build !unix && !windows

package profiles

import "errors"

func lockConfig(string) (func(), error) {
	return nil, errors.New("profile config locking is not supported on this platform")
}
