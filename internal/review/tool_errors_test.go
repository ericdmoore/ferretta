package review

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestToolRecoverySuggestions(t *testing.T) {
	for _, tc := range []struct {
		name, args, kind, suggestion string
	}{
		{"grep", `{"query":"finishJSON","path":"internal/review"}`, "unknown_tool", "search"},
		{"sudo search", `{"query":"x"}`, "unknown_tool", "search"},
		{"shell", `{}`, "unknown_tool", "list_files"},
		{"open_file", `{"path":"internal/review/core.go"}`, "unknown_tool", "read_file"},
		{"open_file", `{"path":"internal/review/"}`, "unknown_tool", "list_files"},
		{"shell", `{`, "unknown_tool", "list_files"},
		{"search", `{"query":"[","regex":true}`, "invalid_arguments", "search"},
		{"search", `{"query":"finishJSON","path":"internal/review","max_results":999}`, "invalid_arguments", "search"},
		{"search", `{"path":"../outside"}`, "invalid_arguments", "search"},
		{"search", `{"query":"x\ny"}`, "invalid_arguments", "list_files"},
		{"read_file", `{"path":"main.go","start_line":-1}`, "invalid_arguments", "read_file"},
		{"read_file", `{"path":"../outside"}`, "invalid_arguments", "list_files"},
		{"read_diff", `{"path":"x"}`, "invalid_arguments", "read_diff"},
		{"list_files", `{"path":"x"}`, "invalid_arguments", "list_files"},
		{"run_checks", `{"cmd":"sudo make check"}`, "invalid_arguments", "run_checks"},
		{"finish_review", `{}`, "invalid_arguments", ""},
		{"request_intent_confirmation", `{}`, "invalid_arguments", ""},
	} {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			_, err := Plan(tc.name, json.RawMessage(tc.args))
			if err == nil {
				t.Fatal("fixture must be rejected")
			}
			var problem toolProblem
			if err := json.Unmarshal([]byte(toolFailure("invalid_arguments", tc.name, json.RawMessage(tc.args), err)), &problem); err != nil {
				t.Fatal(err)
			}
			if problem.Error != tc.kind || problem.RequestedTool != tc.name || problem.Recovery == "" || len(problem.AvailableTools) != 7 || (len(problem.InputSchema) == 0) != (tc.kind == "unknown_tool") {
				t.Fatal(problem)
			}
			if tc.suggestion == "" {
				if problem.SuggestedCall != nil {
					t.Fatal("invented a decision")
				}
				return
			}
			call := problem.SuggestedCall
			if call == nil || call.Name != tc.suggestion {
				t.Fatal(problem)
			}
			command, err := Plan(call.Name, call.Arguments)
			if err != nil {
				t.Fatal("suggested call rejected by planner", err)
			}
			if tc.name == "grep" {
				search := command.(Search)
				if search.path != "internal/review" || search.query != "finishJSON" {
					t.Fatal("lost requested search scope")
				}
			}
		})
	}
}

func TestToolExecutionFailureRecovery(t *testing.T) {
	for _, name := range []string{"read_file", "search", "run_checks"} {
		var problem toolProblem
		_ = json.Unmarshal([]byte(toolFailure("execution_failed", name, nil, errors.New("failed"))), &problem)
		if problem.Error != "execution_failed" || (problem.SuggestedCall != nil) != (name == "read_file") {
			t.Fatal(problem)
		}
	}
}

func TestUnknownToolDoesNotExecuteSuggestion(t *testing.T) {
	m := &fakeModel{replies: []Reply{reply("grep", `{"query":"target","path":"src"}`), reply("run_checks", `{}`), reply("finish_review", finishJSON("lgtm"))}}
	r := runner(m)
	r.Searcher = searchFunc(func(context.Context, string, string, Search) (SearchPage, error) {
		t.Fatal("suggestion executed without a model request")
		return SearchPage{}, nil
	})
	if result := r.Review(context.Background(), policy(t), workspace(), nil, func(Report, []Message) error { return nil }); result.Status != "lgtm" {
		t.Fatal(result)
	}
	var problem toolProblem
	_ = json.Unmarshal([]byte(m.requests[1][len(m.requests[1])-1].Content), &problem)
	if problem.Error != "unknown_tool" || problem.SuggestedCall.Name != "search" {
		t.Fatal(problem)
	}
}

func TestRecoveryRegistryMatchesAdvertisedTools(t *testing.T) {
	var registry []struct {
		Function struct {
			Name       string         `json:"name"`
			Parameters map[string]any `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(tools, &registry); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range registry {
		names = append(names, tool.Function.Name)
		var problem toolProblem
		_ = json.Unmarshal([]byte(toolFailure("invalid_arguments", tool.Function.Name, nil, errors.New("bad arguments"))), &problem)
		var schema map[string]any
		_ = json.Unmarshal(problem.InputSchema, &schema)
		if !reflect.DeepEqual(schema, tool.Function.Parameters) {
			t.Fatal("recovery advertises a different schema")
		}
	}
	var problem toolProblem
	_ = json.Unmarshal([]byte(toolFailure("invalid_arguments", "unknown", nil, errors.New("unknown"))), &problem)
	if !reflect.DeepEqual(names, problem.AvailableTools) {
		t.Fatal("recovery lists different tools")
	}
}

func TestCorrectedSearchContinuesReview(t *testing.T) {
	m := &fakeModel{replies: []Reply{
		reply("search", `{"query":"target","path":"src","max_results":999}`),
		reply("search", `{"query":"target","path":"src","max_results":20}`),
		reply("run_checks", `{}`), reply("finish_review", finishJSON("lgtm")),
	}}
	r := runner(m)
	calls := 0
	r.Searcher = searchFunc(func(_ context.Context, _, revision string, search Search) (SearchPage, error) {
		calls++
		if len(m.requests) != 2 || search.query != "target" || search.path != "src" || search.limit != 20 {
			t.Fatal("executed invalid or substituted search")
		}
		return SearchPage{Revision: revision, Matches: []SearchMatch{}}, nil
	})
	report := r.Review(context.Background(), policy(t), workspace(), nil, func(Report, []Message) error { return nil })
	if report.Status != "lgtm" || calls != 1 {
		t.Fatal(report, calls)
	}
	var problem toolProblem
	if err := json.Unmarshal([]byte(m.requests[1][len(m.requests[1])-1].Content), &problem); err != nil || problem.Error != "invalid_arguments" {
		t.Fatal(problem, err)
	}
}
