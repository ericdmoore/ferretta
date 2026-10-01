# Automatic review and evaluation: implementation plan

Status: design and delivery plan, October 1, 2026. The first serial prototype
is implemented; see [runnable commands and boundaries](evaluation.md). The
remaining sections also describe future DAG, calibration and accounting work.
The first acceptance target is a PR that Ferretta discovers, reviews, and grades,
with three visible milestones: starting, verdict, and scorecard. The original
prototype used comments; the implemented watcher now defaults to review and
evaluation Checks with checkpoint progress. `--publication comments` retains
the original delivery mode. See [current operation](evaluation.md).

## Scope and existing foundation

Ferretta enters after a PR exists. The original implementation agent can use any
workflow; its transcript and a pre-existing confirmed intent record are optional.
A lightweight PR body describing purpose, tradeoffs, and validation is helpful,
but missing sections do not block admission. Author claims remain claims until
supported; an agent-authored body does not establish human-confirmed intent or
grant permissions.

The original `service run` polls PR revisions into SQLite without dispatch.
The new explicit `service watch` admits one PR, publishes milestones, evaluates
its terminal review, and polls authenticated intent replies.
The manual Ollama reviewer has commit-bound tools, checks, private checkpoints,
and opt-in GitHub App proposal publication. Manual resume polls authenticated
human replies once. The prototype adds a read-only scorecard judge and automatic
verdict publication; [bounded repair](repairs.md) is now opt-in, while merge execution remains absent. See [service](service.md),
[proposal sessions](proposals.md), and [configuration](configuration.md).

Build on those adapters and state boundaries. The first runtime is one local
owner with serial execution. Stable job/node/attempt/output/effect identities
must accommodate later parallel waves without implementing a general scheduler
or distributed claims in this prototype.

## Roles and authority

The word evaluator has described two different jobs. Use distinct names in code,
configuration, and comments:

| Role | External output | Authority |
| --- | --- | --- |
| Review / repair worker | Findings and evidence, or candidate repair commits | Perform permitted investigation or repair; propose intent questions |
| Final reviewer | Revision-specific acceptance assessment and unresolved blockers | Assess agreed acceptance criteria; cannot lower them because resources are scarce |
| Allocator / hot seat | A stop-or-continue recommendation and proposed resource allocation | Recommend the next action; the core enforces admission and remaining resources |
| Scorecard judge | An assessment of a worker output or an oversight decision | Grade only; cannot repair, merge, grant resources, or restart work |

Final review and resource allocation remain separate responsibilities even when
one model performs both. A stop caused by exhausted allowance is an incomplete
result, not LGTM. Only the core can authorize another round within existing
permissions. Models can request additional resources, never award themselves any.
The goal is to reach acceptance within the allowance and stop; allowance is a
ceiling, not a spending target. The prototype allocator is deterministic core
policy, reserving required assessment capacity and admitting permitted actions.
Another model call is unnecessary until choosing among strategies adds value.

The prototype's single reviewer produces the review and terminal decision. Its
scorecard can assess both roles from that output; it must not imply that two
independent reviewers participated. The scorecard judge uses a fresh session.
Repair and multi-round hot-seat execution are later work; their rubric contracts
are defined now so the data model does not assume every agent writes commits.

## Evidence hierarchy

Grade in this order: **externalized behavior > process evidence > full transcript**.

1. Default input: published review or repair, PR purpose, applicable authenticated
   intent, exact commits and diff, and check evidence. Allow bounded read-only
   repository tools when necessary to investigate claims.
2. Optional diagnostic input: harness-recorded tool events, arguments or summaries,
   results, outcomes, timing, and truncation. Label assessments using this input.
3. Explicit deeper debugging: private conversation and continuation records under
   a separate access/retention policy. Do not publish them in scorecards.

Tool access does not prove understanding. Tool count, response length, or an
agent's description of its diligence must not substitute for useful outcomes.
Internal policy rules remain enforced invariants, not a permanent process grade.
Do not require or infer private chain-of-thought. A resumed model session can need
full continuation data even when evaluation uses only public outputs.

A compact tool event identifies operation, attempt, tool-call ID, revision, scope,
outcome, duration, and result truncation. Git content can be referenced by commit,
path, and range if the objects remain available. Retain transient check output;
a hash alone cannot reconstruct missing evidence. Mark incomplete tracing when
tools outside Ferretta's mediation could have been used.

## Role-specific rubrics

