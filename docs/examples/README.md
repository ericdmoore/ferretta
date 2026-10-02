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
| [operator-alpaca.toml](operator-alpaca.toml) | Companion Ollama URL, concurrency/residency preferences, and permission ceilings for the local recipes. |

All three recipes request controlled repair publication and leave merging disabled.
The operator must trust the project checks, supply GitHub App setup/allowlists,
and explicitly select a policy when support exists. Adapt `make check`/`make fmt`
for other projects. Secrets and endpoints do not belong in portable recipes.

The local recipes name the previously selected model IDs directly; the profile
holds the Ollama URL. These are not claims of verified availability or
memory guarantees. Qwen explicitly uses provider-default thinking; effective effort
remains unknown. GPT-OSS may fill several roles in separate sessions; the scorecard
must disclose shared model identity.

The divergent recipe names one `litellm` connection and five illustrative
server-side model aliases. Bind the connection to the actual proxy URL in the
operator profile, for example:

```toml
[connections.litellm]
provider = "openai_compatible"
endpoint = "http://127.0.0.1:4000"
```

Configure the proxy to expose those model aliases, or replace the recipe's `model`
values with its existing model names. The aliases are setup requirements, not
claims about particular OpenRouter models, thinking controls, or local inference.
Choose capable models and explicitly grant any paid use; dollar ceilings are
illustrative. There is no Ferretta `routes` table. Cloudflare execution and child-PR
publication remain deferred. Host concurrency one preserves the independent
experiment while scheduling it serially. Compatible inference is not implemented
by the current CLI; these remain design examples.

Read the spec's open decisions alongside these files. `max_repairs` counts worker
attempts, not turns. Child LGTM does not skip later required passes. Final review
always examines the designated resulting revision; Checks should expose which
recipe/stages ran and where constraints or capability gaps prevented completion.
