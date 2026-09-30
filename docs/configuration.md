# Configuration decisions and implementation boundary

## Agreed target

The effective policy resolves field by field:

| Priority | Source | Authority |
| --- | --- | --- |
| 1 | Authorized per-PR settings | Scoped to a PR; spending/permission increases require an allowlisted human |
| 2 | Root `ferretta.toml` from a trusted repository revision | Portable repository policy |
| 3 | CLI user/service profile | Personal or installation defaults, optionally set through `init` |
| 4 | Built-in system defaults | Usable defaults, with `cost` as the primary objective |

Absent fields inherit; explicit values replace. Lists replace as a unit. Invalid
configuration fails visibly instead of falling through to a more permissive
layer. Show the effective settings with their sources, and bind that result to
each review. A policy change submitted in a PR is evidence under review, not
authority to grant that PR more money or tool access.

No root file is required for default policy. Connections, repository watch lists,
credential references and state/workspace paths belong to operator configuration,
not the PR override plane. Explicit CLI objective overrides affect that invocation
within existing permissions. A background service uses its configured profile,
not the defaults of whoever happens to log in to the machine.

## Objectives

Draft [TOML presets](presets/README.md) give `cost`, `time`, and `quality` a
shared proposed structure, with separate routing and planning preferences.
They are design examples, not configuration accepted by the current CLI.
The separate [workflow syntax comparison](workflow-syntax.md) records both
dependency stages and ordered waves without selecting either format.

| Preset | Primary optimization | Unchanged requirements |
| --- | --- | --- |
| `cost` (default) | Minimize additional spending | Capabilities, authorized routes, hard limits and acceptance gates |
| `time` | Minimize time to an acceptable result | Same |
| `quality` | Seek stronger review evidence within allowances | Same |

The eventual UI makes switching easy, for example `ferretta config set objective
quality` or `ferretta review --objective time`. **These commands/flags are not
implemented yet.** Startup should identify the effective objective, its source,
and how to change it. Plans follow the objective and resources; advanced policies
may specify model waves explicitly. A preset cannot silently opt into a paid
provider or weaken the definition of LGTM.

## Implemented surfaces

- GitHub App connection: strict machine JSON in `ferretta/github.json` beneath
  `os.UserConfigDir()`, or the absolute path in `FERRETTA_GITHUB_CONFIG`. Contains
  client/installation IDs and an absolute private-key path, never the key itself.
- Onboarding: `init` discovers metadata and creates a new Ollama policy; `doctor`
  checks readiness without inference/check execution; `demo` shows fictional
  reports. `model test` explicitly runs two bounded inference turns.
- Manual review: explicit trusted `.ferretta/review.json`, selected with
  `review --policy`. The current adapter is Ollama only.
- Service intake: `service run --repo owner/repo` (repeatable), `--state` (absolute
  private directory), `--interval` (default `1m`, range `5s`–`24h`), and `--once`.
  Default state is `ferretta/state` beneath `os.UserConfigDir()`; boot service
  definitions always specify a service-owned absolute path.
- Inspection: `service status --state /absolute/path` reads existing SQLite state
  without credentials, network calls, inference, or starting another owner.

No TOML loader, layered resolver, objective selector, persistent watch-list editor,
subscription auth or encrypted secret database is shipped in this slice. These
decisions establish the contract; the complete TOML schema and JSON migration
will follow implementation of those policy surfaces. Existing JSON remains
explicit and is never silently overwritten or interpreted as TOML.

## Current Ollama JSON policy

`init` writes the selected model/endpoint and a required check after confirmation.
It does not yet save layered user defaults or TOML. Existing files are preserved.

| Field | Meaning |
| --- | --- |
| `provider`, `endpoint`, `model` | Explicit `ollama` route at an HTTP loopback endpoint and installed model ID |
| `thinking` or `effort` | Exactly one: `thinking: "enabled"` for boolean controls, or `effort: "low"`, `"medium"`, `"high"` for named controls |
| `context_tokens` | At least 8,192, bounded by provider-reported model capacity; setup defaults to 16,384; omitted legacy field remains 65,536 |
| `max_turns` | Nonnegative; 0 disables the turn ceiling; setup uses 100; counts model responses across resumed segments |
| `max_tokens_per_turn` | At least 256; setup uses 4,096; must leave at least 2,048 tokens of input room in context |
| `timeout_seconds` | Nonnegative seconds; 0 disables the deadline; setup uses 600; proposal sessions exclude human waiting |
| `checks` | Nonempty list of command argument arrays; trusted operator-selected commands |

Unknown fields, contradictory thinking controls and invalid limits are rejected.
Metadata must establish tools, thinking, requested control support and sufficient
context capacity. Older Ollama versions lacking control metadata have explicit
compatibility for GPT-OSS named effort and Qwen3 boolean thinking; other unknown
controls are rejected. Remote-model metadata is rejected by the local-only route.
Requested settings remain separate from provider-reported execution details.
The context admission check uses Ollama-reported prompt tokens for the unchanged
message prefix when available, plus encoded bytes for newly appended messages,
a framing reserve and output allowance. Before a usage observation, the complete
input is conservatively bounded by bytes. It is not an exact local tokenizer.


For local exploration, this repository's policy selects `gpt-oss:20b`, medium
requested effort, 131,072 context tokens, 16,384 output tokens per reply, and no
turn/deadline ceiling. These are explicit repository choices; `init` still ships
bounded defaults. Cancellation remains available. Context capacity, required
checks, human identity and valid tool arguments remain enforced.

Setup exposes `--max-turns`, `--max-output-tokens`, `--timeout`, and `--context`.
For example, `--max-turns 0 --timeout 0 --context 131072
--max-output-tokens 16384` creates a more permissive local policy. Existing
policies are never overwritten. Provider prompt counts anchor context admission; unseen additions retain a
conservative byte bound. Paged evidence reduces input growth;
automatic compaction is not implemented.

Proposal posting is an operator-selected effect: `review --publish-proposals
--humans login1,login2`. The allowlist is pinned in the private SQLite session.
`review --resume /absolute/session/path` polls for human replies using the same
repository, PR, policy and GitHub App. See [proposal sessions](proposals.md).
