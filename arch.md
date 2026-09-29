# Ferretta architecture

Architecture decisions recorded on 2026-09-29. This document describes the
agreed direction, not a claim that every capability is implemented. Development
principles and the original intent protocol are also documented in [README.md](README.md).

## 1. Product boundary

Ferretta starts after an implementation agent submits a pull request. The
human's earlier interaction with that agent is outside Ferretta and may use any
tools or workflow. A pre-existing confirmed intent record is not required.

Review starts from submitted commits, available PR evidence, and trusted
repository policy. Ferretta evaluates correctness, alignment with human intent,
and whether the implementation is appropriately small and well designed. It
asks consequential intent questions through PR comments and retains authenticated
human decisions as evidence. It must not manufacture findings to appear useful.

## 2. One local coordinator, potentially many model workers

The initial deployment is one local Ferretta installation owning its jobs and
resource ledger. That owner can coordinate multiple models concurrently when
policy and hardware permit. A parallel first wave may be followed by assessment
and another serial or parallel wave. One owner does not imply serial inference.

Identify repositories canonically rather than by checkout directory. Checkouts
and review worktrees for the same repository share the configured local state
authority. A second independent store must not be mistaken for coordinated
ownership of the same jobs.

Multi-installation and cloud coordination are deferred. The initial workflow
model must nevertheless be independent of a particular process or host.

## 3. Durable DAG of work

Represent work as explicit DAG nodes and dependency edges. Support serial
dependencies, parallel branches, joins, and conditional admission of further
work. Validate references and acyclicity before admitting nodes.

Keep stable identities for jobs, nodes, attempts, and external effects. Node
specifications bind work to exact input commits, policy, relevant intent evidence,
role, round/slot, and dependency results. Store execution transitions and results
separately from immutable specifications. Repeated notifications identify existing
work rather than creating a new spending opportunity.

Review/repair cycles are logical rounds expanded into new nodes:

```text
review-1 → repair-1 → review-2 → repair-2 → review-3
```

Do not introduce a back edge or overwrite earlier results. A new revision gets
new revision-bound work while preserving the job's history and resource usage.
Explicit retry attempts remain attributable to their original work.

## 4. Pure core and effect boundaries

The core owns policy: eligibility, transitions, budgets, retries, routing,
permitted tools, and verdict acceptance. It consumes explicit state and inputs
and produces command objects. Thin executors perform effects and return facts.

Keep model APIs, Git/GitHub, process execution, persistence, notification delivery,
and clocks at the boundary. Time, IDs, randomness, model responses, and external
results are explicit inputs. Inject clients for deterministic, offline tests.

Use types, encapsulated data, and validated transitions to represent valid
states. An awaiting-human state carries its question; an accepted review carries
its revision and evidence; an uncertain effect carries its dispatch identity.
Avoid independent flags that permit contradictory states. Validate external
inputs and handle Go zero values explicitly.

## 5. Review, repair, and integration

At least one review stage is required. Repair stages are optional. In a complex
workflow, a required final review evaluates the integrated result.

| Stage | Output and authority |
| --- | --- |
| Review | Structured findings, supporting evidence, intent questions, tradeoffs, or LGTM for an exact revision. PR comments present useful results; notes do not establish human intent. |
| Repair | Candidate commits addressing review findings or failing CI, within granted tool/write permissions. |
| Synthesis / hot seat | An evidence-based assessment and recommendation for a permitted next action. |
| Integration | Selected compatible changes, applied and checked as a combined revision; publication is an executor effect. |
| Final review / merge | Acceptance of the applicable revision, followed by the separately authorized squash-merge effect. |

Parallel reviewers receive the same baseline revision and evidence. Preserve
their independent observations until synthesis. Serial reviewers may receive
earlier findings for refinement and verification. Joins retain failed, missing,
and incomplete results; those outcomes do not count as successful review.
Synthesis preserves dissent and source references rather than treating consensus
as proof of correctness.

Repair workers use isolated worktrees and candidate branches. Alternative
implementations are not automatically combined. One integration authority
selects compatible changes or requests a combined repair; workers do not race
to push to the shared PR branch. Checks may also require isolated workspaces
even when the review's source-access tools are read-only.

**Initially failing CI is valid input to ordinary repair waves.** Passing CI is
not a repair admission requirement. Passing required checks remains a final
acceptance and merge requirement.

## 6. LGTM and automatic squash merge

LGTM is the successful return from a review scope: no unresolved blocking work
remains within that scope for the named revision. It includes supporting
evidence and major tradeoffs. It is not a PROPOSED intent question and does not
require another human confirmation exchange.

A child review's LGTM completes only that child's scope. Its parent must still
validate required coverage, other results, and the applicable revision. Repairs
produce new commits; prior approvals cannot silently approve the changed result.

Ferretta may automatically publish integrated repairs and squash-merge when:

