package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Thinking  string     `json:"thinking,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}
type ToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type Reply struct {
	Model        string  `json:"model"`
	Message      Message `json:"message"`
	Done         bool    `json:"done"`
	DoneReason   string  `json:"done_reason"`
	PromptTokens int     `json:"prompt_eval_count"`
	OutputTokens int     `json:"eval_count"`
}

type Model interface {
	Capabilities(context.Context, Policy) error
	Turn(context.Context, Policy, []Message) (Reply, error)
}
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}
type Ollama struct{ HTTP Doer }

func (o Ollama) post(ctx context.Context, policy Policy, path string, body any, result any) error {
	if policy.config.Provider != "ollama" || o.HTTP == nil {
		return fmt.Errorf("validated policy and HTTP client are required")
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(policy.config.Endpoint, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("local model request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("local model returned HTTP %d", resp.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return fmt.Errorf("model response exceeds 4 MiB")
	}
	return json.Unmarshal(data, result)
}

type ModelInfo struct {
	Capabilities []string `json:"capabilities"`
	RemoteHost   string   `json:"remote_host"`
	RemoteModel  string   `json:"remote_model"`
	Details      struct {
		Family string `json:"family"`
	} `json:"details"`
	Thinking *struct {
		Values []json.RawMessage `json:"values"`
	} `json:"thinking"`
	Info map[string]json.RawMessage `json:"model_info"`
}

func (o Ollama) Info(ctx context.Context, endpoint, model string) (ModelInfo, error) {
	if err := validateOllamaEndpoint(endpoint); err != nil {
		return ModelInfo{}, err
	}
	var info ModelInfo
	err := o.post(ctx, Policy{config: config{Provider: "ollama", Endpoint: endpoint}}, "/api/show", map[string]string{"model": model}, &info)
	return info, err
}

func (info ModelInfo) thinkingValues() []string {
	if info.Thinking != nil {
		var values []string
		for _, raw := range info.Thinking.Values {
			switch strings.TrimSpace(string(raw)) {
			case "true":
				values = append(values, "enabled")
			case `"low"`:
				values = append(values, "low")
			case `"medium"`:
				values = append(values, "medium")
			case `"high"`:
				values = append(values, "high")
			}
		}
		return values
	}
	// Explicit compatibility for older Ollama releases without thinking metadata.
	// Never guess from a model name or treat an unknown family as compatible.
	switch info.Details.Family {
	case "gptoss":
		return []string{"low", "medium", "high"}
	case "qwen3":
		return []string{"enabled"}
	}
	return nil
}

func (info ModelInfo) validate(p Policy) error {
	if info.RemoteHost != "" || info.RemoteModel != "" {
		return fmt.Errorf("local-only policy rejects a remotely hosted model")
	}
	if !slices.Contains(info.Capabilities, "tools") || !slices.Contains(info.Capabilities, "thinking") {
		return fmt.Errorf("model must support tools and thinking")
	}
	requested := p.config.Effort
	if p.config.Thinking == "enabled" {
		requested = "enabled"
	}
	if p.config.Thinking == "provider_default" {
		if !info.permitsThinkingDefault() {
			return fmt.Errorf("model thinking metadata does not establish thinking capability")
		}
	} else if !slices.Contains(info.thinkingValues(), requested) {
		return fmt.Errorf("model does not establish support for requested thinking setting; choose a supported setting or update Ollama")
	}
	var architecture string
	var limit int
	if json.Unmarshal(info.Info["general.architecture"], &architecture) != nil || architecture == "" || json.Unmarshal(info.Info[architecture+".context_length"], &limit) != nil || limit < p.config.ContextTokens {
		return fmt.Errorf("model context capacity is unknown or below context_tokens")
	}
	return nil
}

// The capability check above is still required. Missing control metadata can
// support an explicit request for the provider's default, never an assertion
// that thinking is enabled. Explicitly non-thinking/invalid control metadata
// cannot be bypassed by selecting a default. Model-defined named levels are
// opaque here; Ferretta is not requesting or interpreting a particular level.
func (info ModelInfo) permitsThinkingDefault() bool {
	if info.Thinking == nil {
		return true
	}
	for _, value := range info.Thinking.Values {
		if strings.TrimSpace(string(value)) == "true" {
			return true
		}
		var name string
		if json.Unmarshal(value, &name) == nil && strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}

func (o Ollama) Capabilities(ctx context.Context, p Policy) error {
	info, err := o.Info(ctx, p.config.Endpoint, p.config.Model)
	if err != nil {
		return err
	}
	return info.validate(p)
}

func (o Ollama) Turn(ctx context.Context, p Policy, messages []Message) (Reply, error) {
	if err := checkContext(p, messages); err != nil {
		return Reply{}, err
	}
	var reply Reply
	body := map[string]any{"model": p.config.Model, "messages": messages, "stream": false, "think": p.thinkValue(), "tools": p.tools(),
		"options": map[string]any{"num_predict": p.config.MaxTokens, "num_ctx": p.config.ContextTokens}}
	if err := o.post(ctx, p, "/api/chat", body, &reply); err != nil {
		return Reply{}, err
	}
	if !reply.Done || reply.Message.Role != "assistant" || reply.Model == "" || reply.DoneReason == "length" {
		return reply, fmt.Errorf("model response is incomplete (done=%t, reason=%q, output_tokens=%d)", reply.Done, reply.DoneReason, reply.OutputTokens)
	}
	return reply, nil
}

func (p Policy) tools() json.RawMessage {
	if p.toolSchema != nil {
		return p.toolSchema
	}
	return tools
}

const searchToolDefinition = `"description":"search and grep are exact aliases. Search text files in the exact reviewed commit, not the working tree. Literal query by default; regex:true uses POSIX extended regex. path is an optional literal file/directory scope, not a glob. Returns paths, line numbers and matching line prefixes; binary files are skipped. Use read_file for context. Results are bounded to 48KB and 1–100 matches (default 20). When has_more is true, repeat the same query/path/regex with next_offset. If pagination limit is reached, narrow the query/path. text_truncated marks a shortened matching line; it is not complete evidence.","parameters":{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":1024},"path":{"type":"string"},"regex":{"type":"boolean"},"max_results":{"type":"integer","minimum":1,"maximum":100},"offset":{"type":"integer","minimum":0,"maximum":10000}},"required":["query"],"additionalProperties":false}`

var tools = json.RawMessage(`[
 {"type":"function","function":{"name":"search",` + searchToolDefinition + `}},
 {"type":"function","function":{"name":"grep",` + searchToolDefinition + `}},
 {"type":"function","function":{"name":"request_intent_confirmation","description":"Ask only about ambiguous intended behavior or a consequential tradeoff. Include all context in this question: the human sees only the proposal comment, not your earlier analysis. Do not ask the human to choose a verdict, approve LGTM, approve merging, or permit fixing an established bug. You choose the verdict yourself using finish_review; report bugs with changes_required. Call alone in a turn. Ferretta assigns the version, renders PROPOSED and saves the waiting state; posting requires operator configuration. After CORRECTED, revise the same topic. Never write human confirmation markers.","parameters":{"type":"object","properties":{"topic":{"type":"string"},"question":{"type":"string"},"reason":{"type":"string"},"options":{"type":"array","items":{"type":"string"},"maxItems":4},"recommendation":{"type":"string"}},"required":["topic","question","reason"],"additionalProperties":false}}},
 {"type":"function","function":{"name":"list_files","description":"List files in the exact reviewed commit.","parameters":{"type":"object","properties":{},"additionalProperties":false}}},
 {"type":"function","function":{"name":"read_diff","description":"Read a numbered page of the submitted diff. Defaults to the first 200 lines. Request subsequent ranges as needed.","parameters":{"type":"object","properties":{"start_line":{"type":"integer"},"end_line":{"type":"integer"}},"additionalProperties":false}}},
 {"type":"function","function":{"name":"read_file","description":"Read a numbered page of a file at the reviewed commit. Defaults to the first 200 lines; request subsequent ranges as needed.","parameters":{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer"},"end_line":{"type":"integer"}},"required":["path"],"additionalProperties":false}}},
 {"type":"function","function":{"name":"run_checks","description":"Run the checks configured by the repository policy in the isolated review workspace.","parameters":{"type":"object","properties":{},"additionalProperties":false}}},
 {"type":"function","function":{"name":"finish_review","description":"Complete the review by choosing and submitting your own evidence-backed verdict and major tradeoffs. LGTM requires successful run_checks and no human confirmation. Use empty findings and an empty question for LGTM; use located findings and an empty question for changes_required. Findings need exact source locations. For intent questions use request_intent_confirmation instead.","parameters":{"type":"object","properties":{"verdict":{"type":"string","enum":["lgtm","changes_required"]},"summary":{"type":"string"},"tradeoffs":{"type":"array","items":{"type":"string"}},"findings":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"},"explanation":{"type":"string"}},"required":["path","line","explanation"]}},"question":{"type":"string"}},"required":["verdict","summary","tradeoffs","findings","question"],"additionalProperties":false}}}
]`)
