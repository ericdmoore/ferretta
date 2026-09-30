package review

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// Probe tests two real model turns against inert fixture tool results. No tool
// command is executed. It establishes protocol compatibility, not review quality.
func Probe(ctx context.Context, model Model, p Policy) ([]Reply, error) {
	if model == nil || p.config.Provider != "ollama" {
		return nil, fmt.Errorf("validated local policy and model are required")
	}
	if err := model.Capabilities(ctx, p); err != nil {
		return nil, err
	}
	messages := []Message{{Role: "system", Content: "This is a tool-protocol test. Call exactly the requested tool. Do not answer in prose or call any other tools."}, {Role: "user", Content: "Call list_files with empty arguments now."}}
	var replies []Reply
	for step := 0; step < 2; step++ {
		reply, err := model.Turn(ctx, p, messages)
		if err != nil {
			return replies, err
		}
		replies = append(replies, reply)
		if len(reply.Message.ToolCalls) != 1 {
			return replies, fmt.Errorf("probe needs exactly one requested tool call per turn")
		}
		call := reply.Message.ToolCalls[0]
		cmd, err := Plan(call.Function.Name, call.Function.Arguments)
		if err != nil {
			return replies, err
		}
		if step == 0 {
			if _, ok := cmd.(ListFiles); !ok {
				return replies, fmt.Errorf("probe expected list_files")
			}
			messages = append(messages, reply.Message, Message{Role: "tool", ToolName: "list_files", Content: "probe.txt"}, Message{Role: "user", Content: "Now call read_file for probe.txt, using the previous tool result."})
		} else if read, ok := cmd.(ReadFile); !ok || read.path != "probe.txt" {
			return replies, fmt.Errorf("probe expected read_file for probe.txt after tool continuation")
		}
	}
	return replies, nil
}

func (c CLI) Probe(ctx context.Context, args []string, output, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	flags := flag.NewFlagSet("model test", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("policy", ".ferretta/review.json", "trusted policy; makes two bounded inference calls")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 {
		return fail(fmt.Errorf("model test accepts --policy PATH only"))
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		return fail(err)
	}
	p, err := ParsePolicy(data)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, min(time.Duration(p.config.TimeoutSeconds)*time.Second, 2*time.Minute))
	defer cancel()
	replies, err := Probe(ctx, c.Runner.Model, p)
	if err != nil {
		return fail(fmt.Errorf("model protocol test incomplete (no automatic retry): %w", err))
	}
	models := []string{replies[0].Model, replies[1].Model}
	if err := json.NewEncoder(output).Encode(struct {
		Status string   `json:"status"`
		Models []string `json:"observed_models"`
		Turns  int      `json:"turns"`
	}{"tool continuation passed; review quality not evaluated", models, 2}); err != nil {
		return fail(err)
	}
	return 0
}
