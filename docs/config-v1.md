# Ferretta configuration v1 — specification draft

**Proposed for discussion, not implemented or adopted.** This draft turns the
intentions in [the examples](examples/README.md) into one concrete language. The
current CLI still reads its existing JSON policies. A successful TOML parse does
not make these examples executable by Ferretta.

The proposed authoring form is **ordered waves containing typed stages**, with
explicit revision and evidence inputs. It compiles to the durable DAG described
in [architecture](../arch.md). The [dependency-stage alternative](workflow-syntax.md)
remains available for comparison; this choice is not settled. There is no general
expression language, script interpolation, or user-defined function mechanism.

## 1. Required semantics

- Review an existing PR, optionally repair it, and stop on acceptance or constraints.
- Pause for authenticated human intent decisions without spending compute while
  waiting. No pre-existing confirmed intent is required to start.
- Distinguish independent candidates scheduled serially from a serial chain in
  which each model receives the previous model's changes.
- Fan out from one revision, select useful contributions, integrate them, and
  review the combined result. Candidate LGTM is scoped to that candidate.
- Keep objectives, eligible routes, constraints, and acceptance separate.
- Explain effective configuration and bind it to exact work and evidence.

The three original recipes are acceptance cases for this design, not promises
that all orchestration ships in the first implementation slice.

## 2. Documents, versions, and authority

The optional trusted root `ferretta.toml` contains portable policy. An operator
profile holds connections, permissions, and host settings; a service
selects that profile independently of desktop login. Existing GitHub App
authentication remains an operator concern.

Every explicit document requires integer `schema_version = 1`, independently of
Ferretta's release and model version. Reject missing/unknown versions, unknown
fields, wrong types, duplicate definitions, and unsupported features with their
locations. Older binaries must report unsupported configuration rather than ignore
work. Breaking interpretation changes require a new schema version and explicit
migration. Record the revision of builtin defaults as well; changing defaults
must not mutate in-flight plans.

Additive fields preserving existing meanings may remain within v1; older readers
still reject documents that use fields/features they do not understand. Schema
version alone is not a claim that a particular binary implements every v1 feature.

```text
authorized PR override > trusted repo policy > operator profile defaults > builtin
```

Resolve the objective first; its preset supplies builtin routing/planning
preferences. Resolve other fields through the same ladder. An explicit invocation
objective overrides that invocation's objective within existing grants. A preset
change does not replace explicitly configured stages or routes.

Missing fields inherit. Scalars and arrays replace; arrays never append or merge
by ID. Named tables merge by key. `workflow.waves` replaces the complete wave array,
including nested stages. Empty arrays clear inherited arrays only where the result
is valid; an empty workflow is invalid. There is no `null`/delete-table mechanism
in v1. Unreferenced model definitions may remain. A model's tagged `thinking` table
is an exception: replace it atomically to avoid combining contradictory controls.

Precedence selects requested values; it does not grant authority. Operator grants
bound routes, effects, and paid use. PR overrides cannot authorize their own checks,
tools, money, or permissions. Pin the trusted policy revision separately from the
reviewed code. A PR's proposed config is evidence, not authority for that PR.
Resource increases require an authenticated allowlisted human grant and retain
prior usage. The delivery mechanism for general PR overlays is separate work.

Save source hashes, effective values/origins, non-secret route identities, default
revision, and compiled-plan identity. Normal loading is local only. Public URL
import remains an explicit authoring operation yielding an editable local copy
with provenance; no runtime `extends`, private URL auth, or automatic upstream
updates. Loading/validating a policy does not execute it.

## 3. Policy vocabulary and presets

| Key/table | Meaning |
| --- | --- |
| `schema_version` | Required integer `1`. |
| `objective` | `cost` (default), `time`, or `quality`. |
| `routing.prefer` | `lowest_incremental_cost`, `lowest_expected_latency`, or `strongest_review_evidence`. |
| `planning.strategy` | `serial_then_escalate`, `parallel_when_useful`, or `independent_then_synthesize`. |
| `models.<id>` | Explicit model name and requirements referencing an operator connection. |
| `checks` | Trusted local commands, additional GitHub checks, formatter, protected paths. |
| `limits` | Repository, PR, operation, and reserved allowances. |
| `workflow.waves` | Optional explicit array of waves; replaces automatic planning. |
| `completion` | Required final-review stage and publication/merge policy. |
| `evaluation` | Separate read-only scorecard judge. |

