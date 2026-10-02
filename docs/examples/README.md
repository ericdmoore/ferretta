# Configuration recipes — v1 design drafts

These files describe proposed behavior under the [v1 specification](../config-v1.md).
**The current CLI does not execute this TOML.** The three original comment-only
intentions are now concrete syntax proposals. Numeric allowances are reviewable
choices, not adopted defaults or estimates of model cost/capacity.

| Recipe | What it demonstrates |
| --- | --- |
| [withConstraints.toml](withConstraints.toml) | Bounded review/repair/clarification, scoped allowances, reserved final review, and a ten-minute judge. |
| [ollama-on-64GB.toml](ollama-on-64GB.toml) | GPT-OSS then Qwen; the second pass consumes the first pass's revision and evidence. Each has its own bounded loop. |
| [diverge-and-converge-workflow.toml](diverge-and-converge-workflow.toml) | Independent candidates, selection of zero through four contributions, integration, and final review. |
| [operator-alpaca.toml](operator-alpaca.toml) | Companion model bindings, concurrency/residency preferences, and permission ceilings for the local recipes. |

All three recipes request controlled repair publication and leave merging disabled.
The operator must trust the project checks, supply GitHub App setup/allowlists,
and explicitly select a policy when support exists. Adapt `make check`/`make fmt`
for other projects. Secrets and endpoints do not belong in portable recipes.

The local profile uses previously selected model IDs, not verified availability or
memory guarantees. Qwen explicitly uses provider-default thinking; effective effort
remains unknown. GPT-OSS may fill several roles in separate sessions; the scorecard
must disclose shared model identity.

The divergent recipe requires operator routes `candidate-a`, `candidate-b`,
`candidate-c`, `synthesizer`, and `independent-judge`. These are setup requirements,
not invented OpenRouter model IDs. Choose capable models and grant paid use
explicitly; dollar ceilings are illustrative. No Cloudflare adapter or child-PR
publication is implied. Host concurrency one preserves the same independent
experiment, scheduled serially.

Read the spec's open decisions alongside these files. `max_repairs` counts worker
attempts, not turns. Child LGTM does not skip later required passes. Final review
always examines the designated resulting revision; Checks should expose which
recipe/stages ran and where constraints or capability gaps prevented completion.
