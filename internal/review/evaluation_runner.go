package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// Evaluate uses a separate conversation and only read-only tools. Checkpoint is
// called before model dispatch so an interrupted request is never silently free.
func (r Runner) Evaluate(ctx context.Context, p EvaluationPolicy, snapshot Snapshot, policyBytes []byte, attemptID, repositoryPath string, checkpoint func(Evaluation, []Message) error) Evaluation {
	result := Evaluation{ID: attemptID, OutputID: snapshot.id, Rubric: rubricVersion, PolicySHA256: hashBytes(policyBytes), Head: snapshot.input.PR.Head, Base: snapshot.input.PR.Base, ReviewStatus: snapshot.input.Status,
		Provider: p.route.config.Provider, RequestedModel: p.route.config.Model, RequestedEffort: p.route.config.Effort, RequestedThink: p.route.config.Thinking, Evidence: map[string]string{}}
	finish := func(reason string) Evaluation {
		result.Failure = reason
		if r.Now != nil {
			result.FinishedAt = r.Now()
		}
		return result
	}
	if r.Now == nil || r.Model == nil || r.Exec == nil || checkpoint == nil || p.route.config.Provider == "" || snapshot.id == "" || attemptID == "" || repositoryPath == "" {
		return finish("evaluation requires a validated snapshot, judge, executors, attempt ID and checkpoint")
	}
	result.StartedAt = r.Now()
	ctx, cancel := reviewContext(ctx, p.route)
	defer cancel()
	if err := r.Model.Capabilities(ctx, p.route); err != nil {
		return finish(err.Error())
	}
	w := Workspace{path: repositoryPath, mergeBase: snapshot.input.MergeBase, pr: snapshot.input.PR}
	for id, value := range map[string]any{
		"pr":     snapshot.input.PR,
		"review": map[string]any{"status": snapshot.input.Status, "summary": snapshot.input.Summary, "findings": snapshot.input.Findings, "tradeoffs": snapshot.input.Tradeoffs},
		"checks": map[string]any{"outputs": snapshot.input.Checks, "failure": snapshot.input.CheckFailure},
		"intent": snapshot.input.Intent,
	} {
		data, _ := json.MarshalIndent(value, "", "  ")
		result.Evidence[id] = string(data)
	}
	messages := []Message{{Role: "system", Content: evaluationRubric}, {Role: "user", Content: fmt.Sprintf("Rubric: %s. Evaluate output %s at head %s, base %s. Evidence IDs: pr, review, checks, intent. Use read_evidence for later pages and read-only repository tools for code.\n\nEvidence pr:\n%s\n\nEvidence review:\n%s", rubricVersion, snapshot.id, w.pr.Head, w.pr.Base, linePage([]byte(result.Evidence["pr"]), 0, 0), linePage([]byte(result.Evidence["review"]), 0, 0))}}
	for turn := 0; p.route.config.MaxTurns == 0 || turn < p.route.config.MaxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return finish(err.Error())
		}
		if err := checkContext(p.route, messages); err != nil {
			return finish(err.Error())
		}
		index := len(result.Usage)
		start := r.Now()
		result.Usage = append(result.Usage, UsageEvent{ID: fmt.Sprintf("%s/model/%d", attemptID, turn+1), Kind: "model", Operation: "judge", Outcome: "pending"})
		if err := checkpoint(result, messages); err != nil {
			return finish("persist evaluation before model dispatch: " + err.Error())
		}
		reply, err := r.Model.Turn(ctx, p.route, messages)
		event := &result.Usage[index]
		event.Duration = elapsed(start, r.Now())
		event.Outcome = "completed"
		if reply.PromptTokens > 0 {
			event.PromptTokens = &reply.PromptTokens
		}
		if reply.OutputTokens > 0 {
			event.OutputTokens = &reply.OutputTokens
		}
		if reply.Model != "" && !slices.Contains(result.ObservedModels, reply.Model) {
			result.ObservedModels = append(result.ObservedModels, reply.Model)
		}
		if err != nil {
			event.Outcome = "failed_usage_may_be_unknown"
			return finish(err.Error())
		}
		if reply.PromptTokens > 0 {
			p.route.observation = &ContextObservation{MessageCount: len(messages), PromptTokens: reply.PromptTokens, ToolSchema: hashBytes(p.route.tools())}
		}
		messages = append(messages, reply.Message)
		if err := checkpoint(result, messages); err != nil {
			return finish("persist evaluation reply: " + err.Error())
		}
		if len(reply.Message.ToolCalls) == 0 {
			messages = append(messages, Message{Role: "user", Content: "Use finish_evaluation with summary, worker and oversight. Each role needs all six dimensions. Cite supplied evidence IDs. Prose does not complete evaluation."})
		}
		for _, call := range reply.Message.ToolCalls {
			name := call.Function.Name
			if name == "finish_evaluation" && len(reply.Message.ToolCalls) == 1 {
				a, err := parseAssessment(call.Function.Arguments, result.Evidence)
				if err == nil {
					result.Assessment = a
					return finish("")
				}
				messages = append(messages, Message{Role: "tool", ToolName: name, Content: err.Error()})
				continue
			}
			start := r.Now()
			text, err := r.evaluationTool(ctx, w, result.Evidence, name, call.Function.Arguments)
			outcome := "completed"
			if err != nil {
				outcome, text = "failed", "Tool failed: "+err.Error()+". Available: read_evidence, read_file, read_diff, list_files, search, grep, finish_evaluation (alone)."
			}
			if len(text) > 64000 {
				return finish("evaluation tool result exceeds 64000 bytes")
			}
			id := fmt.Sprintf("tool-%d", len(result.Usage)+1)
			result.Evidence[id] = name + " " + string(call.Function.Arguments) + "\n" + text
			result.Usage = append(result.Usage, UsageEvent{ID: attemptID + "/" + id, Kind: "tool", Operation: name, Outcome: outcome, Duration: elapsed(start, r.Now())})
			messages = append(messages, Message{Role: "tool", ToolName: name, Content: "Evidence ID: " + id + "\n" + text})
			if err := checkpoint(result, messages); err != nil {
				return finish("persist evaluation tool result: " + err.Error())
			}
		}
	}
	return finish("evaluation turn allowance exhausted")
}