- The required final reviewer has accepted the exact resulting PR head.
- Required CI/checks have passed and blocking findings/questions are resolved.
- Applicable base/integration evidence is current and repository protections
  are satisfied.

The executor performs publication and merging under core policy, using expected
revision checks. Preserve concurrent human changes; do not overwrite them or
bypass branch protections. Reconcile uncertain pushes and merges before retrying.
Model approval alone never authorizes stale or incompletely checked code.

## 7. Hot-seat model and the Intern

The hot-seat model assesses whether more work is worth its expected cost. It
may also synthesize results and publish ordinary implementation notes. Its
recommendation is one of:

- Continue with a declared wave, explaining the remaining question and benefit.
- Ask the human a consequential intent question.
- Proceed through required final-review and acceptance gates.
- Stop incomplete, retaining unresolved work and the reason for stopping.

Allocation and final review are distinct responsibilities that may use the same
model. Such reuse is not an independent second opinion. The core authorizes
spending; the hot seat cannot invent routes, grant tools, replenish allowances,
or waive required acceptance criteria.

A configured local fallback, **the Intern**, may repair, synthesize, and provide
final review when the primary route is unavailable or unaffordable. Her valid
LGTM can satisfy final review and lead to squash merge. She must satisfy the
same capabilities, evidence requirements, and remaining constraints.

Reserve capacity for mandatory final review and reporting within the total
allowance. If no model allowance remains, report from captured evidence without
an additional model call. Stopping spending is distinct from approving software.

## 8. Configuration and model routing

Use **`ferretta.toml` at the Git repository root** for portable policy. Explicit
policy selection overrides discovery; invalid explicitly selected configuration
must fail rather than silently choose a different policy. Migration from the
bootstrap JSON format remains implementation work.

Policy describes model aliases, required capabilities and effort, waves, gates,
permitted effects, fallbacks, and resource limits. Machine configuration resolves
connections and credential references. Secrets remain outside the repository.

Resource settings resolve in this order:

```text
CLI defaults → trusted repository defaults → authorized per-PR overrides
```

Record the source of effective settings. Pin trusted policy and resolved route
configuration with each review, excluding secrets. A PR must not authorize its
own increased spending or permissions by changing configuration under review.

Support local inference and OpenRouter through provider adapters. Reject routes
that cannot satisfy required tools, multi-turn continuation, or reasoning
settings. Only explicitly permitted fallbacks are eligible. Record requested
provider/model/effort separately from reported execution details; unavailable
details remain unknown.

`init` discovers candidates from Ollama, LiteLLM, vLLM, and other common model
endpoints. Discovery does not authorize use. A loopback proxy may route remotely;
endpoint location is distinct from inference location. Discovery does not start
services, download models, run inference, execute checks, or overwrite policy.

### GitHub identity and connection

Use a dedicated GitHub App installation for Ferretta's API and Git transport.
Agent proposals must retain the app's bot identity; human decisions require
separate authenticated allowlisted authors. Do not fall back to personal `gh`
credentials or tokens. Machine configuration holds app/installation IDs and a
private-key reference; repository policy does not carry credentials.

The initial connection uses short-lived, repository-scoped read tokens and
manual commands. User OAuth is not required. Registration, app installation,
and local private-key provisioning are operator setup. A shared hosted app's
private key must never be distributed to other installations. Strong separation
from human credentials during untrusted code execution needs an OS isolation
boundary; a detached worktree and environment filtering do not supply one.
See [GitHub App setup](docs/github-app-auth.md) for the implemented scope.

## 9. Resource ledger and stop constraints

Track monetary allowances, model/tool execution time, cycles/rounds, calls/tokens,
and an optional wall-clock deadline from PR creation. Policy may combine limits;
meeting an acceptance condition is distinct from reaching a stop condition.

Compute allowance counts model and tool execution, excluding human waiting.
Sum worker durations when work overlaps: two concurrent one-minute operations
consume two worker-minutes. Report active job duration separately. Do not charge
both an enclosing stage's time and its child effects for the same work. Measured
request duration is not a claim of actual GPU usage.

Before dispatch, atomically admit work and reserve its resource allowance.
Afterward, reconcile actual usage. Include retries, synthesis, final review,
compaction, and fallback calls. Missing usage or uncertain delivery is not zero
cost. Preserve outstanding reservations until their outcome can be reconciled.

Paid-budget exhaustion can remove paid routes while leaving an eligible Intern
route. Compute and cycle limits still apply. Exhausted repair allowance prevents
another repair round; final assessment may use remaining reserved capacity.
A hard execution deadline must not be bypassed by switching models or roles.

Restarts, notifications, new revisions, ownership changes, and fallback selection
do not reset usage. Raising a ceiling does not erase previous charges. Explicit
human intervention is required to resume a job stopped by its constraints;
resolving an intent question alone cannot clear a resource stop.

## 10. Intent and resource decisions

Markers retain the form `VOCAB-{Topic}-vN::`, with positive version numbers and
the terminating double colon. Keep **CORRECTED**, not CORRECTION.

