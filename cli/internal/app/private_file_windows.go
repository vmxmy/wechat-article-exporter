//go:build windows

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func securePrivateFile(file *os.File) error {
	if file == nil {
		return errors.New("private file handle is unavailable")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, 3)
	for _, trustee := range []struct {
		sid  *windows.SID
		kind windows.TRUSTEE_TYPE
	}{{user.User.Sid, windows.TRUSTEE_IS_USER}, {administrators, windows.TRUSTEE_IS_GROUP}, {system, windows.TRUSTEE_IS_USER}} {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: trustee.kind,
				TrusteeValue: windows.TrusteeValueFromSID(trustee.sid),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
