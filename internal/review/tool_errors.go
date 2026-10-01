package review

import (
	"encoding/json"
	"io/fs"
)

type toolSuggestion struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolProblem struct {
	Error          string          `json:"error"`
	RequestedTool  string          `json:"requested_tool"`
	Message        string          `json:"message"`
	Recovery       string          `json:"recovery"`
	AvailableTools []string        `json:"available_tools"`
	InputSchema    json.RawMessage `json:"input_schema,omitempty"`
	SuggestedCall  *toolSuggestion `json:"suggested_call,omitempty"`
}

// Tool feedback is advisory. Only a later, separately validated model call can
// execute the suggestion. The catalogue is read from the advertised registry.
func toolFailure(kind, name string, arguments json.RawMessage, err error) string {
	problem := toolProblem{Error: kind, RequestedTool: name, Message: err.Error()}
	var catalog []struct {
		Function struct {
			Name       string          `json:"name"`
			Parameters json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	_ = json.Unmarshal(tools, &catalog) // Static registry; validated by tests.
	for _, entry := range catalog {
		problem.AvailableTools = append(problem.AvailableTools, entry.Function.Name)
		if entry.Function.Name == name {
			problem.InputSchema = entry.Function.Parameters
		}
	}
	if kind == "invalid_arguments" && problem.InputSchema == nil {
		problem.Error = "unknown_tool"
	}
	problem.Recovery = "Use the advertised input schema to correct your arguments. Suggestions are examples, not actions already performed."
	if problem.Error == "unknown_tool" {
		problem.Recovery = "This tool is unavailable. Changing its arguments will not enable it. Choose an available tool; no shell or sudo tool is provided."
	}
	if kind == "execution_failed" {
		problem.Recovery = "The validated operation failed. Inspect the failure before retrying; do not change tools or permissions to bypass it."
		// Listing the exact commit can help resolve a missing file. Do not
		// automatically suggest re-executing checks or other failed effects.
		if name == "read_file" {
			problem.SuggestedCall = &toolSuggestion{Name: "list_files", Arguments: json.RawMessage(`{}`)}
		}
	} else {
		problem.SuggestedCall = recoveryCall(name, arguments, problem.Error == "unknown_tool")
	}
	data, _ := json.Marshal(problem)
	return string(data)
}

func recoveryCall(name string, arguments json.RawMessage, unknown bool) *toolSuggestion {
	// Salvage only fields useful for read-only navigation. Every suggested call
	// is checked by the same core planner used for real tool requests.
	var hint struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	_ = json.Unmarshal(arguments, &hint)
	var value any
	if unknown && hint.Query != "" {
		name = "search"
	} else if unknown && fs.ValidPath(hint.Path) && hint.Path != "." {
		name = "read_file"
	}
	switch name {
	case "search":
		if hint.Query == "" {
			hint.Query = "text to find"
		}
		if !fs.ValidPath(hint.Path) {
			hint.Path = ""
		}
		value = map[string]any{"query": hint.Query, "path": hint.Path, "regex": false, "max_results": 20, "offset": 0}
	case "read_file":
		if !fs.ValidPath(hint.Path) || hint.Path == "." {
			return &toolSuggestion{Name: "list_files", Arguments: json.RawMessage(`{}`)}
		}
		value = map[string]any{"path": hint.Path, "start_line": 1, "end_line": 200}
	case "read_diff", "list_files", "run_checks":
		value = struct{}{}
	case "finish_review", "request_intent_confirmation":
		return nil // Never invent a verdict or an intent question as an example.
	default:
		return &toolSuggestion{Name: "list_files", Arguments: json.RawMessage(`{}`)}
	}
	data, _ := json.Marshal(value)
	if _, err := Plan(name, data); err != nil {
		return &toolSuggestion{Name: "list_files", Arguments: json.RawMessage(`{}`)}
	}
	return &toolSuggestion{Name: name, Arguments: data}
}
