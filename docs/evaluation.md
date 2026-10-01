# Automatic reviews and scorecards

The prototype adds an explicit `service watch` command for **one selected PR**.
It discovers the PR through the GitHub App, publishes a starting comment, reviews
its exact commits, publishes the verdict, and runs a separate read-only judge
session that publishes a scorecard. It polls authenticated human replies when a
review pauses for an intent question. It does not repair or merge.

`service run` remains intake only. Neither command upgrades itself into the other.
The [implementation plan](review-evaluation.md) records the broader design and
remaining work; this page describes runnable behavior.

## Configure the two roles

The reviewer uses the existing trusted JSON policy, including required checks.
The judge uses the same route/settings vocabulary but **must omit executable
checks**. Both adapters currently require local Ollama. No paid fallback is
implemented. For example, save this as a trusted judge policy outside PR control:

```json
{
  "provider": "ollama",
  "endpoint": "http://127.0.0.1:11434",
  "model": "gpt-oss:20b",
  "effort": "medium",
  "context_tokens": 131072,
  "max_turns": 0,
  "max_tokens_per_turn": 16384,
  "timeout_seconds": 600
}
```

This example allows unlimited judge turns within a ten-minute active-stage
allowance. Choose installed models with sufficient capacity; model names do not
establish capability. Reviewer and judge may use the same model in separate
sessions, but the scorecard discloses that this is not an independent model
opinion. Requested effort/thinking and provider-reported details remain distinct.

## Start a watcher

Run from a checkout whose origin matches the target repository. Provision the
[App connection](github-app-auth.md), use one private state directory for the
installation, and select a PR explicitly:

```sh
bin/ferretta service watch \
  --repo owner/repo --pr 123 \
  --state /absolute/private/ferretta-state \
  --review-policy /absolute/trusted/review.json \
  --judge-policy /absolute/trusted/judge.json \
  --humans your-github-login
```

The command runs in the foreground until interrupted. A missing or draft target
waits without inference or publication; other PRs are observed but never
dispatched. `--interval 5s` adjusts the delay after each sweep (default one minute).
`--once` advances available stages once, stopping at a human wait or terminal
outcome. A processed incomplete result is still a successful watch invocation;
inspect the verdict and scorecard for the actual outcome.

The starting comment names both models and settings, exact head/base commits,
policy hashes, active allowances, and a plain-language plan. Review and evaluation
allowances are separate and cumulative across revisions of the PR. Zero disables
the corresponding limit. Active-stage accounting includes setup overhead and
excludes time awaiting a human; model/tool worker durations are also recorded.
There is no PR-age deadline, repair cycle, paid routing, or monetary meter yet.
Unknown token usage and unmeasured local electricity/hardware costs are explicit.

The reviewer can run only trusted configured checks. These execute PR code as the
OS user: worktrees and environment filtering are not a security sandbox. Keep
credentials outside the checkout and use a suitably isolated execution account.
The judge cannot execute checks, change files, publish, confirm intent, or allocate
resources. It can read exact-commit files/diffs, search, and inspect saved evidence.

## Read the scorecard

Rubric `review-roles-v1` separates **worker output** from the **oversight decision**.
Each has correctness, evidence, intent alignment, judgment, actionability, and
communication dimensions. Scores are 0 (material failure), 1 (substantial gaps),
2 (adequate and supported), or 3 (strong handling of relevant subtleties).
Missing evidence or inapplicability receives `not_assessed`, not zero.

The initial PR roll-up retains both roles and the revision's actual review status;
there is no average or score threshold that grants approval. A high grade cannot
turn failed checks, an incomplete run, or an unresolved question into LGTM.
Grades are automated opinions, **not verified correctness**. The judge sees
external output and read-only evidence, without the reviewer's private transcript
or model identity. Source IDs are validated; whether they substantiate a claim
still requires evaluation and calibration. An evidence index describes citations,
and full source results remain in the private local record.

A first live experiment completed using GPT-OSS for both roles, but its uniformly
high scores and generic explanations were not useful calibration. The rubric now
asks for specific supporting claims, distinguishes adequate from exceptional work,
and explicitly rejects “checks passed” as proof of no defects. This prompt change
is not proof that grading is reliable. Use sampled human audits and curated cases
before using score trends for resource allocation. No recursive judge-of-judge
loop is enabled.

## Grade an existing manual review

```sh
bin/ferretta eval --run .ferretta/runs/pr-123-example --policy /absolute/trusted/judge.json
```

This creates a new `evaluation-*` directory with `scorecard.json` and a private
`session.json`, preserving the original report and earlier assessments. It does
not rerun checks/review or post to GitHub. Exit 0 means structured grading
completed, 2 means evaluation was incomplete, and 1 means setup/output failed.

For an ordinary manual review, `review --eval-policy PATH` grades a terminal
report immediately after saving it. Combine proposal publication and automatic
reply handling through `service watch`; the manual evaluation hook does not
combine with `--publish-proposals` or `--resume`.

## Restart and failure behavior

SQLite retains revision attempts, model/tool usage, private conversations, human
decisions, and publication states. Keep the directory private and use the same
`--state` on restart. The OS lock protects that directory only; different state
directories do not coordinate ownership or prevent duplicate work.

A published milestone has a stable effect identity. Before retrying a missing
response, Ferretta reconciles the exact App-authored comment. Ambiguous effects
are never blindly reposted. Restarts during model execution produce a visible
incomplete result and preserve uncertain consumption; they do not repeat the
model call. Subsequent dispatch is blocked pending operator reconciliation.
There is no reconciliation/allowance-reset UI yet; do not delete state to retry.
Known human waits resume automatically on the same revision and policy.

Completed revisions do not spend again on repeated polls or a force-push back to
an already observed head/base pair. New revisions retain history and cumulative
allowances. Changed policy or human allowlist requires operator reconciliation.
A superseded run never approves the new head. Watch stdout exposes phases,
comment URLs, and active time; `service status` remains an intake snapshot, not a
workflow report or proof of process liveness.

If evaluation fails, publish an explicit incomplete scorecard and preserve the
review verdict. A local provider HTTP 500 was observed during development; a
separate explicit evaluation attempt succeeded. Completion does not validate the
grades. Automatic retries, general DAG/parallel scheduling, multi-worker roll-ups,
monetary reservations, constraint grants, and repair/merge execution remain future
work. Boot templates still start intake only; installing this prototype does not
silently enable model execution at boot.
