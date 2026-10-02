# Configuration decisions and implementation boundary

An agent configuration-authoring skill is deferred until the v1 configuration
language is specified and validated examples exist.

## Agreed target

The effective policy resolves field by field:

| Priority | Source | Authority |
| --- | --- | --- |
| 1 | Authorized per-PR settings | Scoped to a PR; spending/permission increases require an allowlisted human |
| 2 | Root `ferretta.toml` from a trusted repository revision | Portable repository policy |
| 3 | CLI user/service profile | Personal or installation defaults, optionally set through `init` |
| 4 | Built-in system defaults | Usable defaults, with `cost` as the primary objective |

Absent fields inherit; explicit values replace. Lists replace as a unit. Invalid
configuration fails visibly instead of falling through to a more permissive
layer. Show the effective settings with their sources, and bind that result to
each review. A policy change submitted in a PR is evidence under review, not
authority to grant that PR more money or tool access.

No root file is required for default policy. Connections, repository watch lists,
credential references and state/workspace paths belong to operator configuration,
not the PR override plane. Explicit CLI objective overrides affect that invocation
within existing permissions. A background service uses its configured profile,
not the defaults of whoever happens to log in to the machine.

## Objectives

Draft [TOML presets](presets/README.md) give `cost`, `time`, and `quality` a
shared proposed structure, with separate routing and planning preferences.
They are design examples, not configuration accepted by the current CLI.
The separate [workflow syntax comparison](workflow-syntax.md) records both
dependency stages and ordered waves without selecting either format.

The [scorecard prototype](evaluation.md) accepts separate reviewer and judge JSON
policy files with independent allowances. Its role rubric is versioned in code.
The [broader plan](review-evaluation.md) includes future configurable rubrics and
publication settings; it does not settle the TOML schema.

| Preset | Primary optimization | Unchanged requirements |
| --- | --- | --- |
| `cost` (default) | Minimize additional spending | Capabilities, authorized routes, hard limits and acceptance gates |
| `time` | Minimize time to an acceptable result | Same |
| `quality` | Seek stronger review evidence within allowances | Same |

The eventual UI makes switching easy, for example `ferretta config set objective
quality` or `ferretta review --objective time`. **These commands/flags are not
implemented yet.** Startup should identify the effective objective, its source,
and how to change it. Plans follow the objective and resources; advanced policies
may specify model waves explicitly. A preset cannot silently opt into a paid
provider or weaken the definition of LGTM.

## Sharing configuration: public import, local ownership

**Agreed design; not implemented.** Import is a CLI authoring operation that
produces a self-contained, editable local `ferretta.toml`. Normal configuration
loading reads local files and does not fetch upstream policy. We are not adopting
runtime URL inheritance through an `extends` field.

Proposed command:

```sh
ferretta config import https://github.com/owner/repo/blob/main/ferretta.toml
```

Accept public GitHub file links and public raw HTTPS file URLs. The import adapter
retrieves content anonymously; it does not use the GitHub App, personal tokens,
browser sessions, or `gh` credentials. Private-repository URL imports are
intentionally unsupported. Users with private-source access can manually copy
the complete configuration into their project using their own tools. No import
token prompt, OAuth flow, or GitHub CLI dependency is needed. Failure to retrieve
a public file must not trigger an authentication fallback or assume that a 404
proves a repository is private.

The importer validates the schema and materializes configuration into one local
file, preserving useful tables and arrays. Any supported inheritance resolution
belongs at import time; its syntax remains undecided. Record the public source
URL, exact GitHub commit when applicable, source content hash, and import time as
provenance comments. Subsequent edits belong to the local project; provenance
describes the imported source, not the integrity of later local changes.

Preview the result before writing. Preserve an existing file unless the user
explicitly chooses replacement, showing the differences first. Upstream changes
never silently update an imported policy. Fetching or validating a policy does
not execute its checks, start inference, establish model connections, or grant
spending/write permissions. Report unresolved local setup requirements.

This public-only decision supersedes the earlier private-import/token-prompt
proposal. Service reviews, comments, and repairs continue to use the dedicated
GitHub App identity under their existing authorization rules.

