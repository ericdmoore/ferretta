package review

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Eval explicitly starts a new grading attempt without rerunning the review or
// publishing. Existing report and previous scorecards remain untouched.
func (c CLI) Eval(ctx context.Context, args []string, output, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	flags := flag.NewFlagSet("eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	run := flags.String("run", "", "directory containing a saved report.json")
	policyPath := flags.String("policy", "", "explicit trusted judge JSON policy (no checks)")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *run == "" || *policyPath == "" {
		return fail(fmt.Errorf("eval requires --run PATH --policy PATH"))
	}
	policyBytes, err := os.ReadFile(*policyPath)
	if err != nil {
		return fail(err)
	}
	p, err := ParseEvaluationPolicy(policyBytes)
	if err != nil {
		return fail(err)
	}
	data, err := os.ReadFile(filepath.Join(*run, "report.json"))
	if err != nil {
		return fail(err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return fail(err)
	}
	snapshot, err := SnapshotReview(report)
	if err != nil {
		return fail(err)
	}
	dir, err := os.MkdirTemp(*run, "evaluation-")
	if err != nil {
		return fail(err)
	}
	checkpoint := func(e Evaluation, messages []Message) error {
		return Save(filepath.Join(dir, "session.json"), struct {
			Evaluation Evaluation `json:"evaluation"`
			Messages   []Message  `json:"messages"`
		}{e, messages})
	}
	result := c.Runner.Evaluate(ctx, p, snapshot, policyBytes, filepath.Base(dir), ".", checkpoint)
	if err := Save(filepath.Join(dir, "scorecard.json"), result); err != nil {
		return fail(err)
	}
	if _, err := fmt.Fprintln(output, EvaluationMarkdown(result)); err != nil {
		return fail(err)
	}
	fmt.Fprintln(stderr, "Scorecard:", filepath.Join(dir, "scorecard.json"))
	if result.Assessment == nil {
		return 2
	}
	return 0
}

func EvaluationMarkdown(e Evaluation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Ferretta scorecard — automated assessment\n\nCommit: `%s`\n\nJudge: %q via %s; requested effort: %q; requested thinking: %q; effective effort: unknown.\n\nRubric: `%s`; evaluation: `%s`; output: `%s`.\n\nReview outcome: **%s**. These grades do not authorize merging or more work.\n", e.Head, e.RequestedModel, e.Provider, e.RequestedEffort, e.RequestedThink, e.Rubric, e.ID, e.OutputID, e.ReviewStatus)
	if e.Assessment == nil {
		fmt.Fprintf(&b, "\nEvaluation incomplete:\n%s\n", quoted(e.Failure))
	} else {
		fmt.Fprintf(&b, "\n%s\n", quoted(e.Assessment.Summary))
		names := []string{"Correctness", "Evidence", "Intent alignment", "Judgment", "Actionability", "Communication"}
		for i, role := range []RoleGrades{e.Assessment.Worker, e.Assessment.Oversight} {
			fmt.Fprintf(&b, "\n### %s\n", []string{"Worker output", "Oversight decision"}[i])
			for j, g := range role.dimensions() {
				score := "not assessed: " + g.NotAssessed
				if g.Score != nil {
					score = fmt.Sprintf("%d/3", *g.Score)
				}
				fmt.Fprintf(&b, "\n**%s: %s**\n%s\n", names[j], score, quoted(g.Explanation))
				if len(g.Evidence) > 0 {
					fmt.Fprintf(&b, "Evidence IDs: `%s`.\n", strings.Join(g.Evidence, "`, `"))
				}
			}
		}
		// Publish source descriptions, never the private conversation or full
		// tool bodies. The immutable evidence bundle stays in the local record.
		ids := []string{}
		for _, role := range []RoleGrades{e.Assessment.Worker, e.Assessment.Oversight} {
			for _, grade := range role.dimensions() {
				ids = append(ids, grade.Evidence...)
			}
		}
		slices.Sort(ids)
		fmt.Fprintln(&b, "\n### Evidence index")
		for _, id := range slices.Compact(ids) {
			description := map[string]string{"pr": "PR metadata and author claims, not confirmed human intent", "review": "Published verdict, findings and tradeoffs", "checks": "Saved trusted-check output and failure status", "intent": "Recorded questions and authenticated human decisions; may be empty"}[id]
			if description == "" {
				description, _, _ = strings.Cut(e.Evidence[id], "\n")
				if len(description) > 512 {
					description = description[:512] + "…"
				}
			}
			fmt.Fprintf(&b, "\n`%s`:\n%s\n", id, quoted(description))
		}
		fmt.Fprintln(&b, "\nEvidence refers to the named revision. Full source results are retained in the private local scorecard record; this index does not establish that a citation supports its claim.")
	}
	b.WriteString(usageMarkdown("Evaluation", e.Usage))
	return b.String()
}

func usageMarkdown(label string, usage []UsageEvent) string {
	var nanos int64
	var input, output, unknown int
	for _, u := range usage {
		nanos += int64(u.Duration)
		if u.Kind == "model" {
			if u.PromptTokens != nil {
				input += *u.PromptTokens
			}
			if u.OutputTokens != nil {
				output += *u.OutputTokens
			}
			if u.PromptTokens == nil || u.OutputTokens == nil {
				unknown++
			}
		}
	}
	return fmt.Sprintf("\n%s usage: %.2f worker-seconds; known input/output tokens: %d/%d; model operations with missing token counts: %d. Local-only route; API spending is not metered. Hardware/electricity cost is unmeasured.\n", label, float64(nanos)/1e9, input, output, unknown)
}

func quoted(text string) string { return "    " + strings.ReplaceAll(text, "\n", "\n    ") }
