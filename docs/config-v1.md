# Ferretta configuration v1 — specification draft

**Specification draft; not implemented.** The v1 scope is **serial execution with
Ollama and OpenRouter support in the first runnable release**. Field names and
numeric defaults remain proposals for discussion. The current CLI still reads
its existing JSON policies and implements Ollama inference only. A successful
TOML parse does not make [these examples](examples/README.md) executable.

The proposed authoring form is an **ordered array of typed stages**, with explicit
revision and evidence inputs. Each stage finishes before the next begins. Fan-out,
fan-in, selection, integration, and parallel execution are outside v1, including
independent branches scheduled one at a time. The [future branching sketch](examples/future/diverge-and-converge-workflow.toml)
and [syntax alternatives](workflow-syntax.md) preserve those ideas for later.
The serial plan still fits the durable DAG in [architecture](../arch.md); v1
exposes a chain. There is no expression language or script interpolation.

## 1. Required semantics

- Review an existing PR, optionally repair it, and stop on acceptance or constraints.
- Pause for authenticated human intent decisions without spending compute while
  waiting. No pre-existing confirmed intent is required to start.
- Pass the previous stage's revision to the next stage; retain exact evidence
  and model provenance across local and OpenRouter sessions.
- Support tools, multiple turns, reasoning settings, and bounded review/repair
  for both adapters. A cloud model uses the same acceptance gates as a local one.
- Keep objectives, eligible routes, constraints, and acceptance separate.
- Explain effective configuration and bind it to exact work and evidence.

The serial recipes are v1 acceptance cases. Supporting only local inference does
not complete v1; supporting an OpenRouter text-only request does not complete it
either. The divergent recipe is a future design, not a v1 acceptance case.

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

Resolve the objective first; its preset supplies builtin routing
preferences. Resolve other fields through the same ladder. An explicit invocation
objective overrides that invocation's objective within existing grants. A preset
change does not replace explicitly configured stages or model assignments.

Missing fields inherit. Scalars and arrays replace; arrays never append or merge
by ID. Named tables merge by key. `workflow.stages` replaces the complete stage
array. Empty arrays clear inherited arrays only where the result
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
| `models.<id>` | Explicit model name and requirements referencing an operator connection. |
| `checks` | Trusted local commands, additional GitHub checks, formatter, protected paths. |
| `limits` | Repository, PR, operation, and reserved allowances. |
| `workflow.stages` | Optional explicit ordered array of serial stages; replaces automatic planning. |
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
their connection, model name, and thinking requirements. All three objectives
share this minimal automatic plan in v1; they express model preferences, not
different concurrency modes. Explicit stages determine any additional passes.
Missing bindings produce setup guidance without downloading models or inference.
A zero-file repo policy is possible after operator setup; it cannot invent
connections, human allowlists, or trusted project checks.

## 4. Models and roles

Model, connection, and stage IDs use `[a-z][a-z0-9_-]*`. Stage IDs are unique across
the workflow; model/connection IDs are unique within their own collections.

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

