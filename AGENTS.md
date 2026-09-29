# Development requirements

Follow the development principles and intent vocabulary in README.md.

- Keep the implementation pure Go where possible, including dependencies. Verify native amd64 and arm64 targets with CGO disabled.
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

The current CLI inspects existing comments and emits a current snapshot, not a durable confirmation log or merge authorization. Preserve this distinction until durable evidence capture exists.

Only authenticated human confirmation establishes agreed intent. Agents may author PROPOSED markers, but must not author CORRECTED or CONFIRMED markers as human decisions. Amendment semantics remain deferred.
