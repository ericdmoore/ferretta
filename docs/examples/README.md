# Configuration recipes — v1 design drafts

These files describe proposed behavior under the [v1 specification](../config-v1.md).
**The current CLI does not execute this TOML.** V1 is scoped to serial execution
with Ollama and OpenRouter, using the same review/repair semantics. Numeric
allowances are reviewable choices, not adopted defaults or estimates of model
cost/capacity. Field names remain draft.

| Recipe | What it demonstrates |
| --- | --- |
| [withConstraints.toml](withConstraints.toml) | Bounded review/repair/clarification, scoped allowances, reserved final review, and a ten-minute judge. |
| [ollama-on-64GB.toml](ollama-on-64GB.toml) | GPT-OSS then Qwen; the second pass consumes the first pass's revision and evidence. Each has its own bounded loop. |
| [openrouter-serial.toml](openrouter-serial.toml) | GPT-OSS 20B review/repair, then GPT-OSS 120B final review; explicit money/compute limits and a separate judge. |
| [operator-alpaca.toml](operator-alpaca.toml) | Companion Ollama URL, residency preferences, and permission ceilings for the local recipes. |
| [operator-openrouter.toml](operator-openrouter.toml) | Companion OpenRouter credential reference and explicit paid-inference grant. No secret value. |

All three workflow recipes request controlled repair publication and leave merging disabled.
The operator must trust the project checks, supply GitHub App setup/allowlists,
and explicitly select a policy when support exists. Adapt `make check`/`make fmt`
for other projects. Secrets and endpoints do not belong in portable recipes.

The local recipes name the previously selected model IDs directly; the profile
holds the Ollama URL. These are not claims of verified availability or
memory guarantees. Qwen explicitly uses provider-default thinking; effective effort
remains unknown. GPT-OSS may fill several roles in separate sessions; the scorecard
must disclose shared model identity.

Each `[[workflow.stages]]` table is the next step in one chain. The first starts
at `pr.head`; later steps consume the immediately previous step's `.revision`.
Review passes it through unchanged; converge may produce a checked repair. Several
earlier reports can be supplied as evidence without creating parallel branches.

The OpenRouter recipe directly names `openai/gpt-oss-20b` and
`openai/gpt-oss-120b`, listed in the [public model catalog](https://openrouter.ai/api/v1/models)
when this draft was written. Eligibility and pricing must be checked before use;
the examples promise neither availability nor that a given budget will finish.
The operator profile references `env:OPENROUTER_API_KEY`; the actual value stays
in the operator's private environment, excluded from tools/checks and logs.
Final review and judging have protected money and compute within the total PR
allowance. Reusing the assessor as judge is disclosed, not independent validation.

To mix local and cloud roles, provide both operator connections and select the
appropriate `connection` and exact `model` in each model definition. Stage syntax
does not change. This is a v1 requirement, not a shipped capability.

## Future branching example

[diverge-and-converge-workflow.toml](future/diverge-and-converge-workflow.toml)
retains the independent-candidate, selection, and integration experiment for
later. It has no assigned schema version and is **invalid as v1 configuration**.
Scheduling independent branches one at a time would still require branch/join
semantics, so that is also deferred. Its LiteLLM connection and proxy aliases are
illustrative future adapter settings. See the [workflow alternatives](../workflow-syntax.md)
for the unresolved branching authoring choices.

Read the spec's open decisions alongside these files. `max_repairs` counts worker
attempts, not turns. A stage LGTM does not skip later required passes. Final review
always examines the designated resulting revision; Checks should expose which
recipe/stages ran and where constraints or capability gaps prevented completion.
