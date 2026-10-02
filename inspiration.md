# Inspiration

Go agent runtimes, code-review systems, and design ideas worth revisiting as
Ferretta develops. Original reference notes reviewed on September 30, 2026;
the OpenCodeReview entry was added on October 2, 2026. The descriptions below summarize
upstream documentation and inspected sources; the proposed applications and tradeoffs are our own
assessment. This is a reference catalogue, not a dependency list or a commitment
to implement every feature. None of these projects is currently a Ferretta
dependency.

## Projects

### Harness — sausheong/harness

[Repository and README](https://github.com/sausheong/harness)

An embeddable runtime covering the agent loop, tool registry, session storage,
context compaction, token budgets, provider adapters and MCP integration. Its
explicit runtime design makes it a useful reference for understanding the loop
and its execution boundaries.

For Ferretta, study how compaction retains useful evidence and continuation data,
how sessions survive interruption, and how incomplete model responses remain
incomplete. We have already drawn conceptual inspiration for session retention
and handling truncated responses. General compaction and interrupted-model replay
are still future work here.

The tradeoff to examine is how much of its runtime we would need versus a small,
focused implementation within our existing core and executor.

### agent-harness-go — scitrera/agent-harness-go

[Repository and README](https://github.com/scitrera/agent-harness-go)

An embeddable tool-calling runtime with replaceable interfaces for transport,
memory, tool catalogs, approval, observation, session replay and subagent execution
authority.

For Ferretta, study where the runtime ends and the host's durable execution
authority begins. That distinction is relevant to GitHub notification adapters,
human waits and eventual worker coordination.

The tradeoff is interface breadth: adopt a boundary when we have a concrete need
for it. Its reference CLI and surrounding distributed services do not need to
become Ferretta's deployment model.

### go-agent — jgabor/go-agent

[Repository and README](https://github.com/jgabor/go-agent)

An embedded runtime organized around agents, runners, tools, sessions, events and
host-owned policy. Its documentation describes tool metadata, retry safety and
inspectable execution events. Consult its Features & Roadmap table when checking
which advertised capabilities are implemented.

For Ferretta, revisit how tool descriptions, schemas, execution constraints and
retry semantics stay together. Its policy and persistence boundaries informed
our proposal work conceptually. Execution events could later supply truthful PR
progress comments without spending model turns to narrate routine operations.

The tradeoff is preserving product decisions in Ferretta: a generic runtime must
not decide who can confirm intent or whether uncertain GitHub effects may retry.

### Gollem — fugue-labs/gollem

[Repository and README](https://github.com/fugue-labs/gollem)

A runtime emphasizing typed agents, typed tool parameters, schema generation,
structured outputs, validation and output repair. It also documents offline
model fixtures.

For Ferretta, study generating tool schemas from the same Go types used to
validate commands, reducing drift between model-visible schemas and accepted
arguments. Typed structured results are already an influence; automatic schema
generation and a general output-repair system are not implemented here.

The tradeoff is that structurally valid output can still be wrong. A model's
well-formed proposal does not establish human intent. Any repair model call must
also remain visible in provenance and resource accounting.

### Loom — teradata-labs/loom

[Repository and README](https://github.com/teradata-labs/loom)

A broader agent framework with workflow patterns, on-demand skills, context
management, evaluation, a server and a terminal UI. Its workflow vocabulary
includes pipelines, parallel execution, fork/join and iterative execution.

For Ferretta, use those patterns as comparison cases for future review waves:
parallel reviewers, synthesis, conditional repair and final review. On-demand
context loading is also worth comparing with our paged evidence tools.

The tradeoff is platform scope. We can learn from individual workflow semantics
while retaining a small local service and a DAG whose transitions, evidence and
stop conditions remain explicit.

### Galdor — YasserCR/galdor

[Repository and README](https://github.com/YasserCR/galdor)

A Go agent framework emphasizing OpenTelemetry, an embedded dashboard, replay,
evaluation, and MCP/A2A integration.

For Ferretta, study useful trace boundaries and turning recorded behavior into
repeatable evaluation fixtures. A useful evaluation would distinguish valid tool
calls, self-contained intent questions, grounded findings and justified LGTM.

The tradeoff is instrumentation and storage cost. Preserve the evidence needed to
diagnose a review while keeping private model continuation data out of public PR
comments and release artifacts. A dashboard is optional; inspectable state is
valuable on its own.

### OpenCodeReview — alibaba/open-code-review

[Repository](https://github.com/alibaba/open-code-review), inspected at
[`a758d9c`](https://github.com/alibaba/open-code-review/commit/a758d9cbfb689937c7857ad64b2dd66adb58c0c2)
on October 2, 2026; Apache-2.0. This is a Go code-review CLI, so it is a useful
product-level comparison as well as a harness reference.

**Local models:** its [configuration guide](https://github.com/alibaba/open-code-review/blob/a758d9cbfb689937c7857ad64b2dd66adb58c0c2/pages/src/content/docs/en/configuration.md)
explicitly documents Ollama through `http://127.0.0.1:11434/v1`, using its
OpenAI-compatible API, an explicit model ID, and a placeholder API key. The model
must return native structured tool calls. LiteLLM is also a
[provider preset](https://github.com/alibaba/open-code-review/blob/a758d9cbfb689937c7857ad64b2dd66adb58c0c2/internal/llm/providers.go).
OpenRouter is one supported provider; the ordinary CLI workflow runs locally
without a Cloudflare sandbox. We inspected documentation/code, not live inference;
this does not establish that our particular Qwen/GPT-OSS setup will work well.

**Review design:** deterministic file selection, related-file grouping, and
rule matching surround the tool-using agent. Separate positioning and comment
checking stages address source locations and false findings. The
[comment-checking prompt](https://github.com/alibaba/open-code-review/blob/a758d9cbfb689937c7857ad64b2dd66adb58c0c2/internal/config/template/prompts/review_filter_task_system.md)
limits removal to comments the available diff disproves, because that checker
has less context than the original reviewer. Study that evidence boundary; it
does not turn a second model's agreement into verified correctness or a PR grade.

**Evaluation references:** [AACR-Bench](https://github.com/alibaba/aacr-bench) and
its [paper](https://arxiv.org/abs/2601.19494) describe AI-assisted, expert-verified
annotations with repository context. OpenCodeReview reports
[benchmark results](https://github.com/alibaba/open-code-review/tree/a758d9cbfb689937c7857ad64b2dd66adb58c0c2#benchmark)
over 200 PRs from 50 repositories and ten languages, measuring precision, recall,
F1, time, and tokens. Treat those as upstream measurements, not Ferretta results.
The [reflection dataset](https://huggingface.co/datasets/Alibaba-Aone/aacr-bench)
contains 2,145 comments: 1,505 expert-verified correct and 640 incorrect. It tests
whether a checker can distinguish good findings from bad ones; it is not an
overall repair, intent-alignment, or allocation scorecard.

For Ferretta, a useful experiment would use held-out labeled findings to calibrate
the judge, measuring both false approval and false rejection. Separately compare
reviewers' precision and recall against reference findings at exact commits, with
resource use alongside the results. Retain categories: style suggestions should
not count as correctness defects merely because a dataset labels them valid.
Incomplete evidence remains unknown. These experiments fit our preference for
externalized outcomes and do not require parallel workflows in v1.

**Failure recovery:** the [client](https://github.com/alibaba/open-code-review/blob/a758d9cbfb689937c7857ad64b2dd66adb58c0c2/internal/llm/client.go)
configures up to five SDK retries; its [retry boundary](https://github.com/alibaba/open-code-review/blob/a758d9cbfb689937c7857ad64b2dd66adb58c0c2/internal/llm/retry_boundary.go)
classifies transport, timeout, decoding, and stream failures for recorded outcomes.
This is relevant to our stop-on-500 gap. Ferretta must still put retry admission
in core policy, retain uncertain consumption, and avoid repeating tool or GitHub
effects. SDK defaults alone are not our spending or idempotency policy.

The tradeoff is adopting useful parts without importing the whole review pipeline.
Our authenticated intent decisions, repair publication, allowance enforcement,
and exact-revision acceptance remain Ferretta responsibilities. No upstream code,
dependency, model invocation, or benchmark runner has been adopted here.

## Questions to revisit

| Ferretta question | Start with |
| --- | --- |
| How do we compact context without losing unresolved questions or source evidence? | Harness |
| How do tool schemas and Go argument types stay aligned? | Gollem, go-agent |
| How do human waits, replay and execution authority fit together? | agent-harness-go, Harness |
| Which events should explain progress and stopping to the human? | go-agent, Galdor |
| How should parallel review waves join and preserve disagreement? | Loom, agent-harness-go |
| How do we evaluate review quality separately from successful tool execution? | OpenCodeReview/AACR-Bench, Galdor, Gollem |
| How do we calibrate the judge against correct and incorrect findings? | AACR-Bench reflection dataset |
| How do we record and recover from provider failures without hiding retries? | OpenCodeReview, go-agent |

## Applying an idea

Start with an observed Ferretta problem and a small experiment. Record what
improved, what became more complicated, and whether the idea belongs in the core,
an adapter or the executor. Link the resulting decision in [arch.md](arch.md)
and distinguish implemented behavior from planned work.

Prefer concepts we can explain and test with our existing interfaces. Before
copying code or adding a dependency, inspect the relevant implementation, record
its upstream commit and license, and verify the dependency footprint and
CGO-disabled amd64/arm64 builds. A useful abstraction should support fast tests with fake clients and leave
policy with Ferretta's core.

Current [proposal sessions](docs/proposals.md) are one concrete application of
these ideas. The [development principles](README.md#development-principles)
remain the basis for deciding what fits.