Use a versioned rubric family with separate worker and oversight variants. Every
dimension has either a score with justification/evidence, or an explicit
not-assessed reason. Distinguish not applicable from insufficient evidence;
neither is a zero or a pass. Shared initial anchors:

| Score | Meaning |
| --- | --- |
| 0 | Materially fails the dimension; identify the consequential defect |
| 1 | Partially useful, but substantial gaps require correction |
| 2 | Adequate and supported for the stated scope |
| 3 | Strong, well-supported handling of the relevant subtleties |

These are initial anchors, to be refined with examples before interpreting small
score differences. Scores are ordinal assessments, not probabilities.

| Dimension | Worker rubric | Oversight rubric |
| --- | --- | --- |
| Correctness | Findings are accurate; repairs address the defect without introducing regressions | Acceptance or next-action conclusions follow from the available evidence; blockers and stale revisions are recognized |
| Evidence | Claims connect to code, reproduction, or checks; limitations are explicit | Independent results, checks, remaining findings, and missing evidence are considered |
| Intent alignment | Work addresses the human's problem without invented requirements | The integrated result is assessed against applicable intent; consequential ambiguity is escalated |
| Judgment | Consequential issues and tradeoffs receive appropriate attention; changes remain proportionate | Stop, continue, ask, or stop-incomplete is justified by remaining work, impact, and granted resources |
| Actionability | Findings explain consequences and useful remedies; repairs are reviewable | A continuation names a concrete next investigation/repair and expected benefit; a stop explains remaining work |
| Communication | Clear, concise, specific explanations, with appropriate qualification | A clear decision, rationale, tradeoffs, and uncertainty without overstating completion |

A review-only worker is not penalized for making no commits. An oversight agent
is not graded on commits it did not author. More rounds or more severe findings
do not inherently improve a grade. Earned LGTM is a valid strong outcome.

The judge emits an **automated assessment**. Correctness may remain unverified;
model agreement, successful merging, or lack of complaints are not ground truth.
Retain the distinction between model opinion, executable verification, fixture
expectations, and human assessment. Preserve conflicting assessments.

## PR-level roll-up

Retain individual role scorecards and an explainable aggregate snapshot for the
exact integrated revision. The aggregate includes:

- Current lifecycle/acceptance status and unresolved blocking work, separately
  from grades. A high grade cannot override a failed required check or blocker.
- Worker and oversight dimensions, their evidence and coverage, and links to the
  contributing output versions, agents, attempts, nodes, and rounds.
- Missing, failed, stale, superseded, or not-assessed contributions and dissent.
- Final integrated-result assessment, plus total resource use including discarded
  attempts. Successful repairs do not erase earlier failures from history.

Do not average every agent message or wave: extra workers must not manufacture
weight, and several good stylistic scores must not conceal an unsupported stop.
A local branch's success cannot establish that integrated changes are correct.
Parallel work must preserve its common baseline and identify which candidate
commits were incorporated before assessing the integrated revision.

The initial aggregate is a role-separated scorecard, not one numeric grade.
A scalar score remains a policy experiment: specify/version its weights,
consequence rules, missing-data treatment, and sampling unit before computing it.
Show component grades beside any later scalar and keep eligibility independent.
No weights or acceptance threshold have been agreed. Synthetic fixtures will
exercise multi-worker and multi-round aggregation before that runtime exists.

## Three-comment acceptance contract

Normal successful execution for an eligible, unchanged PR produces these ordered
GitHub App comments, once each per review/evaluation identity:

### 1. Starting comment

Publish before the first review model call. Ferretta's local owner claims the job;
the comment names the model assigned to it. Include:

- PR head/base, run identity, trusted policy identity, and plain-language plan.
- Assigned reviewer/provider; requested effort or thinking control; configured
  turn, context, and per-response generation limits. Turns are not reasoning effort.
- Planned scorecard judge/provider/settings, with separate evaluation allowances.
- Effective compute, monetary, elapsed-time, and cycle constraints when supported.
  Clearly distinguish unlimited, disabled, not configured, and unknown. Do not
  advertise a constraint as enforced before the executor can enforce it.
- Requested settings separately from provider-reported execution details; details
  unavailable before execution remain unknown. Never expose credentials or private
  connection details. A permitted model change gets an attributable update.

### 2. Final verdict comment

After review and revision revalidation, publish the exact reviewed commit,
verdict, findings or supported LGTM, check results, tradeoffs, and relevant usage.
An execution failure or exhausted allowance produces a terminal status explaining
incomplete work, not a fabricated verdict. A proposal waiting for a human is an
intermediate state, not review completion.

