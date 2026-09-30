# First review

Start with a small, trusted PR. The current reviewer is advisory: it reads a
submitted PR, runs your configured checks locally, and saves a report. It does
not post comments or change the PR.

## See the result first

Build with Go 1.26.5 and Make, or use a matching release binary when available:

```sh
make build
bin/ferretta demo
bin/ferretta demo --outcome question
bin/ferretta demo --outcome lgtm
```

These are labeled fictional examples, including fictional commits and evidence.
They need no account or model and make no network calls.

## Connect a local model

Install and start [Ollama](https://ollama.com/download), then explicitly download
a model that supports tools and thinking:

```sh
ollama pull qwen3:4b-thinking
bin/ferretta init --discover
```

Discovery reads model inventories from common Ollama, LiteLLM, vLLM, LM Studio,
and llama.cpp endpoints. A discovered endpoint is only a candidate: proxy routing
and capabilities can be unknown. Only the Ollama adapter is implemented. Discovery
never starts a service, downloads a model, generates responses, or runs checks.

From the trusted repository checkout, run:

```sh
bin/ferretta init
```

Choose an installed model and a check command. Setup suggests `make check` when
that target exists, or `go test ./...` for a Go module. Inspect the proposal before
saving: later reviews execute that command against PR code as your local OS user.
A detached worktree is not a security sandbox.

Setup creates `.ferretta/review.json` and refuses to overwrite existing files.
This repository already has a policy; to try a different model, use another path:

```sh
bin/ferretta init --model qwen3:4b-thinking --policy .ferretta/local-review.json
```

Use the same `--policy` path with subsequent commands. `--yes` supports unattended
setup but requires explicit `--model` and `--check '["make","check"]` arguments.
New policies request a 16,384-token context, ten turns, at most 4,096 generated
tokens per turn, and a ten-minute attempt. Larger contexts use more memory;
these defaults are not a hardware sizing guarantee. Large PRs or conversations
may exceed the conservative context admission bound and produce an incomplete
review. No evidence is silently truncated to fit.

Optionally test the selected model's tool protocol before connecting GitHub:

```sh
bin/ferretta model test --policy .ferretta/local-review.json
```

This command **runs inference**: two turns, using the policy's output allowance
and at most two minutes total (or the policy timeout, if shorter). Tool results
are inert fixtures; no repository checks or model-requested commands execute.
It stops on failure without automatic retry. A successful result establishes
basic tool continuation, not review quality or a particular effective reasoning
effort. On alpaca, Qwen `qwen3:4b-thinking` passed this test with thinking enabled,
a 16K context and 4,096 output tokens per turn. A lower 2,048-token probe allowance
produced an incomplete response; allow bounded failure rather than assuming every
small model reliably finishes every task.

## Connect GitHub and verify readiness

```sh
bin/ferretta auth github --setup
```

Follow the printed [GitHub App registration steps](github-app-auth.md). Once you
have an installed App and its private-key file, verify and save its connection:

```sh
bin/ferretta auth github --configure --repo owner/repo \
  --client-id APP_CLIENT_ID --installation-id NUMBER \
  --private-key-file /absolute/private/path/app.pem
bin/ferretta doctor --repo owner/repo --policy .ferretta/local-review.json
```

Connection setup verifies read access before writing a new machine configuration.
It stores the key path, not the key or temporary token, and preserves existing
configuration. `doctor` checks Git, matching checkout origin, App access, policy,
and model capability metadata. It lists required checks without executing them
and never runs inference. Exit 2 means something is not ready; the output gives
the next step. No OpenRouter key is needed: hosted inference and paid/free routing
are not implemented yet.

## Review an actual PR

Replace `owner/repo` and `123` with an open, non-draft PR in your trusted checkout:

```sh
bin/ferretta review --repo owner/repo --pr 123 --policy .ferretta/local-review.json
```

The default output is a readable report with exact commits, findings, questions,
tradeoffs, model provenance, available token usage and next actions. Add
`--format json` for scripts. Exit 0 means advisory LGTM with configured checks
passing; exit 2 means changes, a question, or incomplete work; exit 1 means a
setup/output failure. A report is not permission to merge.

Private checkpoints and a JSON report remain in `.ferretta/runs/`. Do not publish
session files: they contain model messages and repository content. Each invocation
starts a new attempt; resumption and confirmed comment intent are not integrated.

After the first useful review, consult [service installation](service.md) for
macOS/Linux boot supervision. The current service durably collects PR revisions
only; it does not dispatch these reviews automatically.
