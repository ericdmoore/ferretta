# Repository search and tool recovery

The local review registry includes `search`, `grep`, `list_files`, `read_file`,
`read_diff`, `run_checks`, `request_intent_confirmation`, and `finish_review`.
Tool calls are structured requests validated by the core. They are not shell
commands, and there is no general shell or `sudo` tool.

## Search the reviewed commit

`search` and `grep` are exact aliases: both advertise the same argument schema
and use the same validation and executor. Either name can continue a search page
with the same query settings and offset. These are structured tools, not shell
invocations; `grep` does not accept command-line flags or gain extra permissions.

Example model tool call:

```json
{
  "name": "search",
  "arguments": {
    "query": "finishJSON",
    "path": "internal/review",
    "max_results": 20
  }
}
```

| Argument | Behavior |
| --- | --- |
| `query` | Required, 1–1,024 bytes; no NUL, carriage return, or newline. Literal text by default. |
| `path` | Optional literal repository-relative file or directory. Omit or use `.` for the whole commit. No glob or Git pathspec interpretation. |
| `regex` | Defaults to `false`. When `true`, accepts POSIX extended regex validated at the core boundary. |
| `max_results` | Matches per page, 1–100; defaults to 20. |
| `offset` | Number of matching lines to skip, 0–10,000; defaults to 0. |

The executor uses `git grep` against the exact reviewed commit. It does not
search modified working files, untracked files, or another HEAD revision. It
does not invoke a shell, external text converters, or an additional `grep`/`rg`
binary. Binary files are skipped. One match represents a matching line, not each
occurrence on that line. File/directory scopes are literal, including names that
look like Git pathspec expressions.

Results contain the revision, a `matches` array of paths/line numbers/text, and
`has_more`. With additional results, use `next_offset` and the same query, path,
and regex mode. Ordering follows Git's single-threaded commit traversal. Changed
query settings start a new search rather than continuing the previous page.

Pages are bounded by match count and approximately 48 KB of encoded match data.
Each text prefix is limited to 2,048 bytes; `text_truncated` explicitly marks
shortened lines. Use `read_file` for surrounding context, subject to that tool's
own line limits. Search consumes output incrementally and stops its child process
when it has evidence of a further page. If a subsequent offset would exceed the
pagination allowance, `has_more` remains true and a note requests a narrower
query/path instead of returning an unusable continuation offset.

An empty `matches` array is a successful no-match result within the requested
scope, not proof that a symbol is absent from skipped binary content or that the
scope path exists. Process/protocol failures are errors, never empty evidence.

## Recover from tool mistakes

Tool failure messages are JSON with an error category, the requested tool,
diagnostic message, recovery guidance, and the actual advertised tool names.
Known tools also include their advertised input schema.

- `unknown_tool`: that name is unavailable. Retrying with different arguments
  cannot enable it. For example, an unsupported `ripgrep` request containing a
  valid query/path can suggest the registered `search` tool with that scope.
  An unknown file-opening tool with a valid path can suggest `read_file`.
- `invalid_arguments`: the tool exists but its request failed validation.
  Examples include an invalid regex, an excessive result limit, or arguments
  passed to `run_checks`, which only accepts `{}`.
- `execution_failed`: a validated operation failed during execution. Preserve
  the diagnostic and investigate it; the tool does not gain additional authority.
  A failed file read can suggest listing the committed files. Failed checks are
  not automatically retried, replaced, or bypassed.

Where useful, `suggested_call` contains a complete call accepted by the same core
planner. Search recovery examples use literal mode, preserve a valid query/scope,
and reset pagination. If those inputs cannot be salvaged, file listing is a safe
navigation suggestion. File-read range mistakes retain the valid path and suggest
the first page. Verdicts and intent questions are never fabricated as recovery
examples.

Suggestions are advisory. The model must issue a new call, and that call is
validated independently. An unknown call is never silently renamed or executed.
This improves recovery ergonomics without changing review acceptance gates.