Only declared eligible routes may receive work. Presets rank eligible options;
they cannot authorize paid routes. Unknown capability, price, or inference location
stays unknown. Do not infer quality from price, token use, or model confidence.
Until measured ranking exists, use operator preferences and disclose that basis.
Persist scheduling choices so restarts do not rerank completed work.

Without an explicit workflow, the initial planner creates one final review using
the effective model alias `default-review`. Repair is off. Evaluation uses alias
`default-judge` when configured, otherwise reports unavailable. Operator setup
supplies these model definitions through `defaults.models` (section 9), including
their connection, model name, and thinking requirements. Until advanced planning exists,
all three objectives share this minimal plan and disclose that limitation.
Missing bindings produce setup guidance without downloading models or inference.
A zero-file repo policy is possible after operator setup; it cannot invent
connections, human allowlists, or trusted project checks.

## 4. Models and roles

Model, wave, and stage IDs use `[a-z][a-z0-9_-]*`. Stage IDs are unique across
the workflow; model/wave IDs are unique within their own collections.

```toml
[models.worker]
connection = "ollama"
model = "gpt-oss:20b"
inference = "local"
context_tokens = 131072
max_output_tokens = 16384
fallbacks = []

[models.worker.thinking]
mode = "effort"
effort = "medium"
```

Required `connection` names an operator connection; required `model` is the exact
nonempty model identifier requested from that server. Model names may contain the
provider's punctuation, such as `gpt-oss:20b` or `vendor/model-name`; the identifier
rule for Ferretta's own IDs does not restrict them. There is no intermediate
`routes` table. The binding is visible in one place:

```text
reviewer = "oss" -> models.oss (connection + model) -> connection endpoint
```

Portable policy contains no endpoints or secrets. A recipe can name its models
directly while a user/service profile supplies the machine-specific connection:

```toml
# Operator profile, outside the repo policy.
[connections.ollama]
provider = "ollama"
endpoint = "http://127.0.0.1:11434"

# Alternative connection for an explicitly configured LiteLLM proxy.
[connections.litellm]
provider = "openai_compatible"
endpoint = "http://127.0.0.1:4000"
```

The URL locates a server; it does not select a model or specify its API protocol.
For LiteLLM, `model` is its explicitly configured public model alias. Record the
requested alias and reported underlying model/provider separately; the alias may
route to multiple deployments, and missing execution identity remains unknown.
Explain must show the configured protocol, endpoint, model, and requested settings.
Connection/model changes affect future policy identity, not substitutions into
saved sessions. LiteLLM/compatible inference is proposed here; currently only
Ollama inference is implemented, with compatible endpoints used for discovery.

`inference` is `local` (default) or `any`. `any` permits an authorized remote route,
not paid use by itself. A local proxy does not prove local inference. Tools,
multi-turn continuation, and reasoning capability are required for all model roles
in this draft. Registry permissions follow the task: review/selection/evaluation
cannot edit; repair/integration may edit within policy. Judges cannot execute
configured checks; reviewers may run trusted checks despite read-only inspection.

The required `thinking` table is an atomic tagged choice:

| `mode` | Other fields | Meaning |
| --- | --- | --- |
| `effort` | Required `effort`: `low`, `medium`, or `high` | Require the verified provider control. |
| `enabled` | None | Require a verified boolean thinking control. |
| `provider_default` | None | Explicitly accept the provider default; effective thinking/effort may remain unknown. |

Reject contradictory/extra fields; preserve requested versus reported settings.
`context_tokens` defaults to 16,384, must be at least 8,192, and must fit verified
model capacity. `max_output_tokens` defaults to 4,096, must be at least 256, and
must leave at least 2,048 input tokens. These bounds do not prove that a model and
context fit RAM or will reason usefully.

