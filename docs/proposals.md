# Proposal sessions

The reviewer calls `request_intent_confirmation` with a topic, question, reason,
optional recommendation, and up to four options. Ferretta owns marker generation,
versioning, GitHub credentials, delivery reconciliation and human identity checks.
A proposal is a waiting state, not a final review verdict.

## Run and resume

Grant the GitHub App **Pull requests: read/write** and **Contents: read-only**.
From the trusted checkout:

```sh
bin/ferretta review --repo ericdmoore/ferretta --pr 1 \
  --policy .ferretta/review.json --publish-proposals --humans ericdmoore
```

If the model asks a question, Ferretta posts a `PROPOSED-{Topic}-v1::` comment
with exact commit, policy hash and model/attempt provenance. A stable topic suffix
separates decisions for different revisions/policies. The model supplies prose;
it cannot set the version, authenticate a human, or manufacture confirmation.
No model call is made while waiting. No proposal is manufactured if the reviewer
has no consequential intent question.

Reply in a **new, unedited comment** using the exact topic/version from the
proposal, with `CONFIRMED-{Topic}-vN::` and your answer/selection, or
`CORRECTED-{Topic}-vN::` and your correction. A bare confirmation cannot resolve
a multiple-choice proposal. The model must revise a corrected topic into the
next version before finishing; only a human can confirm it.

Use the session path printed by the command:

```sh
bin/ferretta review --repo ericdmoore/ferretta --pr 1 \
  --policy .ferretta/review.json --resume /absolute/path/to/.ferretta/runs/pr-1-SHA-ID
```

This polls **once**. It either reports waiting, resumes inference with the saved
conversation and authenticated decision, or returns the completed report without
spending again. The original human allowlist and App identity are pinned. The
same repository, head/base revisions and exact policy bytes are required.
A newer revision needs a fresh review. Service intake still does not dispatch
reviews or poll human replies automatically.

## Durability and effect identity

A private SQLite database at `.ferretta/runs/state.sqlite` holds proposal-session
checkpoints. Each session directory contains its public report. Checkpoints retain
raw model continuation data; never publish the database or private run files.
One OS owner lock serializes proposal work under that runs directory. Independent
runs directories/installations do not coordinate.

Ferretta commits a pending effect before posting. A comment embeds its stable
effect hash, and reconciliation requires matching content from the configured
App bot. After an ambiguous dispatch, resume only reconciles: absence from a
snapshot is not proof that reposting is safe. A known failure before dispatch
(for example, token acquisition) leaves the draft retryable. An unresolved outcome is surfaced
for operator investigation. Repeated polling does not duplicate comments or
model calls. An interrupted model call is not automatically replayed.

Human decisions retain comment ID, authenticated author, URL, marker and content
hash in the checkpoint. Subsequent edits do not rewrite already accepted history.
Turn counts and active duration carry across resumes; human waiting does not
consume the active-time allowance. Context and output limits still apply.

LGTM remains advisory and requires the configured checks to have passed. This
feature adds no repair, merge, constraint-budget vocabulary, or automatic kickoff/
completion comments. Ordinary review without `--publish-proposals` can produce a
local draft question but does not post it or enable proposal resumption.

## Design references

These are concept references, not imported dependencies or copied code:

- [go-agent](https://github.com/jgabor/go-agent): tool metadata, explicit retry
  safety, and host-owned persistence/policy interfaces.
- [Harness](https://github.com/sausheong/harness): durable sessions and retention
  of provider continuation data; truncated responses remain incomplete.
- [Gollem](https://github.com/fugue-labs/gollem): typed tool arguments and structured
  results. Ferretta validates requests into command types before effects.

We retain Ferretta's small Go loop and pure-core/executor boundary. The broader
frameworks remain useful comparisons as orchestration and compaction develop.