| Vocabulary | Author | Meaning |
| --- | --- | --- |
| `PROPOSED` | Agent | A versioned interpretation or consequential intent question. |
| `CORRECTED` | Authenticated authorized human | Substantial disagreement or unsuitable options; requires a revised proposal at the next version. |
| `CONFIRMED` | Authenticated authorized human | Accepts the exact interpretation/version or an explicitly specified option. |
| `CONSTRAINTPROPOSED` | Agent | Requests a precise change to resource allowances with rationale and remaining work. |
| `CONSTRAINTCONFIRMED` | Authenticated allowlisted human | Authorizes the referenced resource change. |
| `CONSTRAINTDECLINED` | Authenticated allowlisted human | Refuses that resource request without altering agreed intent. |

The constraint markers are adopted architecture vocabulary. They do not yet
exist in the current parser. Models and bots cannot grant themselves resources.
Human authorization comes from authenticated authorship and the configured
allowlist, not a claim in comment text.

Intent and resource decisions have separate state transitions. One human comment
may contain both. A correction does not grant money; a grant does not confirm
intent; a denial does not invalidate intent. Validate the comment before applying
its decisions, and preserve each decision's source and exact version.

Persist grants against explicit allowance versions and deduplicate delivery.
Prefer an explicit new ceiling, even when the human-facing request also shows
an increment. Stale or conflicting decisions must not silently double allowances.
Edits/deletions must not rewrite already-captured historical decision evidence.

Intent confirmation pauses only dependent work; unrelated permitted review may
continue. Multiple pause reasons remain distinct. Amendment semantics are
deferred, and confirmed intent must not be silently overwritten.

## 11. SQLite as the local durable store

**SQLite is the selected backend for a single installation**, supporting durable
state and flexible queries without an external database service.

Store repositories/jobs, DAG nodes/edges, attempts, model continuation, evidence,
effect dispatch/outcomes, reservations, usage, and resource grants. Keep immutable
history alongside the current state required by scheduling. Store private
continuation data separately from public review explanations/exports.

Use transactions and uniqueness constraints for admission, reservations, and
deduplication. Commit before running external effects, then record their outcomes
in another short transaction. Never hold a transaction open while waiting on a
model, tool, or human. A transaction cannot make an external API effect atomic;
explicit uncertain outcomes and reconciliation remain necessary.

Keep SQLite behind the work-store interface and use a configured state location
shared by the repository's checkouts. Use a CGO-free driver, pinned and verified
against the supported targets. `modernc.org/sqlite` is a candidate, not yet an
adopted dependency. JSON remains useful for inspection/export but PR comment
blocks are not the authoritative spending ledger.

## 12. Future distributed execution

Multiple installations sharing jobs must share an ownership and accounting
authority. Hashes identify work and verify evidence; they do not make claims
exclusive. CRDT-style replication may help with immutable evidence and
independent observations, but does not by itself prevent duplicate spending.

Local workers may claim immediately while cloud workers wait a configured delay.
That delay expresses preference. Atomic claims determine ownership. Coordinate
claim state and spending reservations; use leases and ownership epochs so stale
owners cannot mutate current coordination state.

An expired lease does not prove an external call stopped. Preserve uncertain
effects during takeover, fence effects where the destination supports it, and
reconcile before retrying. Do not promise exactly-once provider calls without a
provider guarantee. Takeover preserves the policy, route restrictions, and
remaining allowance.

Independent SQLite files cannot coordinate these decisions across machines.
A future shared coordinator can expose the work-store semantics without moving
all inference to the cloud. No etcd, CRDT library, or cloud service is selected
or required for the initial implementation.

## 13. Implementation discipline and outstanding details

Keep implementation and dependencies pure Go where possible. Verify Linux and
macOS on amd64 and arm64 with CGO disabled. Use pinned compiler/formatter/linter
versions and the same local/CI checks. Coverage is a ratchet, with fast tests of
observable behavior and invariants rather than implementation details.

Test the core with fakes and explicit time/IDs. Test the SQLite adapter using
temporary databases for transactions, duplicate delivery, rollback, and recovery.
Keep live-provider checks separate from the offline suite. The minor/major
release policy for requiring live tests remains undecided.

Remaining implementation choices include exact TOML schemas and migration,
concrete model IDs and default allowances, database location/schema/driver
version, grant payload validation, and detailed recovery protocols. Support for
Markdown-heading-prefixed markers is not decided; current markers start at
column one, and quoted/fenced examples are inert.

The existing CLI provides intent inspection and a bounded single-Ollama review
with JSON checkpoints; the separately developed `init` prototype discovers
local endpoints and prepares an Ollama policy. SQLite, the DAG scheduler,
constraint parsing, notifications/resumption, broader provider routing, repair
waves, and automated merging remain to be implemented. The runtime projects
surveyed during design are references; none has been adopted as a dependency.
