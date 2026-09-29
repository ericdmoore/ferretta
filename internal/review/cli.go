package review

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Process struct {
	Credentials func(context.Context, string) (string, error)
}

func (Process) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = cleanEnvironment(os.Environ())
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s failed: %w\n%s", name, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

type CLI struct{ Runner Runner }

type GitHubConnection interface {
	PullRequests
	Token(context.Context, string) (string, error)
}

func NewCLI(connection GitHubConnection) CLI {
	process := Process{Credentials: connection.Token}
	return CLI{Runner: Runner{Exec: process, GitHub: connection, Fetch: process.Fetch, Model: Ollama{HTTP: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("local model redirects are not permitted")
	}}}, Now: time.Now}}
}

func (c CLI) Run(ctx context.Context, args []string, output, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub owner/repository")
	number := flags.Int("pr", 0, "submitted PR number")
	policyPath := flags.String("policy", ".ferretta/review.json", "trusted local review policy")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *repo == "" || *number <= 0 {
		return fail(fmt.Errorf("review requires --repo owner/repository and --pr N"))
	}
	data, err := os.ReadFile(*policyPath)
	if err != nil {
		return fail(err)
	}
	policy, err := ParsePolicy(data)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.config.TimeoutSeconds)*time.Second)
	defer cancel()
	pr, err := c.Runner.PR(ctx, *repo, *number)
	if err != nil {
		return fail(err)
	}
	workspace, err := os.MkdirTemp("", "ferretta-review-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(workspace)
	w, err := c.Runner.Prepare(ctx, *repo, pr, workspace)
	if err != nil {
		return fail(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.Runner.Exec.Run(cleanup, "", "git", "worktree", "remove", "--force", workspace); err != nil {
			fmt.Fprintln(stderr, "Workspace cleanup:", err)
		}
	}()
	parent := filepath.Join(".ferretta", "runs")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return fail(err)
	}
	runDir, err := os.MkdirTemp(parent, fmt.Sprintf("pr-%d-%s-", pr.Number, pr.Head[:8]))
	if err != nil {
		return fail(err)
	}
	checkpoint := func(report Report, messages []Message) error {
		return Save(filepath.Join(runDir, "session.json"), struct {
			Report   Report    `json:"report"`
			Messages []Message `json:"messages"`
		}{report, messages})
	}
	report := c.Runner.Review(ctx, policy, w, data, checkpoint)
	latest, err := c.Runner.PR(ctx, *repo, *number)
	if err != nil || latest.Head != pr.Head || latest.Base != pr.Base {
		report.Status = "incomplete"
		report.Summary = "PR revision could not be revalidated after review"
	}
	// The public report includes provenance, but not raw model reasoning.
	for i := range report.Attempts {
		report.Attempts[i].Message = Message{Role: "assistant"}
	}
	if err := Save(filepath.Join(runDir, "report.json"), report); err != nil {
		return fail(err)
	}
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return fail(err)
	}
	fmt.Fprintln(stderr, "Review report:", filepath.Join(runDir, "report.json"))
	if report.Status != "lgtm" {
		return 2
	}
	return 0
}
