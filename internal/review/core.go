// Package review implements a bounded, tool-assisted review of a submitted PR.
package review

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"strings"
)

type config struct {
	Provider       string     `json:"provider"`
	Endpoint       string     `json:"endpoint"`
	Model          string     `json:"model"`
	Effort         string     `json:"effort,omitempty"`
	Thinking       string     `json:"thinking,omitempty"`
	ContextTokens  int        `json:"context_tokens,omitempty"`
	MaxTurns       int        `json:"max_turns"`
	MaxTokens      int        `json:"max_tokens_per_turn"`
	TimeoutSeconds int        `json:"timeout_seconds"`
	Checks         [][]string `json:"checks"`
}

// Policy can only be populated by validation. Its zero value is unusable.
type Policy struct{ config config }

func ParsePolicy(data []byte) (Policy, error) {
	var c config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Policy{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Policy{}, fmt.Errorf("policy must contain exactly one JSON value")
	}
	if err := validateOllamaEndpoint(c.Endpoint); err != nil {
		return Policy{}, err
	}
	if c.Provider != "ollama" || strings.TrimSpace(c.Model) == "" {
		return Policy{}, fmt.Errorf("an explicit Ollama model is required")
	}
	if !((c.Thinking == "enabled" && c.Effort == "") || (c.Thinking == "" && (c.Effort == "low" || c.Effort == "medium" || c.Effort == "high"))) {
		return Policy{}, fmt.Errorf("select thinking: enabled OR effort: low, medium, high")
	}
	if c.ContextTokens == 0 {
		c.ContextTokens = 65536
	} // Preserve existing policies.
	if c.ContextTokens < 8192 || c.ContextTokens > 131072 || c.MaxTokens+2048 >= c.ContextTokens {
		return Policy{}, fmt.Errorf("context_tokens must be 8192–131072 with room for input and output")
	}
	if c.MaxTurns < 1 || c.MaxTurns > 30 || c.MaxTokens < 256 || c.MaxTokens > 16384 || c.TimeoutSeconds < 1 || c.TimeoutSeconds > 3600 {
		return Policy{}, fmt.Errorf("invalid review resource limits")
	}
	if len(c.Checks) == 0 {
		return Policy{}, fmt.Errorf("at least one repository check is required")
	}
	for _, command := range c.Checks {
		if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
			return Policy{}, fmt.Errorf("empty check command")
		}
	}
	return Policy{config: c}, nil
}

func validateOllamaEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("local Ollama endpoint must be an HTTP loopback URL")
	}
	return nil
}

func (p Policy) thinkValue() any {
	if p.config.Thinking == "enabled" {
		return true
	}
	return p.config.Effort
}

// Bound context conservatively using encoded bytes plus a framing reserve.
// This is an input admission bound, not a measurement of provider token usage.
func checkContext(p Policy, messages []Message) error {
	data, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	if len(data)+len(tools)+1024+p.config.MaxTokens > p.config.ContextTokens {
		return fmt.Errorf("review exceeds configured context limit; choose a smaller PR or explicitly increase context_tokens")
	}
	return nil
}

type Finding struct {
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Explanation string `json:"explanation"`
}

type candidate struct {
	Verdict   string    `json:"verdict"`
	Summary   string    `json:"summary"`
	Tradeoffs []string  `json:"tradeoffs"`
	Findings  []Finding `json:"findings"`
	Question  string    `json:"question"`
}

type Command interface{ command() }
type ReadFile struct{ path string }

func (ReadFile) command() {}

type ListFiles struct{}

func (ListFiles) command() {}

type RunChecks struct{}

func (RunChecks) command() {}

type Finish struct{ candidate candidate }

func (Finish) command() {}

// Plan validates untrusted tool arguments before constructing effect commands.
func Plan(name string, arguments json.RawMessage) (Command, error) {
	decode := func(target any) error {
		d := json.NewDecoder(bytes.NewReader(arguments))
		d.DisallowUnknownFields()
		if err := d.Decode(target); err != nil {
			return err
		}
		var extra any
		if err := d.Decode(&extra); err != io.EOF {
			return fmt.Errorf("tool arguments must contain exactly one JSON value")
		}
		return nil
	}
	switch name {
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		if err := decode(&args); err != nil {
			return nil, err
		}
		if !fs.ValidPath(args.Path) || args.Path == "." {
			return nil, fmt.Errorf("read_file requires a repository-relative file path")
		}
		return ReadFile{path: args.Path}, nil
	case "list_files", "run_checks":
		if err := decode(&struct{}{}); err != nil {
			return nil, err
		}
		if name == "list_files" {
			return ListFiles{}, nil
		}
		return RunChecks{}, nil
	case "finish_review":
		var c candidate
		if err := decode(&c); err != nil {
			return nil, err
		}
		c.Verdict = strings.ToLower(strings.TrimSpace(c.Verdict))
		if strings.TrimSpace(c.Summary) == "" {
			return nil, fmt.Errorf("verdict requires an evidence-based summary")
		}
		switch c.Verdict {
		case "lgtm":
			if len(c.Findings) != 0 || c.Question != "" {
				return nil, fmt.Errorf("LGTM cannot contain blocking findings or questions")
			}
		case "changes_required":
			if len(c.Findings) == 0 || c.Question != "" {
				return nil, fmt.Errorf("changes_required needs findings and no pending question")
			}
		case "clarification_required":
			if strings.TrimSpace(c.Question) == "" || len(c.Findings) != 0 {
				return nil, fmt.Errorf("clarification requires a question and no final findings")
			}
		default:
			return nil, fmt.Errorf("verdict must be lgtm, changes_required, or clarification_required")
		}
		for _, finding := range c.Findings {
			if !fs.ValidPath(finding.Path) || finding.Path == "." || finding.Line < 1 || strings.TrimSpace(finding.Explanation) == "" {
				return nil, fmt.Errorf("finding requires a valid path, line, and explanation")
			}
		}
		return Finish{candidate: c}, nil
	default:
		return nil, fmt.Errorf("tool %q is not permitted", name)
	}
}

// CheckEvidence is created only after execution; its zero value is unchecked.
type CheckEvidence struct {
	outputs []string
	failure string
}

// Conclusion is only produced through policy checks. Zero means incomplete.
type Conclusion struct {
	accepted *candidate
	reason   string
}

func Conclude(finish Finish, checks CheckEvidence) Conclusion {
	if finish.candidate.Summary == "" {
		return Conclusion{reason: "no validated verdict"}
	}
	if finish.candidate.Verdict == "lgtm" && (len(checks.outputs) == 0 || checks.failure != "") {
		return Conclusion{reason: "required checks have not passed"}
	}
	return Conclusion{accepted: &finish.candidate}
}

func (c Conclusion) Status() string {
	if c.accepted == nil {
		return "incomplete"
	}
	return c.accepted.Verdict
}
