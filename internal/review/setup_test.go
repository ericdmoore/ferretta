package review

import (
	"bytes"
	"context"
	"errors"
	"github.com/ericdmoore/ferretta/internal/github"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func setupFixture(t *testing.T) Setup {
	t.Helper()
	return Setup{
		HTTP: httpFunc(func(r *http.Request) (*http.Response, error) {
			if _, ok := r.Context().Deadline(); !ok {
				t.Fatal("unbounded metadata request")
			}
			if r.Header.Get("Authorization") != "" {
				t.Fatal("discovery leaked credentials")
			}
			data := `{"data":[{"id":"unknown-route-model"}]}`
			switch r.URL.Path {
			case "/api/tags":
				data = `{"models":[{"name":"z-model"},{"name":"a-model"},{"name":"a-model"}]}`
			case "/api/show":
				if r.Method != "POST" {
					t.Fatal("invalid capabilities request")
				}
				data = testModelInfo
			case "/v1/models":
			default:
				t.Fatalf("unexpected request (setup must never infer): %s", r.URL)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data))}, nil
		}),
		Auth: func(ctx context.Context, repo string) (github.Identity, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded auth verification")
			}
			return github.Identity{BotLogin: "ferretta[bot]"}, nil
		},
	}
}

var unattended = []string{"--yes", "--model", "a-model", "--check", `["go","test","./..."]`}

func TestSetupCreatesUsablePolicy(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		t.Run(map[bool]string{true: "interactive", false: "unattended"}[interactive], func(t *testing.T) {
			t.Chdir(t.TempDir())
			args, input := unattended, ""
			if interactive {
				if err := os.WriteFile("Makefile", []byte("check:\n\techo test\n"), 0600); err != nil {
					t.Fatal(err)
				}
				args, input = nil, "1\n\nyes\n"
			}
			var out, stderr bytes.Buffer
			if code := (CLI{Setup: setupFixture(t)}).Init(context.Background(), args, strings.NewReader(input), &out, &stderr); code != 0 {
				t.Fatalf("%d: %s", code, &stderr)
			}
			data, err := os.ReadFile(".ferretta/review.json")
			if err != nil {
				t.Fatal(err)
			}
			p, err := ParsePolicy(data)
			if err != nil || p.config.Model != "a-model" || p.config.Effort != "medium" || p.config.MaxTurns != 100 || p.config.MaxTokens != 4096 || p.config.TimeoutSeconds != 600 {
				t.Fatalf("unusable policy: %s, %v", data, err)
			}
			want := []string{"go", "test", "./..."}
			if interactive {
				want = []string{"make", "check"}
			}
			if !reflect.DeepEqual(p.config.Checks, [][]string{want}) {
				t.Fatal(p.config.Checks)
			}
			info, _ := os.Stat(".ferretta/review.json")
			if info.Mode().Perm() != 0600 {
				t.Fatal("policy permissions", info.Mode())
			}
			for _, text := range []string{"LiteLLM", "vLLM", "LM Studio", "llama.cpp", "inference location/capabilities unknown", "GitHub: not verified", "Created .ferretta/review.json"} {
				if !strings.Contains(out.String(), text) {
					t.Fatalf("missing %q: %s", text, &out)
				}
			}
			if strings.Contains(out.String(), "private auth detail") {
				t.Fatal("auth details exposed")
			}
			if code := setupFixture(t).Run(context.Background(), unattended, nil, io.Discard, io.Discard); code != 1 {
				t.Fatal("existing file accepted")
			}
			after, _ := os.ReadFile(".ferretta/review.json")
			if !bytes.Equal(data, after) {
				t.Fatal("existing policy overwritten")
			}
		})
	}
}