## Implemented surfaces

- GitHub App connection: strict machine JSON in `ferretta/github.json` beneath
  `os.UserConfigDir()`, or the absolute path in `FERRETTA_GITHUB_CONFIG`. Contains
  client/installation IDs and an absolute private-key path, never the key itself.
- Onboarding: `init` discovers metadata and creates a new Ollama policy; `doctor`
  checks readiness without inference/check execution; `demo` shows fictional
  reports. `model test` explicitly runs two bounded inference turns.
- Manual review: explicit trusted `.ferretta/review.json`, selected with
  `review --policy`. The current adapter is Ollama only.
- Evaluation: `eval --run PATH --policy PATH`, or `review --eval-policy PATH`.
  The judge accepts Ollama route/limit fields but rejects executable checks.
- Scoped dispatch: `service watch --repo owner/repo --pr N --state PATH
  --review-policy PATH --judge-policy PATH --humans login`. See the
  [operating guide](evaluation.md) for allowances and recovery. Replace `--pr N`
  with `--all-prs --authors login[,login]` for continuous discovery of ready PRs
  from allowed authors on same-repository branches.
- Watch publication: `--publication checks` (default) or `comments`, pinned for
  the PR workflow. Checks mode retains intent proposal comments and reports
  routine progress/verdict/scorecard through the App's Checks API. Legacy jobs
  require `--publication comments`; changing mode needs operator reconciliation.
- Service intake: `service run --repo owner/repo` (repeatable), `--state` (absolute
  private directory), `--interval` (default `1m`, range `5s`–`24h`), and `--once`.
  Default state is `ferretta/state` beneath `os.UserConfigDir()`; boot service
  definitions always specify a service-owned absolute path.
- Inspection: `service status --state /absolute/path` reads existing SQLite state
  without credentials, network calls, inference, or starting another owner.

No TOML loader, layered resolver, objective selector, persistent watch-list editor,
subscription auth or encrypted secret database is shipped in this slice. These
decisions establish the contract; the complete TOML schema and JSON migration
will follow implementation of those policy surfaces. Existing JSON remains
explicit and is never silently overwritten or interpreted as TOML.

## Current Ollama JSON policy

`init` writes the selected model/endpoint and a required check after confirmation.
It does not yet save layered user defaults or TOML. Existing files are preserved.

| Field | Meaning |
| --- | --- |
| `provider`, `endpoint`, `model` | Explicit `ollama` route at an HTTP loopback endpoint and installed model ID |
| `thinking` or `effort` | Exactly one: `thinking: "enabled"` for a verified boolean control, `thinking: "provider_default"` to explicitly accept the model's default, or `effort: "low"`, `"medium"`, `"high"` for verified named controls |
| `context_tokens` | At least 8,192, bounded by provider-reported model capacity; setup defaults to 16,384; omitted legacy field remains 65,536 |
| `max_turns` | Nonnegative; 0 disables the turn ceiling; setup uses 100; counts model responses across resumed segments |
| `max_tokens_per_turn` | At least 256; setup uses 4,096; must leave at least 2,048 tokens of input room in context |
| `timeout_seconds` | Nonnegative seconds; 0 disables the deadline; setup uses 600; proposal sessions exclude human waiting |
| `checks` | Nonempty list of command argument arrays; trusted operator-selected commands |

Unknown fields, contradictory thinking controls and invalid limits are rejected.
Metadata must establish tools, thinking capability and sufficient context capacity.
Explicitly enabled thinking and named effort also require support for the requested
control. Older Ollama versions lacking control metadata have explicit compatibility
for GPT-OSS named effort and Qwen3 boolean thinking; other unknown controls are
rejected. Remote-model metadata is rejected by the local-only route.
Requested settings remain separate from provider-reported execution details.

### Models with missing thinking-control metadata

