//go:build linux || android

package app

import "os"

// Linux POSIX access ACL entries that grant group/other permissions are
// reflected in the file mode mask checked by validatePassphraseFilePermissions.
func validatePassphraseExtendedACL(*os.File) error { return nil }