`fallbacks` is an ordered array of other model IDs, default empty. Targets must
have empty fallback lists; no recursive fallback graph. Reject missing, duplicate,
and self references. Fallback requires a recorded eligibility or safely reconciled
failure decision and starts a separately attributed session. It retains remaining
work allowances and permissions while using its own declared model settings.
Role requirements cannot silently weaken. Timeout/HTTP failure is not proof of
zero consumption: reconcile uncertainty or stop before more spending.

Reviewer, repairer, selector, integrator, and judge are responsibilities, not
necessarily different models. Reusing a model in separate sessions must be
disclosed; it does not establish model diversity. Deterministic core policy
allocates work initially. Selection chooses contributions, not allowances.
A general model allocator and configurable grading rubrics remain deferred.

### Direct values and references

Use direct values where another named layer would only rename them:

| Concern | Direct form | Meaning |
| --- | --- | --- |
| Requested model | `model = "gpt-oss:20b"` | Exact server model ID or proxy alias, beside its connection. |
| Input revision | `input = "oss-pass.revision"` | Exact output of that earlier stage; `pr.head` is pinned at admission. |
| Evidence | `evidence = ["oss-pass"]` | Earlier structured results, not another evidence alias registry. |
| Check/formatter | `commands = [["make", "check"]]`, `formatter = ["make", "fmt"]` | Trusted argument arrays, not named command profiles. |
| Protected files | `protected_paths = ["go.mod", "go.sum"]` | Repo-relative paths, not named path collections. |
| Allowance | `limits = { compute = "15m", spend_usd = "0.30" }` | Scope-local values, not named budget profiles. |

Keep named model settings when roles reuse the same complete requirements, and
stable stage IDs where consumers/recovery need to identify results. Connections
retain endpoints and credential references at the operator boundary. Do not add
implicit `previous`/`latest` inputs that change meaning when stages are reordered.
Inline per-role model objects and external prompt/rubric-file references are not
part of this draft; add them only if a concrete authoring need justifies them.

## 5. Checks and effects

```toml
[checks]
commands = [["make", "check"]]
github_required = []
formatter = ["make", "fmt"]
protected_paths = ["go.mod", "go.sum"]
```

`commands` must resolve to a nonempty array of nonempty argv arrays for runnable
review. Commands are operator-trusted, not taken from the PR's proposed config.
No implicit shell expansion. Omitted formatter means none; `formatter = []` clears
it. Checks/formatters execute PR code; a worktree is not an OS sandbox.

`github_required` is an additional array of `{name, app_id}` tables, binding check
name to its emitting App. Empty means no additional checks, not a branch-protection
bypass. Ferretta orchestration checks cannot depend on themselves. Required pending,
missing, skipped, or stale checks are not passing evidence. Bind checks to exact
commits and applicable base/integration revisions.

Protected paths add to non-removable repair control-file protections. Entries
cover the file/directory recursively, case-insensitively; no glob/negation syntax.
Mutations invalidate check evidence. Candidates retain the exact checked tree and
parent. Initially failing CI is ordinary repair input, not an admission barrier.

## 6. Constraints and accounting

Limits are ceilings, not spending targets. Applicable scopes intersect; a child
cannot expand a parent. Higher configuration layers replace requested values
subject to authority, while nested runtime scopes add constraints. These are
different operations.

Durations are strings with integer `h`, `m`, `s` components in descending order,
each unit at most once, e.g. `"1h30m"`. At least one component is required.
Money is a nonnegative USD decimal string with at most six fractional digits.
Counts are nonnegative integers. `"unlimited"` is allowed only for compute,
wall clock, and `max_turns`; money must be finite. Zero means zero allowance,
never unlimited. This differs deliberately from legacy JSON zero time/turn fields.

| Scope | Fields |
| --- | --- |
| `limits.repository` | `spend_usd`, `period = "calendar_month_utc"`; required together, otherwise no additional period cap. |
| `limits.pr` | `compute`, `spend_usd`, `wall_clock`, `max_repairs`, `max_reviews`, `max_proposals`, `max_waves`. |
| `limits.review`, `.repair`, `.selection`, `.integration`, `.evaluation` | Per-operation `compute`, `spend_usd`, `max_turns`. |
| `workflow.waves[].limits` and `...stages[].limits` | Cumulative `compute`, `spend_usd` for that instance. |
| `limits.reserve` | `final_review_compute`, `evaluation_compute`, `final_review_spend_usd`, `evaluation_spend_usd`. |

