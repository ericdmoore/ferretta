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
)

type CommentsClient interface {
	Comments(context.Context, string, int) ([]github.Comment, error)
}

type ReviewRunner interface {
	Run(context.Context, []string, io.Writer, io.Writer) int
}

func Run(ctx context.Context, args []string, input io.Reader, output, errors io.Writer, client CommentsClient) int {
	return RunWithReviewer(ctx, args, input, output, errors, client, nil)
}

func RunWithReviewer(ctx context.Context, args []string, input io.Reader, output, errors io.Writer, client CommentsClient, reviewer ReviewRunner) int {
	fail := func(err error) int { fmt.Fprintln(errors, err); return 1 }
	const usage = "usage: ferretta intent parse | ferretta intent inspect --repo owner/repo --pr N --humans login --agents login | ferretta review --repo owner/repo --pr N [--policy .ferretta/review.json]"
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
