package review

import (
	"bytes"
	"strings"
	"testing"
)

func TestReportSummarizesObservedModelsWithoutLosingProvenance(t *testing.T) {
	r := reportFixture()
	a := reply("run_checks", `{}`)
	a.Model = "gpt-oss:20b"
	r.Attempts = []Reply{a, a, a}
	var out bytes.Buffer
	if err := PrintReport(&out, r, "text"); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "Observed model:") != 1 || !strings.Contains(out.String(), "gpt-oss:20b; replies: 3") || !strings.Contains(out.String(), "Reported input + output tokens: 90") {
		t.Fatal(out.String())
	}
	a.Model = "other-model"
	r.Attempts = append(r.Attempts, a, Reply{})
	out.Reset()
	if err := PrintReport(&out, r, "text"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gpt-oss:20b; replies: 3", "other-model; replies: 1", "not reported; replies: 1", "Token usage: not fully reported"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(want, out.String())
		}
	}
	out.Reset()
	if err := PrintReport(&out, r, "json"); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), `"model":"gpt-oss:20b"`) != 3 {
		t.Fatal("raw attempt provenance lost", out.String())
	}
}

func TestScorecardTableKeepsRoleScoresAndUnknownsDistinct(t *testing.T) {
	a := assessmentFixture()
	zero := 0
	a.Worker.Correctness.Score = &zero
	a.Oversight.Correctness.Score = nil
	a.Oversight.Correctness.NotAssessed = "insufficient_evidence"
	text := EvaluationMarkdown(Evaluation{Assessment: &a})
	for _, want := range []string{
		"| Scope | Correctness | Evidence | Intent alignment | Judgment | Actionability | Communication |",
		"| **Worker output** | 0/3 | 2/3 | 2/3 | 2/3 | 2/3 | 2/3 |",
		"| **Oversight decision** | Not assessed | 2/3 | 2/3 | 2/3 | 2/3 | 2/3 |",
		"not assessed: insufficient_evidence", "Evidence IDs:", "### Evidence index",
	} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	if strings.Contains(EvaluationMarkdown(Evaluation{Failure: "provider unavailable"}), "| Scope |") {
		t.Fatal("incomplete evaluation got fake scores")
	}
}