Proposed builtin ceilings, open for discussion:

| Scope | Defaults |
| --- | --- |
| PR | 60m compute; `"0.00"` dollars; unlimited wall clock; four repair attempts, 20 reviews, eight proposal versions, 16 wave admissions. |
| Operation | Review 10m, repair 15m, selection 5m, integration 20m, **evaluation 10m**; unlimited turns within other bounds. Money inherits the PR ceiling unless narrowed. |
| Wave/stage | Remaining enclosing allowance unless narrowed. `converge.max_repairs` is always explicit. |
| Reserves | 10m final review and 10m enabled evaluation; zero evaluation reserve when disabled; zero monetary reserves. |

Defaults do not enable repairs/paid routes. Reserves protect portions of the
existing PR allowance, not extra funds. Ordinary work cannot consume them. Final
review/evaluation draw on their own reserve plus unreserved remainder within their
operation limits; release safely unused reservations afterward. Reject reserves
exceeding a finite parent allowance. Paid final review/judging needs adequate
explicit monetary reservations before optional work; zero does not promise a paid
finale. Plans must be able to admit required final assessment under every limit.
Protect its remaining review/wave counter slots too, and reserve paid final work
against the shared repository ledger so other PRs cannot spend it first. Counter
reservations are derived from the plan and shown by explain, not new allowances.

Count model and tool execution, including checks, retries, selection, integration,
final review, and evaluation. Sum concurrent worker durations; do not also charge
enclosing stage elapsed time. Queueing/human waiting is excluded from compute.
Wall-clock allowance runs from PR creation and includes both. Provider request
duration is observed execution, not a claim about GPU utilization.

Durable PR counters span commits and attempts:

- New review sessions increment `max_reviews`; human-wait resumption does not;
  a fresh retry does.
- Admitted repair and integration sessions increment `max_repairs`, even when
  unproductive. A no-call integration does not. Turns and commits are not cycles.
- New proposal versions increment `max_proposals`; delivery/reconciliation of
  an existing version does not. Corrections may require another slot.
- New wave instances increment `max_waves`; resumption does not.

Stage loop limits and stage/wave allowances cover that instance including retries;
PR totals retain earlier instances too. New heads, restarts, reruns, route/profile
changes, and fallbacks do not reset usage. Monthly repository totals span jobs in
one installation. Charge dispatches to their UTC admission month; retain pending
reservations across rollover. Raising limits retains previous charges.

Atomically reserve before dispatch; reconcile measured use afterward. Unknown use
is not zero. Paid work needs a bounded maximum authorized exposure, not an average
estimate. Local inference has no provider fee but still costs compute; electricity
and hardware costs stay unmeasured. Cancellation may not terminate external work
or charges immediately: retain overruns/uncertainty and deny new work as needed.
The guarantee is controlled admission, not exact remote termination at a deadline.

A bounded stage may return `needs_work` to a declared next stage. A PR/repository
hard stop admits no new work except protected final/reporting work that still fits
every applicable limit. Resuming afterward requires authenticated human intervention;
intent confirmation or calendar rollover alone does not clear a resource stop.
Exhaustion never becomes LGTM. Declining resources leaves agreed intent intact.

## 7. Waves, stage types, and data flow

Each `[[workflow.waves]]` requires `id`, `execution = "serial" | "parallel"`,
and a nonempty `stages` array. Waves execute in order after the prior wave settles.
Serial stage order matters. Parallel siblings are independent; host capacity can
run them one at a time without changing inputs. Optional positive `max_parallel`
narrows a parallel wave's concurrency; it is invalid on a serial wave.

Every stage requires `id`, `kind`, and its kind-specific fields:

