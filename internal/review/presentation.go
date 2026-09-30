package review

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
)

func plain(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
}

// PrintReport presents public evidence only; raw model messages remain private.
func PrintReport(output io.Writer, report Report, format string) error {
	if format == "json" {
		return json.NewEncoder(output).Encode(report)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s — advisory review of PR #%d\nCommit: %s\nBase: %s\nPolicy SHA-256: %s\n\n%s\n", strings.ToUpper(report.Status), report.PR.Number, report.PR.Head, report.PR.Base, report.PolicySHA256, plain(report.Summary))
	for _, f := range report.Findings {
		fmt.Fprintf(&b, "\nFinding: %s:%d\n%s\n", plain(f.Path), f.Line, plain(f.Explanation))
	}
	if report.Question != "" {
		fmt.Fprintf(&b, "\nIntent question (see proposal delivery status):\n%s\n", plain(report.Question))
	}
	for _, proposal := range report.Proposals {
		fmt.Fprintf(&b, "Proposal %s-v%d: %s %s\n", proposal.Topic, proposal.Version, proposal.Delivery, proposal.Comment.URL)
	}
	for _, tradeoff := range report.Tradeoffs {
		fmt.Fprintf(&b, "\nTradeoff: %s\n", plain(tradeoff))
	}
	requested := report.RequestedEffort
	if report.RequestedThinking != "" {
		requested = report.RequestedThinking
	}
	fmt.Fprintf(&b, "\nProvider: %s; requested model: %s; thinking: %s\n", plain(report.Provider), plain(report.RequestedModel), plain(requested))
	if report.EffectiveEffort != nil {
		fmt.Fprintf(&b, "Provider-reported effort: %s\n", plain(*report.EffectiveEffort))
	} else {
		fmt.Fprintln(&b, "Effective reasoning effort: not reported")
	}
	fmt.Fprintf(&b, "Checks executed: %d; model replies: %d; requested context: %d tokens\n", len(report.Checks), len(report.Attempts), report.ContextTokens)
	if !report.StartedAt.IsZero() && !report.FinishedAt.IsZero() {
		fmt.Fprintf(&b, "Elapsed review time: %s\n", report.FinishedAt.Sub(report.StartedAt).Round(time.Millisecond))
	}
	tokens := 0
	known := len(report.Attempts) > 0
	for _, a := range report.Attempts {
		known = known && a.PromptTokens > 0 && a.OutputTokens > 0
		tokens += a.PromptTokens + a.OutputTokens
		fmt.Fprintf(&b, "Observed model: %s\n", plain(a.Model))
	}
	if known {
		fmt.Fprintf(&b, "Reported input + output tokens: %d\n", tokens)
	} else {
		fmt.Fprintln(&b, "Token usage: not fully reported")
	}
	fmt.Fprintln(&b, "Cost: not metered by Ferretta; this route uses local inference.")
	switch report.Status {
	case "lgtm":
		fmt.Fprintln(&b, "Next: inspect the evidence and tradeoffs. This advisory report does not authorize merging.")
	case "awaiting_intent":
		fmt.Fprintln(&b, "Next: reply to the proposal, then run review --resume with the saved session directory and the same policy/repository/PR.")
	case "clarification_required":
		fmt.Fprintln(&b, "Next: resolve the intent question with the human; automatic posting/resumption is not implemented.")
	case "changes_required":
		fmt.Fprintln(&b, "Next: address the findings, then review the new revision.")
	default:
		fmt.Fprintln(&b, "Next: resolve the incomplete-review reason before starting another bounded attempt.")
	}
	_, err := output.Write(b.Bytes())
	return err
}

func (c CLI) Demo(_ context.Context, args []string, output, stderr io.Writer) int {
	flags := flag.NewFlagSet("demo", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outcome := flags.String("outcome", "changes", "changes, question, lgtm")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	r := Report{Status: "changes_required", Summary: "An empty selection produces an out-of-range access.", Findings: []Finding{{Path: "selection.go", Line: 12, Explanation: "The implementation reads items[0] before checking whether the input contains an item."}}, Tradeoffs: []string{"A single empty-input guard preserves the existing function and avoids introducing an abstraction."}, PR: PR{Number: 1, Head: strings.Repeat("a", 40), Base: strings.Repeat("b", 40)}, PolicySHA256: strings.Repeat("c", 64), Provider: "ollama", RequestedModel: "example-model", RequestedThinking: "enabled", ContextTokens: 16384}
	switch *outcome {
	case "changes":
	case "question":
		r.Status = "clarification_required"
		r.Findings = nil
		r.Summary = "The expected behavior for an empty selection is unclear."
		r.Question = "PROPOSED-EmptySelection-v1:: Should an empty selection return an empty result or a validation error?"
	case "lgtm":
		r.Status = "lgtm"
		r.Findings = nil
		r.Summary = "In this fictional example, the empty-input behavior is explicit and covered by passing checks."
		r.Checks = []string{"example check result"}
	default:
		fmt.Fprintln(stderr, "choose --outcome changes, question, or lgtm")
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "demo accepts no positional arguments")
		return 1
	}
	if _, err := fmt.Fprint(output, "ILLUSTRATIVE SAMPLE — fictional PR, commits and evidence; no inference, GitHub calls or checks performed.\n\n"); err != nil {
		return 1
	}
	if err := PrintReport(output, r, "text"); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
