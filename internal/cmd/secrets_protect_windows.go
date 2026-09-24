//go:build windows

package cmd

import (
	"runtime"

	"golang.org/x/sys/windows"
)

// protectSecretFile replaces the file's inherited DACL with one entry: the
// current user, full control, no inheritance. POSIX modes are advisory on
// NTFS — without this the seeded .env stays readable by anyone the directory
// ACL lets in, which is exactly what 0o600 exists to prevent.
func protectSecretFile(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}
	dacl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return err
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
	// entries holds the SID only as a uintptr, which the GC does not trace:
	// without this the Tokenuser buffer could be collected while the syscalls
	// above are still reading it.
	runtime.KeepAlive(user)
	return err
}
