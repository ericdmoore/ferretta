# Objective presets (design draft)

Ferretta should ship three named presets: **cost**, **time**, and **quality**.
These TOML files propose their common vocabulary. They are design examples, not
runtime configuration: the current CLI does not load them. The schema version
is a proposed format version, not a Ferretta release number.

| Preset | Model preference | Planning preference |
| --- | --- | --- |
| [cost.toml](cost.toml) | Lowest additional cost among eligible models | Start with one reviewer; escalate when evidence warrants more work. |
| [time.toml](time.toml) | Lowest expected completion latency | Run useful independent work concurrently when resources permit. |
| [quality.toml](quality.toml) | Strongest relevant review evidence | Seek independent observations, synthesize findings and dissent, then review the integrated result. |

These are preferences within authorized constraints, not guarantees. Cost does
not mean always local; time does not mean always remote; quality does not mean
always the most expensive model. Local inference still consumes compute.
Unknown prices, latency, or capabilities remain unknown. Quality estimates must
come from relevant evaluation or explicit operator preferences, not token use,
price, or model self-confidence. Detailed ranking and tie-breaking remain to be
specified; these names do not imply a working optimizer.

## Simple selection

A repository should need only this root `ferretta.toml` to choose a preset:

```toml
schema_version = 1
objective = "quality"
```

No file means the built-in `cost` preset. The planned CLI should also support
`ferretta config set objective quality` for a user/service profile and
`ferretta review --objective time` for one invocation. These commands are not
implemented. Users should not need to copy an entire preset to change objectives.

Resolve the objective first using authorized PR settings, trusted repository
settings, the user/service profile, then the built-in default. An explicit
invocation objective selects the preset for that invocation within the same
permissions. The selected preset supplies built-in preferences; it is not an
additional override layer. Resolve remaining fields through the existing
configuration ladder. Explicit higher-layer routing or planning preferences
survive a preset change and must appear in the effective configuration.

Absent fields inherit; explicit values replace; lists replace as a unit.
Reject unknown objectives, schema versions, fields, and invalid values rather
than silently choosing a different preset. Display the selected preset,
effective settings, their sources, and any explicit overrides. Save that
resolved policy with each review so later default changes do not alter an
in-flight job.

## Shared rules

All three presets use the same acceptance requirements: at least one review,
required passing checks, resolved blocking findings and intent questions, and
final acceptance tied to the exact resulting revision. An earned LGTM ends the
applicable scope; none of these preferences requires manufacturing extra work.

Routing first filters for authorized connections, required capabilities, and
remaining resources. Only then does the objective rank eligible choices. Missing
eligible routes produce an actionable configuration/incomplete result. Presets
cannot silently authorize paid inference, substitute an unapproved model, or
assume a discovered local endpoint serves local inference.

Planning preferences do not mandate a fixed number of models or waves. Quality
may use one eligible model when that is all policy permits; report that there
was no independent second opinion. Time may run serially on constrained hardware.
Explicit advanced DAG policies take precedence over these planning preferences.
Their syntax is still open. Compare the two candidate
[workflow formats](../workflow-syntax.md): dependency stages and ordered waves.

Repair, publishing, and merging remain subject to separately authorized effects.
The hot seat recommends allocation; the core enforces policy. An explicitly
configured local Intern can be a fallback, subject to the same acceptance gates
and remaining compute allowance.

Presets do not reset or increase budgets. Money, model-and-tool execution time,
repair cycles, and optional wall-clock deadlines are separate constraints.
Parallel execution sums worker time; human waiting is excluded from compute.
Do not impose a model-turn ceiling merely by selecting an objective: model
turns and review/repair cycles are different controls. The v1 draft proposes
concrete resource defaults and fields; they remain open for discussion.

## Scope of this draft

The [v1 specification draft](../config-v1.md) now proposes the broader schema and
[complete recipes](../examples/README.md). These preset preference files remain
compatible with that proposal; neither the schema nor planner is implemented.

The only proposed fields in these files are `schema_version`, `objective`,
`routing.prefer`, and `planning.strategy`, with the values shown above. This
gives us three reviewable preset personalities without pretending the full
policy schema is settled.

The v1 draft proposes model aliases and role assignments, capability/effort
requirements, checks, limits, permitted effects, fallbacks, and explicit waves.
Those details remain under discussion and are not accepted by the CLI.
Credentials, endpoints, watch lists, and state paths remain operator settings;
portable policy refers to configured connections. See
[configuration decisions](../configuration.md) for existing surfaces and
[architecture](../../arch.md) for the broader contract.