# OpenRouter is also required in v1; the adapter knows its HTTPS endpoint.
[connections.openrouter]
provider = "openrouter"
credential_ref = "env:OPENROUTER_API_KEY"
```

The URL locates a server; it does not select a model or specify its API protocol.
Record requested and reported model/provider identities separately; missing
execution identity remains unknown.
Explain must show the configured protocol, endpoint, model, and requested settings.
Connection/model changes affect future policy identity, not substitutions into
saved sessions. Generic compatible inference, including LiteLLM and vLLM, remains
a later adapter extension. Metadata discovery does not imply inference support.
When added, a compatible connection will name its base URL and the exact model
alias exposed by that server; a loopback proxy is not proof of local inference.

`inference` is `local` (default) or `any`. `any` permits an authorized remote route,
not paid use by itself. A local proxy does not prove local inference. Tools,
multi-turn continuation, and reasoning capability are required for all model roles
in this draft. Registry permissions follow the task: review/evaluation
cannot edit; repair may edit within policy. Judges cannot execute
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
context fit RAM or will reason usefully. For OpenRouter, `context_tokens` bounds
Ferretta's conversation budget within verified capacity; it is not an Ollama
`num_ctx` request. The adapter maps `max_output_tokens` to `max_tokens` and accounts
for the selected model's reasoning/output token rules.

`fallbacks` is an ordered array of other model IDs, default empty. Targets must
have empty fallback lists; no recursive fallback graph. Reject missing, duplicate,
and self references. Fallback requires a recorded eligibility or safely reconciled
failure decision and starts a separately attributed session. It retains remaining
work allowances and permissions while using its own declared model settings.
Role requirements cannot silently weaken. Timeout/HTTP failure is not proof of
zero consumption: reconcile uncertainty or stop before more spending.

Reviewer, repairer, and judge are responsibilities, not
necessarily different models. Reusing a model in separate sessions must be
disclosed; it does not establish model diversity. Deterministic core policy
allocates work initially; it admits the next serial operation within allowances.
A general model allocator and configurable grading rubrics remain deferred.

### OpenRouter is a v1 adapter requirement

The [OpenRouter recipe](examples/openrouter-serial.toml) uses explicit model IDs
through an operator-owned connection. Local and cloud models may also alternate
in one chain; each role names its own model definition.

- Use `https://openrouter.ai/api/v1/chat/completions` with Bearer authentication
  resolved from the operator credential reference. The key is never repo policy,
  a tool environment variable, or public output. See [authentication](https://openrouter.ai/docs/api/reference/authentication).
- Include the role's allowed tool schemas each turn; retain assistant tool calls,
  call IDs, and matching results. Execute tools serially under core policy, even
  if one response requests several calls. See [tool calling](https://openrouter.ai/docs/guides/features/tool-calling).
- Translate the requested thinking mode using supported `reasoning` controls.
  Preserve provider-required continuation data, including opaque
  `reasoning_details`, privately across turns/checkpoints. Unsupported requirements
  fail eligibility; requested effort never becomes claimed observed effort.
  See [reasoning controls and continuation](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens).
- Require a concrete model ID, verified capabilities, and parameter-compatible
  upstream routing (`provider.require_parameters = true`). Disable provider
  fallbacks with `provider.allow_fallbacks = false`; model substitution is only
  through Ferretta's explicit `fallbacks`. Automatic model routers and implicit
  model fallback lists are outside v1. Record the upstream provider when reported.
  See [provider routing](https://openrouter.ai/docs/guides/routing/provider-selection).
- Before paid dispatch, reserve a bounded charge using input size, maximum output,
  all applicable billable units, and eligible provider prices. Record actual usage,
  cost, and request identity when reported. Unresolved cost retains the reservation;
  a timeout cannot trigger a blind retry. A model catalog entry is a candidate,
  not proof of affordable execution or tool/reasoning correctness.

An OpenRouter connection still requires `inference = "any"`; paid requests also
need operator permission and a positive remaining money allowance. A free route
needs verified zero pricing, supported capabilities, and the same compute limits.
No paid fallback is implied by the disappearance of a free route. Batch requests
remain a separate future feature; v1 uses the interactive tool loop.

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
| `limits.pr` | `compute`, `spend_usd`, `wall_clock`, `max_repairs`, `max_reviews`, `max_proposals`. |
| `limits.review`, `.repair`, `.evaluation` | Per-operation `compute`, `spend_usd`, `max_turns`. |
| `workflow.stages[].limits` | Cumulative `compute`, `spend_usd` for that stage instance. |
| `limits.reserve` | `final_review_compute`, `evaluation_compute`, `final_review_spend_usd`, `evaluation_spend_usd`. |

Proposed builtin ceilings, open for discussion:

| Scope | Defaults |
| --- | --- |
| PR | 60m compute; `"0.00"` dollars; unlimited wall clock; four repair attempts, 20 reviews, eight proposal versions. |
| Operation | Review 10m, repair 15m, **evaluation 10m**; unlimited turns within other bounds. Money inherits the PR ceiling unless narrowed. |
| Stage | Remaining enclosing allowance unless narrowed. `converge.max_repairs` is always explicit. |
| Reserves | 10m final review and 10m enabled evaluation; zero evaluation reserve when disabled; zero monetary reserves. |

Defaults do not enable repairs/paid routes. Reserves protect portions of the
existing PR allowance, not extra funds. Ordinary work cannot consume them. Final
review/evaluation draw on their own reserve plus unreserved remainder within their
operation limits; release safely unused reservations afterward. Reject reserves
exceeding a finite parent allowance. Paid final review/judging needs adequate
explicit monetary reservations before optional work; zero does not promise a paid
finale. Plans must be able to admit required final assessment under every limit.
Protect its remaining review counter slot too, and reserve paid final work
against the shared repository ledger so other PRs cannot spend it first. Counter
reservations are derived from the plan and shown by explain, not new allowances.

Count model and tool execution, including checks, retries, final review, and
evaluation. Sum operation durations; do not also charge enclosing stage elapsed
time. V1 executes these operations serially. Queueing/human waiting is excluded from compute.
Wall-clock allowance runs from PR creation and includes both. Provider request
duration is observed execution, not a claim about GPU utilization.

Durable PR counters span commits and attempts:

- New review sessions increment `max_reviews`; human-wait resumption does not;
  a fresh retry does.
- Admitted repair sessions increment `max_repairs`, even when unproductive.
  Turns and commits are not cycles.
- New proposal versions increment `max_proposals`; delivery/reconciliation of
  an existing version does not. Corrections may require another slot.

Stage loop limits and stage allowances cover that instance including retries;
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

## 7. Serial stages and data flow

`[[workflow.stages]]` is a native TOML array of tables. Array order is execution
order. Each stage completes before the next starts; no `execution` switch,
`after` graph, wave wrapper, or concurrency setting is needed in v1.

```toml
[[workflow.stages]]
id = "improve"
kind = "converge"
input = "pr.head"
reviewer = "worker"
repairer = "worker"
max_repairs = 2

[[workflow.stages]]
id = "final-review"
kind = "review"
input = "improve.revision"
reviewer = "assessor"
evidence = ["improve"]
```

This fragment omits model definitions, checks, allowances, and completion settings;
the [complete recipes](examples/README.md) include them. Reviewer/repairer may use
Ollama while assessor uses OpenRouter, or all may use the same adapter.

Every stage requires `id`, `kind`, and its kind-specific fields:

| Kind | Required fields | Optional fields | Output |
| --- | --- | --- | --- |
| `review` | `input`, `reviewer` | `evidence`, `limits` | Findings/LGTM for the input revision; no edits. |
| `converge` | `input`, `reviewer`, `repairer`, integer `max_repairs` | `evidence`, `limits` | Bounded review/repair work and last usable revision. |

Model fields name `models.<id>`. `input` is `"pr.head"` or
`"<stage-id>.revision"`. The first stage must use `pr.head`: the head pinned at
admission, never a moving branch. Every later stage must reference the immediately
previous stage's revision. A review passes that revision through unchanged; a
converge stage may return a checked repair. Reject branch resets to `pr.head`,
skipped predecessors, forward references, and cycles rather than approximating
independent candidates as a chain.

`evidence` (default `[]`) names earlier results on this chain to expose; ordering does
not automatically expose private conversations. All work also receives applicable
PR context and authenticated intent evidence.

Evidence may reference several prior assessments without creating a branch/join:
they already ran serially on the same revision lineage. Reject missing, duplicate,
and forward evidence references. Compile ordering edges and artifact inputs
separately; retain stable stage/round/attempt/effect identities. There are no
`select` or `integrate` stage kinds, `candidates`, or partial-join controls in v1.

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
An awaiting-human or uncertain stage also blocks its successors. Whole-job failure
still publishes captured status and evaluates available evidence within remaining
allowance. Carry unresolved findings forward; a new stage cannot erase them.
Repair candidates are private worktrees/retained commits until publication.
Branching candidates, selection, integration, child PR publication, Cloudflare
execution, and multiple-owner coordination remain future work.

### Compiled records and recovery

Compile source tables into a typed stage union; fields from another kind are
errors, not ignored options. Resolve string references to typed revision/result
handles before scheduling. Keep immutable node specifications separate from
execution state. Each node records its PR, input head/base, intent snapshot,
resolved policy, role, stage/round identity, and artifact dependencies.

Results retain the input/output commits, any checked candidate tree and parent,
findings and their sources, actual model provenance, and resource usage.
An unchanged input is a distinct result from a
checked repair candidate. `awaiting_human` carries its exact pending question and
saved session; `uncertain` carries the effect identity and outstanding reservation;
`incomplete` carries a reason and the evidence actually obtained. These must not be
independent flags capable of representing both accepted and blocked work.

Use the single owner's durable store to reconcile repeated notifications against
the same node/attempt/effect. Recovery reuses completed results and resumes only
supported transitions; it never blindly repeats an uncertain model call, question,
or push. This is the target contract, not a claim that arbitrary session
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
last in the array (or the only stage). Its input is the one output revision.
The reviewer receives the workflow outcome manifest, unresolved
blockers, earlier review/repair provenance, and exact revision regardless of extra
`evidence` references. An evidence list cannot hide known blockers.

`publish_repairs` defaults false. If true, once final review is ready, publish its
changed, locally checked input candidate with an expected-head lease, reconcile
publication, then start final review of that exact published head. Only this one
authority updates the parent PR; individual stages do not push themselves.
GitHub CI can run on the repairs while final review inspects them. An unchanged input needs no
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

The [Alpaca](examples/operator-alpaca.toml) and
[OpenRouter](examples/operator-openrouter.toml) profiles propose the companion shape:
`schema_version`, `connections`, `host`, `permissions`, and `defaults`.
Operator keys are rejected in repo/PR policy.

| Operator field | Contract |
| --- | --- |
| `connections.<id>.provider` | `ollama` or `openrouter`, both required in v1. Generic `openai_compatible` inference is deferred; unsupported adapters are errors. |
| `connections.<id>.endpoint` | Required explicit base URL for Ollama. Not allowed for OpenRouter, which uses its adapter's fixed HTTPS endpoint. |
| `connections.<id>.credential_ref` | Required for OpenRouter; v1 uses `env:NAME` with a valid environment variable name. Never a literal secret. Not an Ollama field in v1. |
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
Endpoint URLs must not embed credentials. Resolve an OpenRouter `credential_ref`
only in the adapter's operator context; missing/empty variables fail setup. The
service's private environment is configured outside the repository, independently
of desktop login. Never pass its key to tools, checks, child processes, session
records, or logs. Additional credential backends can follow separately.
Provider/endpoint grants and metadata still determine eligibility. A proxy on
loopback does not establish local inference or grant paid use.

V1 dispatches one workflow operation at a time; human waits release the execution
slot so another admitted PR may proceed. There is no configurable fan-out, even
on larger hosts. The local recipe passes each model's completed revision to the
next. A 64GB machine may still need reduced context; serial scheduling is not a
memory guarantee. Unknown memory requirements remain unknown. Residency preferences
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

Suggested delivery slices, with both adapters required before calling v1 runnable:

1. Agree on semantics; implement pure-Go parsing, typed validation, layered
   resolution, and offline explain output. Execution remains explicit.
2. Add the pure-Go OpenRouter adapter with explicit credential resolution,
   capability checks, tool/reasoning continuation, price-bounded admission, actual
   usage accounting, and safe treatment of unknown outcomes. Keep effects thin
   and inject clients/clocks. A text-only adapter is insufficient.
3. Run serial review/converge/final chains through Ollama, OpenRouter, and a mixed
   chain, building on bounded repair work (#9/#11). Include human waits, separate
   judging, cumulative accounting, and recovery. No branch/join syntax ships in v1.
4. Add public import and richer objective planning. Author an agent skill only
   once the contract and executable examples exist.

Behavior fixtures cover ordered revision/evidence handoff, rejection of branching
fields/inputs, stale head/base/checks, human waits/corrections, reserve protection,
unknown spending/publication, and restart without duplicated work. Both adapter
suites cover multi-turn tool calls, reasoning continuation, rejected capabilities,
and failed requests; OpenRouter also needs missing-key, rate-limit, token/cost,
and ambiguous-charge fixtures. Use fakes and explicit clocks for normal tests.
Real adapter validation belongs to a separately invoked networked suite, never
configuration loading or the normal tests. Release-test cadence remains undecided.

The broader orchestration work in #10 can later add independent candidates,
selection/integration, and parallelism. The [future sketch](examples/future/diverge-and-converge-workflow.toml)
has no assigned schema version and must fail v1 validation. Do not silently run a
branching document as a serial approximation.

## 11. Decisions to discuss before freezing v1

- **Scope settled:** serial stages, Ollama, and OpenRouter are v1; fan-out/fan-in
  are deferred. The flat `workflow.stages` spelling is the proposed representation.
- **Loop name:** is `converge` clear, or is `review_repair` better? The behavior
  includes clarification and bounded repetition, not arbitrary code.
- **Local escalation:** this recipe always runs both model passes. A cost recipe
  might instead skip the heavier pass on scoped LGTM; that gate needs explicit syntax.
- **Final findings:** the draft returns `needs_work`. Should a declared outer-round
  mechanism later admit another serial pass within cumulative limits/reserves?
- **Defaults:** ten minutes for judging and no default turn ceiling follow prior
  direction. Other numeric defaults here are proposals for discussion.
- **Patch size:** the original lines-of-code idea needs rules for generated files,
  deletions, and cumulative versus final diff; it is deferred rather than vaguely
  interpreted as a token/compute constraint.