| Kind | Required fields | Optional fields | Output |
| --- | --- | --- | --- |
| `review` | `input`, `reviewer` | `evidence`, `limits` | Findings/LGTM for the input revision; no edits. |
| `converge` | `input`, `reviewer`, `repairer`, integer `max_repairs` | `evidence`, `limits` | Bounded review/repair work and last usable revision. |
| `select` | Nonempty `candidates`, `selector`, `max_selected` (1–4) | `on_partial`, `limits` | Zero through K selected contributions and reasons. |
| `integrate` | `input`, `selection`, `repairer` | `if_empty`, `limits` | Checked combined candidate, or unchanged input. |

Model fields name `models.<id>`. `input` is `"pr.head"` or
`"<stage-id>.revision"`. `pr.head` means the head pinned at admission, never a
moving branch. Revision references may name earlier review/converge/integrate
results. `evidence` (default `[]`) names earlier results to expose; ordering does
not automatically expose private conversations. All work also receives applicable
PR context and authenticated intent evidence.

`candidates` names converge/integrate results; `selection` names a select result.
References must point to a prior wave or earlier member of the same **serial**
wave. Reject parallel sibling references, forward references, cycles, duplicate
references, and result-type mismatches. Compile ordering edges and artifact inputs
separately. Persist stable stage/round/attempt/effect identities.

Converge has built-in behavior, not a programmable `while`:

```text
review revision
  accepted       -> return scoped LGTM and revision
  clarification  -> await human -> resume saved role/session
  needs_work     -> if allowance remains: repair -> check -> candidate -> review
  loop exhausted -> return needs_work, remaining findings, last usable revision
  failure/limit  -> incomplete, or retain uncertain/awaiting-human state
```

Every repair requires another review, including the last permitted repair, subject
to remaining limits. Failed checks feed repairs within their allowance. Only checked
edits become candidates. The last usable revision is the latest checked candidate
or, if none exists, the input with its actual check status. Retain failed attempts
separately; a usable Git revision is not automatically checked. No-op repairs must
not fabricate commits/acceptance. Repetition appends DAG nodes, never back edges.

`needs_work` is a completed assessment that can feed later repair; `incomplete`
is not. Revision consumers stop on incomplete prerequisites in this draft.
`select.on_partial` is `"stop"` by default, or explicitly `"use_completed"` to
exclude incomplete candidates while reporting their outcomes. Completed candidates
with useful checked changes may be selected despite remaining findings; those
findings travel with the contributions and are not implicitly resolved.

Joins wait for terminal sibling outcomes. Human waits are not terminal; unrelated
siblings may proceed. Uncertain effects pause affected joins until reconciled.
No speculative quorum/early-cancel behavior in v1. Whole-job failure still publishes
captured status and evaluates available evidence within remaining allowance.

Selection may choose zero, with reasons, sources, incompatibilities, and dissent.
`max_selected` bounds distinct candidate IDs, not the number of ideas in a brief.
It creates an implementation brief, not human-confirmed intent or a scorecard.
`integrate.if_empty` defaults to `"stop"`; explicit `"keep_input"` skips model
editing and sends the unchanged input to final review without implying LGTM.
Integration starts from its explicit baseline, combines compatible ideas, and
records incorporated/rejected contributions. It is not blind patch concatenation.
Candidates are private worktrees/retained commits. Child PR publication, Cloudflare
execution, and multiple-owner coordination remain deferred.

### Compiled records and recovery

Compile source tables into a typed stage union; fields from another kind are
errors, not ignored options. Resolve string references to typed revision/result
handles before scheduling. Keep immutable node specifications separate from
execution state. Each node records its PR, input head/base, intent snapshot,
resolved policy, role, stage/round identity, and artifact dependencies.

Results retain the input/output commits, any checked candidate tree and parent,
findings and their sources, selected/incorporated contributions, actual model
provenance, and resource usage. An unchanged input is a distinct result from a
checked repair candidate. `awaiting_human` carries its exact pending question and
saved session; `uncertain` carries the effect identity and outstanding reservation;
`incomplete` carries a reason and the evidence actually obtained. These must not be
independent flags capable of representing both accepted and blocked work.

