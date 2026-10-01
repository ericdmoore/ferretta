package review

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// RepairPolicy is opt-in, local-only and bounded independently of review and
// grading. A validated value is pinned for the lifetime of the PR job.
type RepairPolicy struct {
	route     Policy
	cycles    int
	formatter []string
	protected []string
	digest    string
}

func ParseRepairPolicy(data []byte) (RepairPolicy, error) {
	var wire struct {
		Model     json.RawMessage `json:"model_policy"`
		Cycles    int             `json:"max_cycles"`
		Formatter []string        `json:"formatter"`
		Protected []string        `json:"protected_paths"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&wire); err != nil {
		return RepairPolicy{}, err
	}
	if d.Decode(new(any)) != io.EOF {
		return RepairPolicy{}, fmt.Errorf("repair policy requires one JSON value")
	}
	p, err := ParsePolicy(wire.Model)
	if err != nil {
		return RepairPolicy{}, err
	}
	if wire.Cycles < 1 || wire.Cycles > 100 || p.config.TimeoutSeconds < 1 {
		return RepairPolicy{}, fmt.Errorf("repair requires max_cycles 1–100 and a positive total timeout_seconds")
	}
	if len(wire.Formatter) > 0 && strings.TrimSpace(wire.Formatter[0]) == "" {
		return RepairPolicy{}, fmt.Errorf("empty formatter command")
	}
	for _, path := range wire.Protected {
		if !fs.ValidPath(path) || path == "." {
			return RepairPolicy{}, fmt.Errorf("protected_paths must name repository files or directories")
		}
	}
	return RepairPolicy{route: p, cycles: wire.Cycles, formatter: wire.Formatter, protected: wire.Protected, digest: hashBytes(data)}, nil
}

func (p RepairPolicy) protectedPath(path string) bool {
	// These controls must not be weakened to make a repair pass. Repository-specific
	// check/lint/performance configuration belongs in protected_paths as well.
	for _, prefix := range append([]string{".git", ".ferretta", ".github", ".githooks", ".gitattributes", ".gitmodules", ".gitignore", ".coverage-baseline", "AGENTS.md", "ferretta.toml", "Makefile", "scripts"}, p.protected...) {
		if strings.EqualFold(path, prefix) || strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix)+"/") {
			return true
		}
	}
	return false
}

type ApplyPatch struct{ path, old, replacement string }

func (ApplyPatch) command() {}

type RunFormatter struct{}

func (RunFormatter) command() {}

type FinishRepair struct{ summary string }

func (FinishRepair) command() {}

func planRepair(name string, arguments json.RawMessage, p RepairPolicy) (Command, error) {
	decode := func(value any) error {
		d := json.NewDecoder(bytes.NewReader(arguments))
		d.DisallowUnknownFields()
		if err := d.Decode(value); err != nil {
			return err
		}
		if d.Decode(new(any)) != io.EOF {
			return fmt.Errorf("tool requires one JSON value")
		}
		return nil
	}
	switch name {
	case "apply_patch":
		var a struct {
			Path string `json:"path"`
			Old  string `json:"old_text"`
			New  string `json:"new_text"`
		}
		if err := decode(&a); err != nil {
			return nil, err
		}
		if !fs.ValidPath(a.Path) || a.Path == "." || strings.ContainsAny(a.Path, "\x00\r\n") || p.protectedPath(a.Path) {
			return nil, fmt.Errorf("patch requires an unprotected repository-relative path")
		}
		if a.Old == a.New || len(a.Old)+len(a.New) > 64000 {
			return nil, fmt.Errorf("patch must change content and fit in 64000 bytes")
		}
		return ApplyPatch{a.Path, a.Old, a.New}, nil
	case "run_tests":
		return Plan("run_checks", arguments)
	case "run_formatter":
		if err := decode(&struct{}{}); err != nil {
			return nil, err
		}
		if len(p.formatter) == 0 {
			return nil, fmt.Errorf("no formatter authorized; edit formatted source directly")
		}
		return RunFormatter{}, nil
	case "finish_repair":
		var a struct {
			Summary string `json:"summary"`
		}
		if err := decode(&a); err != nil {
			return nil, err
		}
		if strings.TrimSpace(a.Summary) == "" || len(a.Summary) > 8000 {
			return nil, fmt.Errorf("repair requires an evidence-based summary of at most 8000 bytes")
		}
		return FinishRepair{a.Summary}, nil
	case "read_file", "read_diff", "list_files", "search", "grep", "run_checks", "request_intent_confirmation":
		return Plan(name, arguments)
	default:
		return nil, fmt.Errorf("tool %q is not permitted; use inspection tools, apply_patch, run_tests, run_formatter, request_intent_confirmation or finish_repair", name)
	}
}

// Derive the common inspection schemas, but advertise writable semantics only
// for repair sessions. Review and evaluator registries stay read-only.
func repairTools() json.RawMessage {
	var definitions []struct {
		Type     string         `json:"type"`
		Function map[string]any `json:"function"`
	}
	_ = json.Unmarshal(tools, &definitions)
	kept := definitions[:0]
	for _, d := range definitions {
		if d.Function["name"] == "finish_review" {
			continue
		}
		switch d.Function["name"] {
		case "read_file":
			d.Function["description"] = "Read a numbered page of a regular file in the current repair working tree, including uncommitted edits. Default 200 lines; use start_line/end_line for more."
		case "read_diff":
			d.Function["description"] = "Read a numbered page of the PR diff from merge base through the current repair tree, including original submitted changes and uncommitted repairs. Default 200 lines; use start_line/end_line for more."
		case "list_files":
			d.Function["description"] = "List tracked and new non-ignored files in the current repair working tree."
		case "search", "grep":
			d.Function["description"] = "Exact aliases searching the current repair working tree, including edits. Literal query by default; regex:true enables POSIX extended regex. path is an optional literal scope. Bounded to max_results matches (1–100, default 20); use next_offset for more. Binary, symlink, missing and oversized files are skipped. Long matching lines are truncated to 400 bytes; use read_file for full context."
		case "run_checks":
			d.Function["description"] = "Rerun every trusted check on the current repair tree. No arguments. Prior check results are invalidated by edits or formatter runs."
		case "request_intent_confirmation":
			d.Function["description"] = "Ask only about ambiguous intended behavior or consequential tradeoffs. Make the question self-contained with evidence and alternatives. Never request permission to fix established bugs, finish, or merge. Call alone; harness persists/posts PROPOSED and pauses. After CORRECTED revise the same topic; only a human can confirm."
		}
		kept = append(kept, d)
	}
	common, _ := json.Marshal(kept)
	extra := `
 {"type":"function","function":{"name":"apply_patch","description":"Replace exactly one occurrence of old_text with new_text in a regular UTF-8 file. Empty old_text creates a new file; it must not already exist. No shell or unified diff syntax. Parent directory must exist. Protected controls and symlinks cannot be edited. Changes invalidate checks.","parameters":{"type":"object","properties":{"path":{"type":"string"},"old_text":{"type":"string"},"new_text":{"type":"string"}},"required":["path","old_text","new_text"],"additionalProperties":false}}},
 {"type":"function","function":{"name":"run_tests","description":"Alias for run_checks. Reruns every trusted check on current edits. No arguments. Checks must pass after the last mutation.","parameters":{"type":"object","properties":{},"additionalProperties":false}}},
 {"type":"function","function":{"name":"run_formatter","description":"Runs only the formatter authorized in policy. Invalidates prior checks.","parameters":{"type":"object","properties":{},"additionalProperties":false}}},
 {"type":"function","function":{"name":"finish_repair","description":"Submit a nonempty repair after checks pass on its exact tree. Include addressed findings and remaining tradeoffs. Harness makes and publishes a candidate commit; a separate fresh review decides readiness. Call alone in a turn.","parameters":{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}}}
 ]`
	return append(append(common[:len(common)-1], ','), []byte(extra)...)
}
