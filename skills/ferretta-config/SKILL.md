---
name: ferretta-config
description: Author, explain, or review Ferretta review-policy configurations and examples. Distinguish executable configuration from proposed TOML presets and workflow syntax; use when adapting policy to a repository or designing a configuration draft.
---

# Author Ferretta configurations

Help the user express how Ferretta should review submitted PRs: the objective,
eligible models, ordering, checks, allowances, and permitted effects. Produce
configuration the user can understand and edit, with an honest statement of
what their installed Ferretta can execute.

This is a repository skill. Resolve the links below relative to this file in a
Ferretta checkout; do not assume the target project is the Ferretta repository.
When sharing the skill, retain access to these versioned reference documents.

## Establish the supported surface

Read [configuration decisions](../../docs/configuration.md) first. For a target
installation, check its version and available help/schema without starting a
review. Distinguish the reference checkout's capabilities from the installed
binary's capabilities. If these cannot be verified, state the uncertainty.

At this skill's creation, runnable review policy is explicit Ollama JSON;
layered TOML, objective selectors, URL import, and wave scheduling are designs.
Recheck that status against the current implementation rather than treating
this historical note as a permanent restriction.

- For a runnable policy, use only fields and commands accepted by the target
  version. Consult its parser/help and the implemented-policy section of the
  configuration guide. Do not turn a TOML example into a claim of working support.
- For a design request, label the output as a draft. Preserve a requested syntax
  choice, but do not portray one candidate as adopted project-wide. Keep draft
  examples separate from active policy unless the user requested that destination.

## Match the policy to the user's intent

Inspect existing trusted policy and relevant repository check commands. Reuse
known model choices and constraints. Ask only for missing details that materially
affect the result; do not guess available models, credentials, spending approval,
or permission to publish repairs. Discovery offers candidates, not authorization.

Choose one primary objective: cost (the default), time, or quality. Read
[preset guidance](../../docs/presets/README.md) and the relevant TOML example when
working on objectives. Presets change routing and planning preferences within
existing permissions and allowances; price or token count is not proof of quality.

For explicit ordering, read [workflow alternatives](../../docs/workflow-syntax.md).
Named dependency stages and ordered serial/parallel waves remain candidates.
Use one form per draft. Explain its execution path, joins, and evidence flow;
validate references and acyclicity for dependency graphs. Parallel eligibility
does not guarantee simultaneous inference on the available hardware.

Use [architecture](../../arch.md) selectively for repair rounds, the hot seat,
fallbacks, accounting, and acceptance gates. Preserve these distinctions:

- At least one review is required; repair is optional. Failing initial CI is
  valid repair input. Repairs change the revision and require applicable review
  evidence for the resulting commit.
- Model turns, review/repair cycles, compute time, and wall-clock age are separate
  controls. Do not invent a turn cap when the user requested only a cycle limit.
  Compute includes model and tool execution, sums concurrent worker durations,
  and excludes human waiting. Local models still consume compute allowance.
- The hot seat recommends allocation. Core policy controls spending and effects.
  An Intern fallback must be explicitly eligible and meet the same acceptance
  requirements. Exhaustion yields an incomplete result, not automatic approval.
- LGTM is an earned result for an exact revision. Required checks and unresolved
  blockers cannot be waived by a preset, fallback, or synthesis consensus.

## Keep configuration authority explicit

The target layering is authorized PR settings, trusted repository settings,
user/service profile, then built-in defaults. Missing fields inherit; explicit
values replace; lists replace as a unit. For layering examples, show effective
values and their sources. A PR's proposed policy changes cannot grant that same
PR extra money or permissions. The configured service profile is independent of
who is logged into the host.

Portable policy refers to model connections; credentials, endpoints, watch lists,
and state paths belong to operator configuration in the target design. Current
JSON has an explicit endpoint field: follow the implemented format when authoring
runnable JSON rather than silently applying the future TOML separation.

For intent questions, use [proposal sessions](../../docs/proposals.md). Only
authenticated allowed humans establish intent. Models may propose, but cannot
author human decisions or grant themselves resources. Resource constraint markers
are separate from intent markers and must retain their documented implementation
status.

For sharing, follow the public-import section of the configuration guide. Import
is a proposed CLI authoring operation yielding a self-contained local file with
source provenance. Do not introduce runtime URL inheritance or private-repository
authentication. Private-source users copy the complete file themselves. Never
place secrets in configuration examples or provenance comments.

## Deliver and validate

Preserve existing files and unrelated settings. Explain any consequential changes
to checks, allowances, model eligibility, or effects. Authoring a configuration
does not itself authorize inference, command execution, GitHub writes, service
installation, or dependency downloads.

Include `schema_version = 1` in current TOML drafts. This denotes the proposed
configuration format, not the application's release version. Do not silently
migrate unknown schemas. Parse TOML/JSON syntax with an available parser, then
validate semantics against a supported schema or clearly identify them as draft
semantics. A successful syntax parse alone does not establish runtime support.

Deliver the file location, a short explanation of the expected execution path,
validation performed, and remaining setup or design choices. When editing the
Ferretta repository, follow its AGENTS.md checks before committing. Do not run
live reviews merely to validate an authored example.
