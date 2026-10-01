package review

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericdmoore/ferretta/internal/github"
)

func TestFetchCredentialsAreScoped(t *testing.T) {
	env := []string{"PATH=/bin", "GH_TOKEN=human-secret", "GITHUB_TOKEN=human-secret", "FERRETTA_GITHUB_CONFIG=/key-path", "GIT_CONFIG_COUNT=9", "GIT_CONFIG_VALUE_0=secret", "SSH_AUTH_SOCK=/agent", "SSH_ASKPASS=bad", "GIT_TRACE_CURL=1"}
	command, err := fetchCommand(context.Background(), "o/r", head, "app-secret", env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(command.Args, " "), "app-secret") || strings.Contains(strings.Join(command.Args, " "), "human-secret") {
		t.Fatal("credential in argv")
	}
	joined := strings.Join(command.Env, "\n")
	for _, bad := range []string{"human-secret", "/key-path", "SSH_AUTH_SOCK", "SSH_ASKPASS", "GIT_TRACE_CURL"} {
		if strings.Contains(joined, bad) {
			t.Fatal("inherited credential or trace", bad)
		}
	}
	if !strings.Contains(joined, "http.https://github.com/o/r.git.extraHeader") || !strings.Contains(joined, base64.StdEncoding.EncodeToString([]byte("x-access-token:app-secret"))) || !strings.Contains(joined, "GIT_TERMINAL_PROMPT=0") {
		t.Fatal("missing scoped auth")
	}
	clean := strings.Join(cleanEnvironment(command.Env), "\n")
	if strings.Contains(clean, "app-secret") || strings.Contains(clean, "GIT_CONFIG") {
		t.Fatal("fetch credential inherited by checks")
	}
	for _, tc := range [][3]string{{"bad", head, "token"}, {"o/r", "bad", "token"}, {"o/r", head, ""}, {"o/r", head, "bad\nheader"}} {
		if _, e := fetchCommand(context.Background(), tc[0], tc[1], tc[2], env); e == nil {
			t.Fatal("invalid fetch accepted")
		}
	}
}
func TestCheckProcessDropsCredentialEnvironment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "human-secret")
	t.Setenv("GH_TOKEN", "human-secret")
	t.Setenv("FERRETTA_GITHUB_CONFIG", "key-path")
	data, err := (Process{}).Run(context.Background(), t.TempDir(), "sh", "-c", `printf '%s/%s/%s' "$GITHUB_TOKEN" "$GH_TOKEN" "$FERRETTA_GITHUB_CONFIG"`)
	if err != nil || string(data) != "//" {
		t.Fatal("credential passed to check", err)
	}
}
func TestFetchOutcomes(t *testing.T) {
	ctx := context.Background()
	if e := (Process{}).Fetch(ctx, "o/r", head); e == nil {
		t.Fatal("missing credentials accepted")
	}
	p := Process{Credentials: func(context.Context, string) (string, error) { return "", errors.New("no app") }}
	if e := p.Fetch(ctx, "o/r", head); e == nil {
		t.Fatal("missing auth failure")
	}
	p.Credentials = func(context.Context, string) (string, error) { return "app-secret", nil }
	if e := p.Fetch(ctx, "bad", head); e == nil {
		t.Fatal("bad repo accepted")
	}
	// Fake executable at the effect boundary, not a real network call.
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, body := range []string{"#!/bin/sh\nexit 0\n", "#!/bin/sh\necho app-secret >&2\nexit 1\n"} {
		if e := os.WriteFile(filepath.Join(dir, "git"), []byte(body), 0700); e != nil {
			t.Fatal(e)
		}
		e := p.Fetch(ctx, "o/r", head)
		if strings.Contains(body, "exit 1") {
			if e == nil || strings.Contains(e.Error(), "app-secret") {
				t.Fatal("unsafe fetch error", e)
			}
		} else if e != nil {
			t.Fatal(e)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if e := p.Fetch(canceled, "o/r", head); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestMissingReviewAdapters(t *testing.T) {
	r := runner(&fakeModel{})
	r.GitHub = nil
	if _, e := r.PR(context.Background(), "o/r", 1); e == nil {
		t.Fatal("missing app accepted")
	}
	r.Fetch = nil
	if _, e := r.Prepare(context.Background(), "o/r", pr(), "scratch"); e == nil {
		t.Fatal("implicit fetch fallback")
	}
}

var _ GitHubConnection = (*github.Connection)(nil)
