package review

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

// Doctor inspects readiness, never generates model responses or runs PR checks.
func (c CLI) Doctor(ctx context.Context, args []string, output, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "repository to verify")
	path := flags.String("policy", ".ferretta/review.json", "trusted review policy")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || (*repo != "" && !github.ValidRepository(*repo)) {
		return fail(fmt.Errorf("use --repo owner/repo and --policy PATH"))
	}
	if c.Setup.Exec == nil || c.Setup.Auth == nil || c.Setup.HTTP == nil {
		return fail(fmt.Errorf("diagnostic adapters are not configured"))
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var report bytes.Buffer
	ready := true
	issue := func(area, reason, next string) {
		ready = false
		fmt.Fprintf(&report, "%s: %s\n  Next: %s\n", area, reason, next)
	}
	if _, err := c.Setup.Exec.Run(ctx, "", "git", "--version"); err != nil {
		issue("Git", "unavailable", "Install Git and ensure it is on PATH.")
	} else {
		fmt.Fprintln(&report, "Git: available")
	}
	if *repo == "" {
		issue("GitHub", "repository not selected", "ferretta doctor --repo owner/repo")
	} else {
		identity, err := c.Setup.Auth(ctx, *repo)
		if err != nil {
			issue("GitHub App", "access not verified", "ferretta auth github --setup; then ferretta auth github --repo "+*repo)
		} else {
			fmt.Fprintf(&report, "GitHub App: %s has read access to %s\n", identity.BotLogin, *repo)
		}
		remote, err := c.Setup.Exec.Run(ctx, "", "git", "remote", "get-url", "origin")
		actual := strings.TrimSuffix(strings.TrimSpace(string(remote)), ".git")
		if err != nil || (actual != "https://github.com/"+*repo && actual != "git@github.com:"+*repo) {
			issue("Checkout", "origin does not match selected repository", "Run from a checkout of "+*repo+".")
		} else {
			fmt.Fprintln(&report, "Checkout: origin matches")
		}
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		issue("Policy", "unavailable", "ferretta init --policy "+fmt.Sprintf("%q", *path))
	} else {
		p, err := ParsePolicy(data)
		if err != nil {
			issue("Policy", err.Error(), "Correct the policy; init never overwrites existing files.")
		} else {
			fmt.Fprintf(&report, "Policy: %s; %s; context %d; %d turns; %ds limit\n", *path, p.config.Model, p.config.ContextTokens, p.config.MaxTurns, p.config.TimeoutSeconds)
			if err := (Ollama{HTTP: c.Setup.HTTP}).Capabilities(ctx, p); err != nil {
				issue("Model", err.Error(), "ferretta init --discover; inspect the selected endpoint/model and policy.")
			} else {
				fmt.Fprintln(&report, "Model: local tools/thinking metadata and context capacity verified; inference not tested")
			}
			fmt.Fprintln(&report, "Required checks (not executed):")
			for _, check := range p.config.Checks {
				fmt.Fprintf(&report, "  %q\n", check)
			}
		}
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if ready {
		fmt.Fprintf(&report, "Ready for a bounded advisory review: ferretta review --repo %s --pr N --policy %q\n", *repo, *path)
	}
	fmt.Fprintln(&report, "OpenRouter and automatic review dispatch are not implemented.")
	if _, err := output.Write(report.Bytes()); err != nil {
		return fail(err)
	}
	if !ready {
		return 2
	}
	return 0
}
