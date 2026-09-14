//go:build !unix

package cmd

// Non-unix builds get no auto-started watcher and no single-instance lock:
// the release targets are darwin/linux, and a from-source build elsewhere
func acquireWatchLock(id string) (func(), error) { return func() {}, nil }

func startWatcher() error { return nil }
