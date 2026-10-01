# Development requirements

Follow the development principles and intent vocabulary in README.md.

Markdown-only changes do not require a pull request. Changes that also modify code or other non-Markdown files follow the normal PR workflow.

## Product boundary

- Ferretta enters only after an implementation agent submits a PR. The human's earlier interaction with that agent is outside Ferretta and can use any tools or workflow.
- Start review from the submitted commits and repository-configured policy. A pre-existing confirmed intent record MUST NOT be required to admit a PR for review.
- Prepare the review environment as needed and bind review evidence to exact commits and the policy used.
- Retain model/provider, effort level, and attempt provenance for reviews and clarification questions. Keep requested settings distinct from provider-reported execution details; unavailable details remain unknown.
- Repository policy selects local inference or OpenRouter and specifies tool use, multi-turn review, reasoning capabilities/effort, and resource limits. Reject routes that cannot satisfy required capabilities; only use explicitly permitted fallbacks.
- Keep the review loop resumable, retaining model turns, tool results, and provider-required continuation data. Tool execution and verdict acceptance remain subject to core policy.
- LGTM is a valid outcome when the evidence supports intent alignment, correctness, and an appropriately small, well-designed implementation. Explain major tradeoffs; do not manufacture findings or chase fewer lines at the expense of required behavior or clarity. Model approval cannot override incomplete required checks, blocking questions/findings, or a stale revision.
- Ferretta's reviewer may ask for clarification with PROPOSED comments on the PR. Human replies notify and resume the saved review job; corrections lead to a revised proposal, and confirmations establish intent for review.
- Keep the notification adapter separate from core policy. Repeated delivery must not duplicate model spending or posted questions.
- `service run` currently performs intake only: GitHub App polling and SQLite revision observations with one local owner. It does not dispatch reviews, parse replies, run checks or write to GitHub. `service status` reads persisted observations, not process liveness. The separate explicit `service watch` dispatches one PR with trusted review/judge policies, defaults to review/evaluation Checks with checkpoint progress and intent-question comments. `--publication comments` retains the earlier three-comment behavior; legacy saved workflows require it. Publication mode is pinned, and uncertain check effects must reconcile by App/effect/revision/exact output without redispatch. It polls authenticated intent replies. Preserve the command distinction.
- SQLite intake uses modernc.org/sqlite with CGO disabled. A local OS lock protects one state directory; independent directories/installations do not coordinate. Keep the database private and reject unknown schema versions.
- The primary runtime is a boot service with service-owned configuration and credentials, independent of desktop login. See arch.md and docs/configuration.md for agreed configuration layers and objective presets; their TOML resolver and CLI selectors are not implemented.

## Implementation practices

- Keep the implementation pure Go where possible, including dependencies. Verify native amd64 and arm64 targets with CGO disabled.
- Make impossible states impossible. Model valid application states and transitions with types and structures that prevent inconsistent combinations. Prefer state-specific data, distinct types, encapsulated fields, and invariant-preserving constructors/transitions over independent flags and loosely related optional fields. Validate external inputs at the boundary and explicitly handle Go zero values where compile-time enforcement is insufficient.
- Tests MUST be fast and cover as much behavior as is reasonable. Do not pursue coverage at the expense of useful assertions or maintainability.
- Inject client dependencies so the normal test suite can use fake clients or controlled transports without live network calls.
- Keep the core pure: it composes command objects; thin executors perform effects and return results.
- The core owns policy; the executor owns mechanics. Decisions about eligibility, budget, and retries belong in the core; executors perform operations and report outcomes.
- Make nondeterminism an explicit input: time, randomness, IDs, and model responses enter through the boundary so the same core state and inputs produce the same decisions.
- Test observable behavior and invariants, rather than mirroring implementation details or private call sequences.
- Verify the adapters as well as the core. Maintain a separate networked test suite against real providers for release validation. The policy deciding whether it runs before all minor releases or only major releases is deferred; do not invent that policy. Keep live calls out of the normal suite.
- Unless it's painfully obvious, assume the provider does not handle idempotency. Rely only on clearly established guarantees; reconcile uncertain outcomes before retrying effects, and surface uncertainty when reconciliation is unavailable.
- Exercise network failures and timing rules through fakes, injected clocks, and controlled executors. Avoid real sleeps in core tests.
- Coverage is a ratchet: commit checks must reject decreases against the accepted baseline, and the baseline must retain gains.
- Local and CI checks must use the same pinned compiler, formatter, linter, configurations, and entry points.

Run `make check` before committing. It uses the pinned Go toolchain for formatting checks, vet, offline tests, coverage enforcement, and CGO-disabled amd64/arm64 builds for Linux and macOS. Run `make coverage-update` when coverage improves and commit the updated baseline. Install the local commit hook with `make install-hooks`.

