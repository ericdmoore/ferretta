package intent

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name, text string
		count      int
		wantError  bool
	}{
		{"prose", "Ordinary comment", 0, false},
		{"inline", "PROPOSED-Pure-Go-v1:: Build without CGO", 1, false},
		{"multiple", "PROPOSED-Core-v1::\r\nFirst\r\nCONFIRMED-Core-v1::", 2, false},
		{"quoted", "> CONFIRMED-Core-v1::\n    PROPOSED-Core-v1:: example", 0, false},
		{"fenced", "```text\nPROPOSED-Core-v1:: example\n```\nCONFIRMED-Core-v1::", 1, false},
		{"long fence", "````\n```\nPROPOSED-Core-v1:: example\n````", 0, false},
		{"tilde fence", "  ~~~text\nPROPOSED-Core-v1:: example\n  ~~~", 0, false},
		{"wrong fence", "```\n~~~\nPROPOSED-Core-v1:: example", 0, false},
		{"empty proposal", "PROPOSED-Core-v1::", 0, true},
		{"empty correction", "CORRECTED-Core-v1:: ", 0, true},
		{"unknown vocabulary", "AMENDED-Core-v1:: later", 0, true},
		{"unversioned", "PROPOSED-Core:: text", 0, true},
		{"zero version", "PROPOSED-Core-v0:: text", 0, true},
		{"missing colon", "CONFIRMED-Core-v1:", 0, true},
		{"overflow", "PROPOSED-Core-v999999999999999999999999999:: text", 0, true},
		{"topic whitespace", "PROPOSED- Core-v1:: text", 0, true},
		{"ambiguous topic", "PROPOSED-Core::other-v1:: text", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.text)
			if (err != nil) != tt.wantError {
				t.Fatalf("Parse() error = %v", err)
			}
			if !tt.wantError && len(got) != tt.count {
				t.Fatalf("got %d markers, want %d", len(got), tt.count)
			}
		})
	}
	got, err := Parse("Intro\nPROPOSED-Pure-Go-v2:: First\nSecond\n```\nCONFIRMED-Fake-v1::\n```\nCORRECTED-Pure-Go-v2:: Revise")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Topic != "Pure-Go" || got[0].Version != 2 || got[0].Line != 2 || got[0].Body != "First\nSecond\n```\nCONFIRMED-Fake-v1::\n```" {
		t.Fatalf("lost proposal evidence: %+v", got[0])
	}
	for _, body := range []string{"test exact text", "tools are permitted"} {
		markers, err := Parse("PROPOSED-Go-v1::\t" + body)
		if err != nil || markers[0].Body != body {
			t.Fatalf("body changed: got %+v, error %v", markers, err)
		}
	}
}

func evidence(vocabulary Vocabulary, version int) Evidence {
	return Evidence{CommentID: int64(version), Author: "actor", SHA256: "snapshot", Marker: Marker{Vocabulary: vocabulary, Topic: "Go", Version: version, Body: "Exact wording"}}
}

func TestCorrectionLoopPreservesEvidence(t *testing.T) {
	var state Record
	steps := []struct {
		vocabulary Vocabulary
		role       Role
		version    int
	}{
		{Proposed, Agent, 1}, {Corrected, Human, 1}, {Proposed, Agent, 2}, {Confirmed, Human, 2},
	}
	for _, step := range steps {
		before := append([]Evidence(nil), state.History...)
		command, err := Decide(state, step.role, evidence(step.vocabulary, step.version))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, state.History) {
			t.Fatal("input state was mutated")
		}
		state = command.Record
	}
	if state.Status != Confirmed || state.Version != 2 || len(state.History) != 4 {
		t.Fatalf("unexpected final state: %+v", state)
	}
	if state.History[0].Marker.Version != 1 || state.History[1].Marker.Vocabulary != Corrected {
		t.Fatal("history lost")
	}
}

