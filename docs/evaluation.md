# Automatic reviews and scorecards

`service watch` reviews **one selected PR** or **all eligible PRs in a repository**.
It discovers the PR through the GitHub App, creates review and evaluation Checks,
reviews its exact commits, publishes the verdict, and runs a separate read-only
judge session that publishes a scorecard. It polls authenticated human replies when a
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
  --publication checks \
  --humans your-github-login
```

The command runs in the foreground until interrupted. A missing or draft target
waits without inference or publication; other PRs are observed but never
dispatched. `--interval 5s` adjusts the delay after each sweep (default one minute).
`--once` advances available stages once, stopping at a human wait or terminal
outcome. A processed incomplete result is still a successful watch invocation;
inspect the verdict and scorecard for the actual outcome.

### Automatically pick up new PRs

Replace `--pr 123` with `--all-prs --authors your-github-login`:

```sh
bin/ferretta service watch \
  --repo owner/repo --all-prs --authors your-github-login,trusted-agent-login \
  --state /absolute/private/ferretta-state \
  --review-policy /absolute/trusted/review.json \
  --judge-policy /absolute/trusted/judge.json \
  --humans your-github-login
```

This continuously polls existing and newly opened PRs; no per-PR command, Actions
runner, or webhook is needed for discovery. Ready PRs whose authors are explicitly
allowlisted and whose head branches belong to the watched repository are admitted.
Drafts wait until ready. Forks, unknown source repositories, and other authors are
skipped with a log reason. Author and source repository are checked again before
publication and before executing review work or resuming after a human reply.
A confirmed intent record is not required to start review.

`--authors` permits PR code execution; `--humans` permits intent decisions and
Check retries. These are separate authorities. Allowlisted bot authors do not gain
human confirmation rights. Neither allowlist is inferred from App installation
access. Keep trusted policy files outside PR control.

One local owner runs PR workflows serially. It finishes the current review and
judge session before moving on, so long stages delay discovery of later PRs.
Human waits and individual PR errors do not prevent other admitted PRs from
advancing. Failed PRs are logged and revisited on the next sweep; uncertain model
consumption still prevents redispatch. Completed revisions never spend again just
because the watcher polls or restarts. A new head/base pair starts a new review
with the same cumulative PR allowances. `--once` processes one snapshot and exits
nonzero if any admitted PR could not advance; it does not keep watching.

Use the **same state directory** when broadening a single-PR watcher to repository
watching; existing job identities, policies, publications and allowances remain
intact. Stop the previous owner first. Signed retry receipts are routed by saved
Check ownership, never by PR order. Requests for unknown Checks or PRs outside the
current ready/author/repository scope are durably rejected. Operator retries still
require explicit `--pr N --once` and leave the webhook inbox for the continuous
watcher, preserving requests for other PRs.

The initial Check details name both models and settings, exact head/base commits,
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

## Checks and progress

`service watch` defaults to `--publication checks`. The GitHub App needs approved
repository **Checks: read and write** permission; registration and installation
approval are separate steps. The local PEM and connection JSON stay the same.
Check publication requests a separate repository-scoped token; Git fetch and
comment tokens retain their existing scopes. No Actions runner, inbound webhook,
or public endpoint on the review host is required.

Two entries appear in the PR's Checks section:

| Entry | Meaning |
| --- | --- |
| `Ferretta / review` | Starts in progress; an accepted LGTM completes with success, changes required with failure, and incomplete review with action required |
| `Ferretta / evaluation` | Queues behind review, then runs independently; a completed assessment is neutral, while an incomplete assessment needs attention and preserves the review verdict |

Repairs are a planned **separate stage** that would create commits, followed by
review of the resulting revision. They are not embedded in today's review, and
there is no repair Check yet. The starting plan explicitly says repairs are not
implemented; findings must currently be addressed by a human or implementation agent.

The details show the assigned models, requested settings, allowances, plan, exact
commits, and policy hashes. Recent activity is updated at saved model/tool
checkpoints, coalesced to at most one progress update per ten seconds within a
stage. Stage transitions and final results publish immediately. The same bounded
activity feed goes to stderr; stdout retains machine-readable workflow status,
including check URLs. Operation names and recorded durations are public; prompts,
thinking, tool arguments/results, and private session data are excluded from the
progress feed. Final reports still include their public findings and explanations.

These are updated Check details, not GitHub Actions streaming logs. A long model
or tool call may have no new checkpoint; the timestamp is the last observed
activity, not a heartbeat, completion estimate, or proof of process liveness.

Intent questions still post `PROPOSED` comments. The review Check says it is
awaiting human confirmation and remains in progress while compute pauses.
Authenticated `CORRECTED` and `CONFIRMED` replies resume the saved workflow.
Routine starting/verdict/scorecard comments are omitted in Checks mode. Use
`--publication comments` for the original three-comment behavior.

Publication mode is pinned with the workflow's policy. Saved jobs from earlier
versions remain comment jobs and must restart with `--publication comments`.
Changing the flag does not migrate their effects or reset their allowances;
automatic migration is not implemented. Do not delete state to switch modes.

Check IDs and exact output are saved before dispatch. A lost response is
reconciled against the App, external effect ID, commit, and complete output before
progress continues. Missing, edited or duplicate uncertain results stop for
operator reconciliation; they are never blindly recreated or patched again.
Superseded unfinished revisions receive cancelled Checks on their original SHA.

Line annotations, a self-hosted Actions adapter, and a live log server remain
future work. Checks do not enable repair, merge, extra allowances, or branch protection. Keep evaluation informational while
grading is calibrated. Existing build/test CI remains separate from model review.

## Retry a Check

Retrying `Ferretta / review` creates a fresh model session and new review/evaluation
Checks on the same head/base commits. Retrying `Ferretta / evaluation` creates
only a new judge session and evaluation Check, reusing the original review evidence
and leaving its Check unchanged. Previous attempts, request identity, actor,
source Check and policy provenance remain in SQLite. New Check summaries identify
the retry. Cumulative review/evaluation compute allowances are never reset;
configured turn/context limits still apply to each model session.

Only the latest completed attempt on the current revision is eligible. Stale
Checks, pending intent questions, exhausted allowances and uncertain interrupted
consumption are rejected. A request ID is idempotent, and the same source Check
cannot grant multiple retries under different delivery IDs. Use the new Check for
another intentional attempt. Confirmed intent is preserved for same-revision
review retries; cross-revision carry-forward remains [issue #7](https://github.com/ericdmoore/ferretta/issues/7).

An operator can retry without an inbound endpoint. Stop the watcher first so the
same state directory has one owner, then reuse its policies and human allowlist:

```sh
bin/ferretta service watch --repo ericdmoore/ferretta --pr 6 \
  --state /absolute/private/state --review-policy .ferretta/review.json \
  --judge-policy /absolute/private/judge.json --humans ericdmoore \
  --once --retry-check CHECK_RUN_ID --retry-id operator-attempt-1
