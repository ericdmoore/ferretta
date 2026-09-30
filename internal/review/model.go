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
	if !slices.Contains(info.thinkingValues(), requested) {
		return fmt.Errorf("model does not establish support for requested thinking setting; choose a supported setting or update Ollama")
	}
	var architecture string
	var limit int
	if json.Unmarshal(info.Info["general.architecture"], &architecture) != nil || architecture == "" || json.Unmarshal(info.Info[architecture+".context_length"], &limit) != nil || limit < p.config.ContextTokens {
		return fmt.Errorf("model context capacity is unknown or below context_tokens")
	}
	return nil
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
	body := map[string]any{"model": p.config.Model, "messages": messages, "stream": false, "think": p.thinkValue(), "tools": tools,
		"options": map[string]any{"num_predict": p.config.MaxTokens, "num_ctx": p.config.ContextTokens}}
	if err := o.post(ctx, p, "/api/chat", body, &reply); err != nil {
		return Reply{}, err
	}
	if !reply.Done || reply.Message.Role != "assistant" || reply.Model == "" || reply.DoneReason == "length" {
		return Reply{}, fmt.Errorf("model response is incomplete (done=%t, reason=%q, output_tokens=%d)", reply.Done, reply.DoneReason, reply.OutputTokens)
	}
	return reply, nil
}

var tools = json.RawMessage(`[
 {"type":"function","function":{"name":"list_files","description":"List files in the exact reviewed commit.","parameters":{"type":"object","properties":{},"additionalProperties":false}}},
 {"type":"function","function":{"name":"read_file","description":"Read a repository-relative file at the reviewed commit.","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}}},
 {"type":"function","function":{"name":"run_checks","description":"Run the checks configured by the repository policy in the isolated review workspace.","parameters":{"type":"object","properties":{},"additionalProperties":false}}},
 {"type":"function","function":{"name":"finish_review","description":"Submit an evidence-backed verdict and major tradeoffs. LGTM requires successful run_checks. Findings need exact source locations. Ask a question when human intent is consequentially unclear.","parameters":{"type":"object","properties":{"verdict":{"type":"string","enum":["lgtm","changes_required","clarification_required"]},"summary":{"type":"string"},"tradeoffs":{"type":"array","items":{"type":"string"}},"findings":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"},"explanation":{"type":"string"}},"required":["path","line","explanation"]}},"question":{"type":"string"}},"required":["verdict","summary","tradeoffs","findings","question"],"additionalProperties":false}}}
]`)