func TestSetupDiscoveryAndCancellation(t *testing.T) {
	for _, mode := range []string{"discover", "offline", "cancel", "auth_cancel", "decline", "auth_missing", "no_models", "custom_endpoint"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			s := setupFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			args, input, expected := []string{"--discover", "--repo", "o/r"}, "", 0
			switch mode {
			case "offline", "no_models":
				s.HTTP = httpFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
				if mode == "no_models" {
					args, expected = unattended, 1
				}
			case "cancel":
				cancel()
				expected = 1
			case "auth_cancel":
				s.Auth = func(context.Context, string) (github.Identity, error) { cancel(); return github.Identity{}, ctx.Err() }
				expected = 1
			case "decline":
				args, input = nil, "2\n[\"go\",\"test\"]\nn\n"
			case "auth_missing":
				s.Auth = func(context.Context, string) (github.Identity, error) {
					return github.Identity{}, errors.New("secret-error-detail")
				}
			case "custom_endpoint":
				args = []string{"--discover", "--endpoint", "http://[::1]:12345/"}
				base := s.HTTP
				s.HTTP = httpFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/api/tags" && r.URL.Host != "[::1]:12345" {
						t.Fatal("explicit endpoint ignored")
					}
					return base.Do(r)
				})
			}
			var out, stderr bytes.Buffer
			if code := s.Run(ctx, args, strings.NewReader(input), &out, &stderr); code != expected {
				t.Fatalf("%d: %s", code, &stderr)
			}
			if _, err := os.Stat(".ferretta"); !os.IsNotExist(err) {
				t.Fatal("discovery/cancellation wrote files")
			}
			if mode == "auth_missing" && (!strings.Contains(out.String(), "auth github --setup") || strings.Contains(out.String(), "secret-error-detail")) {
				t.Fatal(out.String())
			}
		})
	}
}

func TestSetupRejectsInvalidInputs(t *testing.T) {
	for _, tt := range []struct {
		name  string
		args  []string
		input string
	}{
		{"flag", []string{"--unknown"}, ""}, {"positional", []string{"extra"}, ""}, {"unattended", []string{"--yes"}, ""},
		{"endpoint", []string{"--endpoint", "https://hosted.test"}, ""}, {"effort", []string{"--effort", "max"}, "1\n\n"},
		{"eof_selection", nil, ""}, {"bad_selection", nil, "x\n"}, {"selection_bounds", nil, "99\n"},
		{"eof_check", nil, "1\n"}, {"bad_check", nil, "1\ninvalid\n"}, {"empty_check", nil, "1\n[]\n"},
		{"eof_accept", nil, "1\n\n"}, {"unlisted", []string{"--model", "unknown"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("Makefile", []byte("check:\n\techo test\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if code := setupFixture(t).Run(context.Background(), tt.args, strings.NewReader(tt.input), io.Discard, io.Discard); code != 1 {
				t.Fatal("invalid input accepted")
			}
			if _, err := os.Stat(".ferretta"); !os.IsNotExist(err) {
				t.Fatal("invalid input wrote files")
			}
		})
	}
	if code := NewCLI(&github.Connection{}).Init(context.Background(), []string{"--help"}, nil, io.Discard, io.Discard); code != 0 {
		t.Fatal("help failed")
	}
}

func TestSetupRejectsIneligibleModel(t *testing.T) {
	for _, body := range []string{`{"capabilities":["tools"]}`, `{"capabilities":["tools","thinking"],"remote_host":"cloud"}`} {
		t.Run(body, func(t *testing.T) {
			t.Chdir(t.TempDir())
			s := setupFixture(t)
			base := s.HTTP
			s.HTTP = httpFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/show" {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
				}
				return base.Do(r)
			})
			if code := s.Run(context.Background(), unattended, nil, io.Discard, io.Discard); code != 1 {
				t.Fatal("ineligible model configured")
			}
			if _, err := os.Stat(".ferretta"); !os.IsNotExist(err) {
				t.Fatal("ineligible model wrote files")
			}
		})
	}
}

type failAfterWriter struct{ writes, limit int }

func (w *failAfterWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes >= w.limit {
		return 0, errors.New("output failed")
	}
	return len(p), nil
}

func TestSetupIOFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		input  io.Reader
		output io.Writer
		args   []string
	}{
		{"read", failingIO{}, io.Discard, nil},
		{"inventory", nil, failingIO{}, []string{"--discover"}},
		{"prompt", strings.NewReader("1\n"), &failAfterWriter{limit: 4}, nil},
		{"preview", nil, &failAfterWriter{limit: 2}, unattended},
		{"write", nil, io.Discard, append(append([]string{}, unattended...), "--policy", "file/child")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tt.name == "write" {
				if err := os.WriteFile("file", []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if code := setupFixture(t).Run(context.Background(), tt.args, tt.input, tt.output, io.Discard); code != 1 {
				t.Fatal("IO error hidden")
			}
			if _, err := os.Stat(".ferretta/review.json"); !os.IsNotExist(err) {
				t.Fatal("failed setup wrote policy")
			}
		})
	}
}

func TestPolicyFileNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := createPolicyFile(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := createPolicyFile(path, []byte("replacement")); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Fatal("policy overwritten")
	}
	if err := createPolicyFile(filepath.Join(path, "child"), nil); err == nil {
		t.Fatal("parent file accepted")
	}
	link := filepath.Join(dir, "symlink")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := createPolicyFile(link, []byte("replacement")); !errors.Is(err, os.ErrExist) {
		t.Fatal("followed a symlink", err)
	}
}

type effectWriter func([]byte)

func (w effectWriter) Write(p []byte) (int, error) { w(p); return len(p), nil }

func TestSetupRechecksBeforeSaving(t *testing.T) {
	for _, mode := range []string{"cancel", "concurrent_policy"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out := effectWriter(func(p []byte) {
				if !bytes.HasPrefix(p, []byte("Policy for")) {
					return
				}
				if mode == "cancel" {
					cancel()
					return
				}
				if err := createPolicyFile(".ferretta/review.json", []byte("other process's policy")); err != nil {
					t.Fatal(err)
				}
			})
			if code := setupFixture(t).Run(ctx, unattended, nil, out, io.Discard); code != 1 {
				t.Fatal("saved after cancellation or concurrent creation")
			}
			data, err := os.ReadFile(".ferretta/review.json")
			if mode == "cancel" {
				if !os.IsNotExist(err) {
					t.Fatal("cancelled setup wrote policy")
				}
			} else if err != nil || string(data) != "other process's policy" {
				t.Fatal("concurrent policy was lost", err)
			}
		})
	}
}

func TestDiscoveryBoundary(t *testing.T) {
	for _, tt := range []struct {
		name, path, body string
		status           int
		want             []string
		bad              bool
	}{
		{"ollama", "/api/tags", `{"models":[{"name":"b"},{"name":"a"},{"name":"a"}]}`, 200, []string{"a", "b"}, false},
		{"compatible", "/v1/models", `{"data":[{"id":"m"}]}`, 200, []string{"m"}, false},
		{"empty", "/api/tags", `{"models":[]}`, 200, nil, false},
		{"auth", "/v1/models", "secret", 401, nil, true},
		{"invalid", "/api/tags", `oops`, 200, nil, true},
		{"wrong_ollama", "/api/tags", `{"data":[]}`, 200, nil, true},
		{"wrong_compatible", "/v1/models", `{"models":[]}`, 200, nil, true},
		{"blank_name", "/api/tags", `{"models":[{"name":" "}]}`, 200, nil, true},
		{"oversize", "/api/tags", strings.Repeat("x", (1<<20)+1), 200, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := httpFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					t.Fatal("discovery made mutation")
				}
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			names, err := discover(context.Background(), client, probe{endpoint: "http://localhost", path: tt.path})
			if (err != nil) != tt.bad || !reflect.DeepEqual(names, tt.want) {
				t.Fatal(names, err)
			}
		})
	}
	if _, err := discover(context.Background(), nil, probe{endpoint: "%"}); err == nil {
		t.Fatal("invalid URL accepted")
	}
	client := httpFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(failingIO{})}, nil
	})
	if _, err := discover(context.Background(), client, probe{endpoint: "http://localhost"}); err == nil {
		t.Fatal("read error lost")
	}
}

func TestSetupExplicitTurnAllowance(t *testing.T) {
	t.Chdir(t.TempDir())
	args := append(append([]string{}, unattended...), "--max-turns", "200")
	if code := setupFixture(t).Run(context.Background(), args, nil, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	data, err := os.ReadFile(".ferretta/review.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePolicy(data)
	if err != nil || p.config.MaxTurns != 200 {
		t.Fatal("explicit allowance not retained", err)
	}
}
