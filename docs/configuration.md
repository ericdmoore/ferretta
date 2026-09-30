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