func elapsed(start, end time.Time) time.Duration {
	if end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

func (r Runner) evaluationTool(ctx context.Context, w Workspace, evidence map[string]string, name string, arguments json.RawMessage) (string, error) {
	if name == "read_evidence" {
		var args struct {
			ID    string `json:"id"`
			Start int    `json:"start_line"`
			End   int    `json:"end_line"`
		}
		d := json.NewDecoder(bytes.NewReader(arguments))
		d.DisallowUnknownFields()
		if err := d.Decode(&args); err != nil {
			return "", err
		}
		var extra any
		if err := d.Decode(&extra); err != io.EOF {
			return "", fmt.Errorf("read_evidence requires exactly one argument object")
		}
		text, ok := evidence[args.ID]
		if !ok || args.Start < 0 || args.End < 0 || (args.End > 0 && args.End < args.Start) {
			return "", fmt.Errorf("read_evidence requires an available ID and ordered nonnegative lines")
		}
		return linePage([]byte(text), args.Start, args.End), nil
	}
	if !slices.Contains([]string{"search", "grep", "read_file", "read_diff", "list_files"}, name) {
		return "", fmt.Errorf("tool %q is not permitted for evaluation", name)
	}
	command, err := Plan(name, arguments)
	if err != nil {
		return "", err
	}
	switch c := command.(type) {
	case Search:
		if r.Searcher == nil {
			return "", fmt.Errorf("search executor unavailable")
		}
		page, err := r.Searcher.Search(ctx, w.path, w.pr.Head, c)
		if err != nil {
			return "", err
		}
		data, _ := json.Marshal(page)
		return string(data), nil
	case ReadFile:
		data, err := r.read(ctx, w, c.path)
		return linePage(data, c.start, c.end), err
	case ReadDiff:
		data, err := r.Exec.Run(ctx, w.path, "git", "diff", "--no-ext-diff", "--no-textconv", "--no-color", w.mergeBase, w.pr.Head, "--")
		return linePage(data, c.start, c.end), err
	default: // The allowlist and Plan leave only ListFiles.
		data, err := r.Exec.Run(ctx, w.path, "git", "ls-tree", "-r", "--name-only", w.pr.Head)
		return strings.TrimSpace(string(data)), err
	}
}
