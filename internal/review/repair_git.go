package review

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var errPushNotDispatched = errors.New("repair push not dispatched")

// Candidate creation and PR-branch publication are deliberately separate. A
// future wave scheduler can inspect/integrate candidates without publishing them.
type RepairGit interface {
	Candidate(context.Context, string, string, string, string, time.Time) (string, error)
	Push(context.Context, string, string, string, string) error
}

func (Process) Candidate(ctx context.Context, dir, parent, tree, id string, at time.Time) (string, error) {
	if !shaPattern.MatchString(parent) || !shaPattern.MatchString(tree) || len(id) != 64 || !retryID.MatchString(id) || at.IsZero() {
		return "", fmt.Errorf("candidate requires exact parent/tree, stable identity and timestamp")
	}
	command := exec.CommandContext(ctx, "git", "-c", "commit.gpgsign=false", "-c", "i18n.commitEncoding=UTF-8", "commit-tree", tree, "-p", parent)
	command.Dir = dir
	date := at.UTC().Format(time.RFC3339)
	command.Env = append(cleanEnvironment(os.Environ()), "GIT_AUTHOR_NAME=Ferretta", "GIT_AUTHOR_EMAIL=ferretta@users.noreply.github.com", "GIT_COMMITTER_NAME=Ferretta", "GIT_COMMITTER_EMAIL=ferretta@users.noreply.github.com", "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	command.Stdin = strings.NewReader("Repair PR findings\n\nFerretta-attempt: " + id + "\n")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("create repair candidate: %w", err)
	}
	commit := strings.TrimSpace(string(output))
	if !shaPattern.MatchString(commit) {
		return "", fmt.Errorf("invalid candidate commit")
	}
	// Retain the deterministic object across restarts and ordinary Git GC. No PR
	// branch is changed. Hooks and the user's commit identity are not consulted.
	cmd := exec.CommandContext(ctx, "git", "update-ref", "refs/ferretta/repairs/"+id, commit)
	cmd.Dir, cmd.Env = dir, cleanEnvironment(os.Environ())
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("retain repair candidate: %w", err)
	}
	return commit, nil
}

func pushCommand(ctx context.Context, repo, branch, parent, candidate, token string, env []string) (*exec.Cmd, error) {
	// Git validates the full ref separately; no shorthand, revision expressions,
	// configured push URLs, remotes, or model-controlled command options.
	if branch == "" || strings.HasPrefix(branch, "-") || strings.ContainsAny(branch, "\r\n\x00") || !shaPattern.MatchString(candidate) {
		return nil, fmt.Errorf("invalid repair branch or candidate")
	}
	check := exec.CommandContext(ctx, "git", "check-ref-format", "refs/heads/"+branch)
	check.Env = cleanEnvironment(env)
	if err := check.Run(); err != nil {
		return nil, fmt.Errorf("invalid repair branch")
	}
	command, err := fetchCommand(ctx, repo, parent, token, env)
	if err != nil {
		return nil, err
	}
	command.Args = []string{"git", "-c", "push.followTags=false", "-c", "push.recurseSubmodules=no", "push", "--no-verify", "--porcelain", "--force-with-lease=refs/heads/" + branch + ":" + parent, "https://github.com/" + repo + ".git", candidate + ":refs/heads/" + branch}
	return command, nil
}

func (p Process) Push(ctx context.Context, repo, branch, parent, candidate string) error {
	if p.WriteCredentials == nil {
		return fmt.Errorf("%w: App contents-write credentials required", errPushNotDispatched)
	}
	token, err := p.WriteCredentials(ctx, repo)
	if err != nil {
		return fmt.Errorf("%w: %v", errPushNotDispatched, err)
	}
	command, err := pushCommand(ctx, repo, branch, parent, candidate, token, os.Environ())
	if err != nil {
		return fmt.Errorf("%w: %v", errPushNotDispatched, err)
	}
	// This executor runs from the trusted checkout. The candidate has already been
	// proved to be one child of parent. The explicit lease enforces compare/swap,
	// including a race after the API preflight; it never permits replacing new work.
	var output bytes.Buffer
	command.Stdout = &output // Git may emit credential-bearing errors; never persist them.
	if err := command.Run(); err != nil {
		return fmt.Errorf("repair push outcome uncertain; reconcile remote head before any retry")
	}
	return nil
}
