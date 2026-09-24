//go:build !windows

package cmd

// POSIX mode bits are real: the 0o600 on the write is the enforcement.
func protectSecretFile(path string) error { return nil }
