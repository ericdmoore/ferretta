# Workflow syntax alternatives (design draft)

**Decision open.** These are two candidate authoring formats for `ferretta.toml`,
recorded for comparison. Neither is implemented or selected. They complement
the [objective presets](presets/README.md): objectives express preferences;
an explicit workflow describes the work and its ordering.

Both examples describe the same workflow:

1. Two reviewers independently examine the same submitted revision.
2. A synthesizer receives both results, retaining disagreements and provenance.
3. A final reviewer evaluates the revision and accumulated evidence.

Model names below are illustrative aliases, not permissions to use providers or
claims that those models are available. The examples omit connections, limits,
checks, and effects so the ordering syntax is easy to compare.

## Option A: named stages with dependency edges

TOML's `[[stages]]` is an array of tables. Each stage has a stable identifier;
its `after` array names prerequisite stages.

```toml
schema_version = 1

[[stages]]
id = "review-a"
role = "review"
model = "qwen"
after = []

[[stages]]
id = "review-b"
role = "review"
model = "gpt-oss"
after = []

[[stages]]
id = "synthesize"
role = "synthesis"
model = "hot-seat"
after = ["review-a", "review-b"]

[[stages]]
id = "final-review"
role = "review"
model = "final-reviewer"
after = ["synthesize"]
```

The two root reviews are eligible to run concurrently. Synthesis joins their
results; final review follows synthesis. File order does not impose execution
order. Changing `review-b` to `after = ["review-a"]` makes those reviews serial.

This represents a **directed acyclic graph**, not a tree: a node can have several
parents, so branches can join. Validation must reject duplicate IDs, missing
references, self-dependencies, and cycles.

The strength is precise dependencies. One branch can proceed while an unrelated
branch is still running. The cost is that the author must track edges and read
across stage definitions to understand the whole plan.

## Option B: ordered waves with explicit execution mode

TOML's `[[waves]]` is also an array of tables. Waves execute in listed order.
Each wave declares whether its members run serially or may run concurrently.
Nested `[[waves.stages]]` tables belong to the most recently declared wave.

```toml
schema_version = 1

[[waves]]
id = "independent-reviews"
execution = "parallel"

[[waves.stages]]
id = "review-a"
role = "review"
model = "qwen"

[[waves.stages]]
id = "review-b"
role = "review"
model = "gpt-oss"

[[waves]]
id = "assessment"
execution = "serial"

[[waves.stages]]
id = "synthesize"
role = "synthesis"
model = "hot-seat"

[[waves.stages]]
id = "final-review"
role = "review"
model = "final-reviewer"
```

The first wave contains parallel reviews. The second waits for that wave, then
runs synthesis followed by final review. Changing the first wave to
`execution = "serial"` orders its reviewers as listed.

The earlier conversational shorthand, `reviewers = ["qwen", "gpt-oss"]`, could
be convenient for a review-only wave. This expanded candidate uses stage tables
so IDs and roles remain explicit, including synthesis and eventual repair.
Whether to offer a shorthand is also undecided.

The strength is an easily scanned sequence of parallel and serial groups.
The cost is a barrier between waves: work in the next wave waits even when it
depends on only part of the previous wave. Arbitrary overlapping branches would
need additional syntax or the dependency format.

## Comparison

| Concern | Dependency stages | Ordered waves |
| --- | --- | --- |
| Native TOML structure | Stage table array plus dependency arrays | Wave table array plus nested stage table arrays |
| Serial execution | An explicit edge to the preceding stage | Listed order inside a serial wave |
| Parallel execution | Stages whose dependencies are satisfied | Members of a parallel wave |
| Join | Name all prerequisite stages | Boundary before the next wave |
| Partial dependencies | Directly expressible | Not expressible in this simple candidate |
| Main authoring burden | Track IDs and edges | Choose wave boundaries and member order |

Either format can produce the durable internal DAG already described in the
[architecture](../arch.md). Supporting both as public syntax is an option, not
a decision: it increases validation, documentation, and migration work. If both
are supported, a policy should select one form rather than mix ambiguous ordering
rules. Effective-plan inspection could show the compiled DAG for either form.

## Shared semantics and remaining questions

- **Readiness is not authorization.** Parallel means eligible for concurrency;
  hardware capacity, capabilities, permissions, and remaining resources still
  control dispatch. These examples do not promise simultaneous inference.
- **Completion is not approval.** A join must retain failed, missing, and
  incomplete results. Ordering alone cannot make them successful or permit merge.
  Exact downstream admission rules for these outcomes still need syntax.
- **Order and evidence flow are related but distinct.** Parallel reviewers use
  the same baseline independently; synthesis consumes both results. We still
  need to specify how a stage selects evidence and which revision it evaluates,
  especially after repairs.
- **Conditions remain open.** We need a representation for repair only when
  needed, human-waiting states, fallback selection, and hot-seat recommendations.
  Neither example introduces a general expression language.
- **Review/repair repetition must remain bounded by policy.** A durable DAG
  cannot contain a back edge. A scheduler can admit another round as new nodes,
  preserving identities and cumulative accounting. Authoring syntax for those
  rounds remains open; this does not imply a model-turn cap.
- **Acceptance is unchanged.** Child LGTM closes its scope. Required final review,
  passing checks, resolved blockers, and exact-revision evidence still govern
  completion and separately authorized merge effects.

The next design decision is which representation feels clearest for ordinary
repository authors, and whether the other should remain an internal form or an
advanced public option. Neither choice is required to review these drafts.
