# Configuration specification

> Proposed design. Configuration and model execution are not implemented by the current CLI. This document describes a proposed addition to the harness specification; it is not a confirmed intent record or authorization to merge. “Amendment” here refers to the specification, not a new intent-protocol marker or amendment semantics for confirmed decisions.

## Challenger amendment: constrained model exploration

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
