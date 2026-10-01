package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepairGitCandidateIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	p := Process{}
	git := func(args ...string) string {
		t.Helper()
		out, err := p.Run(ctx, dir, "git", args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.name", "Human")
	git("config", "user.email", "human@example.test")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("old\n"), 0644)
	git("add", "a.txt")
	git("-c", "commit.gpgsign=false", "commit", "-qm", "initial")
	parent := git("rev-parse", "HEAD")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("fixed\n"), 0644)
	git("add", "a.txt")
	tree := git("write-tree")
	id := hashBytes([]byte("repair"))
	at := time.Unix(1000, 0)
	one, err := p.Candidate(ctx, dir, parent, tree, id, at)
	if err != nil {
		t.Fatal(err)
	}
	two, err := p.Candidate(ctx, dir, parent, tree, id, at)
	if err != nil || two != one {
		t.Fatal("candidate identity changed", one, two, err)
	}
	if git("rev-list", "--parents", "-n", "1", one) != one+" "+parent || git("rev-parse", "HEAD") != parent || git("show", one+":a.txt") != "fixed" {
		t.Fatal("candidate mutated input or lost repair")
	}
	if git("rev-parse", "refs/ferretta/repairs/"+id) != one {
		t.Fatal("candidate not retained")
	}
	if _, err = p.Candidate(ctx, dir, "bad", tree, id, at); err == nil {
		t.Fatal("invalid candidate accepted")
	}
	if _, err = p.Candidate(ctx, dir, parent, head, id, at); err == nil {
		t.Fatal("nonexistent tree accepted")
	}
}

func TestRepairPushScopeAndFailures(t *testing.T) {
	ctx := context.Background()
	cmd, err := pushCommand(ctx, "o/r", "feature/fix", head, candidateSHA, "app-secret", []string{"GH_TOKEN=human-secret", "GIT_TRACE=1", "PATH=" + os.Getenv("PATH")})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	env := strings.Join(cmd.Env, "\n")
	if strings.Contains(args, "secret") || strings.Contains(env, "human-secret") || strings.Contains(env, "GIT_TRACE") || !strings.Contains(args, "--force-with-lease=refs/heads/feature/fix:"+head) || !strings.Contains(args, candidateSHA+":refs/heads/feature/fix") {
		t.Fatal("unsafe publication", args)
	}
	for _, branch := range []string{"", "-bad", "bad\nref", "../bad"} {
		if _, err := pushCommand(ctx, "o/r", branch, head, candidateSHA, "token", nil); err == nil {
			t.Fatal(branch)
		}
	}
	if _, err := pushCommand(ctx, "o/r", "feature/fix", "bad", candidateSHA, "token", nil); err == nil {
		t.Fatal("invalid lease")
	}
	if err := (Process{}).Push(ctx, "o/r", "feature", head, candidateSHA); !errors.Is(err, errPushNotDispatched) {
		t.Fatal(err)
	}
	p := Process{WriteCredentials: func(context.Context, string) (string, error) { return "", errors.New("offline") }}
	if err := p.Push(ctx, "o/r", "feature", head, candidateSHA); !errors.Is(err, errPushNotDispatched) {
		t.Fatal(err)
	}
	p.WriteCredentials = func(context.Context, string) (string, error) { return "app-secret", nil }
	if err := p.Push(ctx, "o/r", "", head, candidateSHA); !errors.Is(err, errPushNotDispatched) {
		t.Fatal(err)
	}
	// Fake Git boundary: no network or credential is sent to GitHub.
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, body := range []string{"#!/bin/sh\nexit 0\n", "#!/bin/sh\nif [ \"$1\" = check-ref-format ]; then exit 0; fi\necho app-secret >&2\nexit 1\n"} {
		_ = os.WriteFile(filepath.Join(dir, "git"), []byte(body), 0700)
		err := p.Push(ctx, "o/r", "feature", head, candidateSHA)
		if strings.Contains(body, "exit 1") {
			if err == nil || errors.Is(err, errPushNotDispatched) || strings.Contains(err.Error(), "app-secret") {
				t.Fatal("uncertain error not sanitized", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{"#!/bin/sh\necho invalid\n", "#!/bin/sh\nif [ \"$1\" = update-ref ]; then exit 1; fi\necho " + candidateSHA + "\n"} {
		_ = os.WriteFile(filepath.Join(dir, "git"), []byte(body), 0700)
		if _, err := p.Candidate(ctx, dir, head, treeSHA, hashBytes([]byte("x")), time.Unix(1, 0)); err == nil {
			t.Fatal("bad git response accepted")
		}
	}
}

func TestRepairLeasePreservesConcurrentHumanCommit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	p := Process{}
	git := func(args ...string) string {
		t.Helper()
		out, err := p.Run(ctx, dir, "git", args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.name", "Human")
	git("config", "user.email", "human@example.test")
	git("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "input")
	parent := git("rev-parse", "HEAD")
	git("init", "--bare", "-q", remote)
	git("push", remote, parent+":refs/heads/feature")
	tree := git("rev-parse", "HEAD^{tree}")
	candidate, err := p.Candidate(ctx, dir, parent, tree, hashBytes([]byte("candidate")), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	// Another actor updates the server after the API expected-head preflight.
	git("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "human update")
	human := git("rev-parse", "HEAD")
	git("push", remote, human+":refs/heads/feature")
	cmd, err := pushCommand(ctx, "o/r", "feature", parent, candidate, "test-token", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	cmd.Dir = dir
	cmd.Args[len(cmd.Args)-2] = remote // Offline adapter fixture only.
	if err := cmd.Run(); err == nil {
		t.Fatal("stale lease overwrote human commit")
	}
	if got := git("--git-dir="+remote, "rev-parse", "refs/heads/feature"); got != human {
		t.Fatal("human commit lost")
	}
	// The same candidate succeeds when the expected parent really is current.
	git("--git-dir="+remote, "update-ref", "refs/heads/feature", parent)
	cmd, err = pushCommand(ctx, "o/r", "feature", parent, candidate, "test-token", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	cmd.Dir = dir
	cmd.Args[len(cmd.Args)-2] = remote
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if git("--git-dir="+remote, "rev-parse", "refs/heads/feature") != candidate {
		t.Fatal("candidate absent")
	}
}
