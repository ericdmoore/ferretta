// Package cli adapts command-line input and provider evidence to the core.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/intent"
	"github.com/ericdmoore/ferretta/internal/service"
)

type CommentsClient interface {
	Comments(context.Context, string, int) ([]github.Comment, error)
}

type ReviewRunner interface {
	Run(context.Context, []string, io.Writer, io.Writer) int
}

type Onboarding interface {
	Init(context.Context, []string, io.Reader, io.Writer, io.Writer) int
	Doctor(context.Context, []string, io.Writer, io.Writer) int
	Demo(context.Context, []string, io.Writer, io.Writer) int
	Probe(context.Context, []string, io.Writer, io.Writer) int
}

func Run(ctx context.Context, args []string, input io.Reader, output, errors io.Writer, client CommentsClient) int {
	return RunWithReviewer(ctx, args, input, output, errors, client, nil)
}

func RunWithReviewer(ctx context.Context, args []string, input io.Reader, output, errors io.Writer, client CommentsClient, reviewer ReviewRunner) int {
	fail := func(err error) int { fmt.Fprintln(errors, err); return 1 }
	if len(args) > 0 && args[0] == "eval" {
		evaluator, ok := reviewer.(interface {
			Eval(context.Context, []string, io.Writer, io.Writer) int
		})
		if !ok {
			return fail(fmt.Errorf("evaluation adapter is not configured"))
		}
		return evaluator.Eval(ctx, args[1:], output, errors)
	}
	if len(args) > 0 && (args[0] == "init" || args[0] == "doctor" || args[0] == "demo" || args[0] == "model") {
		onboarding, ok := reviewer.(Onboarding)
		if !ok {
			return fail(fmt.Errorf("onboarding adapter is not configured"))
		}
		switch args[0] {
		case "init":
			return onboarding.Init(ctx, args[1:], input, output, errors)
		case "doctor":
			return onboarding.Doctor(ctx, args[1:], output, errors)
		case "demo":
			return onboarding.Demo(ctx, args[1:], output, errors)
		case "model":
			if len(args) < 2 || args[1] != "test" {
				return fail(fmt.Errorf("use ferretta model test [--policy PATH]; this runs bounded inference"))
			}
			return onboarding.Probe(ctx, args[2:], output, errors)
		}
	}
	const usage = "usage: ferretta demo | ferretta init | ferretta doctor --repo owner/repo | ferretta model test | ferretta service run --repo owner/repo [--state /absolute/path] | ferretta service watch --repo owner/repo --pr N --state /absolute/path --review-policy PATH --judge-policy PATH --humans login [--publication checks|comments] | ferretta service status [--state /absolute/path] | ferretta auth github --repo owner/repo | ferretta intent parse | ferretta intent inspect --repo owner/repo --pr N --humans login --agents login | ferretta review --repo owner/repo --pr N [--policy .ferretta/review.json] [--eval-policy PATH] | ferretta eval --run PATH --policy PATH"
	if len(args) > 0 && args[0] == "service" {
		if len(args) > 1 && args[1] == "watch" {
			watcher, ok := reviewer.(interface {
				Watch(context.Context, []string, io.Writer, io.Writer) int
			})
			if !ok {
				return fail(fmt.Errorf("workflow adapter is not configured"))
			}
			return watcher.Watch(ctx, args[2:], output, errors)
		}
		source, _ := client.(service.Source)
		return (service.CLI{Source: source}).Run(ctx, args[1:], output, errors)
	}
	if len(args) > 0 && args[0] == "auth" {
		if len(args) < 2 || args[1] != "github" {
			return fail(fmt.Errorf("use ferretta auth github --repo owner/repo"))
		}
		flags := flag.NewFlagSet("auth github", flag.ContinueOnError)
		flags.SetOutput(errors)
		repo := flags.String("repo", "", "GitHub owner/repository to verify")
		setup := flags.Bool("setup", false, "show GitHub App registration and connection steps")
		configure := flags.Bool("configure", false, "verify and save a new machine App connection")
		clientID := flags.String("client-id", "", "GitHub App client ID")
		installation := flags.Int64("installation-id", 0, "GitHub App installation ID")
		keyPath := flags.String("private-key-file", "", "absolute path to private key; never the key itself")
		if err := flags.Parse(args[2:]); err != nil {
			return 1
		}
		if *setup {
			if flags.NArg() != 0 || *configure || *clientID != "" || *installation != 0 || *keyPath != "" {
				return fail(fmt.Errorf("--setup shows instructions only"))
			}
			_, err := fmt.Fprintln(output, githubSetupGuide)
			if err != nil {
				return fail(err)
			}
			return 0
		}
		if flags.NArg() != 0 || !github.ValidRepository(*repo) {
			return fail(fmt.Errorf("provide --repo owner/repository"))
		}
		if *configure {
			connector, ok := client.(interface {
				Configure(context.Context, string, github.AppConfig) (github.Identity, error)
			})
			if !ok {
				return fail(fmt.Errorf("GitHub App configuration adapter required"))
			}
			identity, err := connector.Configure(ctx, *repo, github.AppConfig{ClientID: *clientID, InstallationID: *installation, PrivateKeyFile: *keyPath})
			if err != nil {
				return fail(err)
			}
			if err := json.NewEncoder(output).Encode(identity); err != nil {
				return fail(err)
			}
			return 0
		}
		if *clientID != "" || *installation != 0 || *keyPath != "" {
			return fail(fmt.Errorf("connection fields require --configure"))
		}
		auth, ok := client.(interface {
			Status(context.Context, string) (github.Identity, error)
		})
		if !ok {
			return fail(fmt.Errorf("GitHub App connection is required"))
		}
		status, err := auth.Status(ctx, *repo)
		if err != nil {
			return fail(err)
		}
		if err := json.NewEncoder(output).Encode(status); err != nil {
			return fail(err)
		}
		return 0
	}
	if len(args) > 0 && args[0] == "review" {
		if reviewer == nil {
			return fail(fmt.Errorf("review runner is not configured"))
		}
		return reviewer.Run(ctx, args[1:], output, errors)
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := fmt.Fprintln(output, usage); err != nil {
			return fail(err)
		}
		return 0
	}
	if len(args) < 2 || args[0] != "intent" {
		return fail(fmt.Errorf("%s", usage))
	}
	encode := func(value any) int {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(value); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "intent" && args[1] == "parse" {
		if len(args) != 2 {
			return fail(fmt.Errorf("intent parse reads standard input and accepts no arguments"))
		}
		data, err := io.ReadAll(io.LimitReader(input, (8<<20)+1))
		if err != nil {
			return fail(err)
		}
		if len(data) > 8<<20 {
			return fail(fmt.Errorf("input exceeds 8 MiB"))
		}
		markers, err := intent.Parse(string(data))
		if err != nil {
			return fail(err)
		}
		return encode(markers)
	}
	if args[0] != "intent" || args[1] != "inspect" {
		return fail(fmt.Errorf("unknown command"))
	}
	flags := flag.NewFlagSet("intent inspect", flag.ContinueOnError)
	flags.SetOutput(errors)
	repo := flags.String("repo", "", "GitHub owner/repository")
	pr := flags.Int("pr", 0, "pull request number")
	humans := flags.String("humans", "", "comma-separated authorized human logins")
	agents := flags.String("agents", "", "comma-separated authorized agent logins")
	if err := flags.Parse(args[2:]); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *repo == "" || *pr <= 0 {
		return fail(fmt.Errorf("provide --repo and a positive --pr; positional arguments are not accepted"))
	}
	policy := intent.Policy{Humans: strings.Split(*humans, ","), Agents: strings.Split(*agents, ",")}
	if err := policy.Validate(); err != nil {
		return fail(err)
	}
	if client == nil {
		return fail(fmt.Errorf("GitHub client is required"))
	}
	comments, err := client.Comments(ctx, *repo, *pr)
	if err != nil {
		return fail(err)
	}
	evidence := make([]intent.Comment, 0, len(comments))
	for _, comment := range comments {
		evidence = append(evidence, intent.Comment{ID: comment.ID, Author: comment.User.Login, AuthorType: comment.User.Type, URL: comment.URL, Body: comment.Body, Edited: comment.UpdatedAt != comment.CreatedAt})
	}
	report, err := intent.Inspect(evidence, policy)
	if err != nil {
		return fail(err)
	}
	result := struct {
		Repository string `json:"repository"`
		PR         int    `json:"pr"`
		intent.Report
	}{*repo, *pr, report}
	if code := encode(result); code != 0 {
		return code
	}
	if !report.Valid {
		return 1
	}
	return 0
}

const githubSetupGuide = `Connect Ferretta using its own GitHub App identity:
1. Register an App: https://github.com/settings/apps/new
   Choose an available name, set a homepage, disable webhooks, and leave user OAuth unconfigured.
   Repository permissions: Contents read-only and Pull requests read/write for proposal posting (read-only suffices for inspection).
2. Install the App on your account, selecting only the repository you want reviewed.
3. Copy the App client ID and installation ID. Generate/download its private key.
   Put the key outside the repository in a private directory, using chmod 600 on the file.
4. Verify access and create machine configuration (existing files are preserved):
   ferretta auth github --configure --repo owner/repo --client-id APP_CLIENT_ID --installation-id NUMBER --private-key-file /absolute/path/app.pem
5. Check readiness:
   ferretta doctor --repo owner/repo
Configuration is stored under your OS user configuration directory in ferretta/github.json.
Set FERRETTA_GITHUB_CONFIG to an absolute service-owned path when provisioning a boot service.
No personal gh login is used. No inference or GitHub comment writes occur during connection setup.`