Use the single owner's durable store to reconcile repeated notifications against
the same node/attempt/effect. Recovery reuses completed results and resumes only
supported transitions; it never blindly repeats an uncertain model call, question,
push, or selection. This is the target contract, not a claim that arbitrary session
replay or the full DAG scheduler is implemented today.

## 8. Completion, publication, and scorecards

```toml
[completion]
review = "final-review"
publish_repairs = true
merge = "never"

[evaluation]
enabled = true
judge = "oss"
```

An explicit workflow names exactly one required final `review` stage. It must be
last in the last serial wave, or the only stage in the last wave. Its input is the
one output revision. The reviewer receives the workflow outcome manifest, unresolved
blockers, selection/integration provenance, and exact revision regardless of extra
`evidence` references. An evidence list cannot hide known blockers.

`publish_repairs` defaults false. If true, once final review is ready, publish its
changed, locally checked input candidate with an expected-head lease, reconcile
publication, then start final review of that exact published head. Only this one
authority updates the parent PR; siblings never push themselves. GitHub CI can
run on the repairs while final review inspects them. An unchanged input needs no
push. This is a proposed orchestration contract built on the basic repair design.

If publication is false, final review may inspect a private candidate, but cannot
make the unchanged PR head pass or merge a different revision; label it advisory.
Acceptance requires final LGTM, current required local and GitHub/protection checks,
and resolved blockers. Pending CI keeps completion pending. Failed CI or final
findings returns `needs_work`. Do not silently restart the workflow or add a final
repair loop; another attempt needs explicit admission and the same cumulative ledger.

`merge` is `"never"` (default) or `"squash"`, additionally requiring operator
permission and all acceptance gates. Recheck head/base and protections before the
effect; reconcile unknown outcomes. Human pushes supersede old publication/acceptance
authority. Carry intent with source/applicability, flag unresolved applicability,
and create new revision-bound work without resetting usage. General cross-revision
reconciliation remains separately tracked in issue #7.

Explicit recipes specify `evaluation.enabled` and, if true, `judge`. Automatic
planning enables it when `default-judge` is configured. Evaluate after terminal
workflow outcomes, including incomplete ones, with the judge's separate ten-minute
default allowance. Grade externalized behavior first, process evidence second,
and private transcripts only when needed and authorized. Preserve role scores and
uncertainty, not an unexplained average. Evaluation cannot change acceptance,
allocate work, or grant resources. Exhaustion makes the scorecard incomplete,
not the verdict different.

Checks carry routine lifecycle details, exact revisions, model/requested settings,
usage, and stop reasons. Intent questions remain comments. Replies resume the
relevant saved session; do not paste the entire comment history into every model
or restart all work. Corrections require a new proposal version; only authenticated
allowlisted human confirmations establish intent. Constraint proposals retain
their separate adopted vocabulary and grant state.

## 9. Operator profile and host scheduling

The [Alpaca profile](examples/operator-alpaca.toml) proposes the companion shape:
`schema_version`, `connections`, `host`, `permissions`, and `defaults`.
Operator keys are rejected in repo/PR policy.

| Operator field | Contract |
| --- | --- |
| `connections.<id>.provider` | `ollama`, future `openai_compatible` (e.g. LiteLLM), or future `openrouter`; unsupported adapters are errors. |
| `connections.<id>.endpoint` | Explicit base URL for Ollama/compatible APIs. Compatible adapters append paths such as `chat/completions` to this base, preserving any configured prefix. OpenRouter uses its adapter's fixed endpoint. |
| `connections.<id>.credential_ref` | Required for OpenRouter; optional for an authenticated compatible server. Never a literal secret. Reference backend is an operator integration detail. |
| `host.max_parallel_models` | Positive installation-wide ceiling, default 1. |
| `host.model_residency` | `none` (default) or `prefer_loaded`, within dependencies and fairness. |
| `host.memory_headroom` | Optional positive integer `GiB` string; desired space for OS/tools/other apps, not an OS reservation or proof a model fits. |
| `host.ollama_keep_alive` | Optional finite duration requested from Ollama; not exclusive control of a shared server. |
| `permissions.paid_inference`, `.repair_commits`, `.squash_merge` | Booleans defaulting false, bounding repo requests. |
| `permissions.intent_humans`, `.resource_humans` | Explicit authenticated-human login allowlists, default empty. |
| `defaults` | Optional portable-policy overlay, without another schema version. |

