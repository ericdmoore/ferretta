# Live review experiments

## PR #1: verdict finalization and evidence quality

Recorded September 30, 2026. These were real local inference runs against
[PR #1](https://github.com/ericdmoore/ferretta/pull/1), using the current feature
branch's harness to review the older submitted implementation. They did not
review the unsubmitted proposal-harness changes.

- Reviewed head: `08815c3e86ab596e09daf0b9b68a6411579f1eac`.
- Base: `6f22d7c0975589dc1d3fad28c8940515eaad1643`.
- Policy SHA-256: `5909eaf077caf3fdde43e038b5a02fb2b59161509dc30549a3c83b8d3787fae8`.
- Ollama `gpt-oss:20b`, requested medium effort, 131,072-token context,
  16,384-token output allowance; no turn or active-time ceiling.
- Required check: `make check`, executed in a detached review worktree.
- GitHub App authentication and exact-revision revalidation succeeded.
- Effective effort was not reported by the provider. Token totals include
  repeated input across turns; they are not unique context size or metered cost.

| Observation | Initial attempt | After finalization guidance change |
| --- | --- | --- |
| Harness result | `awaiting_intent`, local draft | Advisory `lgtm` |
| Model replies | 16 | 13 |
| Elapsed review time | 2m 22.860s | 1m 9.519s |
| Reported input + output tokens | 193,522 | 132,490 |
| Largest reported input | 23,257 tokens | 16,831 tokens |
| Required checks | Passed | Passed |
| Calls to `read_diff` | 0 | 0 |

### Observed failure and change

The initial attempt wrote a prose LGTM, then called
`request_intent_confirmation` asking the human to choose between review verdicts.
An invalid topic was rejected; a revised topic passed structural validation and
became a local draft. There was no consequential product-intent question. This
was an inappropriate handoff of the reviewer's own responsibility.

Clarified the system prompt, verdict/proposal tool descriptions, and recovery
message after a response without tool calls. They now explicitly assign verdict
selection to the reviewer and describe how to submit LGTM or located findings
through `finish_review`. Existing acceptance checks remain in force; prose is
not converted into approval, and no human confirmation is fabricated.

The retry eventually called `finish_review` with LGTM, an empty findings list,
and an empty tradeoffs list. It had also tried the unavailable `search` tool
three times, received rejections, and recovered by reading a file.

This is one observed successful finalization after a prompt change, not a
controlled benchmark or proof of dependable improvement. Sampling and different
evidence paths can affect both outcomes and latency.

### Remaining implementation and evaluation gaps

1. **Evidence coverage is not established by completion.** Neither run requested
   the diff. The successful run read six distinct files under `internal/review`,
   while the PR changes 19 paths, including CI/release workflows, packaging, and
   CLI wiring. The harness verifies finding locations and checks but does not
   establish which changed behaviors were reviewed. Consider explicit review
   scope/evidence tracking; merely requiring one diff call would not establish
   adequate coverage either.
2. **Verdict quality is weakly validated.** The final summary broadly claimed no
   bugs or security concerns, without concrete supporting analysis or tradeoffs.
   Require useful evidence and major tradeoffs when present, while avoiding
   manufactured findings or boilerplate merely to populate fields.
3. **Search and recovery ergonomics need work.** A bounded search tool against
   the exact reviewed revision could avoid repeated large file reads. Unknown
   tool errors should direct the model toward available tools; currently they
   only reject the unsupported name. Both were subsequently implemented in the
   [search and recovery follow-up](review-tools.md); the observations here describe
   the earlier runs.
4. **Proposal usefulness exceeds structural validation.** Topic syntax, required
   fields, and human identity checks do not ensure a question concerns intended
   software behavior. Prompt guidance helps, but both genuine questions and
   inappropriate verdict questions need evaluation. Avoid treating keyword
   matching as a general semantic validator.
5. **Users need live progress.** Ordinary reviews emit their report at completion;
   this investigation inspected private checkpoints to observe tool activity.
   A public progress stream could report stages, tool names, check status, and
   waiting states without exposing raw model reasoning.

Before relying on automated acceptance, evaluate against PRs with known defects
and known-clean changes, as well as legitimate intent ambiguities. Passing the
tool protocol and repository tests alone does not measure defect detection.

### Artifacts and validation

Local private run directories (ignored by Git):

- `.ferretta/runs/pr-1-08815c3e-1656248877/`
- `.ferretta/runs/pr-1-08815c3e-3627468164/`

Each contains an advisory report and private continuation data. These artifacts
are not included in this document or release assets. No proposal or verdict was
posted to GitHub, and no repair or merge was performed. Offline `make check`
passed after the prompt changes; statement coverage remains 99.20%.

## Search and recovery follow-up

Added the registered search tool and structured recovery responses, with offline
Git fixtures verifying exact-revision reads, literal scoping, regex queries,
pagination, output bounds, and failed-operation handling. Fake-client review
tests verify that a rejected call performs no search and a corrected follow-up
can continue the review. Suggestions are never executed automatically.

A further live PR #1 review used the same model, revision, and policy above.
It completed with advisory LGTM after 19 replies in 2m 6.153s, with 218,496
reported input/output tokens and passing checks. It attempted `open_file` once;
the structured response listed the actual tools, after which the model called
`read_file` and continued. This attempt supplied three tradeoffs in its final
verdict, but still did not request `read_diff` or `search`. It therefore provides
one live recovery example, not live proof of search usage or review completeness.

The live build suggested file listing for that unknown call. The final recovery
implementation further preserves a valid path from unknown file-opening calls
by suggesting `read_file`; that refinement is covered by offline tests. Private
artifacts are in `.ferretta/runs/pr-1-08815c3e-1432435817/`. No GitHub comment,
repair, or merge was performed.
