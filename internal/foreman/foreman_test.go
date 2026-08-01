package foreman

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sandbox pins BABYSIT_HOME so nothing touches the real ~/.babysit.
func sandbox(t *testing.T) {
	t.Helper()
	t.Setenv("BABYSIT_HOME", t.TempDir())
}

func TestSaveLoadRoundTrip(t *testing.T) {
	sandbox(t)
	want := Record{
		ID: "fm-a", Owner: "long",
		ProjectDir: "/repo", WorkspaceDir: "/repo",
		WorkspaceRef: "workspace:4", WorkspaceTitle: "bbs foreman fm-a",
		Session: "/s/abc.yaml", Status: "idle", Heartbeat: Now(),
	}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load("fm-a")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round trip lost fields:\n got %+v\nwant %+v", got, want)
	}
}

func TestLiveFollowsHeartbeat(t *testing.T) {
	cases := []struct {
		name      string
		heartbeat string
		want      bool
	}{
		{"fresh", time.Now().UTC().Format(time.RFC3339), true},
		{"just inside", time.Now().Add(-StaleAfter + time.Minute).UTC().Format(time.RFC3339), true},
		{"expired", time.Now().Add(-StaleAfter - time.Minute).UTC().Format(time.RFC3339), false},
		// A foreman that never heartbeat is not live — routing work to it
		// would park the ticket on a session that never came up.
		{"empty", "", false},
		{"garbage", "not-a-time", false},
	}
	for _, c := range cases {
		if got := (Record{Heartbeat: c.heartbeat}).Live(); got != c.want {
			t.Errorf("%s: Live()=%v want %v", c.name, got, c.want)
		}
	}
}

// One unparseable record must not blank the whole foreman list — the dashboard
// reads this directory to decide what it can assign to.
func TestListSkipsCorruptRecords(t *testing.T) {
	sandbox(t)
	if err := Save(Record{ID: "fm-b", Heartbeat: Now()}); err != nil {
		t.Fatal(err)
	}
	if err := Save(Record{ID: "fm-a", Heartbeat: Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path("fm-broken"), []byte("\tnot: [yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(), "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := List()
	if len(got) != 2 {
		t.Fatalf("want 2 usable records, got %d: %+v", len(got), got)
	}
	if got[0].ID != "fm-a" || got[1].ID != "fm-b" {
		t.Errorf("not id-sorted: %+v", got)
	}
}

func TestListEmptyWhenNoDir(t *testing.T) {
	sandbox(t)
	if got := List(); got != nil {
		t.Errorf("want nil before the dir exists, got %+v", got)
	}
}

// Save must not leave the .tmp it writes through, or List would report a
// phantom foreman on the next read.
func TestSaveLeavesNoTempFile(t *testing.T) {
	sandbox(t)
	if err := Save(Record{ID: "fm-a", Heartbeat: Now()}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "fm-a.yaml" {
		t.Errorf("unexpected dir contents: %+v", entries)
	}
}

func TestSaveRejectsRecordWithoutID(t *testing.T) {
	sandbox(t)
	if err := Save(Record{Owner: "long"}); err == nil {
		t.Fatal("want an error for an id-less record")
	}
}

func TestRemoveIsIdempotent(t *testing.T) {
	sandbox(t)
	if err := Save(Record{ID: "fm-a", Heartbeat: Now()}); err != nil {
		t.Fatal(err)
	}
	if err := Remove("fm-a"); err != nil {
		t.Fatal(err)
	}
	if err := Remove("fm-a"); err != nil {
		t.Fatalf("removing a gone record must be a no-op, got %v", err)
	}
	if _, err := Load("fm-a"); err == nil {
		t.Fatal("record still loads after Remove")
	}
}