```

`CHECK_RUN_ID` is the numeric ID in the Check details URL. Choose a new request ID
for a new intentional attempt; reuse it if command delivery/outcome is uncertain.
Neither restarting the watcher nor deleting state is the retry mechanism.

To use GitHub's **individual Check re-run control**, configure optional webhook
delivery. This handles `check_run.rerequested`; suite-wide **Re-run all** is not
implemented. GitHub sends the signal; it does not run the model itself.

1. Create a random webhook secret of at least 32 characters. Store it outside the
   repository in a private regular file (mode 0600), and enter the same value in
   the GitHub App's webhook settings. It is separate from the App's PEM key.
2. Start continuous watch with `--webhook-listen 127.0.0.1:8787` and
   `--webhook-secret-file /absolute/private/webhook-secret`, alongside the usual
   repository, PR, policies, state and human allowlist arguments. Do not add `--once`.
3. Forward a public HTTPS URL to `http://127.0.0.1:8787/github/webhook` using your
   reverse proxy/tunnel, preserving the request body and GitHub headers. Set that
   HTTPS URL as the App webhook URL and activate webhook delivery. Checks write
   permission provides the relevant Check event subscription.
4. An allowlisted human can re-request an individual Ferretta Check. The watcher
   verifies the HMAC-SHA256 signature, installation, repository, App and sender,
   persists the request before acknowledging it, and processes it on the next poll.
   Receipt acknowledgement means queued, not admitted; logs report acceptance or
   rejection. Unrelated events are ignored. Repeated delivery does not repeat work.

The listener binds only to loopback and never starts a tunnel. Keep the service
running for button delivery; durable queued requests survive restarts. This does
not require a GitHub Actions runner. See GitHub's [re-request API](https://docs.github.com/en/rest/checks/runs#rerequest-a-check-run).

## Read the scorecard

Rubric `review-roles-v1` separates **worker output** from the **oversight decision**.
The score table uses these two roles as rows, with correctness, evidence, intent
alignment, judgment, actionability, and communication as columns. Detailed reasons
and evidence references follow the table. Scores are 0 (material failure), 1 (substantial gaps),
2 (adequate and supported), or 3 (strong handling of relevant subtleties).
Missing evidence or inapplicability appears as **Not assessed**, not zero.
The review details group observed model IDs with reply counts; raw per-turn
provenance remains in the private saved report.

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
an already observed head/base pair. Explicit admitted Check retries create separate
attempts. New revisions retain history and cumulative
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
