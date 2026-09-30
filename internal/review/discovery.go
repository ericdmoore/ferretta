package review

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// probe describes a metadata request, never an inference request. Hints describe
// common port conventions, not a verified service identity or inference location.
type probe struct {
	hint, endpoint, path string
}

func discoveryPlan(ollamaEndpoint string) []probe {
	return []probe{
		{"Ollama API", strings.TrimRight(ollamaEndpoint, "/"), "/api/tags"},
		{"compatible API (commonly LiteLLM)", "http://127.0.0.1:4000", "/v1/models"},
		{"compatible API (commonly vLLM)", "http://127.0.0.1:8000", "/v1/models"},
		{"compatible API (commonly LM Studio)", "http://127.0.0.1:1234", "/v1/models"},
		{"compatible API (commonly llama.cpp)", "http://127.0.0.1:8080", "/v1/models"},
	}
}

func discover(ctx context.Context, client Doer, p probe) ([]string, error) {
	if client == nil {
		return nil, fmt.Errorf("model inventory client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+p.path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d (authentication or server setup may be required)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("model inventory exceeds 1 MiB")
	}
	var inventory struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &inventory); err != nil {
		return nil, fmt.Errorf("invalid model inventory")
	}
	var names []string
	if p.path == "/api/tags" {
		if inventory.Models == nil {
			return nil, fmt.Errorf("missing models array")
		}
		for _, model := range inventory.Models {
			names = append(names, model.Name)
		}
	} else {
		if inventory.Data == nil {
			return nil, fmt.Errorf("missing data array")
		}
		for _, model := range inventory.Data {
			names = append(names, model.ID)
		}
	}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("empty model identifier")
		}
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}
