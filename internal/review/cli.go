package review

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
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

type CLI struct {
	Runner          Runner
	Setup           Setup
	Proposals       ProposalGitHub
	Checks          github.Checks
	PublicationMode string
	Progress        io.Writer
	Retry           *retryRequest
}

type GitHubConnection interface {
	PullRequests
	Token(context.Context, string) (string, error)
	Status(context.Context, string) (github.Identity, error)
}

func NewCLI(connection GitHubConnection) CLI {
	process := Process{Credentials: connection.Token}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("local model redirects are not permitted")
	}}
	proposals, _ := connection.(ProposalGitHub)
	checks, _ := connection.(github.Checks)
	return CLI{Proposals: proposals, Checks: checks, Runner: Runner{Exec: process, Searcher: process, GitHub: connection, Fetch: process.Fetch, Model: Ollama{HTTP: client}, Now: time.Now}, Setup: Setup{HTTP: client, Exec: process, Auth: connection.Status}}
}

func (c CLI) Run(ctx context.Context, args []string, output, stderr io.Writer) int {
	evaluationContext := ctx
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub owner/repository")
	number := flags.Int("pr", 0, "submitted PR number")
	policyPath := flags.String("policy", ".ferretta/review.json", "trusted local review policy")
	publish := flags.Bool("publish-proposals", false, "post intent proposals through the GitHub App and checkpoint for resumption")
	humans := flags.String("humans", "", "comma-separated allowlisted human logins; pinned in a new proposal session")
	resume := flags.String("resume", "", "resume/poll a durable proposal session directory")
	format := flags.String("format", "text", "text or json")
	evalPolicy := flags.String("eval-policy", "", "grade a completed manual review using an explicit judge policy")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *repo == "" || *number <= 0 || (*format != "text" && *format != "json") {
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
	if *evalPolicy != "" {
		if *publish || *resume != "" {
			return fail(fmt.Errorf("--eval-policy applies to ordinary manual reviews; use service watch for automatic proposal/evaluation workflows"))
		}
		judgeBytes, err := os.ReadFile(*evalPolicy)
		if err != nil {
			return fail(err)
		}
		if _, err := ParseEvaluationPolicy(judgeBytes); err != nil {
			return fail(err)
		}
	}
	if *publish || *resume != "" {
		return c.runProposals(ctx, *repo, *number, policy, data, *humans, *resume, *format, output, stderr)
	}
	if *humans != "" {
		return fail(fmt.Errorf("--humans requires --publish-proposals"))
	}
	ctx, cancel := reviewContext(ctx, policy)
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
	if err := PrintReport(output, report, *format); err != nil {
		return fail(err)
	}
	fmt.Fprintln(stderr, "Review report:", filepath.Join(runDir, "report.json"))
	if *evalPolicy != "" && report.Status != "awaiting_intent" {
		evalOutput := output
		if *format == "json" {
			evalOutput = stderr
		}
		if code := c.Eval(evaluationContext, []string{"--run", runDir, "--policy", *evalPolicy}, evalOutput, stderr); code != 0 {
			return 2
		}
	}
	if report.Status != "lgtm" {
		return 2
	}
	return 0
}

func (c CLI) Init(ctx context.Context, args []string, input io.Reader, output, stderr io.Writer) int {
	return c.Setup.Run(ctx, args, input, output, stderr)
}

func reviewContext(ctx context.Context, p Policy) (context.Context, context.CancelFunc) {
	if p.config.TimeoutSeconds == 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(p.config.TimeoutSeconds)*time.Second)
}