### 3. Scorecard comment

After verdict publication, trigger a separately configured judge. Publish the
worker/oversight scorecard and PR roll-up with scope, output identity, rubric
version, judge provenance, evidence, unknowns, and separately attributed usage.
For this prototype, all normal completion paths are evaluated; do not silently
sample away the promised scorecard. If evaluation fails or cannot be admitted,
publish an explicit failed/incomplete evaluation status in its place.

An assessment cannot restart repairs or change the verdict. A future policy may
consume assessments through an explicit transition; it must not form an implicit
recursive evaluation loop. There is no judge-of-the-judge call on every PR.

Additional comments are permitted for genuine proposals, human responses, model
changes, failures, or superseded revisions. Ordinary success needs only the three
milestones. On a newer PR revision, retain the old evidence, mark its scope, and
admit new work under policy without resetting consumed allowances. Revalidate
before publication; races after that check are handled by exact-commit labeling
and later supersession, not a claim of atomic GitHub publication.

## Durable execution and publication

Extend intake with explicit trusted dispatch configuration and a target scope.
Keep intake-only operation available. A PR's own config changes cannot authorize
execution, paid routes, or publication. Use configured human allowlists and App
identity; no personal-token fallback. A draft PR remains outside normal admission.

Persist states with required state-specific data: ready, executing, awaiting human,
review completed, publication pending/uncertain/published, evaluation ready,
evaluating, assessed, failed, or superseded. Publication and evaluation failures
must not erase a completed review. Prefer separate typed transitions for review,
evaluation, and effects over a Cartesian product of independent flags.

Before effects, persist stable identities and admission/reservation facts. Repeated
polls and ordinary restarts must not repeat completed spending or comments. Reuse
the existing App-comment reconciliation pattern with exact content and bot identity.
An interrupted model request has uncertain consumption; do not blindly replay it.
Surface uncertainty and retain its reservation until resolved or explicitly handled.

An evaluation targets an immutable output snapshot: exact revisions, report/hash,
confirmed-intent evidence, trusted review policy, rubric/judge policy, and check
references. A delivery replay retains its evaluation ID. An intentional regrade
creates a new attributable attempt; it does not overwrite the prior scorecard.
Edits to a published comment must not silently change the already graded snapshot.

Automatic human-reply polling wakes the existing proposal session. Preserve the
current protocol: CORRECTED requires a revised PROPOSED version; only authenticated
allowlisted human CONFIRMED establishes agreement. Recheck revision and policy.
Intent confirmation never grants additional resources.

The serial service resumes pending durable stages; it is not necessary to add a
second daemon. Also expose an explicit local command to grade a saved report and
an opt-in post-review hook. Candidate spellings are `ferretta eval --run PATH` and
`ferretta review ... --eval`; neither exists yet. CLI and service share the core.

## Resource ledger and model allocation

Attribute review, repair, oversight, and grading separately within the job's total
allowance. Ledger entries reference job/node/attempt/operation IDs and provider
evidence. Keep estimates, reservations, reported usage, and reconciled charges
distinct; missing usage is not zero and a reservation is not a second charge.

First fields: input/output tokens and available cache/reasoning accounting,
model/tool execution duration, human wait, queue time, elapsed duration, monetary
amount/currency when known, and human clarification/intervention counts. Preserve
provider definitions: nested token categories must not be added twice. Local-only
inference can have zero incremental API charge while hardware/electricity cost
remains unmeasured. Subscription allowance is distinct from money when available.

Compute is the sum of model and tool execution durations, excluding human wait;
future parallel work sums worker durations. Do not also charge enclosing stage
duration. Time measured at the adapter is not actual GPU time. Enforce admitted
deadlines with cancellation and report any overrun or uncertain remote execution.
Track turns without imposing a turn ceiling unless policy requests one.

Prefer a capable different-family judge when an approved route is available;
same-model judging is an explicit option, not an independent second opinion.
Keep author model identity out of grading content where feasible while preserving
it in provenance. Verify tool/reasoning capabilities; discovery is not permission.
Do not send local-only repository content to a cloud judge without an authorized
route and data policy.

Allocate premium compute where it can reduce consequential error or rework:
difficult implementation, consequential final review, and calibration samples.
Routine scorecards need not all use a premium model. Concrete escalation signals
can include conflicting findings, failing checks, important code areas, and lack
of progress; a model's confidence alone is insufficient. Random audits later
help reveal confidently missed defects. Do not equate model price, token count,
or effort setting with demonstrated quality. No fixed budget split is selected.

