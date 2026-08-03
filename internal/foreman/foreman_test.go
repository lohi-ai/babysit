package foreman

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// The id becomes a file name, and it arrives from the command line today and
// over HTTP once the dashboard spawns foremen. Unchecked, "../../x" escapes
// the foremen directory: Save writes a record outside ~/.babysit and Remove
// deletes whatever .yaml already sits there.
func TestIDCannotEscapeTheForemenDir(t *testing.T) {
	sandbox(t)
	outside := filepath.Join(filepath.Dir(Dir()), "victim.yaml")
	if err := os.WriteFile(outside, []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"../victim", "../../victim", "a/../../victim", "/etc/victim", ".hidden"} {
		if err := Save(Record{ID: id, Heartbeat: Now()}); err == nil {
			t.Errorf("Save(%q) was allowed", id)
		}
		if _, err := Load(id); err == nil {
			t.Errorf("Load(%q) was allowed", id)
		}
		if err := Remove(id); err == nil {
			t.Errorf("Remove(%q) was allowed", id)
		}
	}

	if b, err := os.ReadFile(outside); err != nil || string(b) != "precious\n" {
		t.Errorf("file outside the foremen dir was touched: %q %v", b, err)
	}
}

func TestValidIDAcceptsRealIDs(t *testing.T) {
	for _, id := range []string{"fm-babysit", "fm-my_repo.v2", "a", "FM-1"} {
		if err := ValidID(id); err != nil {
			t.Errorf("ValidID(%q) = %v, want nil", id, err)
		}
	}
}

// Two writers on one record — a foreman heartbeating while the dashboard
// updates it — must not publish a mixture of both payloads. A fixed
// "<id>.yaml.tmp" lets them interleave into the same file before the rename.
func TestConcurrentSavesNeverPublishAMixture(t *testing.T) {
	sandbox(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := "short"
			if i%2 == 0 {
				owner = strings.Repeat("long", 200)
			}
			if err := Save(Record{ID: "fm-a", Owner: owner, Heartbeat: Now()}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	r, err := Load("fm-a")
	if err != nil {
		t.Fatalf("record is unreadable after concurrent saves: %v", err)
	}
	if r.Owner != "short" && r.Owner != strings.Repeat("long", 200) {
		t.Errorf("owner is a mixture of two writers: %q", r.Owner)
	}
	entries, err := os.ReadDir(Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
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

// ─── grant ───────────────────────────────────────────────────────────────────

func TestNoGrantSerializesToNothing(t *testing.T) {
	sandbox(t)
	if err := Save(Record{ID: "fm-a", Heartbeat: Now()}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path("fm-a"))
	if err != nil {
		t.Fatal(err)
	}
	// Criterion 8: without a grant the record must be what it always was.
	if strings.Contains(string(b), "grant") {
		t.Errorf("a record with no grant mentions grant:\n%s", b)
	}
}

func TestGrantRoundTrips(t *testing.T) {
	sandbox(t)
	want := Record{ID: "fm-a", Heartbeat: Now(), Grant: &Grant{
		GrantedBy: "long", At: Now(), ExpiresAt: Now(),
		MaxApprovals: 3, Used: 1, Tickets: []string{"bs-a", "bs-b"},
	}}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load("fm-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Grant == nil {
		t.Fatal("grant did not survive the round trip")
	}
	if got.Grant.GrantedBy != "long" || got.Grant.MaxApprovals != 3 ||
		got.Grant.Used != 1 || len(got.Grant.Tickets) != 2 {
		t.Errorf("grant came back changed: %+v", got.Grant)
	}
}

func TestAllows(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour).Format(time.RFC3339)
	future := now.Add(time.Hour).Format(time.RFC3339)

	cases := []struct {
		name   string
		grant  *Grant
		hold   *Hold
		ticket string
		ok     bool
		reason string
	}{
		{"no grant is default autonomy", nil, nil, "bs-a", true, ""},
		{"unbounded allows anything", &Grant{}, nil, "bs-a", true, ""},
		{"unexpired allows", &Grant{ExpiresAt: future}, nil, "bs-a", true, ""},
		{"expired denies", &Grant{ExpiresAt: past}, nil, "bs-a", false, "grant expired"},
		{"corrupt expiry denies", &Grant{ExpiresAt: "soon"}, nil, "bs-a", false, "unreadable"},
		{"budget left allows", &Grant{MaxApprovals: 2, Used: 1}, nil, "bs-a", true, ""},
		{"budget spent denies", &Grant{MaxApprovals: 2, Used: 2}, nil, "bs-a", false, "budget spent"},
		{"in scope allows", &Grant{Tickets: []string{"bs-a"}}, nil, "bs-a", true, ""},
		{"out of scope denies", &Grant{Tickets: []string{"bs-b"}}, nil, "bs-a", false, "outside the grant"},
		{"scoped but unnamed denies", &Grant{Tickets: []string{"bs-b"}}, nil, "", false, "no ticket was named"},
		{"hold escalates", nil, &Hold{HeldBy: "long", At: Now()}, "bs-a", false, "human hold"},
		{"hold wins over grant", &Grant{}, &Hold{HeldBy: "long", At: Now()}, "bs-a", false, "human hold"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, reason := Record{ID: "fm-a", Grant: c.grant, Hold: c.hold}.Allows(c.ticket, now)
			if ok != c.ok {
				t.Fatalf("Allows = %v (%s), want %v", ok, reason, c.ok)
			}
			if !ok && !strings.Contains(reason, c.reason) {
				t.Errorf("reason %q does not mention %q", reason, c.reason)
			}
		})
	}
}

func TestNoHoldSerializesToNothing(t *testing.T) {
	sandbox(t)
	if err := Save(Record{ID: "fm-a", Heartbeat: Now()}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path("fm-a"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hold") {
		t.Errorf("a record with no hold mentions hold:\n%s", b)
	}
}

func TestHoldRoundTrips(t *testing.T) {
	sandbox(t)
	want := Record{ID: "fm-a", Heartbeat: Now(), Hold: &Hold{HeldBy: "long", At: Now()}}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load("fm-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Hold == nil {
		t.Fatal("hold did not survive the round trip")
	}
	if got.Hold.HeldBy != "long" || got.Hold.At == "" {
		t.Errorf("hold came back changed: %+v", got.Hold)
	}
}

// The budget only bounds anything if spending it is recorded.
func TestConsumeSpendsBudget(t *testing.T) {
	sandbox(t)
	if err := Save(Record{ID: "fm-a", Heartbeat: Now(), Grant: &Grant{MaxApprovals: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := Consume("fm-a"); err != nil {
		t.Fatal(err)
	}
	r, err := Load("fm-a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Grant.Used != 1 {
		t.Fatalf("Used = %d, want 1", r.Grant.Used)
	}
	if ok, reason := r.Allows("bs-a", time.Now()); ok {
		t.Errorf("a spent grant still allows (%s)", reason)
	}
}

func TestConsumeWithoutGrantErrors(t *testing.T) {
	sandbox(t)
	if err := Save(Record{ID: "fm-a", Heartbeat: Now()}); err != nil {
		t.Fatal(err)
	}
	if err := Consume("fm-a"); err == nil {
		t.Fatal("consuming a nonexistent grant must error")
	}
}
