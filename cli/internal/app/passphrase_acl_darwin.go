//go:build darwin

package app

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	darwinAttrBitMapCount      = 5
	darwinAttrExtendedSecurity = 0x00400000
	darwinKauthFileSecNoACL    = ^uint32(0)
	darwinKauthFileSecMagic    = 0x012cc16d
)

type darwinAttrReference struct {
	Offset int32
	Length uint32
}

// macOS extended ACLs are independent of the owner/group/other mode bits. A
// 0600 file may still grant another principal access, so reject every actual
// extended ACL entry. The query is made with fgetattrlist on the already-open,
// no-follow descriptor and therefore cannot be redirected through a pathname.
func validatePassphraseExtendedACL(file *os.File) error {
	if file == nil {
		return errors.New("inspect passphrase file ACL: opened file handle is required")
	}
	attributes := unix.Attrlist{Bitmapcount: darwinAttrBitMapCount, Commonattr: darwinAttrExtendedSecurity}
	buffer := make([]byte, 64<<10)
	_, _, errno := syscall.Syscall6(syscall.SYS_FGETATTRLIST,
		file.Fd(),
		uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		0,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("inspect passphrase file extended ACL: %w", errno)
	}
	if len(buffer) < 4+int(unsafe.Sizeof(darwinAttrReference{})) {
		return errors.New("inspect passphrase file extended ACL: truncated attribute response")
	}
	responseLength := int(binary.LittleEndian.Uint32(buffer[:4]))
	if responseLength < 4+int(unsafe.Sizeof(darwinAttrReference{})) || responseLength > len(buffer) {
		return errors.New("inspect passphrase file extended ACL: invalid attribute response length")
	}
	referenceOffset := 4
	reference := darwinAttrReference{
		Offset: int32(binary.LittleEndian.Uint32(buffer[referenceOffset : referenceOffset+4])),
		Length: binary.LittleEndian.Uint32(buffer[referenceOffset+4 : referenceOffset+8]),
	}
	if reference.Length == 0 {
		return nil
	}
	dataStart := referenceOffset + int(reference.Offset)
	dataEnd := dataStart + int(reference.Length)
	if dataStart < referenceOffset+8 || dataEnd < dataStart || dataEnd > responseLength || reference.Length < 40 {
		return errors.New("inspect passphrase file extended ACL: invalid ACL attribute reference")
	}
	if binary.LittleEndian.Uint32(buffer[dataStart:dataStart+4]) != darwinKauthFileSecMagic {
		return errors.New("inspect passphrase file extended ACL: invalid kauth_filesec magic")
	}
	// kauth_filesec: magic(4), owner UUID(16), group UUID(16), then
	// kauth_acl whose first field is the entry count.
	entryCount := binary.LittleEndian.Uint32(buffer[dataStart+36 : dataStart+40])
	if entryCount != 0 && entryCount != darwinKauthFileSecNoACL {
		return errors.New("passphrase file must not have an extended access-control list")
	}
	return nil
}