Run the separate networked suite with `FERRETTA_TEST_REPO=owner/repo FERRETTA_TEST_PR=number make test-network`, using an existing PR with comments. `GITHUB_TOKEN` is optional for public repositories and required when the fixture needs authentication. This suite only reads GitHub data; it must not post test comments.

Ferretta’s GitHub-facing commands now authenticate through a dedicated GitHub App installation
configured in machine-level `ferretta/github.json` (or absolute
`FERRETTA_GITHUB_CONFIG`). Never introduce a fallback to personal `gh`, `GH_TOKEN`,
or `GITHUB_TOKEN` credentials. Fetch tokens are repository-scoped and read-only; proposal creation uses a
separate internal token with pull_requests:write and contents:read.
Do not write credentials to logs, reports, sessions, Git configuration, or command
arguments. The process environment filter is not a sandbox: checks still run as
the OS user. Keep operator credentials separate from untrusted code execution.

Run `FERRETTA_TEST_REPO=owner/repo FERRETTA_TEST_PR=number make test-network-app`
after provisioning app credentials; it verifies identity and reads an existing PR
with comments. It never posts comments or generates model responses.

The intent CLI inspects existing comments and emits a current snapshot, not a durable confirmation log or merge authorization. The manual `review` command runs a bounded local Ollama tool loop in a detached worktree, executes trusted policy checks, and saves an advisory report plus private session checkpoints. With --publish-proposals and an explicit human allowlist, it posts PROPOSED comments and saves private SQLite checkpoints. --resume polls once for authenticated CORRECTED/CONFIRMED replies and resumes the same revision/policy. General interrupted-model replay remains unimplemented. `service watch` polls intent replies automatically; uncertain execution stops rather than replaying model spending. Store locks serialize all proposal runs under the same runs directory; independent directories/installations do not coordinate. Never repost uncertain effects without reconciliation.

CI uploads build artifacts. Version-tag workflows stage archives and checksums in draft GitHub releases; publication and release-test cadence remain separate decisions. Do not upload private `.ferretta/runs/` session data as build or release artifacts.

Only authenticated human confirmation establishes agreed intent. Agents may author PROPOSED markers, but must not author CORRECTED or CONFIRMED markers as human decisions. Amendment semantics remain deferred.

Onboarding commands are implemented: `init` probes common local inventories and
creates only Ollama policies; `doctor` checks metadata/readiness. Neither runs
inference, downloads models, starts services, or executes checks. Preserve existing
policy/connection files. Discovery supplies candidates, not routing permission.
`demo` is explicitly fictional. `model test` is separate, explicit two-turn
inference using inert tool results; passing it does not establish review quality.

Run metadata-only fixtures with `FERRETTA_TEST_MODEL_API=ollama
FERRETTA_TEST_MODEL_ENDPOINT=http://127.0.0.1:11434
FERRETTA_TEST_MODEL=qwen3:4b-thinking make test-network-models` (on one shell line).
Use `compatible` for `/v1/models`. The separate inference suite requires an
explicit absolute `FERRETTA_TEST_MODEL_POLICY` and `make test-network-model-tools`.
Normal tests never invoke either live suite.

`thinking: "provider_default"` is an explicit policy choice that sends Ollama
`think:null`. Missing thinking-control metadata may be accepted only in this mode;
tools/thinking capability, local routing and context checks remain mandatory.
Enabled state and effective effort remain unknown. Never silently downgrade an
`enabled`/named-effort policy or let `init --thinking auto` select this mode.

The Hugo site lives in site/. Run make tools-site once to install the pinned
compiler, then make site-check for template/link/installer validation. The Pages
workflow uses the same commands and deploys only main. Keep install.sh as the
single installer source mounted by Hugo; never publish private run data. Installer
fixtures in internal/installtest use fake downloads and commands. All optional
components require explicit selection; do not collect unused OpenRouter keys or
claim LiteLLM/OpenRouter inference is implemented. Public installation requires
a published release and HTTPS; drafting assets does not publish them.

Check publication uses a separate repository-scoped Checks write token; fetch and comment token scopes stay unchanged. Public progress contains only harness operation names and accounting. No native Actions streaming logs or check-rerun webhook handler is implemented.

Evaluation uses a separate read-only session. `eval` grades saved reports without
publication; `service watch` posts its scorecard through the App. Grades never
authorize merge, resource increases or more work. Preserve unknown usage and
requested-vs-observed settings. Private judge/reviewer conversations stay local.
The rubric is an automated assessment, not calibrated ground truth; parallel
roll-ups, monetary reservations, repair and merge execution are not implemented.
