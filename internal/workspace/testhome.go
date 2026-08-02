package workspace

import "testing"

// TestHome redirects the workspace store into a temp dir for the duration of a
// test and returns it.
//
// It exists so tests have an obvious right thing to call, but it is not what
// makes them safe — Dir() panics on an unset BABYSIT_HOME under test, so a
// test that forgets this helper fails loudly instead of writing into the
// human's real ~/.babysit. Redirecting BABYSIT_STATE_DIR does NOT redirect
// this store: that variable is read by internal/config, this one by
// internal/identity, and neither reads the other.
func TestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("BABYSIT_HOME", home)
	return home
}
