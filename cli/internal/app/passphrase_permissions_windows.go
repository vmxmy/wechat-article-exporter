//go:build windows

package app

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func validatePassphraseFilePermissions(path string, file *os.File, _ os.FileInfo) error {
	if file == nil {
		return fmt.Errorf("inspect passphrase file ACL: opened file handle is required")
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("inspect passphrase file ACL: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("inspect passphrase file owner: %w", err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if owner == nil || !owner.Equals(user.User.Sid) {
		return fmt.Errorf("passphrase file must be owned by the current user")
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("passphrase file must have a protected access-control list")
	}
	control, _, err := sd.Control()
	if err != nil {
		return fmt.Errorf("inspect passphrase file ACL protection: %w", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("passphrase file access-control list must not inherit permissions")
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	allowed := []*windows.SID{user.User.Sid, administrators, system}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var raw *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &raw); err != nil {
			return fmt.Errorf("inspect passphrase file ACL entry %d: %w", index, err)
		}
		if raw == nil {
			return fmt.Errorf("inspect passphrase file ACL entry %d: empty ACE", index)
		}
		header := raw.Header
		if header.AceSize < uint16(unsafe.Sizeof(windows.ACE_HEADER{})) {
			return fmt.Errorf("inspect passphrase file ACL entry %d: truncated ACE", index)
		}
		if header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			// Deny and audit ACEs do not grant access. Object/callback/unknown
			// allow ACEs require different SID offsets, so fail closed rather
			// than silently accepting an authorization we did not parse.
			if isWindowsAllowACE(header.AceType) {
				return fmt.Errorf("passphrase file contains unsupported allow ACE type %d", header.AceType)
			}
			continue
		}
		minimumSize := uint16(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart) + 8)
		if header.AceSize < minimumSize {
			return fmt.Errorf("inspect passphrase file ACL entry %d: truncated allow ACE", index)
		}
		ace := (*windows.ACCESS_ALLOWED_ACE)(unsafe.Pointer(raw))
		aceBytes := unsafe.Slice((*byte)(unsafe.Pointer(raw)), int(header.AceSize))
		sidOffset := int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart))
		subAuthorityCount := int(aceBytes[sidOffset+1])
		if sidOffset+8+4*subAuthorityCount > len(aceBytes) {
			return fmt.Errorf("inspect passphrase file ACL entry %d: truncated SID", index)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() {
			return fmt.Errorf("inspect passphrase file ACL entry %d: invalid SID", index)
		}
		permitted := false
		for _, candidate := range allowed {
			if sid.Equals(candidate) {
				permitted = true
				break
			}
		}
		if !permitted && ace.Mask != 0 {
			return fmt.Errorf("passphrase file grants access to an unauthorized principal")
		}
	}
	return nil
}

func isWindowsAllowACE(aceType uint8) bool {
	switch aceType {
	case 0x00, // ACCESS_ALLOWED_ACE_TYPE
		0x04, // ACCESS_ALLOWED_COMPOUND_ACE_TYPE
		0x05, // ACCESS_ALLOWED_OBJECT_ACE_TYPE
		0x09, // ACCESS_ALLOWED_CALLBACK_ACE_TYPE
		0x0b: // ACCESS_ALLOWED_CALLBACK_OBJECT_ACE_TYPE
		return true
	default:
		return false
	}
}