Watch lists, state paths, GitHub App key references, author admission, and boot
service setup retain their existing operator surfaces during migration. Loading
a profile never starts a watcher. Progress/proposal posting stays subject to
configured App/watcher authorization. Examples are inert until explicitly selected.

Connections select the adapter protocol rather than guessing it from URL/port.
Endpoint URLs must not embed credentials; use `credential_ref` where needed.
Provider/endpoint grants and metadata still determine eligibility. A proxy on
loopback does not establish local inference or grant paid use.

The local recipe passes each model's completed revision to the next. To run
independent candidates serially, keep each `input = "pr.head"` and use host
concurrency one instead. A 64GB machine may need reduced context as well as limited
concurrency. Unknown memory requirements remain unknown. Residency preferences
cannot reserve inference slots through human waits, starve other PRs, download
models, or silently choose paid fallbacks.

## 10. Validation, migration, and implementation slices

Proposed commands, **not implemented**:

```sh
ferretta config validate --file ferretta.toml --profile /path/to/operator.toml
ferretta config explain --file ferretta.toml --profile /path/to/operator.toml
ferretta config set objective quality
ferretta review --objective time
```

Validation parses/types the document, resolves layers, checks references/result
types, builds an acyclic plan, and checks static limits/authority. Offline validation
reports unresolved bindings/runtime capabilities without probing or inference.
Explain shows values/origins, actual routes, plan/data flow, total/reserved allowance,
and unmet setup needs. Distinguish syntax errors, semantic errors, unsupported
features, and operationally unresolved settings. Neither command executes checks
or writes to GitHub.

Keep JSON explicit until an opt-in migration preserves effective settings:
`max_tokens_per_turn` becomes `max_output_tokens`; `effort`/`thinking` becomes the
tagged table; zero turns/time becomes `"unlimited"`. Keep review/repair/judge
allowances separate. Existing timeout and proposed aggregate compute do not have
identical accounting semantics: surface conversions requiring a human choice.
Preview migration and preserve sources. Never combine implicit JSON/TOML authority
or reinterpret saved attempts under a new schema.

Suggested delivery slices:

1. Agree on semantics; implement pure-Go parsing, typed validation, layered
   resolution, and offline explain output. Execution remains explicit.
2. Run one review/converge/final chain through existing adapters and bounded repair
   work (#9/#11), with cumulative accounting and recovery.
3. Under #10, implement model-ordered serial waves, then candidates/selection/
   integration and capacity-permitted parallelism. Reject unsupported kinds until
   implemented; never approximate an independent fork as a dependent chain.
4. Add public import and richer objective planning. Author an agent skill only
   once the contract and executable examples exist.

Behavior fixtures cover independent versus dependent inputs; zero/fewer-than-K
selection; partial joins; stale head/base/checks; human waits/corrections; concurrent
reservations; uncertain spending/publication; and restart without duplicated work.
Use fakes and explicit clocks for normal tests; real adapters have separate suites.

## 11. Decisions to discuss before freezing v1

- **Authoring:** are ordered waves sufficient, or are arbitrary partial dependencies
  important enough to expose `after` in v1?
- **Loop name:** is `converge` clear, or is `review_repair` better? The behavior
  includes clarification and bounded repetition, not arbitrary code.
- **Local escalation:** this recipe always runs both model passes. A cost recipe
  might instead skip the heavier pass on scoped LGTM; that gate needs explicit syntax.
- **Final findings:** the draft returns `needs_work`. Should a declared outer-round
  mechanism admit another wave automatically within cumulative limits/reserves?
- **Defaults:** ten minutes for judging and no default turn ceiling follow prior
  direction. Other numeric defaults here are proposals for discussion.
- **Patch size:** the original lines-of-code idea needs rules for generated files,
  deletions, and cumulative versus final diff; it is deferred rather than vaguely
  interpreted as a token/compute constraint.
