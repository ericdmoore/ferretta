# Inspiration

Go agent runtimes, code-review systems, and design ideas worth revisiting as
Ferretta develops. Original reference notes reviewed on September 30, 2026;
the code-review comparisons were added on October 2, 2026. The descriptions below
summarize upstream documentation and inspected sources; the proposed applications
and tradeoffs are our own assessment. This is a reference catalogue, not a dependency list or a commitment
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

### Gito — Nayjest/Gito

[Repository](https://github.com/Nayjest/Gito), inspected at
[`f48498d`](https://github.com/Nayjest/Gito/commit/f48498d192ede9aa81808c2579c69cc5d7919e20)
on October 2, 2026; Python, MIT.

**Local models:** the [README](https://github.com/Nayjest/Gito/blob/f48498d192ede9aa81808c2579c69cc5d7919e20/README.md)
explicitly supports Ollama, vLLM, llama.cpp, SGLang and LM Studio through its
ai-microcore integration. `LLM_API_BASE` selects an endpoint. The CLI and CI
workflows do not require OpenRouter or a Cloudflare execution environment.

**Configuration and review design:** machine-level connection settings live
separately from shareable `.gito/config.toml` review behavior. The
[cookbook](https://github.com/Nayjest/Gito/blob/f48498d192ede9aa81808c2579c69cc5d7919e20/documentation/config_cookbook.md)
explains overrides and copying defaults, including the consequence that a full
copy pins settings and stops receiving improved defaults. Its
[review implementation](https://github.com/Nayjest/Gito/blob/f48498d192ede9aa81808c2579c69cc5d7919e20/gito/core.py)
validates structured findings and exposes skipped files or malformed responses
as warnings. That distinction matters when an otherwise empty report could look
like a successful review.

Its [default policy](https://github.com/Nayjest/Gito/blob/f48498d192ede9aa81808c2579c69cc5d7919e20/gito/config.toml)
filters findings using model-reported confidence and severity. Asking for high
confidence, or asking a summary prompt for a numeric grade, does not calibrate
those scores. No labeled review-quality benchmark was identified in the
documentation and source tree inspected for this comparison.

For Ferretta, study config ergonomics and explicit coverage gaps. Keep imported
policy declarative: Gito's executable Python post-processing is a different trust
boundary from our proposed TOML. Endpoint compatibility also does not establish
tool-use, reasoning, or review-quality requirements.

### Kodus — kodustech/kodus-ai

[Repository](https://github.com/kodustech/kodus-ai), inspected at
[`14439a9`](https://github.com/kodustech/kodus-ai/commit/14439a95335f07c92cb445ccea3069a9b2904e30)
on October 2, 2026; TypeScript. Its
[license](https://github.com/kodustech/kodus-ai/blob/14439a95335f07c92cb445ccea3069a9b2904e30/license.md)
is AGPL-3.0 except explicitly marked enterprise code, which has commercial terms.
Record the relevant file's license before considering code reuse.

**Local models and hosting:** [BYOK configuration](https://github.com/kodustech/kodus-ai/blob/14439a95335f07c92cb445ccea3069a9b2904e30/docs/en/how_to_use/byok.mdx)
accepts a custom OpenAI-compatible base URL and model ID, including self-hosted
endpoints. The self-hosted application is a larger service stack than Ferretta.
Its [sandbox configuration](https://github.com/kodustech/kodus-ai/blob/14439a95335f07c92cb445ccea3069a9b2904e30/docs/en/how_to_deploy/deploy_kodus/sandbox.mdx)
offers local execution inside the worker container; remote E2B is optional.
Neither OpenRouter nor Cloudflare is a required inference route.

**Evaluation design:** the [eval suite](https://github.com/kodustech/kodus-ai/blob/14439a95335f07c92cb445ccea3069a9b2904e30/evals/README.md)
separates offline integration checks using a scripted model from live quality
measurements. It evaluates finding recall, source anchoring, duplicate removal,
severity, formatting and verifier behavior separately. Infrastructure failures
are not treated as quality measurements, and regression floors are tied to the
judge used to calibrate them.

The [standalone scorer](https://github.com/kodustech/kodus-ai/blob/14439a95335f07c92cb445ccea3069a9b2904e30/evals/scorer/README.md)
re-scores saved findings without repeating the review. Submissions retain harness
version, model, requested reasoning settings, execution mode, tokens and latency.
Replay and live runs are distinguished. The
[judge-agreement study](https://github.com/kodustech/kodus-ai/blob/14439a95335f07c92cb445ccea3069a9b2904e30/evals/investigation/agreement/README.md)
compares candidate judges with an incumbent model. Agreement with that model is
not independent evidence of correctness; human-labeled cases remain necessary
for Ferretta's calibration. Matching known findings also cannot establish that
every unmatched finding is false.

For Ferretta, this is a useful reference for repeatable scorecards, evaluation
provenance and quality regression tests. Preserve separate worker and oversight
rubrics: finding-level precision/recall does not grade intent alignment, repair
correctness or resource allocation by itself. Borrow evaluation concepts without
requiring the surrounding platform or changing our serial v1 scope.

### CodeCanary — alansikora/codecanary

[Website](https://codecanary.sh/) and [repository](https://github.com/alansikora/codecanary),
inspected at
[`15a2b9b`](https://github.com/alansikora/codecanary/commit/15a2b9b64475701dbf7cc9e89fe712d77fa893d3)
on October 2, 2026; Go, MIT.

**Local models:** its [configuration reference](https://github.com/alansikora/codecanary/blob/15a2b9b64475701dbf7cc9e89fe712d77fa893d3/docs/configuration.md)
documents `provider: openai` with an `api_base` override for Ollama and other
compatible endpoints. Review and triage models are configurable separately. The
[adapter](https://github.com/alansikora/codecanary/blob/15a2b9b64475701dbf7cc9e89fe712d77fa893d3/internal/review/provider_compat.go)
still requires a nonempty configured key, even when the server ignores it.

Local inference and hosted integration are separate: the stock
[GitHub Action](https://github.com/alansikora/codecanary/blob/15a2b9b64475701dbf7cc9e89fe712d77fa893d3/action.yml)
exchanges GitHub OIDC credentials at `oidc.codecanary.sh` for an App token. The
repository includes that broker's Cloudflare Worker. This is an authentication
service, not a requirement to execute model inference in a Cloudflare sandbox.

**Review lifecycle:** its [review flow](https://github.com/alansikora/codecanary/blob/15a2b9b64475701dbf7cc9e89fe712d77fa893d3/docs/review-flow.md)
uses deterministic rules to decide which existing threads need model
re-evaluation. Changes since the previous review determine eligibility; the full
PR diff supplies context. Unchanged threads can avoid another model call, and
acknowledgments prevent repeated replies. Findings are checked against changed
files and nearby diff lines. These checks constrain location and scope; they do
not establish that the claimed bug is real. No labeled quality benchmark was
identified in the inspected documentation and source tree.

For Ferretta, study incremental review and carrying findings across commits.
Reuse must retain revision/policy provenance and explicitly invalidate stale
evidence. CodeCanary's reply interpretation must not substitute for our
authenticated `CORRECTED`/`CONFIRMED` decisions. Its small Go provider/platform
interfaces are also worth comparing with our executor boundaries.

### Harrier — saifullahsaeed/pr-review-action

[Repository](https://github.com/saifullahsaeed/pr-review-action), now presented as
Harrier, inspected at
[`c46353e`](https://github.com/saifullahsaeed/pr-review-action/commit/c46353eefaa150ce082f07134837dc3b175b20fb)
on October 2, 2026; TypeScript. The inspected
[LICENSE](https://github.com/saifullahsaeed/pr-review-action/blob/c46353eefaa150ce082f07134837dc3b175b20fb/LICENSE)
is MIT, despite the repository's older Apache-2.0 description.

**Local models:** the [README](https://github.com/saifullahsaeed/pr-review-action/blob/c46353eefaa150ce082f07134837dc3b175b20fb/README.md)
provides an Ollama `--endpoint http://localhost:11434/v1` example. The
[client](https://github.com/saifullahsaeed/pr-review-action/blob/c46353eefaa150ce082f07134837dc3b175b20fb/src/llm/client.ts)
uses configurable OpenAI-compatible chat completions, supporting local serving
or remote providers such as OpenRouter. It runs as a CLI, container or GitHub
Action; private endpoints require a runner with network access to them.

**Review and gate design:** deterministic scanners feed a structured report,
alongside bounded LLM review passes and an
[adversarial verifier](https://github.com/saifullahsaeed/pr-review-action/blob/c46353eefaa150ce082f07134837dc3b175b20fb/src/llm/verifier.ts).
The verifier records reasons for dropping findings; if its call fails, it retains
the original findings and records failure. This is a second assessment, not a
calibrated grade. The inspected tests include fixtures and planted defects, but
do not establish measured accuracy of a live reviewer/judge.

The [quality gate](https://github.com/saifullahsaeed/pr-review-action/blob/c46353eefaa150ce082f07134837dc3b175b20fb/src/gate.ts)
distinguishes pass, fail and incomplete. It compares deterministic findings with
a base revision to separate existing debt from new or worsened findings; missing
required probes or a failed baseline remain incomplete. AI findings are advisory
to that gate. The Action takes gate policy from the trusted base revision.

For Ferretta, study explicit coverage and baseline states, and reporting partial
work honestly. Its fixed LLM passes are not a replacement for our resumable
tool-using harness. Before adding a verifier, measure both false findings removed
and real findings incorrectly suppressed on held-out labeled cases.

## Questions to revisit

| Ferretta question | Start with |
| --- | --- |
| How do we compact context without losing unresolved questions or source evidence? | Harness |
| How do tool schemas and Go argument types stay aligned? | Gollem, go-agent |
| How do human waits, replay and execution authority fit together? | agent-harness-go, Harness |
| Which events should explain progress and stopping to the human? | go-agent, Galdor |
| How should parallel review waves join and preserve disagreement? | Loom, agent-harness-go |
| How do we evaluate review quality separately from successful tool execution? | OpenCodeReview/AACR-Bench, Kodus evals, Galdor, Gollem |
| How do we calibrate the judge against correct and incorrect findings? | AACR-Bench reflection dataset; Kodus judge-agreement methods, with human labels |
| How do we re-score saved outputs without rerunning the reviewer? | Kodus standalone scorer |
| How do we make layered config understandable and shareable? | Gito configuration cookbook |
| Which findings and replies need re-evaluation after a new commit? | CodeCanary review flow |
| How do we distinguish existing debt, new defects and incomplete checking? | Harrier quality gate |
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
