package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

var testTime = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func testPR(n int) github.PullRequest {
	return github.PullRequest{Number: n, Head: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), State: "OPEN"}
}
func snapshot(t *testing.T, repo string, pulls []github.PullRequest, at time.Time) RecordSnapshot {
	t.Helper()
	c, err := PlanSnapshot(repo, pulls, at)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func privateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	return dir
}
func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := privateDir(t)
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}
func readStatus(t *testing.T, dir string) Status {
	t.Helper()
	s, err := ReadStatus(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func record(t *testing.T, s *Store, c RecordSnapshot) {
	t.Helper()
	if err := s.Record(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}
func TestSnapshotIdentityAndAdmission(t *testing.T) {
	a := snapshot(t, "Owner/Repo", []github.PullRequest{testPR(1)}, testTime)
	b := snapshot(t, "owner/repo", []github.PullRequest{testPR(1)}, testTime.Add(time.Hour))
	if a.revisions[0].ID != b.revisions[0].ID || a.repository != "owner/repo" {
		t.Fatal("unstable repository/revision identity")
	}
	for _, changed := range []github.PullRequest{
		{Number: 2, Head: testPR(1).Head, Base: testPR(1).Base, State: "OPEN"},
		{Number: 1, Head: strings.Repeat("c", 40), Base: testPR(1).Base, State: "OPEN"},
		{Number: 1, Head: testPR(1).Head, Base: strings.Repeat("c", 40), State: "OPEN"},
	} {
		c := snapshot(t, "owner/repo", []github.PullRequest{changed}, testTime)
		if c.revisions[0].ID == a.revisions[0].ID {
			t.Fatal("different revision shares identity")
		}
	}
	c := snapshot(t, "other/repo", []github.PullRequest{testPR(1)}, testTime)
	if c.revisions[0].ID == a.revisions[0].ID {
		t.Fatal("different repository shares identity")
	}
	draft := testPR(2)
	draft.Draft = true
	if len(snapshot(t, "o/r", []github.PullRequest{draft}, testTime).revisions) != 0 {
		t.Fatal("draft admitted")
	}
	bad := []github.PullRequest{testPR(1), testPR(1), testPR(1), testPR(1), testPR(1)}
	bad[0].Number = 0
	bad[1].Head = "bad"
	bad[2].Base = "bad"
	bad[3].State = "CLOSED"
	bad[4].Head = strings.Repeat("A", 40)
	for _, p := range bad {
		if _, err := PlanSnapshot("o/r", []github.PullRequest{p}, testTime); err == nil {
			t.Fatal("invalid PR admitted")
		}
	}
	if _, err := PlanSnapshot("o/r", []github.PullRequest{testPR(1), testPR(1)}, testTime); err == nil {
		t.Fatal("overlapping pagination admitted")
	}
	if _, err := PlanSnapshot("bad", nil, testTime); err == nil {
		t.Fatal("bad repository")
	}
	if _, err := PlanSnapshot("o/r", nil, time.Time{}); err == nil {
		t.Fatal("missing clock")
	}
}

func TestDurableSnapshotsAndOwner(t *testing.T) {
	s, dir := openTestStore(t)
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("second owner admitted")
	}
	if err := s.Record(context.Background(), RecordSnapshot{}); err == nil {
		t.Fatal("zero command accepted")
	}
	first := snapshot(t, "O/R", []github.PullRequest{testPR(1)}, testTime)
	record(t, s, first)
	record(t, s, snapshot(t, "o/r", []github.PullRequest{testPR(1)}, testTime.Add(time.Minute)))
	record(t, s, snapshot(t, "other/repo", []github.PullRequest{testPR(1)}, testTime))
	status := readStatus(t, dir)
	if status.Mode != "intake-only" || status.ObservedRevisions != 2 || len(status.Candidates) != 2 || !status.Candidates[0].FirstSeen.Equal(testTime) {
		t.Fatalf("bad status %+v", status)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	changed := testPR(1)
	changed.Head = strings.Repeat("c", 40)
	record(t, s, snapshot(t, "o/r", []github.PullRequest{changed}, testTime.Add(2*time.Minute)))
	record(t, s, first) // stale delivery cannot restore old revision
	status = readStatus(t, dir)
	if status.ObservedRevisions != 3 || status.Candidates[0].Head != changed.Head {
		t.Fatal("lost history/current revision", status)
	}
	if !status.Polls[0].ObservedAt.Equal(testTime.Add(2 * time.Minute)) {
		t.Fatal("stale poll overwrote newer poll")
	}
	changed.Draft = true
	record(t, s, snapshot(t, "o/r", []github.PullRequest{changed}, testTime.Add(3*time.Minute)))
	status = readStatus(t, dir)
	if status.ObservedRevisions != 3 || len(status.Candidates) != 1 || status.Candidates[0].Repository != "other/repo" {
		t.Fatal("draft remained current or another repo lost")
	}
	record(t, s, snapshot(t, "other/repo", nil, testTime.Add(time.Hour)))
	if len(readStatus(t, dir).Candidates) != 0 {
		t.Fatal("closed PR remained current")
	}
}

func TestSnapshotRollback(t *testing.T) {
	s, dir := openTestStore(t)
	record(t, s, snapshot(t, "o/r", []github.PullRequest{testPR(1)}, testTime))
	// Storage faults at successive writes must leave the old snapshot intact.
	for _, table := range []string{"observations", "current_prs", "polls"} {
		if _, err := s.db.Exec("CREATE TRIGGER fail_write BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'disk fault'); END"); err != nil {
			t.Fatal(err)
		}
		err := s.Record(context.Background(), snapshot(t, "o/r", []github.PullRequest{testPR(2)}, testTime.Add(time.Minute)))
		if err == nil {
			t.Fatal("storage failure lost")
		}
		status := readStatus(t, dir)
		if status.ObservedRevisions != 1 || len(status.Candidates) != 1 || status.Candidates[0].PR != 1 || !status.Polls[0].ObservedAt.Equal(testTime) {
			t.Fatal("partial transaction published", status)
		}
		if _, err := s.db.Exec("DROP TRIGGER fail_write"); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Record(ctx, snapshot(t, "o/r", nil, testTime)); err == nil {
		t.Fatal("canceled transaction accepted")
	}
}

func TestPrivateAndCompatibleState(t *testing.T) {
	if _, err := OpenStore("relative"); err == nil {
		t.Fatal("relative state accepted")
	}
	if _, err := ReadStatus(context.Background(), "relative"); err == nil {
		t.Fatal("relative reader accepted")
	}
	if _, err := ReadStatus(context.Background(), privateDir(t)); err == nil {
		t.Fatal("reader created missing state")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(filepath.Join(file, "state")); err == nil {
		t.Fatal("invalid path accepted")
	}
	dir := privateDir(t)
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("public state directory accepted")
	}
	for _, entry := range []string{"owner.lock", "state.sqlite"} {
		dir := privateDir(t)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(file, filepath.Join(dir, entry)); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenStore(dir); err == nil {
			t.Fatal("symlink followed")
		}
	}
	s, dir := openTestStore(t)
	if _, err := s.db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadStatus(context.Background(), dir); err == nil {
		t.Fatal("reader accepted future schema")
	}
	s.Close()
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("owner accepted future schema")
	}
	if err := os.Chmod(filepath.Join(dir, "state.sqlite"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("public database accepted")
	}
	corrupt := privateDir(t)
	if err := os.Mkdir(corrupt, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corrupt, "state.sqlite"), []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(corrupt); err == nil {
		t.Fatal("corrupt database accepted")
	}
	if _, err := ReadStatus(context.Background(), corrupt); err == nil {
		t.Fatal("corrupt database read")
	}
}

type sourceFunc func(context.Context, string) ([]github.PullRequest, error)

func (f sourceFunc) OpenPullRequests(ctx context.Context, repo string) ([]github.PullRequest, error) {
	return f(ctx, repo)
}

type recorderFunc func(context.Context, RecordSnapshot) error

func (f recorderFunc) Record(ctx context.Context, c RecordSnapshot) error { return f(ctx, c) }

func TestPollingRecoveryAndCancellation(t *testing.T) {
	s, dir := openTestStore(t)
	at := testTime
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pass := 0
	failures := 0
	r := Runner{Store: s, Now: func() time.Time { return at }, Source: sourceFunc(func(_ context.Context, repo string) ([]github.PullRequest, error) {
		if repo == "o/r" && pass == 1 {
			return nil, errors.New("temporary failure")
		}
		if pass == 2 {
			return []github.PullRequest{{Number: 0}}, nil
		}
		return []github.PullRequest{testPR(1)}, nil
	}), Report: func(string, error) { failures++ }, Wait: func(_ context.Context, d time.Duration) error {
		if d != time.Minute {
			t.Fatal("wrong interval")
		}
		status := readStatus(t, dir)
		if len(status.Candidates) != 2 || status.ObservedRevisions != 2 {
			t.Fatal("failed read removed previous input")
		}
		pass++
		at = at.Add(time.Minute)
		if pass == 3 {
			cancel()
			return ctx.Err()
		}
		return nil
	}}
	if err := r.Run(ctx, []string{"o/r", "other/repo"}, time.Minute, false); err != nil {
		t.Fatal(err)
	}
	if failures != 3 {
		t.Fatal("errors not reported", failures)
	}
	if err := r.Run(ctx, []string{"o/r"}, time.Minute, false); err != nil {
		t.Fatal(err)
	}
	if err := Wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := Wait(context.Background(), 0); err != nil {
		t.Fatal(err)
	} // no real sleep
}

func TestPollingErrors(t *testing.T) {
	errTest := errors.New("failed")
	r := Runner{Source: sourceFunc(func(context.Context, string) ([]github.PullRequest, error) { return nil, nil }), Store: recorderFunc(func(context.Context, RecordSnapshot) error { return nil }), Now: func() time.Time { return testTime }, Wait: func(context.Context, time.Duration) error { return errTest }, Report: func(string, error) {}}
	ctx := context.Background()
	if err := r.Run(ctx, []string{"o/r"}, time.Minute, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx, []string{"o/r"}, time.Minute, false); !errors.Is(err, errTest) {
		t.Fatal("wait failure lost")
	}
	if err := r.Run(ctx, nil, time.Minute, true); err == nil {
		t.Fatal("no repository")
	}
	if err := r.Run(ctx, []string{"bad"}, time.Minute, true); err == nil {
		t.Fatal("bad repository")
	}
	if err := (Runner{}).Run(ctx, []string{"o/r"}, time.Minute, true); err == nil {
		t.Fatal("zero runner")
	}
	r.Store = recorderFunc(func(context.Context, RecordSnapshot) error { return errTest })
	if err := r.Run(ctx, []string{"o/r"}, time.Minute, true); !errors.Is(err, errTest) {
		t.Fatal("store failure lost")
	}
	r.Source = sourceFunc(func(context.Context, string) ([]github.PullRequest, error) { return nil, errTest })
	if err := r.Run(ctx, []string{"o/r"}, time.Minute, true); !errors.Is(err, errTest) {
		t.Fatal("poll failure lost")
	}
	ctx, cancel := context.WithCancel(ctx)
	r.Source = sourceFunc(func(context.Context, string) ([]github.PullRequest, error) { cancel(); return nil, ctx.Err() })
	if err := r.Run(ctx, []string{"o/r"}, time.Minute, true); err != nil {
		t.Fatal("shutdown reported as failure")
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("write failure") }

func TestServiceCLI(t *testing.T) {
	c := CLI{Source: sourceFunc(func(context.Context, string) ([]github.PullRequest, error) {
		return []github.PullRequest{testPR(1)}, nil
	})}
	ctx := context.Background()
	dir := privateDir(t)
	args := []string{"run", "--repo", "O/R", "--repo", "other/repo", "--state", dir, "--once"}
	if code := c.Run(ctx, args, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	var output bytes.Buffer
	if code := c.Run(ctx, []string{"status", "--state", dir}, &output, io.Discard); code != 0 || !strings.Contains(output.String(), `"observed_revisions":2`) {
		t.Fatal(code, output.String())
	}
	if code := c.Run(ctx, []string{"status", "--state", dir}, brokenWriter{}, io.Discard); code != 1 {
		t.Fatal("write error lost")
	}
	if code := c.Run(ctx, []string{"status", "--state", privateDir(t)}, io.Discard, io.Discard); code != 1 {
		t.Fatal("missing state accepted")
	}
	for _, bad := range [][]string{nil, {"wrong"}, {"run", "--bad"}, {"run", "extra"}, {"run"}, {"run", "--repo", "bad"}, {"run", "--repo", "o/r", "--repo", "O/R"}, {"run", "--repo", "o/r", "--interval", "0s"}, {"run", "--repo", "o/r", "--state", "relative"}} {
		if c.Run(ctx, bad, io.Discard, io.Discard) != 1 {
			t.Fatal("bad command", bad)
		}
	}
	if (CLI{}).Run(ctx, args, io.Discard, io.Discard) != 1 {
		t.Fatal("missing source accepted")
	}
	c.Source = sourceFunc(func(context.Context, string) ([]github.PullRequest, error) {
		return nil, errors.New("provider unavailable")
	})
	if c.Run(ctx, args, io.Discard, io.Discard) != 1 {
		t.Fatal("failure hidden")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	c.Source = sourceFunc(func(context.Context, string) ([]github.PullRequest, error) { return nil, nil })
	if c.Run(ctx, []string{"run", "--repo", "o/r", "--once"}, io.Discard, io.Discard) != 0 {
		t.Fatal("default location")
	}
	if _, err := os.Stat(filepath.Join(root, "ferretta", "state", "state.sqlite")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if c.Run(ctx, []string{"status"}, io.Discard, io.Discard) != 1 {
		t.Fatal("no config root")
	}
}