func TestInvalidTransitions(t *testing.T) {
	pending := Record{Topic: "Go", Version: 1, Status: Proposed}
	confirmed := Record{Topic: "Go", Version: 1, Status: Confirmed}
	corrected := Record{Topic: "Go", Version: 1, Status: Corrected}
	tests := []struct {
		name  string
		state Record
		role  Role
		e     Evidence
	}{
		{"human proposal", Record{}, Human, evidence(Proposed, 1)},
		{"agent confirmation", pending, Agent, evidence(Confirmed, 1)},
		{"agent correction", pending, Agent, evidence(Corrected, 1)},
		{"untrusted actor", pending, "", evidence(Confirmed, 1)},
		{"missing proposal", Record{}, Human, evidence(Confirmed, 1)},
		{"stale version", pending, Human, evidence(Confirmed, 2)},
		{"confirmation after correction", corrected, Human, evidence(Confirmed, 1)},
		{"replacement", confirmed, Agent, evidence(Proposed, 2)},
		{"duplicate confirmation", confirmed, Human, evidence(Confirmed, 1)},
		{"pending replacement", pending, Agent, evidence(Proposed, 2)},
		{"skipped version", corrected, Agent, evidence(Proposed, 3)},
		{"unknown vocabulary", pending, Human, evidence("AMENDED", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decide(tt.state, tt.role, tt.e); err == nil {
				t.Fatal("accepted invalid transition")
			}
		})
	}
	for _, modify := range []func(*Evidence){
		func(e *Evidence) { e.Author = "" }, func(e *Evidence) { e.CommentID = 0 },
		func(e *Evidence) { e.SHA256 = "" }, func(e *Evidence) { e.Marker.Topic = "" },
		func(e *Evidence) { e.Marker.Version = 0 }, func(e *Evidence) { e.Marker.Topic = "other" },
		func(e *Evidence) { e.Marker.Body = "" },
	} {
		e := evidence(Corrected, 1)
		modify(&e)
		if _, err := Decide(pending, Human, e); err == nil {
			t.Fatalf("accepted invalid evidence: %+v", e)
		}
	}
}

func comment(id int64, author, body string) Comment {
	return Comment{ID: id, Author: author, AuthorType: "User", URL: "https://example.test/comment", Body: body}
}

func TestInspect(t *testing.T) {
	policy := Policy{Humans: []string{"human"}, Agents: []string{"agent"}}
	comments := []Comment{
		comment(4, "HUMAN", "CONFIRMED-Go-v2:: Agreed"),
		comment(1, "agent", "PROPOSED-Go-v1:: Only Go"),
		comment(3, "agent", "PROPOSED-Go-v2:: Go plus external tools"),
		comment(2, "human", "CORRECTED-Go-v1:: External tools are fine"),
		comment(5, "visitor", "Ordinary discussion"),
		comment(6, "agent", "PROPOSED-Another-v1:: pending"),
	}
	report, err := Inspect(comments, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || len(report.Records) != 2 || report.Records[0].Topic != "Another" || report.Records[1].Status != Confirmed {
		t.Fatalf("unexpected report: %+v", report)
	}
	if comments[0].ID != 4 {
		t.Fatal("input reordered")
	}
	if len(report.Records[1].History[0].SHA256) != 64 {
		t.Fatal("missing snapshot hash")
	}

	for _, bad := range []Comment{
		comment(7, "visitor", "CONFIRMED-Another-v1::"),
		comment(7, "human", "CONFIRMED-Another-v2::"),
		comment(7, "agent", "AMENDED-Another-v1:: text"),
		{ID: 7, Author: "human", AuthorType: "Bot", Body: "CONFIRMED-Another-v1::"},
		{ID: 7, Author: "human", AuthorType: "User", Edited: true, Body: "CONFIRMED-Another-v1::"},
		comments[0],
	} {
		got, err := Inspect(append(append([]Comment(nil), comments...), bad), policy)
		if err != nil || got.Valid || len(got.Findings) == 0 {
			t.Fatalf("bad comment accepted: %+v, %v", got, err)
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	for _, p := range []Policy{
		{}, {Humans: []string{"h"}}, {Humans: []string{"h"}, Agents: []string{"H"}},
		{Humans: []string{" h"}, Agents: []string{"a"}}, {Humans: []string{""}, Agents: []string{"a"}},
	} {
		if _, err := Inspect(nil, p); err == nil {
			t.Fatalf("invalid policy accepted: %+v", p)
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, text := range []string{"", "PROPOSED-Go-v1:: pure Go", "```\nCONFIRMED-Go-v1::\n```"} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		markers, err := Parse(text)
		if err != nil {
			return
		}
		for _, marker := range markers {
			if marker.Topic == "" || marker.Version < 1 || marker.Line < 1 || strings.TrimSpace(marker.Topic) != marker.Topic {
				t.Fatalf("invalid parsed marker: %+v", marker)
			}
		}
	})
}