Suggested starting judge settings, pending selection/calibration: medium effort
where supported, ten minutes active compute, no turn ceiling, read-only tools,
and no paid route. The earlier same-model example used local gpt-oss:20b with
131,072 context and 16,384 generated tokens per turn. These are prototype proposals,
not universal defaults, a change to current policy, or proof of judging quality.
Choosing the actual separate judge remains an operator configuration decision.

## Delivery sequence

1. **Records and contracts:** typed immutable outputs, rubric versions, per-role
   assessments, aggregate references, and ledger/reservation transitions. Validate
   boundaries, retain unknowns, and use injected clients, clocks, IDs, and responses.
2. **Local evaluation:** strict structured judge output, read-only evidence tools,
   saved-report command and post-review hook. Persist review success before grading;
   isolate grading failure. Add the first executable fixture corpus and fake judge.
3. **GitHub publication:** shared durable publisher for starting, verdict, and
   scorecard comments, using the existing proposal effect rules. No full transcript
   or private session paths in public comments. Treat PR content and model text as
   untrusted data, including attempts to instruct the judge to award high scores.
4. **Service dispatch/resume:** policy admission, one local owner, serial durable
   stages, automatic proposal-reply polling, revision supersession, and recovery.
   Reserve evaluation allowance within job limits before promising the plan.
5. **Dogfood and compare:** build/start the prototype from a trusted checkout before
   opening its code PR. Configure that PR as the acceptance target so installing
   the service does not accidentally dispatch every existing PR. Opening the PR
   must trigger the review without a manual command. Record actual milestone
   comment URLs, revisions, usage, and limitations as acceptance evidence.

This planning commit starts `feat/review-evaluation` from the work in PR #3.
The implementation PR should contain a coherent runnable slice and reference
this plan. Its base must account for the existing stacked PRs at publication time.
Opening a PR does not upgrade a running installation. Do not mark the feature
complete merely because an evaluator produces valid JSON or three comments exist.

## Validation and acceptance evidence

Normal tests remain fast/offline with fake GitHub/model clients and injected time.
Use behavior/invariant tests for:

- Three ordered milestone effects on the unchanged successful path, once each.
- Exact revision/policy binding, independent rubric versions, and immutable regrades.
- Human wait/resume, bot rejection, correction/reproposal, and no waiting-time charge.
- Duplicate events, crash boundaries, uncertain posting/model outcomes, and no blind
  retry or double accounting. A judge failure cannot erase a completed verdict.
- Unknown usage versus zero, provider subcategories, shared limits, and cancellation.
- Six scored-or-unassessed dimensions, evidence validation, unsupported verdicts,
  role applicability, and no tool-count or response-length reward.
- Roll-up examples with parallel/serial contributions, missing work, dissent,
  superseded revisions, and later repairs without averaging away blocking status.

Run `make check` and maintain the coverage ratchet; verify all four CGO-disabled
targets. Run `make site-check` when documentation/site changes require it. Do not
add live model calls to normal tests or change the read-only network-test contract.
The explicitly authorized dogfood run posts real milestone comments and uses the
configured model; it is separate from those adapter suites.

Start calibration with a supported defect, a clean change deserving LGTM, an
unsupported finding, ambiguous intent, and honest insufficient evidence. Include
oversight decisions that stop too soon or continue without benefit. Keep expected
answers hidden from the agent being tested. Use a small human-assessed sample to
check grading usefulness and stability; production PRs need not all be human graded.
Valid structured output, model agreement, or a high score alone does not prove
correctness. Compare total outcome quality, compute, money, and human intervention.

## Deferred work and remaining choices

Automatic merging, real parallel waves, multi-installation coordination,
OpenRouter/subscription adapters, the layered TOML resolver, automated sampling,
and comprehensive feedback collection are outside the first acceptance run.
Preserve their interfaces and current status rather than claiming them implemented.

Before the live run, select the approved reviewer/judge routes, effective allowances,
target PR, and comment publication settings. Resolve rubric examples through the
small calibration set. Numeric PR weights, automatic use of grades for admission,
and production sampling/retention policies remain explicit later decisions.

Research supporting calibration rather than automatic trust:
[self-preference bias](https://arxiv.org/abs/2410.21819),
[weak-model oversight](https://arxiv.org/abs/2407.04622), and
[evaluation practices](https://developers.openai.com/api/docs/guides/evaluation-best-practices).
These inform experiments; they do not establish Ferretta's grading accuracy.