Some installed models advertise tools and thinking but omit the separate
`thinking.values` controls. Alpaca's `qwen3.8:27b-mlx` is one such model. Select
`thinking: "provider_default"` to explicitly accept the provider's default instead
of requesting an unverified control. Ferretta sends `think: null`, following
[Ollama's API semantics](https://docs.ollama.com/capabilities/thinking).
Whether thinking is enabled, and its effective effort, remain unknown. This is
different from `thinking: "enabled"`; a model default may change when the provider
or model changes. Policies that require enabled thinking should keep that setting
and require suitable metadata or an established adapter compatibility rule.

The default option does not supply missing tool/thinking capabilities, invent a
context limit, permit a remote route, or override explicit non-thinking control
metadata such as `values: [false]`. `init --thinking auto` never silently selects
it. An explicit choice is required; review and judge policies both support it.

For an already installed Qwen model, create a separate policy:

```sh
bin/ferretta init --yes --model qwen3.8:27b-mlx \
  --thinking provider_default --context 131072 \
  --max-turns 0 --max-output-tokens 16384 --timeout 600 \
  --check '["make","check"]' --policy .ferretta/qwen27-review.json
bin/ferretta doctor --repo owner/repo --policy .ferretta/qwen27-review.json
bin/ferretta model test --policy .ferretta/qwen27-review.json
```

The model name is a local installation choice, not a portable download promise.
Setup preserves existing files and performs metadata checks only. The last command
explicitly runs two inference turns with inert tool results. Success establishes
tool continuation, not reasoning effort, review quality, or memory requirements
for every workload. This example has unlimited turns within a ten-minute active
deadline; context and output allowances remain enforced. Model weights and context
both consume memory, so select context capacity for the intended workload.

### Context accounting

The context admission check uses Ollama-reported prompt tokens for the unchanged
message prefix when available, plus encoded bytes for newly appended messages,
a framing reserve and output allowance. Before a usage observation, the complete
input is conservatively bounded by bytes. It is not an exact local tokenizer.


For local exploration, this repository's policy selects `gpt-oss:20b`, medium
requested effort, 131,072 context tokens, 16,384 output tokens per reply, and no
turn/deadline ceiling. These are explicit repository choices; `init` still ships
bounded defaults. Cancellation remains available. Context capacity, required
checks, human identity and valid tool arguments remain enforced.

Setup exposes `--max-turns`, `--max-output-tokens`, `--timeout`, and `--context`.
For example, `--max-turns 0 --timeout 0 --context 131072
--max-output-tokens 16384` creates a more permissive local policy. Existing
policies are never overwritten. Provider prompt counts anchor context admission; unseen additions retain a
conservative byte bound. Paged evidence reduces input growth;
automatic compaction is not implemented.

Proposal posting is an operator-selected effect: `review --publish-proposals
--humans login1,login2`. The allowlist is pinned in the private SQLite session.
`review --resume /absolute/session/path` polls for human replies using the same
repository, PR, policy and GitHub App. See [proposal sessions](proposals.md).

## Challenger amendment: constrained model exploration

> Proposed design. Challenger exploration and its configuration are not implemented. This section describes a proposed addition to the harness specification; it is not a confirmed intent record or authorization to merge. “Amendment” here refers to the specification, not a new intent-protocol marker or amendment semantics for confirmed decisions.

Ferretta should learn which models perform well on a particular repository while respecting the repository's budget, quality, and execution constraints. Broad benchmarks can inform candidate selection, but repository-specific evidence should determine whether a challenger should replace an incumbent.

An **incumbent** is the current configured or evidence-supported model for a review role. A **challenger** is an eligible alternative evaluated independently on the same work. Exploration delegates challenger selection to Ferretta without requiring the operator to name every model in advance.

The first scope is review of existing PR revisions, including correctness and intent alignment. This proposal does not define competitive code generation or allow experimental models to change repository contents.

### Configuration intent

Exploration is opt-in. The operator controls:

- The review roles that may participate.
- The probability that an eligible PR enters a comparison, for example `0.10` for 10% on average.
- The candidate eligibility constraints, including permitted providers, data handling requirements, required capabilities, and supported context size.
- Maximum execution cost and latency, and a separate exploration spending cap over a configured accounting period.
- How candidates are selected within that envelope.
- How comparison evidence is evaluated and whether promotion is manual or automatic under an explicit policy.

The following sketch illustrates the configuration's meaning, not a finalized schema or runnable configuration:

```yaml
exploration:
  enabled: true
  roles: [correctness, intent_alignment]
  sampling:
    unit: pull_request
    probability: 0.10
  challenger:
    selection: discover_within_constraints
    count: 1
    constraints: inherit_review_constraints
  budget:
    per_comparison: <maximum incremental cost>
    per_period: <maximum total exploration cost>
    period: <accounting period>
  execution:
    mode: shadow
  promotion:
    mode: manual
```

Exploration constraints may narrow the normal review constraints; they must not broaden them. Unspecified model identity does not mean unrestricted provider or model access. If the constraints cannot be verified, the model is ineligible. If no candidate qualifies, skip exploration and record the reason.

The actual configuration format, units, inheritance syntax, and validation errors remain to be specified. An enabled policy must resolve to explicit limits before it can schedule work; placeholder or missing limits do not imply unlimited spending.

### Sampling and admission

Sampling applies after ordinary PR readiness and exploration eligibility checks. A probability of 0.10 means approximately 10% of eligible PRs are selected over time, not exactly one in each group of ten. Budget and capacity limits may reduce the proportion that actually runs.

The selection is stable for a repository and PR under a recorded sampling policy version. Repeated webhook delivery, reruns, and new commits must not generate fresh chances to enter exploration. Time, randomness, and any sampling seed enter through explicit boundary inputs; the core makes a reproducible decision and records its basis.

Initially, admit at most one comparison per PR per sampling policy version. Pin its revision when scheduling it. A later commit does not silently start another comparison, and evidence from the pinned revision cannot authorize review of the new revision. Any future repeated-comparison policy must explicitly define its additional budget and evidence weighting.

Record eligible, selected, admitted, completed, and skipped counts separately, including skip reasons. Otherwise a budget-limited sample can appear representative when it disproportionately excludes large or expensive PRs. Changing policy versions must not silently reset spending limits or authorize duplicate in-flight work.

### Candidate discovery and selection

Discovery supplies a versioned inventory of model identities and known capabilities. It does not grant eligibility. Core policy filters that inventory against the repository's constraints before choosing a challenger distinct from the incumbent.

The initial selection strategy should be simple and inspectable: choose among eligible alternatives using explicit random input and record the candidate set and selection basis. More adaptive allocation may follow once reliable outcome evidence exists; no particular bandit algorithm is required by this proposal.

Resolve and retain the provider, model identity, and version where available. If a provider exposes only a mutable alias, record that limitation and the observation time. Results must not imply that an alias permanently identifies the same model behavior.

### Comparable execution

Both reviewers receive the same repository revision, base revision, intent version, review role, and equivalent context and tool permissions. Record prompts, harness configuration, available context, execution limits, and relevant adapter differences so comparisons can be interpreted. Each model receives an independent workspace and does not see the other's findings before producing its own.

An incumbent's normal review can serve as its side of the comparison only when those inputs and conditions match. Otherwise skip the comparison or budget explicitly for another incumbent run. Do not charge a repeated incumbent run to ordinary review merely to hide exploration costs.

Challengers initially run in shadow mode. Their output is retained as evaluation evidence and does not automatically satisfy required review coverage, block a PR, or authorize a merge. A potentially important challenger finding can enter the ordinary verification workflow; any resulting review decision must rest on verified evidence under the existing review policy.

Review failure, timeout, incomplete coverage, and uncertain execution outcomes are distinct from a clean review. Preserve these outcomes in comparisons rather than dropping unsuccessful runs from the reported sample.

### Budget and failure policy

A 10% PR sampling rate is not a 10% spending limit. Exploration has both a per-comparison limit and an aggregate limit over an explicit accounting period, within the repository's overall budget.

Before scheduling operations, reserve budget for incremental model runs and their verification, including any extra incumbent execution. Account for concurrent reservations so parallel comparisons cannot each consume the same remaining budget. The execution plan must enforce bounded usage; an average cost estimate alone is insufficient to promise a hard cap. Skip work whose maximum authorized exposure cannot fit the remaining limits.

Reconcile actual usage against reservations. Retain unresolved reservations when provider outcomes or charges are uncertain. A timeout does not establish that an operation failed or was free. Reconcile before retrying where possible; otherwise surface uncertainty and do not blindly repeat potentially chargeable effects.

Budget exhaustion stops additional exploration without weakening ordinary review requirements. Record actual model charges and other measurable verification costs separately, including the pricing basis and accounting uncertainty. Human review time is a separate measure unless an explicit conversion policy exists.

### Evaluation: useful outcomes on this repository

Dollars per token remains useful accounting data, but is not the primary selection objective. Models can consume different numbers of tokens, require different retries, and impose different verification effort to produce a useful result.

Retain paired results and report at least:

| Measure | Interpretation |
| --- | --- |
| Cost per completed review | Execution cost including retries and required verification; report failed-run spending alongside it |
| Cost per verified actionable finding | Spend relative to unique, verified useful discoveries; report zero-finding cases without inventing a finite ratio |
| False-positive rate | Findings adjudicated incorrect or non-actionable, with unresolved findings reported separately |
| Missed verified findings | Verified findings discovered by one reviewer and missed by the other; this is not a measure of all defects present |
| Completion rate and latency | Reliability and time to a completed review under the configured limits |
| Human adjudication effort | Human attention required to establish whether results are useful |

Deduplicate equivalent findings before counting them. Preserve severity and correctness versus intent-alignment distinctions. More findings does not automatically mean better review, and two reviewers reporting no findings does not establish that a PR is clean.

Tests, reproductions, and authenticated human adjudication provide evidence for findings. Model judgments may assist verification but must not become ground truth merely because a model declares a winner. Record the evidence, verifier, verification method, and unresolved disagreements. Agent interpretation never becomes human-confirmed intent through a comparison or promotion decision.

Report sample sizes, ties, unresolved outcomes, and the evaluation period alongside results. Comparisons describe observed performance on the admitted sample; do not present them as universal model rankings or complete defect-recall estimates. Retain enough sampling and admission information to identify selection bias.

### Incumbents and promotion

Maintain an incumbent per repository and review role initially. More granular choices, such as language or task category, require enough evidence to support those distinctions. Until then, use the explicitly configured incumbent and report insufficient evidence instead of claiming a repository winner.

Manual promotion is the initial mode. Automatic promotion requires a separately explicit policy defining minimum comparable evidence, quality floors, acceptable regressions, treatment of uncertainty, and the cost/latency tradeoff. Cheap execution cannot compensate for violation of a hard quality or eligibility constraint. Ties and inconclusive comparisons preserve the incumbent.

A promotion affects future scheduling and records the evidence and policy that justified it. It does not retroactively change completed reviews or authorize merges. Keep incumbent history so an operator can restore an earlier choice.

Model, prompt, harness, context, or tool changes can invalidate a comparison's applicability. Record these versions and avoid silently pooling materially different conditions. Evidence freshness, reevaluation triggers, and any automatic promotion thresholds remain open design decisions; historical wins do not establish permanent superiority.

### Architecture and observable invariants

The pure core owns eligibility, sampling, selection, budget reservations, retry decisions, and promotion policy. It consumes explicit inventory, time, random input, usage reports, and model outcomes, then composes command objects. Thin executors perform discovery, model calls, verification operations, and evidence persistence and report their results.

The feature requires durable comparison and budget state before it can make accumulated-performance claims or safely coordinate concurrent spending. The current CLI's comment snapshot is not that state and must not be presented as a historical evaluation log.

Tests should use fake providers, injected clocks and randomness, and controlled outcomes to verify that:

- Identical state and inputs produce identical sampling and scheduling decisions.
- Duplicate events cannot schedule duplicate comparisons or spend a reservation twice.
- Ineligible candidates never receive repository data.
- Exploration stays within its limits, including concurrent admissions and uncertain charges.
- Compared results retain their exact revision, intent, model, and harness provenance.
- Failures and unresolved findings remain visible rather than counting as clean reviews or wins.
- Shadow results cannot bypass normal review or human-confirmation requirements.
- Promotion occurs only under its configured evidence and eligibility rules.

Adapter validation belongs in a separate networked suite. Normal tests must remain fast, deterministic, and offline.
