# Bounded repairs

`service watch --repair-policy /absolute/path/repair.json` opts into one repair
worker at a time. Without this flag, the watcher continues to review and grade
without editing or pushing. Repair requires Checks publication, a same-repository
PR branch, and the GitHub App installation's **Contents: read and write** grant.
Approve the changed permission on the installation as well as on the App.

A run follows this sequence:

1. Review the submitted revision and publish findings or LGTM.
2. When there are concrete findings or failing trusted checks, admit a repair
   within the cumulative cycle and compute allowances. Initially failing checks
   are a reason to repair, not an admission barrier.
3. Edit an isolated persistent worktree, run all trusted checks on the edited
   tree, and create an immutable candidate commit with provenance.
4. Publish that commit to the existing PR branch only if its current head still
   matches the input. On the next poll, review the new revision from scratch.
5. Publish a scorecard for the final review. When repair is incomplete or cannot
   be admitted, retain the findings and grade the original review; exhaustion
   never becomes LGTM.

The input revision has a **Ferretta / repair** Check with model, requested
thinking/effort, constraints, progress, outcome, and candidate commit ID. Its
review verdict is retained. After publication, its queued evaluation is cancelled
in favor of the new revision's review and evaluation Checks. Normal repository
CI also runs on the pushed commit according to its GitHub workflows. Ferretta
does not combine GitHub CI results into merge authorization or merge anything;
local required checks and external CI remain separately visible. An advisory
LGTM on the old revision cannot approve the new revision.

## Policy

Use [the local repair example](https://github.com/ericdmoore/ferretta/blob/main/docs/examples/repair-local.json) as a starting point:

```json
{
  "model_policy": {
    "provider": "ollama",
    "endpoint": "http://127.0.0.1:11434",
    "model": "gpt-oss:20b",
    "effort": "medium",
    "context_tokens": 131072,
    "max_turns": 0,
    "max_tokens_per_turn": 16384,
    "timeout_seconds": 1200,
    "checks": [["make", "check"]]
  },
  "max_cycles": 2,
  "formatter": ["make", "fmt"],
  "protected_paths": ["go.mod", "go.sum", ".go-version", ".golangci.yml"]
}
```

`max_cycles` must be 1–100 and the repair timeout must be positive. The timeout
is a cumulative active allowance for this PR under the pinned policy, covering
model, tools, workspace setup, candidate creation and publication. Human waits
are excluded. New revisions and retries do not reset these allowances. Review
and judge allowances remain separate; repair cannot borrow them. A repair is
not admitted after either of those allowances is already exhausted. This is
not a prediction that the remaining final-review time will be sufficient.
`max_turns: 0` allows as many turns as the active allowance and context permit.
Only local Ollama inference is supported, so dollars remain unmetered.

`formatter` is optional. Commands are argv arrays without a shell, supplied by
trusted operator configuration, not discovered from PR content. List additional
repository check, lint, performance and policy controls in `protected_paths`;
each entry protects that file or directory recursively, with case-insensitive matching for portability. Built-in protection
covers `.git`, `.ferretta`, `.github`, `.githooks`, `.gitattributes`, `.gitmodules`,
`.gitignore`, `.coverage-baseline`, `AGENTS.md`, `ferretta.toml`, `Makefile`, and
`scripts`. Changes to protected controls require a separate human-managed change.
Tests for behavior can be edited, but the model is instructed not to weaken
requirements, and the resulting commit receives a fresh review.

The model gets `apply_patch` (an exact text replacement or new-file creation),
`run_tests`/`run_checks`, and an optional `run_formatter`, alongside inspection
and proposal tools. Inspection sees uncommitted edits; `read_diff` includes both submitted PR changes and repairs. File tools reject
symlinks and paths outside the workspace. Each edit/formatter invalidates check
evidence, and finishing verifies the exact Git tree against the successful
checks. Checks that mutate files must be rerun on the resulting tree.

These worktrees and environment filters are not an OS security sandbox. Trusted
checks execute PR code as the service account. Use a suitably isolated account
and machine; a model never receives the publication token through its tools.

## Running

From the trusted repository checkout:

```sh
ferretta service watch \
  --repo owner/repo --all-prs --authors trusted-human,trusted-agent \
  --state /absolute/private/repair-watcher-state \
  --review-policy /absolute/private/review.json \
  --repair-policy /absolute/private/repair.json \
  --judge-policy /absolute/private/judge.json \
  --humans trusted-human
```

Policies, their presence/absence, publication mode, and human allowlist are pinned
in the durable job. Repair-enabled jobs use workflow version 2 so older binaries refuse to discard their repair state. Changing a policy for an existing job fails explicitly and
needs operator reconciliation. Do not delete state to bypass an allowance or an
uncertain effect. Use a fresh, not-previously-admitted PR to test a new policy.

Reviewers and repairers can request consequential intent clarification through
`PROPOSED-{Topic}-vN::`. Only authenticated allowlisted humans can supply
`CORRECTED` or `CONFIRMED` decisions. The workspace and session persist while
waiting; resumption reruns checks. Confirmed intent is carried to a fresh review
of Ferretta's exact published candidate, with source evidence retained. General
reconciliation across arbitrary human commits and edited/deleted intent comments
remains [issue #7](https://github.com/ericdmoore/ferretta/issues/7).

## Recovery and publication

A repair has stable job/run/attempt identities, a fixed parent, an immutable tree,
and a deterministic commit timestamp and message. Candidate refs live under
`refs/ferretta/repairs/`; workspaces live under the private state directory's
`repairs/`. They are retained for diagnosis, not uploaded as build artifacts.
Automatic retention cleanup is not implemented yet.

The publication executor separately obtains a repository-scoped contents-write
installation token. It verifies the candidate is exactly one child of the
reviewed head, then uses an explicit `--force-with-lease=<branch>:<expected-head>`
compare-and-swap. Despite Git's option name, the validated candidate only appends;
the expected-head lease prevents overwriting an intervening human commit or
branch reset. No unqualified force push or implicit personal credentials are used.

Before a push, the durable effect becomes uncertain. A lost response is reconciled
by finding the exact candidate at the PR head, or proving it is an ancestor of
that head. An absent candidate is not evidence that the push never happened;
Ferretta stops for operator reconciliation instead of blindly retrying. A failure
known to precede dispatch can retry the same effect. Interrupted inference stops
as incomplete with uncertain consumption rather than automatically replaying it.
Check publication uses the existing durable reconciliation rules.

Parallel repair waves, candidate ranking/integration, model residency scheduling,
and child PRs belong to [issue #10](https://github.com/ericdmoore/ferretta/issues/10).
Automatic merging, paid routing, resource grants, full TOML configuration, and
grading calibration are outside this repair slice. Repair Checks do not yet have
a dedicated rerun action; review/evaluation retry behavior is unchanged.
