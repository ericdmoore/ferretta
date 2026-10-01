package review

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Shape reported by alpaca's installed Qwen MLX model. Its alias is not used
// for capability inference: only explicit provider metadata and policy matter.
const qwenMLXInfo = `{"capabilities":["completion","tools","thinking"],"details":{"family":"qwen3_5"},"model_info":{"general.architecture":"qwen3_5","qwen3_5.context_length":262144}}`

func defaultThinkingJSON() string {
	return strings.Replace(policyJSON, `"effort":"medium"`, `"thinking":"provider_default"`, 1)
}

func TestProviderDefaultDoesNotWeakenExplicitControls(t *testing.T) {
	p, err := ParsePolicy([]byte(defaultThinkingJSON()))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, metadata string
		policy         Policy
		admitted       bool
	}{
		{"missing controls explicitly accepted", qwenMLXInfo, p, true},
		{"missing controls cannot establish enabled", qwenMLXInfo, qwenPolicy(t), false},
		{"missing controls cannot establish medium", qwenMLXInfo, policy(t), false},
		{"no tool support", strings.Replace(qwenMLXInfo, `"tools",`, "", 1), p, false},
		{"no thinking capability", strings.Replace(qwenMLXInfo, `,"thinking"`, "", 1), p, false},
		{"remote inference", strings.Replace(qwenMLXInfo, `"details":`, `"remote_host":"cloud","details":`, 1), p, false},
		{"unknown context", strings.Replace(qwenMLXInfo, "262144", `"unknown"`, 1), p, false},
		{"insufficient context", strings.Replace(qwenMLXInfo, "262144", "8192", 1), p, false},
		{"boolean controls", strings.Replace(qwenMLXInfo, `"details":`, `"thinking":{"values":[false,true]},"details":`, 1), p, true},
		{"named controls", strings.Replace(qwenMLXInfo, `"details":`, `"thinking":{"values":["extended"],"default":"extended"},"details":`, 1), p, true},
		{"explicit no thinking", strings.Replace(qwenMLXInfo, `"details":`, `"thinking":{"values":[false],"default":false},"details":`, 1), p, false},
		{"empty controls", strings.Replace(qwenMLXInfo, `"details":`, `"thinking":{"values":[]},"details":`, 1), p, false},
		{"invalid controls", strings.Replace(qwenMLXInfo, `"details":`, `"thinking":{"values":[false,null,42,{},""]},"details":`, 1), p, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var info ModelInfo
			if err := json.Unmarshal([]byte(tt.metadata), &info); err != nil {
				t.Fatal(err)
			}
			if err := info.validate(tt.policy); (err == nil) != tt.admitted {
				t.Fatal("wrong admission", err)
			}
		})
	}
	for _, raw := range []string{strings.Replace(defaultThinkingJSON(), `"thinking":"provider_default"`, `"thinking":"provider_default","effort":"medium"`, 1), strings.Replace(defaultThinkingJSON(), "provider_default", "default", 1)} {
		if _, err := ParsePolicy([]byte(raw)); err == nil {
			t.Fatal("ambiguous configuration accepted")
		}
	}
}

func TestProviderDefaultWireAndProvenance(t *testing.T) {
	p, err := ParsePolicy([]byte(defaultThinkingJSON()))
	if err != nil {
		t.Fatal(err)
	}
	o := Ollama{HTTP: httpFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if string(body["think"]) != "null" {
			t.Fatal("provider default must send explicit null", string(body["think"]))
		}
		return httpResponse(200, `{"model":"qwen3.8:27b-mlx","done":true,"message":{"role":"assistant"}}`), nil
	})}
	if _, err := o.Turn(context.Background(), p, []Message{{Role: "user", Content: "test"}}); err != nil {
		t.Fatal(err)
	}
	m := &fakeModel{replies: workflowReplies()}
	r := runner(m)
	report := r.Review(context.Background(), p, workspace(), []byte(defaultThinkingJSON()), func(Report, []Message) error { return nil })
	if report.Status != "lgtm" || report.RequestedThinking != "provider_default" || report.RequestedEffort != "" || report.EffectiveEffort != nil {
		t.Fatal("default misrepresented as observed effort", report)
	}
	var output bytes.Buffer
	if err := PrintReport(&output, report, "text"); err != nil || !strings.Contains(output.String(), "provider_default") || !strings.Contains(output.String(), "not reported") {
		t.Fatal(output.String(), err)
	}
	judge, err := ParseEvaluationPolicy([]byte(strings.Replace(evaluationPolicyJSON(), `"effort":"medium"`, `"thinking":"provider_default"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	evaluated := r.Evaluate(context.Background(), judge, snapshotFixture(t), nil, "judge", ".", func(Evaluation, []Message) error { return nil })
	if evaluated.Assessment == nil || evaluated.RequestedThink != "provider_default" || evaluated.EffectiveEffort != nil {
		t.Fatal(evaluated)
	}
}

func TestSetupAndDoctorRequireExplicitProviderDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	s := setupFixture(t)
	original := s.HTTP
	s.HTTP = httpFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/show" {
			return httpResponse(200, qwenMLXInfo), nil
		}
		return original.Do(req)
	})
	var output, stderr bytes.Buffer
	if code := s.Run(context.Background(), unattended, nil, &output, &stderr); code != 1 || !strings.Contains(stderr.String(), "--thinking provider_default") {
		t.Fatal(code, stderr.String())
	}
	if _, err := os.Stat(".ferretta/review.json"); !os.IsNotExist(err) {
		t.Fatal("auto silently chose a default", err)
	}
	args := append(append([]string{}, unattended...), "--thinking", "provider_default")
	if code := s.Run(context.Background(), args, nil, &output, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	data, err := os.ReadFile(".ferretta/review.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePolicy(data)
	if err != nil || p.config.Thinking != "provider_default" || p.config.Effort != "" {
		t.Fatal(string(data), err)
	}
	if !strings.Contains(output.String(), "effective effort remain unknown") {
		t.Fatal(output.String())
	}
	c := doctorCLI(t)
	c.Setup.HTTP = s.HTTP
	output.Reset()
	if code := c.Doctor(context.Background(), []string{"--repo", "o/r"}, &output, &stderr); code != 0 || !strings.Contains(output.String(), "enabled state and effective effort unknown") {
		t.Fatal(code, output.String(), stderr.String())
	}
	bad := append(append([]string{}, unattended...), "--effort", "provider_default", "--policy", "other.json")
	if code := s.Run(context.Background(), bad, nil, io.Discard, io.Discard); code != 1 {
		t.Fatal("thinking mode accepted as effort")
	}
}
